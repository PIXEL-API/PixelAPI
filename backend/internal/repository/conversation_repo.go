package repository

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/ent/supportmessage"
	"github.com/Wei-Shaw/sub2api/ent/supportthread"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"

	entsql "entgo.io/ent/dialect/sql"
)

type conversationRepository struct {
	client *dbent.Client
}

func NewConversationRepository(client *dbent.Client) service.ConversationRepository {
	return &conversationRepository{client: client}
}

func (r *conversationRepository) CreateWithMessage(ctx context.Context, conv *service.Conversation, msg *service.ConversationMessage) error {
	if conv == nil || msg == nil {
		return service.ErrConversationInputRequired
	}
	if conv.UserID <= 0 {
		return service.ErrConversationInputRequired
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	txClient := tx.Client()
	thread, err := ensureSupportThread(ctx, txClient, conv, msg)
	if err != nil {
		return err
	}

	createdMsg, err := txClient.SupportMessage.Create().
		SetThreadID(thread.ID).
		SetSenderType(msg.SenderType).
		SetNillableSenderID(msg.SenderID).
		SetMessageType(msg.MessageType).
		SetContentFormat(msg.ContentFormat).
		SetTitle(strings.TrimSpace(conv.Subject)).
		SetContent(msg.Content).
		SetSource(strings.TrimSpace(conv.Source)).
		SetSourceID(strings.TrimSpace(conv.SourceID)).
		SetMetadata(normalizeRepositoryMetadata(msg.Metadata)).
		SetCreatedAt(msg.CreatedAt).
		Save(ctx)
	if err != nil {
		if isUniqueViolationOnIndex(err, map[string]struct{}{"idx_support_messages_thread_source_unique": {}}) {
			return service.ErrConversationDuplicateSource.WithCause(err)
		}
		return err
	}

	update := supportThreadLastMessageUpdate(txClient.SupportThread.UpdateOneID(thread.ID), createdMsg)
	if msg.SenderType != service.ConversationSenderTypeSystem {
		update.
			SetSubject(normalizeSupportThreadSubject(conv.Subject)).
			SetStatus(conv.Status).
			SetPriority(conv.Priority).
			SetType(conv.Type)
		if conv.AssignedAdminID != nil {
			update.SetAssignedAdminID(*conv.AssignedAdminID)
		}
	}
	supportThreadApplyReadPointer(update, createdMsg)

	updatedThread, err := update.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrConversationNotFound, nil)
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	applySupportThreadEntityToService(conv, updatedThread)
	applySupportMessageEntityToService(msg, createdMsg)
	return nil
}

func (r *conversationRepository) AddMessage(ctx context.Context, conversationID int64, msg *service.ConversationMessage, nextStatus string) (*service.Conversation, error) {
	if conversationID <= 0 || msg == nil {
		return nil, service.ErrConversationInputRequired
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	txClient := tx.Client()
	createdMsg, err := txClient.SupportMessage.Create().
		SetThreadID(conversationID).
		SetSenderType(msg.SenderType).
		SetNillableSenderID(msg.SenderID).
		SetMessageType(msg.MessageType).
		SetContentFormat(msg.ContentFormat).
		SetContent(msg.Content).
		SetMetadata(normalizeRepositoryMetadata(msg.Metadata)).
		SetCreatedAt(msg.CreatedAt).
		Save(ctx)
	if err != nil {
		return nil, err
	}

	update := supportThreadLastMessageUpdate(txClient.SupportThread.UpdateOneID(conversationID), createdMsg).
		SetStatus(nextStatus)
	supportThreadApplyReadPointer(update, createdMsg)

	updatedThread, err := update.Save(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrConversationNotFound, nil)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetByID(ctx, updatedThread.ID)
}

func (r *conversationRepository) GetByID(ctx context.Context, id int64) (*service.Conversation, error) {
	if id <= 0 {
		return nil, service.ErrConversationInputRequired
	}
	m, err := r.client.SupportThread.Query().
		Where(supportthread.IDEQ(id)).
		WithUser().
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrConversationNotFound, nil)
	}
	return supportThreadEntityToService(m), nil
}

func (r *conversationRepository) GetByIDForUser(ctx context.Context, userID, id int64) (*service.Conversation, error) {
	if userID <= 0 || id <= 0 {
		return nil, service.ErrConversationInputRequired
	}
	m, err := r.client.SupportThread.Query().
		Where(supportthread.IDEQ(id), supportthread.UserIDEQ(userID)).
		WithUser().
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrConversationNotFound, nil)
	}
	return supportThreadEntityToService(m), nil
}

