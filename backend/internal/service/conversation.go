package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	ConversationStatusOpen         = domain.ConversationStatusOpen
	ConversationStatusPendingUser  = domain.ConversationStatusPendingUser
	ConversationStatusPendingAdmin = domain.ConversationStatusPendingAdmin
	ConversationStatusResolved     = domain.ConversationStatusResolved
	ConversationStatusClosed       = domain.ConversationStatusClosed

	ConversationKindTicket       = domain.ConversationKindTicket
	ConversationKindSystemNotice = domain.ConversationKindSystemNotice

	ConversationPriorityLow    = domain.ConversationPriorityLow
	ConversationPriorityNormal = domain.ConversationPriorityNormal
	ConversationPriorityHigh   = domain.ConversationPriorityHigh
	ConversationPriorityUrgent = domain.ConversationPriorityUrgent

	ConversationTypeSupport      = domain.ConversationTypeSupport
	ConversationTypeNotice       = domain.ConversationTypeNotice
	ConversationTypeBilling      = domain.ConversationTypeBilling
	ConversationTypeSubscription = domain.ConversationTypeSubscription
	ConversationTypeAccount      = domain.ConversationTypeAccount
	ConversationTypeSecurity     = domain.ConversationTypeSecurity

	ConversationSenderTypeUser   = domain.ConversationSenderTypeUser
	ConversationSenderTypeAdmin  = domain.ConversationSenderTypeAdmin
	ConversationSenderTypeSystem = domain.ConversationSenderTypeSystem

	ConversationMessageTypeText         = domain.ConversationMessageTypeText
	ConversationMessageTypeNotice       = domain.ConversationMessageTypeNotice
	ConversationMessageTypeOperationLog = domain.ConversationMessageTypeOperationLog
	ConversationMessageTypeSystemEvent  = domain.ConversationMessageTypeSystemEvent

	ConversationContentFormatPlain    = domain.ConversationContentFormatPlain
	ConversationContentFormatMarkdown = domain.ConversationContentFormatMarkdown
)

var (
	ErrConversationNotFound             = infraerrors.NotFound("CONVERSATION_NOT_FOUND", "conversation not found")
	ErrConversationMessageNotFound      = infraerrors.NotFound("CONVERSATION_MESSAGE_NOT_FOUND", "conversation message not found")
	ErrConversationInputRequired        = infraerrors.BadRequest("CONVERSATION_INPUT_REQUIRED", "conversation input is required")
	ErrConversationSubjectInvalid       = infraerrors.BadRequest("CONVERSATION_SUBJECT_INVALID", "conversation subject is invalid")
	ErrConversationContentRequired      = infraerrors.BadRequest("CONVERSATION_CONTENT_REQUIRED", "conversation message content is required")
	ErrConversationContentTooLong       = infraerrors.BadRequest("CONVERSATION_CONTENT_TOO_LONG", "conversation message content is too long")
	ErrConversationStatusInvalid        = infraerrors.BadRequest("CONVERSATION_STATUS_INVALID", "conversation status is invalid")
	ErrConversationPriorityInvalid      = infraerrors.BadRequest("CONVERSATION_PRIORITY_INVALID", "conversation priority is invalid")
	ErrConversationTypeInvalid          = infraerrors.BadRequest("CONVERSATION_TYPE_INVALID", "conversation type is invalid")
	ErrConversationSenderInvalid        = infraerrors.BadRequest("CONVERSATION_SENDER_INVALID", "conversation sender is invalid")
	ErrConversationMessageTypeInvalid   = infraerrors.BadRequest("CONVERSATION_MESSAGE_TYPE_INVALID", "conversation message type is invalid")
	ErrConversationContentFormatInvalid = infraerrors.BadRequest("CONVERSATION_CONTENT_FORMAT_INVALID", "conversation content format is invalid")
	ErrConversationClosed               = infraerrors.Conflict("CONVERSATION_CLOSED", "conversation is closed")
	ErrConversationKindInvalid          = infraerrors.BadRequest("CONVERSATION_KIND_INVALID", "conversation kind is invalid")
	ErrConversationReadOnly             = infraerrors.Conflict("CONVERSATION_READ_ONLY", "conversation is read-only")
	ErrConversationNoticeReference      = infraerrors.BadRequest("CONVERSATION_NOTICE_REFERENCE_INVALID", "referenced notice is invalid")
	ErrConversationAssigneeInvalid      = infraerrors.BadRequest("CONVERSATION_ASSIGNEE_INVALID", "conversation assignee is invalid")
	ErrConversationAdminRequired        = infraerrors.Forbidden("CONVERSATION_ADMIN_REQUIRED", "admin permission is required")
	ErrConversationDuplicateSource      = infraerrors.Conflict("CONVERSATION_DUPLICATE_SOURCE", "conversation source already exists")
)

