package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSystemNoticeSendKeepsBusinessTypeAndRedactsSensitiveContent(t *testing.T) {
	var gotInput *CreateConversationInput
	repo := &conversationRepoStub{
		createWithMessageFunc: func(_ context.Context, conv *Conversation, msg *ConversationMessage) error {
			gotInput = &CreateConversationInput{
				UserID:        conv.UserID,
				Subject:       conv.Subject,
				Content:       msg.Content,
				Kind:          conv.Kind,
				Type:          conv.Type,
				Source:        conv.Source,
				SourceID:      conv.SourceID,
				ContentFormat: msg.ContentFormat,
				ActorType:     msg.SenderType,
			}
			conv.ID = 9
			return nil
		},
	}
	svc := NewSystemNoticeService(NewConversationService(repo, &conversationUserRepoStub{
		users: map[int64]*User{7: {ID: 7, Status: StatusActive}},
	}))

	out, err := svc.Send(context.Background(), SystemNoticeInput{
		UserID:   7,
		Subject:  "支付提醒",
		Content:  "payment_trade_no=abc access_token=secret sk-test password=123",
		Type:     ConversationTypeBilling,
		Source:   SystemNoticeSourcePaymentOrder,
		SourceID: "19:paid",
	})

	require.NoError(t, err)
	require.NotNil(t, out)
	require.NotNil(t, gotInput)
	require.Equal(t, ConversationKindSystemNotice, gotInput.Kind)
	require.Equal(t, ConversationTypeBilling, gotInput.Type)
	require.Equal(t, ConversationSenderTypeSystem, gotInput.ActorType)
	require.Equal(t, ConversationContentFormatPlain, gotInput.ContentFormat)
	require.NotContains(t, gotInput.Content, "access_token")
	require.NotContains(t, gotInput.Content, "payment_trade_no")
	require.NotContains(t, gotInput.Content, "password")
	require.NotContains(t, gotInput.Content, "sk-test")
}

func TestSystemNoticeSendBestEffortIgnoresDuplicateSource(t *testing.T) {
	repo := &conversationRepoStub{
		getNoticeBySourceFunc: func(context.Context, int64, string, string) (*Conversation, error) {
			return &Conversation{ID: 88, UserID: 7, Kind: ConversationKindSystemNotice}, nil
		},
		createWithMessageFunc: func(context.Context, *Conversation, *ConversationMessage) error {
			t.Fatal("duplicate source must not create another notice")
			return nil
		},
	}
	svc := NewSystemNoticeService(NewConversationService(repo, &conversationUserRepoStub{
		users: map[int64]*User{7: {ID: 7, Status: StatusActive}},
	}))

	svc.SendBestEffort(context.Background(), SystemNoticeInput{
		UserID:   7,
		Subject:  "订阅提醒",
		Content:  "订阅已开通",
		Type:     ConversationTypeSubscription,
		Source:   SystemNoticeSourceSubscription,
		SourceID: "5:created",
	})
}

func TestSystemNoticeAccountChangedOnlyNotifiesOwnerAndTrackedFields(t *testing.T) {
	created := make([]*Conversation, 0)
	repo := &conversationRepoStub{
		createWithMessageFunc: func(_ context.Context, conv *Conversation, _ *ConversationMessage) error {
			cp := *conv
			cp.ID = int64(len(created) + 1)
			created = append(created, &cp)
			return nil
		},
		getNoticeBySourceFunc: func(context.Context, int64, string, string) (*Conversation, error) {
			return nil, ErrConversationNotFound
		},
	}
	svc := NewSystemNoticeService(NewConversationService(repo, &conversationUserRepoStub{
		users: map[int64]*User{12: {ID: 12, Status: StatusActive}},
	}))
	ownerID := int64(12)

	svc.NotifyAccountChanged(context.Background(), &Account{
		ID:           3,
		Name:         "main",
		AccountLevel: AccountLevelFree,
		OwnerUserID:  &ownerID,
		GroupIDs:     []int64{1},
	}, &Account{
		ID:           3,
		Name:         "main changed",
		AccountLevel: AccountLevelPlus,
		OwnerUserID:  &ownerID,
		GroupIDs:     []int64{1, 2},
	})

	require.Len(t, created, 2)
	require.Equal(t, int64(12), created[0].UserID)
	require.Equal(t, ConversationTypeAccount, created[0].Type)
	require.Equal(t, SystemNoticeSourceAccount, created[0].Source)

	svc.NotifyAccountCreated(context.Background(), &Account{ID: 4, Name: "admin pool"})
	require.Len(t, created, 2)
}