func (r *conversationRepository) GetSystemNoticeBySource(ctx context.Context, userID int64, source, sourceID string) (*service.Conversation, error) {
	source = strings.TrimSpace(source)
	sourceID = strings.TrimSpace(sourceID)
	if userID <= 0 || source == "" || sourceID == "" {
		return nil, service.ErrConversationInputRequired
	}
	m, err := r.client.SupportMessage.Query().
		Where(
			supportmessage.SourceEQ(source),
			supportmessage.SourceIDEQ(sourceID),
			supportmessage.HasThreadWith(supportthread.UserIDEQ(userID)),
		).
		WithThread(func(q *dbent.SupportThreadQuery) {
			q.WithUser()
		}).
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrConversationNotFound, nil)
	}
	out := supportThreadEntityToService(m.Edges.Thread)
	if out == nil {
		return nil, service.ErrConversationNotFound
	}
	out.Source = m.Source
	out.SourceID = m.SourceID
	return out, nil
}

func (r *conversationRepository) List(ctx context.Context, params pagination.PaginationParams, filters service.ConversationListFilters) ([]service.Conversation, *pagination.PaginationResult, error) {
	q := r.client.SupportThread.Query().WithUser()
	q = applyConversationFilters(q, filters, false)

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	items, err := q.
		Offset(params.Offset()).
		Limit(params.Limit()).
		Order(conversationListOrders(params)...).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	conversations := supportThreadEntitiesToService(items)
	if err := r.applyUnreadState(ctx, conversations); err != nil {
		return nil, nil, err
	}
	return conversations, paginationResultFromTotal(int64(total), params), nil
}

func (r *conversationRepository) ListForUser(ctx context.Context, userID int64, params pagination.PaginationParams, filters service.ConversationListFilters) ([]service.Conversation, *pagination.PaginationResult, error) {
	if userID <= 0 {
		return nil, nil, service.ErrConversationInputRequired
	}
	filters.UserID = userID
	q := r.client.SupportThread.Query().WithUser()
	q = applyConversationFilters(q, filters, true)

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	items, err := q.
		Offset(params.Offset()).
		Limit(params.Limit()).
		Order(conversationListOrders(params)...).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	conversations := supportThreadEntitiesToService(items)
	if err := r.applyUnreadState(ctx, conversations); err != nil {
		return nil, nil, err
	}
	return conversations, paginationResultFromTotal(int64(total), params), nil
}

func (r *conversationRepository) ListMessages(ctx context.Context, conversationID int64, params pagination.PaginationParams) ([]service.ConversationMessage, *pagination.PaginationResult, error) {
	if conversationID <= 0 {
		return nil, nil, service.ErrConversationInputRequired
	}
	q := r.client.SupportMessage.Query().
		Where(supportmessage.ThreadIDEQ(conversationID))

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	items, err := q.
		Offset(params.Offset()).
		Limit(params.Limit()).
		Order(dbent.Asc(supportmessage.FieldID)).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	return supportMessageEntitiesToService(items), paginationResultFromTotal(int64(total), params), nil
}

func (r *conversationRepository) MarkRead(ctx context.Context, conversationID int64, readerType string, readUntilMessageID *int64) (*service.Conversation, error) {
	if conversationID <= 0 {
		return nil, service.ErrConversationInputRequired
	}
	if readUntilMessageID != nil && *readUntilMessageID <= 0 {
		return nil, service.ErrConversationInputRequired
	}
	current, err := r.client.SupportThread.Get(ctx, conversationID)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrConversationNotFound, nil)
	}

	readMessageID := current.LastMessageID
	if readUntilMessageID != nil {
		readMessageID = readUntilMessageID
		if current.LastMessageID != nil && *readMessageID > *current.LastMessageID {
			readMessageID = current.LastMessageID
		}
	}
	if readMessageID == nil {
		return supportThreadEntityToService(current), nil
	}

	readMessage, err := r.client.SupportMessage.Query().
		Where(supportmessage.IDEQ(*readMessageID), supportmessage.ThreadIDEQ(conversationID)).
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrConversationMessageNotFound, nil)
	}

	update := r.client.SupportThread.UpdateOneID(conversationID)
	switch readerType {
	case service.ConversationSenderTypeUser:
		update.SetUserLastReadMessageID(readMessage.ID).
			SetUserLastReadAt(readMessage.CreatedAt)
	case service.ConversationSenderTypeAdmin:
		update.SetAdminLastReadMessageID(readMessage.ID).
			SetAdminLastReadAt(readMessage.CreatedAt)
	default:
		return supportThreadEntityToService(current), nil
	}

	updated, err := update.Save(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrConversationNotFound, nil)
	}
	return r.GetByID(ctx, updated.ID)
}

