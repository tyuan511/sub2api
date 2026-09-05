package schema

import (
	"encoding/json"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// APIKeyRouteConfigOutbox stores reliable cache-invalidation events committed
// in the same transaction as an API key route configuration change.
type APIKeyRouteConfigOutbox struct {
	ent.Schema
}

func (APIKeyRouteConfigOutbox) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "api_key_route_config_outbox"}}
}

func (APIKeyRouteConfigOutbox) Fields() []ent.Field {
	return []ent.Field{
		field.String("event_key").MaxLen(160).NotEmpty().Unique(),
		field.Int64("api_key_id"),
		field.Int64("route_version").Positive(),
		field.String("event_type").MaxLen(64).Default("api_key_route_config_changed"),
		field.JSON("payload", json.RawMessage{}).
			Optional().
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Int("attempts").Default(0).Min(0),
		field.Time("available_at").Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("claimed_at").Optional().Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("claimed_by").MaxLen(128).Optional().Nillable(),
		field.Time("delivered_at").Optional().Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("last_error").MaxLen(512).Optional().Nillable(),
		field.Time("created_at").Immutable().Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (APIKeyRouteConfigOutbox) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("api_key", APIKey.Type).
			Ref("route_config_outbox_events").
			Field("api_key_id").
			Unique().
			Required(),
	}
}

func (APIKeyRouteConfigOutbox) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("available_at", "id").
			Annotations(entsql.IndexWhere("delivered_at IS NULL")),
		index.Fields("api_key_id", "created_at"),
	}
}
