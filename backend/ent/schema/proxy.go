package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Proxy holds the schema definition for the Proxy entity.
type Proxy struct {
	ent.Schema
}

func (Proxy) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "proxies"},
	}
}

func (Proxy) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
		mixins.SoftDeleteMixin{},
	}
}

func (Proxy) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			MaxLen(100).
			NotEmpty(),
		field.String("protocol").
			MaxLen(20).
			NotEmpty(),
		field.String("host").
			MaxLen(255).
			NotEmpty(),
		field.Int("port"),
		field.String("username").
			MaxLen(100).
			Optional().
			Nillable(),
		field.String("password").
			MaxLen(100).
			Optional().
			Nillable(),
		field.Int64("owner_user_id").
			Optional().
			Nillable(),
		// platform 为空字符串表示通用代理（所有平台可用）。
		field.String("platform").
			MaxLen(32).
			Default(""),
		// required_account_level 为空字符串表示所有等级可用。
		field.String("required_account_level").
			MaxLen(20).
			Default(""),
		field.String("status").
			MaxLen(20).
			Default("active"),
		field.Int("max_accounts").
			Default(0),
		field.Time("expires_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("Proxy expiration time (NULL means never expires)."),
		field.String("fallback_mode").
			MaxLen(20).
			Default("none").
			Comment("Fallback target on expiry: none | proxy | direct."),
		field.Int64("backup_proxy_id").
			Optional().
			Nillable().
			Comment("Backup proxy id when fallback_mode=proxy (self-reference)."),
		field.Int("expiry_warn_days").
			Default(7).
			Comment("Days before expiry to flag as expiring-soon (per proxy)."),
	}
}

// Edges 定义代理实体的关联关系。
func (Proxy) Edges() []ent.Edge {
	return []ent.Edge{
		// accounts: 使用此代理的账户（反向边）
		edge.From("accounts", Account.Type).
			Ref("proxy"),
		edge.From("owner", User.Type).
			Ref("owned_proxies").
			Field("owner_user_id").
			Unique(),
		edge.From("backup_proxy", Proxy.Type).
			Ref("fallback_sources").
			Field("backup_proxy_id").
			Unique().
			Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("fallback_sources", Proxy.Type),
	}
}

func (Proxy) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
		index.Fields("owner_user_id"),
		index.Fields("platform", "required_account_level"),
		index.Fields("expires_at"),
		index.Fields("backup_proxy_id"),
		index.Fields("deleted_at"),
	}
}