func (r *conversationRepository) UpdateStatus(ctx context.Context, conversationID int64, status string) (*service.Conversation, error) {
	if conversationID <= 0 {
		return nil, service.ErrConversationInputRequired
	}
	updated, err := r.client.SupportThread.UpdateOneID(conversationID).
		SetStatus(status).
		Save(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrConversationNotFound, nil)
	}
	return r.GetByID(ctx, updated.ID)
}

func (r *conversationRepository) UpdateAssignee(ctx context.Context, conversationID int64, adminID *int64) (*service.Conversation, error) {
	if conversationID <= 0 {
		return nil, service.ErrConversationInputRequired
	}
	update := r.client.SupportThread.UpdateOneID(conversationID)
	if adminID == nil {
		update.ClearAssignedAdminID()
	} else {
		update.SetAssignedAdminID(*adminID)
	}
	updated, err := update.Save(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrConversationNotFound, nil)
	}
	return r.GetByID(ctx, updated.ID)
}

func (r *conversationRepository) CountUnreadForUser(ctx context.Context, userID int64) (int64, error) {
	if userID <= 0 {
		return 0, service.ErrConversationInputRequired
	}
	count, err := r.client.SupportThread.Query().
		Where(
			supportthread.UserIDEQ(userID),
			supportUnreadPredicate(service.ConversationSenderTypeUser),
		).
		Count(ctx)
	return int64(count), err
}

func (r *conversationRepository) CountUnreadForAdmin(ctx context.Context) (int64, error) {
	count, err := r.client.SupportThread.Query().
		Where(
			supportUnreadPredicate(service.ConversationSenderTypeAdmin),
		).
		Count(ctx)
	return int64(count), err
}

func (r *conversationRepository) applyUnreadState(ctx context.Context, conversations []service.Conversation) error {
	for i := range conversations {
		userUnread, err := r.threadUnread(ctx, conversations[i].ID, service.ConversationSenderTypeUser, conversations[i].UserLastReadMessageID)
		if err != nil {
			return err
		}
		adminUnread, err := r.threadUnread(ctx, conversations[i].ID, service.ConversationSenderTypeAdmin, conversations[i].AdminLastReadMessageID)
		if err != nil {
			return err
		}
		conversations[i].UserUnread = userUnread
		conversations[i].AdminUnread = adminUnread
	}
	return nil
}

func (r *conversationRepository) threadUnread(ctx context.Context, threadID int64, readerType string, readID *int64) (bool, error) {
	if threadID <= 0 {
		return false, nil
	}
	preds := []predicate.SupportMessage{
		supportmessage.ThreadIDEQ(threadID),
	}
	if readID != nil {
		preds = append(preds, supportmessage.IDGT(*readID))
	}
	switch readerType {
	case service.ConversationSenderTypeUser:
		preds = append(preds, supportmessage.SenderTypeNEQ(service.ConversationSenderTypeUser))
	case service.ConversationSenderTypeAdmin:
		preds = append(preds, supportmessage.SenderTypeEQ(service.ConversationSenderTypeUser))
	default:
		return false, nil
	}
	return r.client.SupportMessage.Query().Where(preds...).Exist(ctx)
}

func ensureSupportThread(ctx context.Context, txClient *dbent.Client, conv *service.Conversation, msg *service.ConversationMessage) (*dbent.SupportThread, error) {
	createdAt := conv.CreatedAt
	if createdAt.IsZero() {
		createdAt = msg.CreatedAt
	}
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	updatedAt := conv.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	lastMessageAt := msg.CreatedAt
	if lastMessageAt.IsZero() {
		lastMessageAt = createdAt
	}

	id, err := txClient.SupportThread.Create().
		SetUserID(conv.UserID).
		SetSubject(normalizeSupportThreadSubject(conv.Subject)).
		SetStatus(initialSupportThreadStatus(conv, msg)).
		SetPriority(conv.Priority).
		SetType(conv.Type).
		SetNillableAssignedAdminID(conv.AssignedAdminID).
		SetLastMessageAt(lastMessageAt).
		SetCreatedAt(createdAt).
		SetUpdatedAt(updatedAt).
		OnConflictColumns(supportthread.FieldUserID).
		Update(func(u *dbent.SupportThreadUpsert) {
			u.SetUpdatedAt(updatedAt)
		}).
		ID(ctx)
	if err != nil {
		return nil, err
	}
	return txClient.SupportThread.Query().
		Where(supportthread.IDEQ(id)).
		Only(ctx)
}