func TestSystemNoticeAccountOwnerTransferNotifiesOldAndNewOwnersOnly(t *testing.T) {
	created := make([]*Conversation, 0)
	repo := &conversationRepoStub{
		createWithMessageFunc: func(_ context.Context, conv *Conversation, _ *ConversationMessage) error {
			cp := *conv
			cp.ID = int64(len(created) + 1)
			created = append(created, &cp)
			return nil
		},
		getNoticeBySourceFunc: func(context.Context, int64, string, string) (*Conversation, error) {
			return nil, ErrConversationNotFound
		},
	}
	svc := NewSystemNoticeService(NewConversationService(repo, &conversationUserRepoStub{
		users: map[int64]*User{
			12: {ID: 12, Status: StatusActive},
			13: {ID: 13, Status: StatusActive},
		},
	}))
	beforeOwnerID := int64(12)
	afterOwnerID := int64(13)
	updatedAt := time.Unix(1700000000, 0)

	svc.NotifyAccountChanged(context.Background(), &Account{
		ID:           3,
		Name:         "main",
		AccountLevel: AccountLevelFree,
		OwnerUserID:  &beforeOwnerID,
		GroupIDs:     []int64{1},
		UpdatedAt:    updatedAt.Add(-time.Minute),
	}, &Account{
		ID:           3,
		Name:         "main",
		AccountLevel: AccountLevelPlus,
		OwnerUserID:  &afterOwnerID,
		GroupIDs:     []int64{2},
		UpdatedAt:    updatedAt,
	})

	require.Len(t, created, 2)
	require.Equal(t, int64(12), created[0].UserID)
	require.Equal(t, "3_owner_removed_1700000000000000000", created[0].SourceID)
	require.Equal(t, int64(13), created[1].UserID)
	require.Equal(t, "3_owner_assigned_1700000000000000000", created[1].SourceID)
	for _, conv := range created {
		require.NotContains(t, conv.SourceID, "12")
		require.NotContains(t, conv.SourceID, "13")
	}
}

func TestSystemNoticeGroupRateNoticesDoNotExposeInternalIDsToUserContent(t *testing.T) {
	created := make([]*Conversation, 0)
	messages := make([]*ConversationMessage, 0)
	repo := &conversationRepoStub{
		createWithMessageFunc: func(_ context.Context, conv *Conversation, msg *ConversationMessage) error {
			cp := *conv
			cp.ID = int64(len(created) + 1)
			created = append(created, &cp)
			msgCp := *msg
			messages = append(messages, &msgCp)
			return nil
		},
		getNoticeBySourceFunc: func(context.Context, int64, string, string) (*Conversation, error) {
			return nil, ErrConversationNotFound
		},
	}
	svc := NewSystemNoticeService(NewConversationService(repo, &conversationUserRepoStub{
		users: map[int64]*User{
			11: {ID: 11, Status: StatusActive},
			22: {ID: 22, Status: StatusActive},
		},
	}))

	svc.NotifyGroupRateMultiplierChanged(context.Background(), []int64{11, 11, 22}, &Group{
		ID:             77,
		Name:           "VIP",
		RateMultiplier: 2.0,
		UpdatedAt:      time.Unix(1700000000, 0),
	}, 1.0, 2.0, "rate_changed")

	require.Len(t, created, 2)
	require.Len(t, messages, 2)
	require.ElementsMatch(t, []int64{11, 22}, []int64{created[0].UserID, created[1].UserID})
	for _, msg := range messages {
		require.Contains(t, msg.Content, "VIP")
		require.NotContains(t, msg.Content, "77")
		require.NotContains(t, msg.Content, "user_id")
	}
}

func TestSystemNoticeSendReturnsDuplicateSource(t *testing.T) {
	repo := &conversationRepoStub{
		getNoticeBySourceFunc: func(context.Context, int64, string, string) (*Conversation, error) {
			return &Conversation{ID: 88, UserID: 7, Kind: ConversationKindSystemNotice}, nil
		},
	}
	svc := NewSystemNoticeService(NewConversationService(repo, &conversationUserRepoStub{
		users: map[int64]*User{7: {ID: 7, Status: StatusActive}},
	}))

	_, err := svc.Send(context.Background(), SystemNoticeInput{
		UserID:   7,
		Subject:  "订阅提醒",
		Content:  "订阅已开通",
		Source:   SystemNoticeSourceSubscription,
		SourceID: "5:created",
	})

	require.True(t, errors.Is(err, ErrConversationDuplicateSource))
}
