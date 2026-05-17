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

// ShopProduct holds the schema definition for store products.
type ShopProduct struct {
	ent.Schema
}

func (ShopProduct) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "shop_products"},
	}
}

func (ShopProduct) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (ShopProduct) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("category_id").
			Optional().
			Nillable(),
		field.String("name").
			MaxLen(150).
			NotEmpty(),
		field.String("cover_url").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("description").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Float("price").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,2)"}).
			Default(0),
		field.Float("original_price").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,2)"}),
		field.Bool("enabled").
			Default(true),
		field.Int("sort_order").
			Default(0),
		field.Int("min_purchase").
			Default(1),
		field.Int("max_purchase").
			Default(1),
		field.Bool("auto_delivery").
			Default(true),
	}
}

func (ShopProduct) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("category", ShopCategory.Type).
			Ref("products").
			Field("category_id").
			Unique(),
		edge.To("card_keys", ShopCardKey.Type),
		edge.To("orders", ShopOrder.Type),
	}
}

func (ShopProduct) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("category_id"),
		index.Fields("enabled"),
		index.Fields("sort_order"),
	}
}
