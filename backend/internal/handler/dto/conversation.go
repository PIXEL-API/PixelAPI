package dto

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type Conversation struct {
	ID                     int64                 `json:"id"`
	UserID                 int64                 `json:"user_id"`
	UserEmail              string                `json:"user_email,omitempty"`
	UserName               string                `json:"user_name,omitempty"`
	Subject                string                `json:"subject"`
	Status                 string                `json:"status"`
	Kind                   string                `json:"kind"`
	Priority               string                `json:"priority"`
	Type                   string                `json:"type"`
	Source                 string                `json:"source,omitempty"`
	SourceID               string                `json:"source_id,omitempty"`
	ReferencedNoticeID     *int64                `json:"referenced_notice_id,omitempty"`
	AssignedAdminID        *int64                `json:"assigned_admin_id,omitempty"`
	LastMessageID          *int64                `json:"last_message_id,omitempty"`
	LastMessageSenderType  string                `json:"last_message_sender_type"`
	LastMessageExcerpt     string                `json:"last_message_excerpt"`
	LastMessageAt          time.Time             `json:"last_message_at"`
	UserLastReadMessageID  *int64                `json:"user_last_read_message_id,omitempty"`
	UserLastReadAt         *time.Time            `json:"user_last_read_at,omitempty"`
	AdminLastReadMessageID *int64                `json:"admin_last_read_message_id,omitempty"`
	AdminLastReadAt        *time.Time            `json:"admin_last_read_at,omitempty"`
	UserUnread             bool                  `json:"user_unread"`
	AdminUnread            bool                  `json:"admin_unread"`
	CreatedAt              time.Time             `json:"created_at"`
	UpdatedAt              time.Time             `json:"updated_at"`
	Messages               []ConversationMessage `json:"messages,omitempty"`
}

type ConversationMessage struct {
	ID             int64          `json:"id"`
	ConversationID int64          `json:"conversation_id"`
	SenderType     string         `json:"sender_type"`
	SenderID       *int64         `json:"sender_id,omitempty"`
	MessageType    string         `json:"message_type"`
	ContentFormat  string         `json:"content_format"`
	Content        string         `json:"content"`
	Metadata       map[string]any `json:"metadata"`
	CreatedAt      time.Time      `json:"created_at"`
}

type UserConversation struct {
	ID                    int64                     `json:"id"`
	UserID                int64                     `json:"user_id"`
	Subject               string                    `json:"subject"`
	Status                string                    `json:"status"`
	Kind                  string                    `json:"kind"`
	Priority              string                    `json:"priority"`
	Type                  string                    `json:"type"`
	ReferencedNoticeID    *int64                    `json:"referenced_notice_id,omitempty"`
	LastMessageSenderType string                    `json:"last_message_sender_type"`
	LastMessageExcerpt    string                    `json:"last_message_excerpt"`
	LastMessageAt         time.Time                 `json:"last_message_at"`
	UserUnread            bool                      `json:"user_unread"`
	CreatedAt             time.Time                 `json:"created_at"`
	UpdatedAt             time.Time                 `json:"updated_at"`
	Messages              []UserConversationMessage `json:"messages,omitempty"`
}

type UserConversationMessage struct {
	ID             int64     `json:"id"`
	ConversationID int64     `json:"conversation_id"`
	SenderType     string    `json:"sender_type"`
	MessageType    string    `json:"message_type"`
	ContentFormat  string    `json:"content_format"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}

func ConversationFromService(c *service.Conversation) *Conversation {
	if c == nil {
		return nil
	}
	return &Conversation{
		ID:                     c.ID,
		UserID:                 c.UserID,
		UserEmail:              c.UserEmail,
		UserName:               c.UserName,
		Subject:                c.Subject,
		Status:                 c.Status,
		Kind:                   c.Kind,
		Priority:               c.Priority,
		Type:                   c.Type,
		Source:                 c.Source,
		SourceID:               c.SourceID,
		ReferencedNoticeID:     c.ReferencedNoticeID,
		AssignedAdminID:        c.AssignedAdminID,
		LastMessageID:          c.LastMessageID,
		LastMessageSenderType:  c.LastMessageSenderType,
		LastMessageExcerpt:     c.LastMessageExcerpt,
		LastMessageAt:          c.LastMessageAt,
		UserLastReadMessageID:  c.UserLastReadMessageID,
		UserLastReadAt:         c.UserLastReadAt,
		AdminLastReadMessageID: c.AdminLastReadMessageID,
		AdminLastReadAt:        c.AdminLastReadAt,
		UserUnread:             c.UserUnread,
		AdminUnread:            c.AdminUnread,
		CreatedAt:              c.CreatedAt,
		UpdatedAt:              c.UpdatedAt,
		Messages:               ConversationMessagesFromService(c.Messages),
	}
}

func ConversationsFromService(items []service.Conversation) []Conversation {
	out := make([]Conversation, 0, len(items))
	for i := range items {
		if item := ConversationFromService(&items[i]); item != nil {
			out = append(out, *item)
		}
	}
	return out
}

func UserConversationFromService(c *service.Conversation) *UserConversation {
	if c == nil {
		return nil
	}
	return &UserConversation{
		ID:                    c.ID,
		UserID:                c.UserID,
		Subject:               c.Subject,
		Status:                c.Status,
		Kind:                  c.Kind,
		Priority:              c.Priority,
		Type:                  c.Type,
		ReferencedNoticeID:    c.ReferencedNoticeID,
		LastMessageSenderType: c.LastMessageSenderType,
		LastMessageExcerpt:    c.LastMessageExcerpt,
		LastMessageAt:         c.LastMessageAt,
		UserUnread:            c.UserUnread,
		CreatedAt:             c.CreatedAt,
		UpdatedAt:             c.UpdatedAt,
		Messages:              UserConversationMessagesFromService(c.Messages),
	}
}

func UserConversationsFromService(items []service.Conversation) []UserConversation {
	out := make([]UserConversation, 0, len(items))
	for i := range items {
		if item := UserConversationFromService(&items[i]); item != nil {
			out = append(out, *item)
		}
	}
	return out
}

func ConversationMessageFromService(m *service.ConversationMessage) *ConversationMessage {
	if m == nil {
		return nil
	}
	return &ConversationMessage{
		ID:             m.ID,
		ConversationID: m.ConversationID,
		SenderType:     m.SenderType,
		SenderID:       m.SenderID,
		MessageType:    m.MessageType,
		ContentFormat:  m.ContentFormat,
		Content:        m.Content,
		Metadata:       m.Metadata,
		CreatedAt:      m.CreatedAt,
	}
}

func UserConversationMessageFromService(m *service.ConversationMessage) *UserConversationMessage {
	if m == nil {
		return nil
	}
	return &UserConversationMessage{
		ID:             m.ID,
		ConversationID: m.ConversationID,
		SenderType:     m.SenderType,
		MessageType:    m.MessageType,
		ContentFormat:  m.ContentFormat,
		Content:        m.Content,
		CreatedAt:      m.CreatedAt,
	}
}

func UserConversationMessagesFromService(items []service.ConversationMessage) []UserConversationMessage {
	out := make([]UserConversationMessage, 0, len(items))
	for i := range items {
		if item := UserConversationMessageFromService(&items[i]); item != nil {
			out = append(out, *item)
		}
	}
	return out
}

func ConversationMessagesFromService(items []service.ConversationMessage) []ConversationMessage {
	out := make([]ConversationMessage, 0, len(items))
	for i := range items {
		if item := ConversationMessageFromService(&items[i]); item != nil {
			out = append(out, *item)
		}
	}
	return out
}
