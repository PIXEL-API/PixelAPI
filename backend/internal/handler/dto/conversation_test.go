package dto

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserConversationFromServiceOmitsInternalFields(t *testing.T) {
	adminID := int64(2)
	readID := int64(7)
	now := time.Now()
	conv := &service.Conversation{
		ID:                     10,
		UserID:                 1,
		UserEmail:              "user@example.com",
		UserName:               "alice",
		Subject:                "subject",
		Status:                 service.ConversationStatusPendingUser,
		Kind:                   service.ConversationKindTicket,
		Priority:               service.ConversationPriorityNormal,
		Type:                   service.ConversationTypeSupport,
		Source:                 "billing",
		SourceID:               "internal-42",
		AssignedAdminID:        &adminID,
		LastMessageID:          &readID,
		LastMessageSenderType:  service.ConversationSenderTypeAdmin,
		LastMessageExcerpt:     "hello",
		LastMessageAt:          now,
		UserLastReadMessageID:  &readID,
		UserLastReadAt:         &now,
		AdminLastReadMessageID: &readID,
		AdminLastReadAt:        &now,
		UserUnread:             true,
		AdminUnread:            true,
		CreatedAt:              now,
		UpdatedAt:              now,
		Messages: []service.ConversationMessage{
			{
				ID:             20,
				ConversationID: 10,
				SenderType:     service.ConversationSenderTypeAdmin,
				SenderID:       &adminID,
				MessageType:    service.ConversationMessageTypeText,
				ContentFormat:  service.ConversationContentFormatPlain,
				Content:        "hello",
				Metadata:       map[string]any{"admin_note_id": "secret"},
				CreatedAt:      now,
			},
		},
	}

	body, err := json.Marshal(UserConversationFromService(conv))
	require.NoError(t, err)
	jsonBody := string(body)

	for _, field := range []string{
		"user_email",
		"user_name",
		"source",
		"source_id",
		"assigned_admin_id",
		"last_message_id",
		"user_last_read_message_id",
		"user_last_read_at",
		"admin_last_read_message_id",
		"admin_last_read_at",
		"admin_unread",
		"sender_id",
		"metadata",
		"admin_note_id",
	} {
		require.False(t, strings.Contains(jsonBody, field), "user conversation response leaked %s", field)
	}
	require.Contains(t, jsonBody, `"user_unread":true`)
	require.Contains(t, jsonBody, `"content":"hello"`)
}