type Conversation struct {
	ID                     int64
	UserID                 int64
	UserEmail              string
	UserName               string
	Subject                string
	Status                 string
	Kind                   string
	Priority               string
	Type                   string
	Source                 string
	SourceID               string
	ReferencedNoticeID     *int64
	AssignedAdminID        *int64
	LastMessageID          *int64
	LastMessageSenderType  string
	LastMessageExcerpt     string
	LastMessageAt          time.Time
	UserLastReadMessageID  *int64
	UserLastReadAt         *time.Time
	AdminLastReadMessageID *int64
	AdminLastReadAt        *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
	UserUnread             bool
	AdminUnread            bool
	Messages               []ConversationMessage
}

type ConversationMessage struct {
	ID             int64
	ConversationID int64
	SenderType     string
	SenderID       *int64
	MessageType    string
	ContentFormat  string
	Content        string
	Metadata       map[string]any
	CreatedAt      time.Time
}

type CreateConversationInput struct {
	UserID             int64
	Subject            string
	Content            string
	Kind               string
	Priority           string
	Type               string
	Source             string
	SourceID           string
	ReferencedNoticeID *int64
	ContentFormat      string
	ActorID            int64
	ActorType          string
}

type AddConversationMessageInput struct {
	ConversationID int64
	ActorID        int64
	ActorType      string
	Content        string
	MessageType    string
	ContentFormat  string
	Metadata       map[string]any
}

type ConversationListFilters struct {
	UserID          int64
	Status          string
	Kind            string
	Priority        string
	Type            string
	AssignedAdminID int64
	Search          string
	UnreadOnly      bool
}

type ConversationMessageListFilters struct {
	BeforeID int64
	Latest   bool
}

type ConversationRepository interface {
	CreateWithMessage(ctx context.Context, conv *Conversation, msg *ConversationMessage) error
	AddMessage(ctx context.Context, conversationID int64, msg *ConversationMessage, nextStatus string) (*Conversation, error)
	GetByID(ctx context.Context, id int64) (*Conversation, error)
	GetByIDForUser(ctx context.Context, userID, id int64) (*Conversation, error)
	GetSystemNoticeBySource(ctx context.Context, userID int64, source, sourceID string) (*Conversation, error)
	List(ctx context.Context, params pagination.PaginationParams, filters ConversationListFilters) ([]Conversation, *pagination.PaginationResult, error)
	ListForUser(ctx context.Context, userID int64, params pagination.PaginationParams, filters ConversationListFilters) ([]Conversation, *pagination.PaginationResult, error)
	ListMessages(ctx context.Context, conversationID int64, params pagination.PaginationParams, filters ConversationMessageListFilters) ([]ConversationMessage, *pagination.PaginationResult, error)
	MarkRead(ctx context.Context, conversationID int64, readerType string, readUntilMessageID *int64) (*Conversation, error)
	UpdateStatus(ctx context.Context, conversationID int64, status string) (*Conversation, error)
	UpdateAssignee(ctx context.Context, conversationID int64, adminID *int64) (*Conversation, error)
	CountUnreadForUser(ctx context.Context, userID int64) (int64, error)
	CountUnreadForAdmin(ctx context.Context) (int64, error)
}

type ConversationService struct {
	repo     ConversationRepository
	userRepo UserRepository
}

func NewConversationService(repo ConversationRepository, userRepo UserRepository) *ConversationService {
	return &ConversationService{repo: repo, userRepo: userRepo}
}

func (s *ConversationService) CreateByUser(ctx context.Context, input *CreateConversationInput) (*Conversation, error) {
	if input == nil {
		return nil, ErrConversationInputRequired
	}
	input.ActorID = input.UserID
	input.ActorType = ConversationSenderTypeUser
	input.Kind = ConversationKindTicket
	input.Source = ""
	input.SourceID = ""
	input.ContentFormat = ConversationContentFormatPlain
	return s.create(ctx, input)
}

