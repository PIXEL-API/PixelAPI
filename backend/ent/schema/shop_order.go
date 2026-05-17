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

// ShopOrder holds the schema definition for store orders.
type ShopOrder struct {
	ent.Schema
}

func (ShopOrder) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "shop_orders"},
	}
}

func (ShopOrder) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (ShopOrder) Fields() []ent.Field {
	return []ent.Field{
		field.String("order_no").
			MaxLen(64).
			NotEmpty().
			Unique(),
		field.Int64("user_id"),
		field.Int64("product_id"),
		field.String("product_name").
			MaxLen(150).
			NotEmpty(),
		field.String("product_cover_url").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("product_description").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Float("unit_price").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,2)"}),
		field.Int("quantity"),
		field.Float("total_amount").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,2)"}),
		field.String("payment_method").
			MaxLen(30),
		field.Int64("payment_order_id").
			Optional().
			Nillable(),
		field.String("status").
			MaxLen(30).
			Default("pending"),
		field.JSON("delivered_cards", []string{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Time("paid_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("completed_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("cancelled_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("failed_reason").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
	}
}

func (ShopOrder) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("shop_orders").
			Field("user_id").
			Unique().
			Required(),
		edge.From("product", ShopProduct.Type).
			Ref("orders").
			Field("product_id").
			Unique().
			Required(),
		edge.To("card_keys", ShopCardKey.Type),
	}
}

func (ShopOrder) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("product_id"),
		index.Fields("payment_order_id").Unique(),
		index.Fields("status"),
		index.Fields("created_at"),
	}
}
