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

// SupportThread holds the schema definition for a user's single support thread.
type SupportThread struct {
	ent.Schema
}

func (SupportThread) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "support_threads"},
	}
}

func (SupportThread) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (SupportThread) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id").
			Comment("工单服务所属用户 ID"),
		field.String("subject").
			MaxLen(200).
			Default("工单服务").
			Comment("线程展示标题"),
		field.String("status").
			MaxLen(30).
			Default(domain.ConversationStatusOpen).
			Comment("当前人工工单状态: open, pending_user, pending_admin, resolved, closed"),
		field.String("priority").
			MaxLen(20).
			Default(domain.ConversationPriorityNormal).
			Comment("当前人工工单优先级: low, normal, high, urgent"),
		field.String("type").
			MaxLen(40).
			Default(domain.ConversationTypeSupport).
			Comment("当前人工工单类型: support, notice, billing, subscription, account, security"),
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

func (SupportThread) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("support_threads").
			Field("user_id").
			Unique().
			Required(),
		edge.From("assigned_admin", User.Type).
			Ref("assigned_support_threads").
			Field("assigned_admin_id").
			Unique(),
		edge.To("messages", SupportMessage.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (SupportThread) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").Unique(),
		index.Fields("status", "last_message_at"),
		index.Fields("assigned_admin_id", "status"),
		index.Fields("last_message_at"),
	}
}