func (s *ConversationService) CreateByAdmin(ctx context.Context, input *CreateConversationInput) (*Conversation, error) {
	if input == nil {
		return nil, ErrConversationInputRequired
	}
	if err := s.ensureActorAdmin(ctx, input.ActorID); err != nil {
		return nil, err
	}
	input.ActorType = ConversationSenderTypeAdmin
	if strings.TrimSpace(input.Kind) == "" {
		input.Kind = ConversationKindTicket
	}
	return s.create(ctx, input)
}

func (s *ConversationService) CreateSystemNotice(ctx context.Context, input *CreateConversationInput) (*Conversation, error) {
	if input == nil {
		return nil, ErrConversationInputRequired
	}
	if err := s.ensureActorAdmin(ctx, input.ActorID); err != nil {
		return nil, err
	}
	return s.CreateSystemNoticeInternal(ctx, input)
}

func (s *ConversationService) CreateSystemNoticeInternal(ctx context.Context, input *CreateConversationInput) (*Conversation, error) {
	if input == nil {
		return nil, ErrConversationInputRequired
	}
	input.ActorID = 0
	input.ActorType = ConversationSenderTypeSystem
	input.Kind = ConversationKindSystemNotice
	if strings.TrimSpace(input.Type) == "" {
		input.Type = ConversationTypeNotice
	}
	input.Priority = ConversationPriorityNormal
	return s.create(ctx, input)
}

func (s *ConversationService) create(ctx context.Context, input *CreateConversationInput) (*Conversation, error) {
	if input == nil {
		return nil, ErrConversationInputRequired
	}
	if input.UserID <= 0 {
		return nil, ErrConversationInputRequired
	}
	if _, err := s.userRepo.GetByID(ctx, input.UserID); err != nil {
		return nil, err
	}
	kind := normalizeConversationKind(input.Kind)
	if !isValidConversationKind(kind) {
		return nil, ErrConversationKindInvalid
	}
	if kind == ConversationKindSystemNotice && input.ActorType != ConversationSenderTypeSystem {
		return nil, ErrConversationSenderInvalid
	}
	if kind == ConversationKindTicket && input.ActorType == ConversationSenderTypeSystem {
		return nil, ErrConversationSenderInvalid
	}
	if input.ReferencedNoticeID != nil {
		return nil, ErrConversationNoticeReference
	}
	source := strings.TrimSpace(input.Source)
	sourceID := strings.TrimSpace(input.SourceID)
	if kind == ConversationKindSystemNotice && source != "" && sourceID != "" {
		if _, err := s.repo.GetSystemNoticeBySource(ctx, input.UserID, source, sourceID); err == nil {
			return nil, ErrConversationDuplicateSource
		} else if !errors.Is(err, ErrConversationNotFound) {
			return nil, err
		}
	}

	subject, err := normalizeConversationSubject(input.Subject)
	if err != nil {
		return nil, err
	}
	content, err := normalizeConversationContent(input.Content)
	if err != nil {
		return nil, err
	}
	priority := normalizeConversationPriority(input.Priority)
	if !isValidConversationPriority(priority) {
		return nil, ErrConversationPriorityInvalid
	}
	convType := normalizeConversationType(input.Type)
	if !isValidConversationType(convType) {
		return nil, ErrConversationTypeInvalid
	}
	contentFormat := normalizeConversationContentFormat(input.ContentFormat)
	if !isValidConversationContentFormat(contentFormat) {
		return nil, ErrConversationContentFormatInvalid
	}
	if !isValidConversationSenderType(input.ActorType) || (input.ActorType != ConversationSenderTypeSystem && input.ActorID <= 0) {
		return nil, ErrConversationSenderInvalid
	}

	now := time.Now()
	status := nextStatusForSender(input.ActorType)
	if kind == ConversationKindSystemNotice {
		status = ConversationStatusOpen
	}
	conv := &Conversation{
		UserID:             input.UserID,
		Subject:            subject,
		Status:             status,
		Kind:               kind,
		Priority:           priority,
		Type:               convType,
		Source:             source,
		SourceID:           sourceID,
		ReferencedNoticeID: input.ReferencedNoticeID,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if input.ActorType == ConversationSenderTypeAdmin {
		conv.AssignedAdminID = &input.ActorID
	}
	var senderID *int64
	if input.ActorType != ConversationSenderTypeSystem {
		senderID = &input.ActorID
	}
	messageType := ConversationMessageTypeText
	if kind == ConversationKindSystemNotice {
		messageType = ConversationMessageTypeNotice
	}
	metadata := map[string]any{}
	if kind == ConversationKindSystemNotice && (source != "" || sourceID != "") {
		metadata["source"] = source
		metadata["source_id"] = sourceID
	}
	msg := &ConversationMessage{
		SenderType:    input.ActorType,
		SenderID:      senderID,
		MessageType:   messageType,
		ContentFormat: contentFormat,
		Content:       content,
		Metadata:      metadata,
		CreatedAt:     now,
	}

	if err := s.repo.CreateWithMessage(ctx, conv, msg); err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}
	return s.repo.GetByID(ctx, conv.ID)
}

