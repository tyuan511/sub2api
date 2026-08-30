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

// SupportTicketRead tracks the last message read by one user or administrator.
type SupportTicketRead struct{ ent.Schema }

func (SupportTicketRead) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "support_ticket_reads"}}
}

func (SupportTicketRead) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("ticket_id"),
		field.Int64("reader_id"),
		field.Int64("last_read_message_id"),
		field.Time("read_at").Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (SupportTicketRead) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("ticket_id", "reader_id").Unique(),
		index.Fields("reader_id", "read_at"),
	}
}
