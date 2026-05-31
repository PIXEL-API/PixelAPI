package schema

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ConversationMessage holds the schema definition for messages inside a conversation.
type ConversationMessage struct {
	ent.Schema
}

func (ConversationMessage) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "conversation_messages"},
	}
}

func (ConversationMessage) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("conversation_id").
			Comment("会话 ID"),
		field.String("sender_type").
			MaxLen(20).
			Comment("发送方: user, admin, system"),
		field.Int64("sender_id").
			Optional().
			Nillable().
			Comment("发送方用户 ID，系统消息为空"),
		field.String("message_type").
			MaxLen(30).
			Default(domain.ConversationMessageTypeText).
			Comment("消息类型: text, notice, operation_log, system_event"),
		field.String("content_format").
			MaxLen(20).
			Default(domain.ConversationContentFormatPlain).
			Comment("内容格式: plain, markdown"),
		field.String("content").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			NotEmpty().
			Comment("消息内容"),
		field.JSON("metadata", map[string]any{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}).
			Comment("消息元数据"),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (ConversationMessage) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("conversation", Conversation.Type).
			Ref("messages").
			Field("conversation_id").
			Unique().
			Required(),
		edge.From("sender", User.Type).
			Ref("sent_conversation_messages").
			Field("sender_id").
			Unique(),
	}
}

func (ConversationMessage) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("conversation_id", "id"),
		index.Fields("conversation_id", "created_at"),
		index.Fields("sender_type", "sender_id"),
	}
}