func (s *ConversationService) AddUserMessage(ctx context.Context, input *AddConversationMessageInput) (*Conversation, error) {
	if input == nil {
		return nil, ErrConversationInputRequired
	}
	input.ActorType = ConversationSenderTypeUser
	input.MessageType = ConversationMessageTypeText
	input.ContentFormat = ConversationContentFormatPlain
	input.Metadata = nil
	return s.addMessage(ctx, input, true)
}

func (s *ConversationService) AddAdminMessage(ctx context.Context, input *AddConversationMessageInput) (*Conversation, error) {
	if input == nil {
		return nil, ErrConversationInputRequired
	}
	if err := s.ensureActorAdmin(ctx, input.ActorID); err != nil {
		return nil, err
	}
	input.ActorType = ConversationSenderTypeAdmin
	input.MessageType = ConversationMessageTypeText
	input.ContentFormat = ConversationContentFormatPlain
	input.Metadata = nil
	return s.addMessage(ctx, input, false)
}

func (s *ConversationService) addMessage(ctx context.Context, input *AddConversationMessageInput, enforceUserOwner bool) (*Conversation, error) {
	if input.ConversationID <= 0 || input.ActorID <= 0 {
		return nil, ErrConversationInputRequired
	}
	var conv *Conversation
	var err error
	if enforceUserOwner {
		conv, err = s.repo.GetByIDForUser(ctx, input.ActorID, input.ConversationID)
	} else {
		conv, err = s.repo.GetByID(ctx, input.ConversationID)
	}
	if err != nil {
		return nil, err
	}
	if conv.Status == ConversationStatusClosed && input.ActorType != ConversationSenderTypeUser {
		return nil, ErrConversationClosed
	}

	content, err := normalizeConversationContent(input.Content)
	if err != nil {
		return nil, err
	}
	messageType := normalizeConversationMessageType(input.MessageType)
	if !isValidConversationMessageType(messageType) {
		return nil, ErrConversationMessageTypeInvalid
	}
	contentFormat := normalizeConversationContentFormat(input.ContentFormat)
	if !isValidConversationContentFormat(contentFormat) {
		return nil, ErrConversationContentFormatInvalid
	}
	if !isValidConversationSenderType(input.ActorType) {
		return nil, ErrConversationSenderInvalid
	}

	msg := &ConversationMessage{
		SenderType:    input.ActorType,
		SenderID:      &input.ActorID,
		MessageType:   messageType,
		ContentFormat: contentFormat,
		Content:       content,
		Metadata:      normalizeConversationMetadata(input.Metadata),
		CreatedAt:     time.Now(),
	}
	return s.repo.AddMessage(ctx, input.ConversationID, msg, nextStatusForSender(input.ActorType))
}

func (s *ConversationService) ListForUser(ctx context.Context, userID int64, params pagination.PaginationParams, filters ConversationListFilters) ([]Conversation, *pagination.PaginationResult, error) {
	if userID <= 0 {
		return nil, nil, ErrConversationInputRequired
	}
	filters.UserID = userID
	return s.repo.ListForUser(ctx, userID, normalizePagination(params), normalizeConversationFilters(filters))
}

func (s *ConversationService) ListAdmin(ctx context.Context, params pagination.PaginationParams, filters ConversationListFilters) ([]Conversation, *pagination.PaginationResult, error) {
	return s.repo.List(ctx, normalizePagination(params), normalizeConversationFilters(filters))
}

func (s *ConversationService) GetForUser(ctx context.Context, userID, id int64) (*Conversation, error) {
	if userID <= 0 || id <= 0 {
		return nil, ErrConversationInputRequired
	}
	return s.repo.GetByIDForUser(ctx, userID, id)
}

