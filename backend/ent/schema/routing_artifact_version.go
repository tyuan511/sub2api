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

// RoutingArtifactVersion registers immutable strategy, score, feature, and
// model objects. Lifecycle columns may change; content-addressed fields may not
// be updated after publication by the service layer.
type RoutingArtifactVersion struct{ ent.Schema }

func (RoutingArtifactVersion) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "routing_artifact_versions"}}
}

func (RoutingArtifactVersion) Fields() []ent.Field {
	return []ent.Field{
		field.String("artifact_kind").MaxLen(24).NotEmpty(),
		field.String("version").MaxLen(96).NotEmpty(),
		field.String("parent_version").MaxLen(96).Optional().Nillable(),
		field.String("platform").MaxLen(32).NotEmpty(),
		field.String("model_family").MaxLen(96).NotEmpty(),
		field.String("endpoint_kind").MaxLen(32).NotEmpty(),
		field.String("preference").MaxLen(16).Optional().Nillable(),
		field.String("status").MaxLen(16).Default("draft"),
		field.String("schema_version").MaxLen(64).NotEmpty(),
		field.String("checksum").MaxLen(128).NotEmpty(),
		field.JSON("payload", json.RawMessage{}).Optional().SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("dependencies", json.RawMessage{}).Optional().SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("lineage", json.RawMessage{}).Optional().SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Int64("created_by").Optional().Nillable(),
		field.Time("activated_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("retired_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (RoutingArtifactVersion) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("creator", User.Type).Ref("routing_artifact_versions").Field("created_by").Unique(),
	}
}

func (RoutingArtifactVersion) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("artifact_kind", "version").Unique(),
		index.Fields("platform", "model_family", "endpoint_kind", "artifact_kind", "status"),
		index.Fields("created_at", "id"),
	}
}
