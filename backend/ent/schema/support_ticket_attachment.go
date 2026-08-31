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

// SupportTicketAttachment stores metadata for a private image object.
type SupportTicketAttachment struct{ ent.Schema }

func (SupportTicketAttachment) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "support_ticket_attachments"}}
}

func (SupportTicketAttachment) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("ticket_id"),
		field.Int64("message_id"),
		field.Int64("uploader_id"),
		field.String("storage_key").MaxLen(500).Unique(),
		field.String("original_name").MaxLen(255),
		field.String("content_type").MaxLen(64),
		field.Int64("size"),
		field.Int("width"),
		field.Int("height"),
		field.String("sha256").MaxLen(64),
		field.Time("hidden_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int64("hidden_by").Optional().Nillable(),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (SupportTicketAttachment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("ticket_id", "message_id"),
		index.Fields("uploader_id", "created_at"),
	}
}