func (s *ConversationService) GetAdmin(ctx context.Context, id int64) (*Conversation, error) {
	if id <= 0 {
		return nil, ErrConversationInputRequired
	}
	return s.repo.GetByID(ctx, id)
}

func (s *ConversationService) ListMessagesForUser(ctx context.Context, userID, conversationID int64, params pagination.PaginationParams, filters ConversationMessageListFilters) ([]ConversationMessage, *pagination.PaginationResult, error) {
	if userID <= 0 || conversationID <= 0 {
		return nil, nil, ErrConversationInputRequired
	}
	if filters.BeforeID < 0 {
		return nil, nil, ErrConversationInputRequired
	}
	if _, err := s.repo.GetByIDForUser(ctx, userID, conversationID); err != nil {
		return nil, nil, err
	}
	return s.repo.ListMessages(ctx, conversationID, normalizePagination(params), filters)
}

func (s *ConversationService) ListMessagesAdmin(ctx context.Context, conversationID int64, params pagination.PaginationParams, filters ConversationMessageListFilters) ([]ConversationMessage, *pagination.PaginationResult, error) {
	if conversationID <= 0 {
		return nil, nil, ErrConversationInputRequired
	}
	if filters.BeforeID < 0 {
		return nil, nil, ErrConversationInputRequired
	}
	if _, err := s.repo.GetByID(ctx, conversationID); err != nil {
		return nil, nil, err
	}
	return s.repo.ListMessages(ctx, conversationID, normalizePagination(params), filters)
}

func (s *ConversationService) MarkReadForUser(ctx context.Context, userID, conversationID int64, readUntilMessageID *int64) (*Conversation, error) {
	if userID <= 0 || conversationID <= 0 {
		return nil, ErrConversationInputRequired
	}
	if readUntilMessageID != nil && *readUntilMessageID <= 0 {
		return nil, ErrConversationInputRequired
	}
	if _, err := s.repo.GetByIDForUser(ctx, userID, conversationID); err != nil {
		return nil, err
	}
	return s.repo.MarkRead(ctx, conversationID, ConversationSenderTypeUser, readUntilMessageID)
}

func (s *ConversationService) CloseForUser(ctx context.Context, userID, conversationID int64) (*Conversation, error) {
	if userID <= 0 || conversationID <= 0 {
		return nil, ErrConversationInputRequired
	}
	if _, err := s.repo.GetByIDForUser(ctx, userID, conversationID); err != nil {
		return nil, err
	}
	return s.repo.UpdateStatus(ctx, conversationID, ConversationStatusClosed)
}

func (s *ConversationService) MarkReadAdmin(ctx context.Context, conversationID int64, readUntilMessageID *int64) (*Conversation, error) {
	if conversationID <= 0 {
		return nil, ErrConversationInputRequired
	}
	if readUntilMessageID != nil && *readUntilMessageID <= 0 {
		return nil, ErrConversationInputRequired
	}
	if _, err := s.repo.GetByID(ctx, conversationID); err != nil {
		return nil, err
	}
	return s.repo.MarkRead(ctx, conversationID, ConversationSenderTypeAdmin, readUntilMessageID)
}

func (s *ConversationService) UpdateStatusAdmin(ctx context.Context, conversationID int64, status string) (*Conversation, error) {
	if conversationID <= 0 {
		return nil, ErrConversationInputRequired
	}
	status = strings.TrimSpace(status)
	if !isValidConversationStatus(status) {
		return nil, ErrConversationStatusInvalid
	}
	if _, err := s.repo.GetByID(ctx, conversationID); err != nil {
		return nil, err
	}
	return s.repo.UpdateStatus(ctx, conversationID, status)
}

func (s *ConversationService) UpdateAssigneeAdmin(ctx context.Context, conversationID int64, adminID *int64) (*Conversation, error) {
	if conversationID <= 0 {
		return nil, ErrConversationInputRequired
	}
	if _, err := s.repo.GetByID(ctx, conversationID); err != nil {
		return nil, err
	}
	if adminID != nil {
		if *adminID <= 0 {
			return nil, ErrConversationInputRequired
		}
		admin, err := s.userRepo.GetByID(ctx, *adminID)
		if err != nil {
			return nil, err
		}
		if admin == nil || !admin.IsAdmin() {
			return nil, ErrConversationAssigneeInvalid
		}
	}
	return s.repo.UpdateAssignee(ctx, conversationID, adminID)
}

