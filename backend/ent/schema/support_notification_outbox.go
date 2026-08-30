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

// SupportNotificationOutbox provides retryable Telegram delivery after commit.
type SupportNotificationOutbox struct{ ent.Schema }

func (SupportNotificationOutbox) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "support_notification_outbox"}}
}

func (SupportNotificationOutbox) Fields() []ent.Field {
	return []ent.Field{
		field.String("event_type").MaxLen(32),
		field.Int64("ticket_id"),
		field.Int64("message_id").Optional().Nillable(),
		field.Int64("target_admin_id"),
		field.Int64("telegram_message_id").Optional().Nillable(),
		field.String("payload").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("status").MaxLen(16).Default("pending"),
		field.Int("attempt_count").Default(0),
		field.Time("next_attempt_at").Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("locked_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("last_error").SchemaType(map[string]string{dialect.Postgres: "text"}).Optional().Nillable(),
		field.Time("sent_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (SupportNotificationOutbox) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "next_attempt_at"),
		index.Fields("target_admin_id", "created_at"),
		index.Fields("ticket_id", "created_at"),
		index.Fields("target_admin_id", "telegram_message_id").Unique(),
	}
}
