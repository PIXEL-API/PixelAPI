package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type conversationRepoStub struct {
	createWithMessageFunc   func(context.Context, *Conversation, *ConversationMessage) error
	addMessageFunc          func(context.Context, int64, *ConversationMessage, string) (*Conversation, error)
	getByIDFunc             func(context.Context, int64) (*Conversation, error)
	getByIDForUserFunc      func(context.Context, int64, int64) (*Conversation, error)
	getNoticeBySourceFunc   func(context.Context, int64, string, string) (*Conversation, error)
	listFunc                func(context.Context, pagination.PaginationParams, ConversationListFilters) ([]Conversation, *pagination.PaginationResult, error)
	listForUserFunc         func(context.Context, int64, pagination.PaginationParams, ConversationListFilters) ([]Conversation, *pagination.PaginationResult, error)
	listMessagesFunc        func(context.Context, int64, pagination.PaginationParams) ([]ConversationMessage, *pagination.PaginationResult, error)
	markReadFunc            func(context.Context, int64, string, *int64) (*Conversation, error)
	updateStatusFunc        func(context.Context, int64, string) (*Conversation, error)
	updateAssigneeFunc      func(context.Context, int64, *int64) (*Conversation, error)
	countUnreadForUserFunc  func(context.Context, int64) (int64, error)
	countUnreadForAdminFunc func(context.Context) (int64, error)
}

func (s *conversationRepoStub) CreateWithMessage(ctx context.Context, conv *Conversation, msg *ConversationMessage) error {
	if s.createWithMessageFunc != nil {
		return s.createWithMessageFunc(ctx, conv, msg)
	}
	return nil
}

func (s *conversationRepoStub) AddMessage(ctx context.Context, conversationID int64, msg *ConversationMessage, nextStatus string) (*Conversation, error) {
	if s.addMessageFunc != nil {
		return s.addMessageFunc(ctx, conversationID, msg, nextStatus)
	}
	return &Conversation{ID: conversationID, Kind: ConversationKindTicket}, nil
}

func (s *conversationRepoStub) GetByID(ctx context.Context, id int64) (*Conversation, error) {
	if s.getByIDFunc != nil {
		return s.getByIDFunc(ctx, id)
	}
	return &Conversation{ID: id, UserID: 1, Kind: ConversationKindTicket, Status: ConversationStatusOpen}, nil
}

func (s *conversationRepoStub) GetByIDForUser(ctx context.Context, userID, id int64) (*Conversation, error) {
	if s.getByIDForUserFunc != nil {
		return s.getByIDForUserFunc(ctx, userID, id)
	}
	return &Conversation{ID: id, UserID: userID, Kind: ConversationKindTicket, Status: ConversationStatusOpen}, nil
}

func (s *conversationRepoStub) GetSystemNoticeBySource(ctx context.Context, userID int64, source, sourceID string) (*Conversation, error) {
	if s.getNoticeBySourceFunc != nil {
		return s.getNoticeBySourceFunc(ctx, userID, source, sourceID)
	}
	return nil, ErrConversationNotFound
}

func (s *conversationRepoStub) List(ctx context.Context, params pagination.PaginationParams, filters ConversationListFilters) ([]Conversation, *pagination.PaginationResult, error) {
	if s.listFunc != nil {
		return s.listFunc(ctx, params, filters)
	}
	return nil, &pagination.PaginationResult{}, nil
}

func (s *conversationRepoStub) ListForUser(ctx context.Context, userID int64, params pagination.PaginationParams, filters ConversationListFilters) ([]Conversation, *pagination.PaginationResult, error) {
	if s.listForUserFunc != nil {
		return s.listForUserFunc(ctx, userID, params, filters)
	}
	return nil, &pagination.PaginationResult{}, nil
}

func (s *conversationRepoStub) ListMessages(ctx context.Context, conversationID int64, params pagination.PaginationParams) ([]ConversationMessage, *pagination.PaginationResult, error) {
	if s.listMessagesFunc != nil {
		return s.listMessagesFunc(ctx, conversationID, params)
	}
	return nil, &pagination.PaginationResult{}, nil
}

func (s *conversationRepoStub) MarkRead(ctx context.Context, conversationID int64, readerType string, readUntilMessageID *int64) (*Conversation, error) {
	if s.markReadFunc != nil {
		return s.markReadFunc(ctx, conversationID, readerType, readUntilMessageID)
	}
	return &Conversation{ID: conversationID, Kind: ConversationKindTicket}, nil
}