func initialSupportThreadStatus(conv *service.Conversation, msg *service.ConversationMessage) string {
	if msg != nil && msg.SenderType == service.ConversationSenderTypeSystem {
		return service.ConversationStatusOpen
	}
	if conv != nil && strings.TrimSpace(conv.Status) != "" {
		return conv.Status
	}
	return service.ConversationStatusOpen
}

func supportThreadLastMessageUpdate(update *dbent.SupportThreadUpdateOne, msg *dbent.SupportMessage) *dbent.SupportThreadUpdateOne {
	return update.
		SetLastMessageID(msg.ID).
		SetLastMessageSenderType(msg.SenderType).
		SetLastMessageExcerpt(conversationMessageExcerpt(msg.Content)).
		SetLastMessageAt(msg.CreatedAt)
}

func supportThreadApplyReadPointer(update *dbent.SupportThreadUpdateOne, msg *dbent.SupportMessage) {
	switch msg.SenderType {
	case service.ConversationSenderTypeUser:
		update.SetUserLastReadMessageID(msg.ID).
			SetUserLastReadAt(msg.CreatedAt)
	case service.ConversationSenderTypeAdmin:
		update.SetAdminLastReadMessageID(msg.ID).
			SetAdminLastReadAt(msg.CreatedAt)
	}
}

func applyConversationFilters(q *dbent.SupportThreadQuery, filters service.ConversationListFilters, userScope bool) *dbent.SupportThreadQuery {
	if filters.UserID > 0 {
		q = q.Where(supportthread.UserIDEQ(filters.UserID))
	}
	if filters.Status != "" {
		q = q.Where(supportthread.StatusEQ(filters.Status))
	}
	if filters.Kind != "" && filters.Kind != service.ConversationKindTicket {
		q = q.Where(supportthread.IDEQ(-1))
	}
	if filters.Priority != "" {
		q = q.Where(supportthread.PriorityEQ(filters.Priority))
	}
	if filters.Type != "" {
		q = q.Where(supportthread.TypeEQ(filters.Type))
	}
	if filters.AssignedAdminID > 0 {
		q = q.Where(supportthread.AssignedAdminIDEQ(filters.AssignedAdminID))
	}
	if filters.Search != "" {
		q = q.Where(
			supportthread.Or(
				supportthread.SubjectContainsFold(filters.Search),
				supportthread.LastMessageExcerptContainsFold(filters.Search),
				supportthread.HasUserWith(
					dbuser.Or(
						dbuser.EmailContainsFold(filters.Search),
						dbuser.UsernameContainsFold(filters.Search),
					),
				),
			),
		)
	}
	if filters.UnreadOnly {
		readerType := service.ConversationSenderTypeAdmin
		if userScope {
			readerType = service.ConversationSenderTypeUser
		}
		q = q.Where(supportUnreadPredicate(readerType))
	}
	return q
}

func supportUnreadPredicate(readerType string) predicate.SupportThread {
	readField := supportthread.FieldAdminLastReadMessageID
	senderPredicate := func(sub *entsql.Selector) {
		sub.Where(entsql.EQ(sub.C(supportmessage.FieldSenderType), service.ConversationSenderTypeUser))
	}
	if readerType == service.ConversationSenderTypeUser {
		readField = supportthread.FieldUserLastReadMessageID
		senderPredicate = func(sub *entsql.Selector) {
			sub.Where(entsql.NEQ(sub.C(supportmessage.FieldSenderType), service.ConversationSenderTypeUser))
		}
	}
	return predicate.SupportThread(func(s *entsql.Selector) {
		messages := entsql.Table(supportmessage.Table)
		sub := entsql.Select(messages.C(supportmessage.FieldID)).
			From(messages).
			Where(
				entsql.ColumnsEQ(s.C(supportthread.FieldID), messages.C(supportmessage.FieldThreadID)),
			)
		senderPredicate(sub)
		sub.Where(
			entsql.Or(
				entsql.IsNull(s.C(readField)),
				entsql.ColumnsGT(messages.C(supportmessage.FieldID), s.C(readField)),
			),
		)
		s.Where(entsql.Exists(sub))
	})
}

