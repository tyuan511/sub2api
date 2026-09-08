package schema

import (
	"encoding/json"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RoutingAttempt is a bounded point-in-time decision/outcome fact. Candidate
// JSON is validated before insertion and can never contain request content or
// credentials.
type RoutingAttempt struct{ ent.Schema }

func (RoutingAttempt) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "routing_attempts"}}
}

func (RoutingAttempt) Fields() []ent.Field {
	return []ent.Field{
		field.String("event_id").MaxLen(64).NotEmpty().Unique(),
		field.String("routing_decision_id").MaxLen(64).NotEmpty(),
		field.String("request_id").MaxLen(64).Optional().Nillable(),
		field.Int64("api_key_id").Optional().Nillable(),
		field.Int64("route_version").Positive(),
		field.Int64("initial_group_id").Optional().Nillable(),
		field.Int64("attempted_group_id").Optional().Nillable(),
		field.Int64("effective_group_id").Optional().Nillable(),
		field.Int64("selected_group_id").Optional().Nillable(),
		field.String("schedule_mode").MaxLen(16).NotEmpty(),
		field.String("smart_preference").MaxLen(16).Optional().Nillable(),
		field.Int("smart_balance_bps").Optional().Nillable().Min(0).Max(10000),
		field.Int("routing_min_success_rate").Default(50).Min(50).Max(95),
		field.Int64("routing_state_version").Optional().Nillable().Positive(),
		field.Int("attempt_index").Default(0).Min(0).Max(7),
		field.String("platform").MaxLen(32).NotEmpty(),
		field.String("model_family").MaxLen(96).NotEmpty(),
		field.String("endpoint_kind").MaxLen(32).NotEmpty(),
		field.String("strategy_version").MaxLen(96).NotEmpty(),
		field.String("score_version").MaxLen(96).NotEmpty(),
		field.String("feature_schema_version").MaxLen(96).NotEmpty(),
		field.String("model_version").MaxLen(96).Optional().Nillable(),
		field.String("experiment_id").MaxLen(96).Optional().Nillable(),
		field.Int("experiment_bucket").Optional().Nillable().Min(0).Max(9999),
		field.Float("sample_probability").Default(1),
		field.Float("action_propensity").Optional().Nillable(),
		field.String("assignment_reason").MaxLen(32).Default("deterministic"),
		field.JSON("candidates", json.RawMessage{}).Optional().SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.String("selected_reason").MaxLen(64).Optional().Nillable(),
		field.String("outcome_visibility").MaxLen(16).Default("observed"),
		field.String("outcome_category").MaxLen(64).Optional().Nillable(),
		field.Bool("retryable").Default(false),
		field.Bool("semantic_output").Default(false),
		field.Bool("switched_group").Default(false),
		field.Bool("sticky_broken").Default(false),
		field.String("breaker_transition").MaxLen(32).Optional().Nillable(),
		field.Int("queue_ms").Optional().Nillable().Min(0),
		field.Int("ttft_ms").Optional().Nillable().Min(0),
		field.Int("duration_ms").Optional().Nillable().Min(0),
		field.JSON("actual_usage", json.RawMessage{}).Optional().SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("billable_usage", json.RawMessage{}).Optional().SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Float("actual_cost").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Float("billed_cost").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Bool("cache_cold_due_to_failover").Default(false),
		field.String("event_priority").MaxLen(16).Default("diagnostic"),
		field.Time("occurred_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (RoutingAttempt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("routing_decision_id", "attempt_index"),
		index.Fields("request_id"),
		index.Fields("api_key_id", "created_at"),
		index.Fields("effective_group_id", "created_at"),
		index.Fields("experiment_id", "created_at"),
	}
}