func (s *conversationRepoStub) UpdateStatus(ctx context.Context, conversationID int64, status string) (*Conversation, error) {
	if s.updateStatusFunc != nil {
		return s.updateStatusFunc(ctx, conversationID, status)
	}
	return &Conversation{ID: conversationID, Status: status, Kind: ConversationKindTicket}, nil
}

func (s *conversationRepoStub) UpdateAssignee(ctx context.Context, conversationID int64, adminID *int64) (*Conversation, error) {
	if s.updateAssigneeFunc != nil {
		return s.updateAssigneeFunc(ctx, conversationID, adminID)
	}
	return &Conversation{ID: conversationID, AssignedAdminID: adminID, Kind: ConversationKindTicket}, nil
}

func (s *conversationRepoStub) CountUnreadForUser(ctx context.Context, userID int64) (int64, error) {
	if s.countUnreadForUserFunc != nil {
		return s.countUnreadForUserFunc(ctx, userID)
	}
	return 0, nil
}

func (s *conversationRepoStub) CountUnreadForAdmin(ctx context.Context) (int64, error) {
	if s.countUnreadForAdminFunc != nil {
		return s.countUnreadForAdminFunc(ctx)
	}
	return 0, nil
}

type conversationUserRepoStub struct {
	users map[int64]*User
}

func (s *conversationUserRepoStub) GetByID(_ context.Context, id int64) (*User, error) {
	if user, ok := s.users[id]; ok {
		return user, nil
	}
	return nil, ErrUserNotFound
}

func (s *conversationUserRepoStub) Create(context.Context, *User) error {
	panic("unexpected Create call")
}

func (s *conversationUserRepoStub) GetByEmail(context.Context, string) (*User, error) {
	panic("unexpected GetByEmail call")
}

func (s *conversationUserRepoStub) GetFirstAdmin(context.Context) (*User, error) {
	panic("unexpected GetFirstAdmin call")
}

func (s *conversationUserRepoStub) Update(context.Context, *User) error {
	panic("unexpected Update call")
}

func (s *conversationUserRepoStub) Delete(context.Context, int64) error {
	panic("unexpected Delete call")
}

func (s *conversationUserRepoStub) GetUserAvatar(context.Context, int64) (*UserAvatar, error) {
	panic("unexpected GetUserAvatar call")
}

func (s *conversationUserRepoStub) UpsertUserAvatar(context.Context, int64, UpsertUserAvatarInput) (*UserAvatar, error) {
	panic("unexpected UpsertUserAvatar call")
}

func (s *conversationUserRepoStub) DeleteUserAvatar(context.Context, int64) error {
	panic("unexpected DeleteUserAvatar call")
}

