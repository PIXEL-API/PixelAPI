package schema

import (
	"time"

	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Conversation holds the schema definition for a user-admin communication thread.
type Conversation struct {
	ent.Schema
}

func (Conversation) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "conversations"},
	}
}

func (Conversation) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (Conversation) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id").
			Comment("会话所属用户 ID"),
		field.String("subject").
			MaxLen(200).
			NotEmpty().
			Comment("会话主题"),
		field.String("status").
			MaxLen(30).
			Default(domain.ConversationStatusPendingAdmin).
			Comment("状态: open, pending_user, pending_admin, resolved, closed"),
		field.String("kind").
			MaxLen(30).
			Default(domain.ConversationKindTicket).
			Comment("会话形态: ticket, system_notice"),
		field.String("priority").
			MaxLen(20).
			Default(domain.ConversationPriorityNormal).
			Comment("优先级: low, normal, high, urgent"),
		field.String("type").
			MaxLen(40).
			Default(domain.ConversationTypeSupport).
			Comment("类型: support, notice, billing, subscription, account, security"),
		field.String("source").
			MaxLen(80).
			Default("").
			Comment("来源模块，可为空"),
		field.String("source_id").
			MaxLen(120).
			Default("").
			Comment("来源业务对象 ID，可为空"),
		field.Int64("referenced_notice_id").
			Optional().
			Nillable().
			Comment("工单引用的系统通知 ID"),
		field.Int64("assigned_admin_id").
			Optional().
			Nillable().
			Comment("负责人管理员 ID"),
		field.Int64("last_message_id").
			Optional().
			Nillable().
			Comment("最后一条消息 ID"),
		field.String("last_message_sender_type").
			MaxLen(20).
			Default("").
			Comment("最后一条消息发送方"),
		field.String("last_message_excerpt").
			MaxLen(240).
			Default("").
			Comment("最后一条消息摘要"),
		field.Time("last_message_at").
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("最后消息时间"),
		field.Int64("user_last_read_message_id").
			Optional().
			Nillable().
			Comment("用户最后已读消息 ID"),
		field.Time("user_last_read_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("用户最后已读时间"),
		field.Int64("admin_last_read_message_id").
			Optional().
			Nillable().
			Comment("管理员侧最后已读消息 ID"),
		field.Time("admin_last_read_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("管理员侧最后已读时间"),
	}
}

func (Conversation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("conversations").
			Field("user_id").
			Unique().
			Required(),
		edge.From("assigned_admin", User.Type).
			Ref("assigned_conversations").
			Field("assigned_admin_id").
			Unique(),
		edge.To("referenced_by_conversations", Conversation.Type).
			From("referenced_notice").
			Field("referenced_notice_id").
			Unique(),
		edge.To("messages", ConversationMessage.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Conversation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "last_message_at"),
		index.Fields("status", "last_message_at"),
		index.Fields("kind", "last_message_at"),
		index.Fields("assigned_admin_id", "status"),
		index.Fields("source", "source_id"),
		index.Fields("referenced_notice_id"),
	}
}