func conversationListOrders(params pagination.PaginationParams) []supportthread.OrderOption {
	sortBy := strings.ToLower(strings.TrimSpace(params.SortBy))
	sortOrder := params.NormalizedSortOrder(pagination.SortOrderDesc)

	field := supportthread.FieldLastMessageAt
	switch sortBy {
	case "created_at":
		field = supportthread.FieldCreatedAt
	case "updated_at":
		field = supportthread.FieldUpdatedAt
	case "priority":
		field = supportthread.FieldPriority
	case "status":
		field = supportthread.FieldStatus
	case "id":
		field = supportthread.FieldID
	}

	if sortOrder == pagination.SortOrderAsc {
		if field == supportthread.FieldID {
			return []supportthread.OrderOption{dbent.Asc(field)}
		}
		return []supportthread.OrderOption{dbent.Asc(field), dbent.Asc(supportthread.FieldID)}
	}

	if field == supportthread.FieldID {
		return []supportthread.OrderOption{dbent.Desc(field)}
	}
	return []supportthread.OrderOption{dbent.Desc(field), dbent.Desc(supportthread.FieldID)}
}

func supportThreadEntityToService(m *dbent.SupportThread) *service.Conversation {
	if m == nil {
		return nil
	}
	out := &service.Conversation{
		ID:                     m.ID,
		UserID:                 m.UserID,
		Subject:                m.Subject,
		Status:                 m.Status,
		Kind:                   service.ConversationKindTicket,
		Priority:               m.Priority,
		Type:                   m.Type,
		AssignedAdminID:        m.AssignedAdminID,
		LastMessageID:          m.LastMessageID,
		LastMessageSenderType:  m.LastMessageSenderType,
		LastMessageExcerpt:     m.LastMessageExcerpt,
		LastMessageAt:          m.LastMessageAt,
		UserLastReadMessageID:  m.UserLastReadMessageID,
		UserLastReadAt:         m.UserLastReadAt,
		AdminLastReadMessageID: m.AdminLastReadMessageID,
		AdminLastReadAt:        m.AdminLastReadAt,
		CreatedAt:              m.CreatedAt,
		UpdatedAt:              m.UpdatedAt,
		UserUnread:             conversationUnread(m.LastMessageID, m.UserLastReadMessageID, m.LastMessageSenderType != service.ConversationSenderTypeUser),
		AdminUnread:            conversationUnread(m.LastMessageID, m.AdminLastReadMessageID, m.LastMessageSenderType == service.ConversationSenderTypeUser),
		Messages:               supportMessageEntitiesToService(m.Edges.Messages),
	}
	if m.Edges.User != nil {
		out.UserEmail = m.Edges.User.Email
		out.UserName = m.Edges.User.Username
	}
	return out
}

func supportThreadEntitiesToService(models []*dbent.SupportThread) []service.Conversation {
	out := make([]service.Conversation, 0, len(models))
	for i := range models {
		if item := supportThreadEntityToService(models[i]); item != nil {
			out = append(out, *item)
		}
	}
	return out
}

func applySupportThreadEntityToService(dst *service.Conversation, src *dbent.SupportThread) {
	if dst == nil || src == nil {
		return
	}
	converted := supportThreadEntityToService(src)
	*dst = *converted
}

func supportMessageEntityToService(m *dbent.SupportMessage) *service.ConversationMessage {
	if m == nil {
		return nil
	}
	return &service.ConversationMessage{
		ID:             m.ID,
		ConversationID: m.ThreadID,
		SenderType:     m.SenderType,
		SenderID:       m.SenderID,
		MessageType:    m.MessageType,
		ContentFormat:  m.ContentFormat,
		Content:        m.Content,
		Metadata:       normalizeRepositoryMetadata(m.Metadata),
		CreatedAt:      m.CreatedAt,
	}
}

func supportMessageEntitiesToService(models []*dbent.SupportMessage) []service.ConversationMessage {
	out := make([]service.ConversationMessage, 0, len(models))
	for i := range models {
		if item := supportMessageEntityToService(models[i]); item != nil {
			out = append(out, *item)
		}
	}
	return out
}

func applySupportMessageEntityToService(dst *service.ConversationMessage, src *dbent.SupportMessage) {
	if dst == nil || src == nil {
		return
	}
	converted := supportMessageEntityToService(src)
	*dst = *converted
}

func conversationUnread(lastID *int64, readID *int64, fromCounterparty bool) bool {
	if lastID == nil || !fromCounterparty {
		return false
	}
	return readID == nil || *readID < *lastID
}

func conversationMessageExcerpt(content string) string {
	content = strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if content == "" {
		return ""
	}
	if utf8.RuneCountInString(content) <= 220 {
		return content
	}
	runes := []rune(content)
	return string(runes[:220])
}

func normalizeSupportThreadSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return "工单服务"
	}
	return subject
}

func normalizeRepositoryMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	return metadata
}
