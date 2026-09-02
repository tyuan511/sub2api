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

// AccountProxy holds the edge schema for the account ↔ proxy many-to-many binding.
//
// 一个账号可以绑定多个代理，每个代理各自带一个并发上限。
// 该表为空时账号退回旧行为：只使用 accounts.proxy_id 与 accounts.concurrency。
type AccountProxy struct {
	ent.Schema
}

func (AccountProxy) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "account_proxies"},
		// Composite primary key: (account_id, proxy_id).
		field.ID("account_id", "proxy_id"),
	}
}

func (AccountProxy) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("account_id"),
		field.Int64("proxy_id"),
		// concurrency: 该代理单独的并发上限；账号总并发为各代理之和。
		field.Int("concurrency").
			Default(3),
		// sort_order: 展示与轮询顺序，越小越靠前。
		field.Int("sort_order").
			Default(0),
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

func (AccountProxy) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("account", Account.Type).
			Unique().
			Required().
			Field("account_id"),
		edge.To("proxy", Proxy.Type).
			Unique().
			Required().
			Field("proxy_id"),
	}
}

func (AccountProxy) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("proxy_id"),
		index.Fields("account_id", "sort_order"),
	}
}
