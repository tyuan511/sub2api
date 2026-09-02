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

// AccountProxy stores an account-scoped proxy binding. A proxy can be shared
// by multiple accounts, so concurrency belongs to this binding rather than to
// the proxy itself.
type AccountProxy struct {
	ent.Schema
}

func (AccountProxy) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "account_proxies"},
	}
}

func (AccountProxy) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id").Unique().Immutable(),
		field.Int64("account_id"),
		field.Int64("proxy_id"),
		field.Int("concurrency").Default(3),
		field.Int("position").Default(0),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{
			dialect.Postgres: "timestamptz",
		}),
	}
}

func (AccountProxy) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("account", Account.Type).Unique().Required().Field("account_id").Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("proxy", Proxy.Type).Unique().Required().Field("proxy_id").Annotations(entsql.OnDelete(entsql.Restrict)),
	}
}

func (AccountProxy) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("proxy_id"),
		index.Fields("account_id"),
		index.Fields("account_id", "proxy_id").Unique(),
	}
}
