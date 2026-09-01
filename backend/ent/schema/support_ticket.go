package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SupportTicket stores the mutable state of one customer support request.
type SupportTicket struct{ ent.Schema }

func (SupportTicket) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "support_tickets"}}
}

func (SupportTicket) Fields() []ent.Field {
	return []ent.Field{
		field.String("ticket_no").MaxLen(32).NotEmpty().Unique(),
		field.Int64("user_id"),
		field.String("title").MaxLen(200).NotEmpty(),
		field.String("category").MaxLen(32).NotEmpty(),
		field.String("status").MaxLen(24).Default("open"),
		field.String("priority").MaxLen(16).Default("normal"),
		field.Int64("last_message_id").Optional().Nillable(),
		field.Time("last_message_at").Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("resolved_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("closed_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (SupportTicket) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").Unique(),
		index.Fields("last_message_at", "id"),
		index.Fields("status", "last_message_at"),
		index.Fields("category", "created_at"),
	}
}
