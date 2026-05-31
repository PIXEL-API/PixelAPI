package domain

const (
	ConversationStatusOpen         = "open"
	ConversationStatusPendingUser  = "pending_user"
	ConversationStatusPendingAdmin = "pending_admin"
	ConversationStatusResolved     = "resolved"
	ConversationStatusClosed       = "closed"
)

const (
	ConversationKindTicket       = "ticket"
	ConversationKindSystemNotice = "system_notice"
)

const (
	ConversationPriorityLow    = "low"
	ConversationPriorityNormal = "normal"
	ConversationPriorityHigh   = "high"
	ConversationPriorityUrgent = "urgent"
)

const (
	ConversationTypeSupport      = "support"
	ConversationTypeNotice       = "notice"
	ConversationTypeBilling      = "billing"
	ConversationTypeSubscription = "subscription"
	ConversationTypeAccount      = "account"
	ConversationTypeSecurity     = "security"
)

const (
	ConversationSenderTypeUser   = "user"
	ConversationSenderTypeAdmin  = "admin"
	ConversationSenderTypeSystem = "system"
)

const (
	ConversationMessageTypeText         = "text"
	ConversationMessageTypeNotice       = "notice"
	ConversationMessageTypeOperationLog = "operation_log"
	ConversationMessageTypeSystemEvent  = "system_event"
)

const (
	ConversationContentFormatPlain    = "plain"
	ConversationContentFormatMarkdown = "markdown"
)
