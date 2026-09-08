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

type RoutingExperiment struct{ ent.Schema }

func (RoutingExperiment) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "routing_experiments"}}
}

func (RoutingExperiment) Fields() []ent.Field {
	return []ent.Field{
		field.String("experiment_key").MaxLen(96).NotEmpty().Unique(),
		field.String("platform").MaxLen(32).NotEmpty(),
		field.String("model_family").MaxLen(96).NotEmpty(),
		field.String("endpoint_kind").MaxLen(32).NotEmpty(),
		field.String("preference").MaxLen(16).NotEmpty(),
		field.String("baseline_strategy_version").MaxLen(96).NotEmpty(),
		field.String("candidate_strategy_version").MaxLen(96).NotEmpty(),
		field.String("status").MaxLen(16).Default("draft"),
		field.Int("allocation_bps").Default(0).Min(0).Max(10000),
		field.String("bucket_salt_checksum").MaxLen(128).NotEmpty(),
		field.JSON("guardrails", json.RawMessage{}).Optional().SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("offline_replay", json.RawMessage{}).Optional().SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("last_evaluation", json.RawMessage{}).Optional().SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Time("last_evaluated_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("started_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("stopped_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("stop_reason").MaxLen(512).Optional().Nillable(),
		field.Int64("approved_by").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (RoutingExperiment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("approver", User.Type).Ref("routing_experiments").Field("approved_by").Unique(),
	}
}

func (RoutingExperiment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("platform", "model_family", "endpoint_kind", "preference", "status"),
	}
}