func (s *conversationUserRepoStub) List(context.Context, pagination.PaginationParams) ([]User, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (s *conversationUserRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, UserListFilters) ([]User, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}

func (s *conversationUserRepoStub) GetLatestUsedAtByUserIDs(context.Context, []int64) (map[int64]*time.Time, error) {
	panic("unexpected GetLatestUsedAtByUserIDs call")
}

func (s *conversationUserRepoStub) GetLatestUsedAtByUserID(context.Context, int64) (*time.Time, error) {
	panic("unexpected GetLatestUsedAtByUserID call")
}

func (s *conversationUserRepoStub) UpdateUserLastActiveAt(context.Context, int64, time.Time) error {
	panic("unexpected UpdateUserLastActiveAt call")
}

func (s *conversationUserRepoStub) UpdateBalance(context.Context, int64, float64) error {
	panic("unexpected UpdateBalance call")
}

func (s *conversationUserRepoStub) DeductBalance(context.Context, int64, float64) error {
	panic("unexpected DeductBalance call")
}

func (s *conversationUserRepoStub) UpdateConcurrency(context.Context, int64, int) error {
	panic("unexpected UpdateConcurrency call")
}

func (s *conversationUserRepoStub) ExistsByEmail(context.Context, string) (bool, error) {
	panic("unexpected ExistsByEmail call")
}

func (s *conversationUserRepoStub) RemoveGroupFromAllowedGroups(context.Context, int64) (int64, error) {
	panic("unexpected RemoveGroupFromAllowedGroups call")
}

func (s *conversationUserRepoStub) AddGroupToAllowedGroups(context.Context, int64, int64) error {
	panic("unexpected AddGroupToAllowedGroups call")
}

func (s *conversationUserRepoStub) RemoveGroupFromUserAllowedGroups(context.Context, int64, int64) error {
	panic("unexpected RemoveGroupFromUserAllowedGroups call")
}

func (s *conversationUserRepoStub) ListUserAuthIdentities(context.Context, int64) ([]UserAuthIdentityRecord, error) {
	panic("unexpected ListUserAuthIdentities call")
}

func (s *conversationUserRepoStub) UnbindUserAuthProvider(context.Context, int64, string) error {
	panic("unexpected UnbindUserAuthProvider call")
}

func (s *conversationUserRepoStub) UpdateTotpSecret(context.Context, int64, *string) error {
	panic("unexpected UpdateTotpSecret call")
}

func (s *conversationUserRepoStub) EnableTotp(context.Context, int64) error {
	panic("unexpected EnableTotp call")
}

func (s *conversationUserRepoStub) DisableTotp(context.Context, int64) error {
	panic("unexpected DisableTotp call")
}

func TestConversationCreateByUserSanitizesInternalFields(t *testing.T) {
	repo := &conversationRepoStub{
		createWithMessageFunc: func(_ context.Context, conv *Conversation, msg *ConversationMessage) error {
			conv.ID = 10
			require.Equal(t, ConversationKindTicket, conv.Kind)
			require.Empty(t, conv.Source)
			require.Empty(t, conv.SourceID)
			require.Equal(t, ConversationSenderTypeUser, msg.SenderType)
			require.Equal(t, ConversationContentFormatPlain, msg.ContentFormat)
			return nil
		},
		getByIDFunc: func(_ context.Context, id int64) (*Conversation, error) {
			return &Conversation{ID: id, UserID: 1, Kind: ConversationKindTicket}, nil
		},
	}
	svc := NewConversationService(repo, &conversationUserRepoStub{
		users: map[int64]*User{1: {ID: 1, Role: RoleUser, Status: StatusActive}},
	})

	out, err := svc.CreateByUser(context.Background(), &CreateConversationInput{
		UserID:        1,
		Subject:       "help",
		Content:       "need help",
		Kind:          ConversationKindSystemNotice,
		Source:        "billing",
		SourceID:      "secret-id",
		ContentFormat: ConversationContentFormatMarkdown,
	})

	require.NoError(t, err)
	require.Equal(t, int64(10), out.ID)
}

func TestConversationCreateByUserRejectsOtherUserNoticeReference(t *testing.T) {
	repo := &conversationRepoStub{
		getByIDForUserFunc: func(_ context.Context, userID, id int64) (*Conversation, error) {
			t.Fatal("notice references are no longer resolved because support messages share one thread")
			return nil, nil
		},
	}
	svc := NewConversationService(repo, &conversationUserRepoStub{
		users: map[int64]*User{1: {ID: 1, Role: RoleUser, Status: StatusActive}},
	})
	noticeID := int64(99)

	_, err := svc.CreateByUser(context.Background(), &CreateConversationInput{
		UserID:             1,
		Subject:            "help",
		Content:            "need help",
		ReferencedNoticeID: &noticeID,
	})

	require.ErrorIs(t, err, ErrConversationNoticeReference)
}

func TestConversationCreateByUserRejectsTicketReference(t *testing.T) {
	repo := &conversationRepoStub{
		getByIDForUserFunc: func(context.Context, int64, int64) (*Conversation, error) {
			return &Conversation{ID: 99, UserID: 1, Kind: ConversationKindTicket}, nil
		},
	}
	svc := NewConversationService(repo, &conversationUserRepoStub{
		users: map[int64]*User{1: {ID: 1, Role: RoleUser, Status: StatusActive}},
	})
	noticeID := int64(99)

	_, err := svc.CreateByUser(context.Background(), &CreateConversationInput{
		UserID:             1,
		Subject:            "help",
		Content:            "need help",
		ReferencedNoticeID: &noticeID,
	})

	require.ErrorIs(t, err, ErrConversationNoticeReference)
}

func TestConversationAddUserMessageUsesOwnerScopeAndSanitizesFields(t *testing.T) {
	repo := &conversationRepoStub{
		getByIDForUserFunc: func(_ context.Context, userID, id int64) (*Conversation, error) {
			require.Equal(t, int64(1), userID)
			require.Equal(t, int64(10), id)
			return &Conversation{ID: id, UserID: userID, Kind: ConversationKindTicket, Status: ConversationStatusOpen}, nil
		},
		addMessageFunc: func(_ context.Context, conversationID int64, msg *ConversationMessage, nextStatus string) (*Conversation, error) {
			require.Equal(t, int64(10), conversationID)
			require.Equal(t, ConversationSenderTypeUser, msg.SenderType)
			require.Equal(t, ConversationMessageTypeText, msg.MessageType)
			require.Equal(t, ConversationContentFormatPlain, msg.ContentFormat)
			require.Empty(t, msg.Metadata)
			require.Equal(t, ConversationStatusPendingAdmin, nextStatus)
			return &Conversation{ID: conversationID, Kind: ConversationKindTicket}, nil
		},
	}
	svc := NewConversationService(repo, &conversationUserRepoStub{})

	_, err := svc.AddUserMessage(context.Background(), &AddConversationMessageInput{
		ConversationID: 10,
		ActorID:        1,
		Content:        "hello",
		MessageType:    ConversationMessageTypeOperationLog,
		ContentFormat:  ConversationContentFormatMarkdown,
		Metadata:       map[string]any{"internal": true},
	})

	require.NoError(t, err)
}

func TestConversationAddUserMessageReopensClosedSingleThread(t *testing.T) {
	repo := &conversationRepoStub{
		getByIDForUserFunc: func(_ context.Context, userID, id int64) (*Conversation, error) {
			require.Equal(t, int64(1), userID)
			require.Equal(t, int64(10), id)
			return &Conversation{ID: id, UserID: userID, Kind: ConversationKindTicket, Status: ConversationStatusClosed}, nil
		},
		addMessageFunc: func(_ context.Context, conversationID int64, msg *ConversationMessage, nextStatus string) (*Conversation, error) {
			require.Equal(t, int64(10), conversationID)
			require.Equal(t, ConversationSenderTypeUser, msg.SenderType)
			require.Equal(t, ConversationStatusPendingAdmin, nextStatus)
			return &Conversation{ID: conversationID, Kind: ConversationKindTicket, Status: nextStatus}, nil
		},
	}
	svc := NewConversationService(repo, &conversationUserRepoStub{})

	out, err := svc.AddUserMessage(context.Background(), &AddConversationMessageInput{
		ConversationID: 10,
		ActorID:        1,
		Content:        "new issue",
	})

	require.NoError(t, err)
	require.Equal(t, ConversationStatusPendingAdmin, out.Status)
}

func TestConversationAddAdminMessageSanitizesPublicReplyFields(t *testing.T) {
	repo := &conversationRepoStub{
		getByIDFunc: func(_ context.Context, id int64) (*Conversation, error) {
			require.Equal(t, int64(10), id)
			return &Conversation{ID: id, UserID: 1, Kind: ConversationKindTicket, Status: ConversationStatusOpen}, nil
		},
		addMessageFunc: func(_ context.Context, conversationID int64, msg *ConversationMessage, nextStatus string) (*Conversation, error) {
			require.Equal(t, int64(10), conversationID)
			require.Equal(t, ConversationSenderTypeAdmin, msg.SenderType)
			require.Equal(t, ConversationMessageTypeText, msg.MessageType)
			require.Equal(t, ConversationContentFormatPlain, msg.ContentFormat)
			require.Empty(t, msg.Metadata)
			require.Equal(t, ConversationStatusPendingUser, nextStatus)
			return &Conversation{ID: conversationID, Kind: ConversationKindTicket}, nil
		},
	}
	svc := NewConversationService(repo, &conversationUserRepoStub{
		users: map[int64]*User{2: {ID: 2, Role: RoleAdmin, Status: StatusActive}},
	})

	_, err := svc.AddAdminMessage(context.Background(), &AddConversationMessageInput{
		ConversationID: 10,
		ActorID:        2,
		Content:        "admin reply",
		MessageType:    ConversationMessageTypeOperationLog,
		ContentFormat:  ConversationContentFormatMarkdown,
		Metadata:       map[string]any{"internal_note_id": "secret"},
	})

	require.NoError(t, err)
}

func TestConversationAdminOperationsRequireAdminActor(t *testing.T) {
	repo := &conversationRepoStub{
		createWithMessageFunc: func(context.Context, *Conversation, *ConversationMessage) error {
			t.Fatal("CreateWithMessage must not be called for non-admin actor")
			return nil
		},
		getByIDFunc: func(context.Context, int64) (*Conversation, error) {
			t.Fatal("GetByID must not be called for non-admin actor")
			return nil, nil
		},
		addMessageFunc: func(context.Context, int64, *ConversationMessage, string) (*Conversation, error) {
			t.Fatal("AddMessage must not be called for non-admin actor")
			return nil, nil
		},
	}
	svc := NewConversationService(repo, &conversationUserRepoStub{
		users: map[int64]*User{
			2: {ID: 2, Role: RoleUser, Status: StatusActive},
		},
	})

	_, err := svc.CreateByAdmin(context.Background(), &CreateConversationInput{
		UserID:  1,
		ActorID: 2,
		Subject: "hello",
		Content: "notice",
	})
	require.ErrorIs(t, err, ErrConversationAdminRequired)

	_, err = svc.CreateSystemNotice(context.Background(), &CreateConversationInput{
		UserID:  1,
		ActorID: 2,
		Subject: "hello",
		Content: "notice",
	})
	require.ErrorIs(t, err, ErrConversationAdminRequired)

	_, err = svc.AddAdminMessage(context.Background(), &AddConversationMessageInput{
		ConversationID: 10,
		ActorID:        2,
		Content:        "reply",
	})
	require.ErrorIs(t, err, ErrConversationAdminRequired)
}

func TestConversationCreateSystemNoticeInternalDoesNotRequireAdminActor(t *testing.T) {
	repo := &conversationRepoStub{
		createWithMessageFunc: func(_ context.Context, conv *Conversation, msg *ConversationMessage) error {
			conv.ID = 11
			require.Equal(t, int64(1), conv.UserID)
			require.Equal(t, ConversationKindSystemNotice, conv.Kind)
			require.Equal(t, ConversationStatusOpen, conv.Status)
			require.Equal(t, ConversationTypeNotice, conv.Type)
			require.Equal(t, "account", conv.Source)
			require.Equal(t, "42:created", conv.SourceID)
			require.Equal(t, ConversationSenderTypeSystem, msg.SenderType)
			require.Nil(t, msg.SenderID)
			require.Equal(t, ConversationMessageTypeNotice, msg.MessageType)
			return nil
		},
		getByIDFunc: func(_ context.Context, id int64) (*Conversation, error) {
			return &Conversation{ID: id, UserID: 1, Kind: ConversationKindSystemNotice, Status: ConversationStatusClosed}, nil
		},
	}
	svc := NewConversationService(repo, &conversationUserRepoStub{
		users: map[int64]*User{1: {ID: 1, Role: RoleUser, Status: StatusActive}},
	})

	out, err := svc.CreateSystemNoticeInternal(context.Background(), &CreateConversationInput{
		UserID:   1,
		Subject:  "账号变动提醒",
		Content:  "你的账号已更新。",
		Source:   "account",
		SourceID: "42:created",
		ActorID:  0,
	})

	require.NoError(t, err)
	require.Equal(t, int64(11), out.ID)
}

func TestConversationCreateSystemNoticeInternalDedupesBySource(t *testing.T) {
	repo := &conversationRepoStub{
		getNoticeBySourceFunc: func(_ context.Context, userID int64, source, sourceID string) (*Conversation, error) {
			require.Equal(t, int64(1), userID)
			require.Equal(t, "payment_order", source)
			require.Equal(t, "99:completed", sourceID)
			return &Conversation{ID: 88, UserID: userID, Kind: ConversationKindSystemNotice}, nil
		},
		createWithMessageFunc: func(context.Context, *Conversation, *ConversationMessage) error {
			t.Fatal("duplicate system notice must not be created")
			return nil
		},
	}
	svc := NewConversationService(repo, &conversationUserRepoStub{
		users: map[int64]*User{1: {ID: 1, Role: RoleUser, Status: StatusActive}},
	})

	_, err := svc.CreateSystemNoticeInternal(context.Background(), &CreateConversationInput{
		UserID:   1,
		Subject:  "订单完成",
		Content:  "订单已完成。",
		Source:   "payment_order",
		SourceID: "99:completed",
	})

	require.ErrorIs(t, err, ErrConversationDuplicateSource)
}

func TestConversationAddUserMessageRejectsOtherUserConversation(t *testing.T) {
	repoErr := errors.New("owner mismatch")
	repo := &conversationRepoStub{
		getByIDForUserFunc: func(context.Context, int64, int64) (*Conversation, error) {
			return nil, repoErr
		},
	}
	svc := NewConversationService(repo, &conversationUserRepoStub{})

	_, err := svc.AddUserMessage(context.Background(), &AddConversationMessageInput{
		ConversationID: 10,
		ActorID:        1,
		Content:        "hello",
	})

	require.ErrorIs(t, err, repoErr)
}

func TestConversationUserScopedOperationsRejectOtherUserConversation(t *testing.T) {
	repoErr := errors.New("owner mismatch")
	repo := &conversationRepoStub{
		getByIDForUserFunc: func(_ context.Context, userID, id int64) (*Conversation, error) {
			require.Equal(t, int64(1), userID)
			require.Equal(t, int64(10), id)
			return nil, repoErr
		},
		listMessagesFunc: func(context.Context, int64, pagination.PaginationParams) ([]ConversationMessage, *pagination.PaginationResult, error) {
			t.Fatal("ListMessages must not be called before user ownership is verified")
			return nil, nil, nil
		},
		markReadFunc: func(context.Context, int64, string, *int64) (*Conversation, error) {
			t.Fatal("MarkRead must not be called before user ownership is verified")
			return nil, nil
		},
		updateStatusFunc: func(context.Context, int64, string) (*Conversation, error) {
			t.Fatal("UpdateStatus must not be called before user ownership is verified")
			return nil, nil
		},
	}
	svc := NewConversationService(repo, &conversationUserRepoStub{})

	_, _, err := svc.ListMessagesForUser(context.Background(), 1, 10, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.ErrorIs(t, err, repoErr)

	_, err = svc.MarkReadForUser(context.Background(), 1, 10, nil)
	require.ErrorIs(t, err, repoErr)

	_, err = svc.CloseForUser(context.Background(), 1, 10)
	require.ErrorIs(t, err, repoErr)
}

func TestConversationUserScopedOperationsRequirePositiveUserID(t *testing.T) {
	repo := &conversationRepoStub{
		listForUserFunc: func(context.Context, int64, pagination.PaginationParams, ConversationListFilters) ([]Conversation, *pagination.PaginationResult, error) {
			t.Fatal("ListForUser must not be called with invalid user id")
			return nil, nil, nil
		},
		getByIDForUserFunc: func(context.Context, int64, int64) (*Conversation, error) {
			t.Fatal("GetByIDForUser must not be called with invalid user id")
			return nil, nil
		},
		countUnreadForUserFunc: func(context.Context, int64) (int64, error) {
			t.Fatal("CountUnreadForUser must not be called with invalid user id")
			return 0, nil
		},
	}
	svc := NewConversationService(repo, &conversationUserRepoStub{})

	_, _, err := svc.ListForUser(context.Background(), 0, pagination.PaginationParams{Page: 1, PageSize: 20}, ConversationListFilters{})
	require.ErrorIs(t, err, ErrConversationInputRequired)

	_, err = svc.GetForUser(context.Background(), 0, 10)
	require.ErrorIs(t, err, ErrConversationInputRequired)

	_, _, err = svc.ListMessagesForUser(context.Background(), 0, 10, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.ErrorIs(t, err, ErrConversationInputRequired)

	_, err = svc.MarkReadForUser(context.Background(), 0, 10, nil)
	require.ErrorIs(t, err, ErrConversationInputRequired)

	_, err = svc.CloseForUser(context.Background(), 0, 10)
	require.ErrorIs(t, err, ErrConversationInputRequired)

	_, err = svc.CountUnreadForUser(context.Background(), 0)
	require.ErrorIs(t, err, ErrConversationInputRequired)
}

func TestConversationMarkReadForUserPassesReadUntilMessageID(t *testing.T) {
	readUntilMessageID := int64(88)
	repo := &conversationRepoStub{
		getByIDForUserFunc: func(_ context.Context, userID, id int64) (*Conversation, error) {
			require.Equal(t, int64(1), userID)
			require.Equal(t, int64(10), id)
			return &Conversation{ID: id, UserID: userID, Kind: ConversationKindTicket, Status: ConversationStatusOpen}, nil
		},
		markReadFunc: func(_ context.Context, conversationID int64, readerType string, readUntil *int64) (*Conversation, error) {
			require.Equal(t, int64(10), conversationID)
			require.Equal(t, ConversationSenderTypeUser, readerType)
			require.NotNil(t, readUntil)
			require.Equal(t, readUntilMessageID, *readUntil)
			return &Conversation{ID: conversationID, Kind: ConversationKindTicket}, nil
		},
	}
	svc := NewConversationService(repo, &conversationUserRepoStub{})

	_, err := svc.MarkReadForUser(context.Background(), 1, 10, &readUntilMessageID)

	require.NoError(t, err)
}

func TestConversationSupportThreadRemainsWritableAfterNoticeMessages(t *testing.T) {
	repo := &conversationRepoStub{
		getByIDForUserFunc: func(context.Context, int64, int64) (*Conversation, error) {
			return &Conversation{ID: 10, UserID: 1, Kind: ConversationKindTicket, Status: ConversationStatusOpen}, nil
		},
		getByIDFunc: func(context.Context, int64) (*Conversation, error) {
			return &Conversation{ID: 10, UserID: 1, Kind: ConversationKindTicket, Status: ConversationStatusOpen}, nil
		},
		addMessageFunc: func(_ context.Context, conversationID int64, _ *ConversationMessage, nextStatus string) (*Conversation, error) {
			return &Conversation{ID: conversationID, Kind: ConversationKindTicket, Status: nextStatus}, nil
		},
		updateStatusFunc: func(_ context.Context, conversationID int64, status string) (*Conversation, error) {
			return &Conversation{ID: conversationID, Kind: ConversationKindTicket, Status: status}, nil
		},
		updateAssigneeFunc: func(_ context.Context, conversationID int64, adminID *int64) (*Conversation, error) {
			return &Conversation{ID: conversationID, Kind: ConversationKindTicket, AssignedAdminID: adminID}, nil
		},
	}
	svc := NewConversationService(repo, &conversationUserRepoStub{
		users: map[int64]*User{2: {ID: 2, Role: RoleAdmin, Status: StatusActive}},
	})

	out, err := svc.AddUserMessage(context.Background(), &AddConversationMessageInput{ConversationID: 10, ActorID: 1, Content: "reply"})
	require.NoError(t, err)
	require.Equal(t, ConversationStatusPendingAdmin, out.Status)

	out, err = svc.AddAdminMessage(context.Background(), &AddConversationMessageInput{ConversationID: 10, ActorID: 2, Content: "reply"})
	require.NoError(t, err)
	require.Equal(t, ConversationStatusPendingUser, out.Status)

	out, err = svc.UpdateStatusAdmin(context.Background(), 10, ConversationStatusResolved)
	require.NoError(t, err)
	require.Equal(t, ConversationStatusResolved, out.Status)

	adminID := int64(2)
	out, err = svc.UpdateAssigneeAdmin(context.Background(), 10, &adminID)
	require.NoError(t, err)
	require.Equal(t, adminID, *out.AssignedAdminID)

	out, err = svc.CloseForUser(context.Background(), 1, 10)
	require.NoError(t, err)
	require.Equal(t, ConversationStatusClosed, out.Status)
}

func TestConversationCloseForUserUsesOwnerScope(t *testing.T) {
	repo := &conversationRepoStub{
		getByIDForUserFunc: func(_ context.Context, userID, id int64) (*Conversation, error) {
			require.Equal(t, int64(1), userID)
			require.Equal(t, int64(10), id)
			return &Conversation{ID: id, UserID: userID, Kind: ConversationKindTicket, Status: ConversationStatusOpen}, nil
		},
		updateStatusFunc: func(_ context.Context, conversationID int64, status string) (*Conversation, error) {
			require.Equal(t, int64(10), conversationID)
			require.Equal(t, ConversationStatusClosed, status)
			return &Conversation{ID: conversationID, Status: status, Kind: ConversationKindTicket}, nil
		},
	}
	svc := NewConversationService(repo, &conversationUserRepoStub{})

	out, err := svc.CloseForUser(context.Background(), 1, 10)

	require.NoError(t, err)
	require.Equal(t, ConversationStatusClosed, out.Status)
}

func TestConversationUpdateAssigneeRequiresAdminUser(t *testing.T) {
	repo := &conversationRepoStub{}
	svc := NewConversationService(repo, &conversationUserRepoStub{
		users: map[int64]*User{
			2: {ID: 2, Role: RoleUser, Status: StatusActive},
		},
	})
	assigneeID := int64(2)

	_, err := svc.UpdateAssigneeAdmin(context.Background(), 10, &assigneeID)

	require.ErrorIs(t, err, ErrConversationAssigneeInvalid)
}
