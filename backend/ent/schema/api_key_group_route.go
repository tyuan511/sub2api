package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// APIKeyGroupRoute stores one ordered physical-group candidate for an API key.
type APIKeyGroupRoute struct {
	ent.Schema
}

func (APIKeyGroupRoute) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "api_key_group_routes"},
	}
}

func (APIKeyGroupRoute) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("api_key_id"),
		field.Int64("group_id"),
		field.Int("priority").
			Min(0),
		field.Bool("enabled").
			Default(true),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (APIKeyGroupRoute) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("api_key", APIKey.Type).
			Ref("group_routes").
			Field("api_key_id").
			Unique().
			Required(),
		edge.From("group", Group.Type).
			Ref("api_key_group_routes").
			Field("group_id").
			Unique().
			Required(),
	}
}

func (APIKeyGroupRoute) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("api_key_id", "group_id").Unique(),
		index.Fields("api_key_id", "priority").Unique(),
		index.Fields("group_id", "api_key_id"),
		index.Fields("api_key_id", "enabled", "priority"),
	}
}