func (s *ConversationService) CountUnreadForUser(ctx context.Context, userID int64) (int64, error) {
	if userID <= 0 {
		return 0, ErrConversationInputRequired
	}
	return s.repo.CountUnreadForUser(ctx, userID)
}

func (s *ConversationService) CountUnreadForAdmin(ctx context.Context) (int64, error) {
	return s.repo.CountUnreadForAdmin(ctx)
}

func (s *ConversationService) ensureActorAdmin(ctx context.Context, actorID int64) error {
	if actorID <= 0 {
		return ErrConversationAdminRequired
	}
	admin, err := s.userRepo.GetByID(ctx, actorID)
	if err != nil {
		return err
	}
	if admin == nil || !admin.IsAdmin() {
		return ErrConversationAdminRequired
	}
	return nil
}

func normalizeConversationSubject(subject string) (string, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" || utf8.RuneCountInString(subject) > 200 {
		return "", ErrConversationSubjectInvalid
	}
	return subject, nil
}

func normalizeConversationContent(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", ErrConversationContentRequired
	}
	if utf8.RuneCountInString(content) > 10000 {
		return "", ErrConversationContentTooLong
	}
	return content, nil
}

func normalizeConversationPriority(priority string) string {
	priority = strings.TrimSpace(priority)
	if priority == "" {
		return ConversationPriorityNormal
	}
	return priority
}

func normalizeConversationKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return ConversationKindTicket
	}
	return kind
}

func normalizeConversationType(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return ConversationTypeSupport
	}
	return t
}

func normalizeConversationMessageType(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return ConversationMessageTypeText
	}
	return t
}

func normalizeConversationContentFormat(format string) string {
	format = strings.TrimSpace(format)
	if format == "" {
		return ConversationContentFormatPlain
	}
	return format
}

func normalizeConversationMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	return metadata
}

func normalizeConversationFilters(filters ConversationListFilters) ConversationListFilters {
	filters.Status = strings.TrimSpace(filters.Status)
	filters.Kind = strings.TrimSpace(filters.Kind)
	filters.Priority = strings.TrimSpace(filters.Priority)
	filters.Type = strings.TrimSpace(filters.Type)
	filters.Search = strings.TrimSpace(filters.Search)
	if len(filters.Search) > 200 {
		filters.Search = string([]rune(filters.Search)[:200])
	}
	return filters
}

func normalizePagination(params pagination.PaginationParams) pagination.PaginationParams {
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}
	if params.SortOrder == "" {
		params.SortOrder = pagination.SortOrderDesc
	}
	return params
}

func isValidConversationKind(kind string) bool {
	switch kind {
	case ConversationKindTicket, ConversationKindSystemNotice:
		return true
	default:
		return false
	}
}

func isValidConversationStatus(status string) bool {
	switch status {
	case ConversationStatusOpen, ConversationStatusPendingUser, ConversationStatusPendingAdmin, ConversationStatusResolved, ConversationStatusClosed:
		return true
	default:
		return false
	}
}

func isValidConversationPriority(priority string) bool {
	switch priority {
	case ConversationPriorityLow, ConversationPriorityNormal, ConversationPriorityHigh, ConversationPriorityUrgent:
		return true
	default:
		return false
	}
}

func isValidConversationType(t string) bool {
	switch t {
	case ConversationTypeSupport, ConversationTypeNotice, ConversationTypeBilling, ConversationTypeSubscription, ConversationTypeAccount, ConversationTypeSecurity:
		return true
	default:
		return false
	}
}

func isValidConversationSenderType(senderType string) bool {
	switch senderType {
	case ConversationSenderTypeUser, ConversationSenderTypeAdmin, ConversationSenderTypeSystem:
		return true
	default:
		return false
	}
}

func isValidConversationMessageType(messageType string) bool {
	switch messageType {
	case ConversationMessageTypeText, ConversationMessageTypeNotice, ConversationMessageTypeOperationLog, ConversationMessageTypeSystemEvent:
		return true
	default:
		return false
	}
}

func isValidConversationContentFormat(format string) bool {
	switch format {
	case ConversationContentFormatPlain, ConversationContentFormatMarkdown:
		return true
	default:
		return false
	}
}

func nextStatusForSender(senderType string) string {
	switch senderType {
	case ConversationSenderTypeUser:
		return ConversationStatusPendingAdmin
	case ConversationSenderTypeAdmin, ConversationSenderTypeSystem:
		return ConversationStatusPendingUser
	default:
		return ConversationStatusOpen
	}
}
