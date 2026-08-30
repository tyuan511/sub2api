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

// AdminTelegramBinding links one administrator to one private Telegram chat.
type AdminTelegramBinding struct{ ent.Schema }

func (AdminTelegramBinding) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "admin_telegram_bindings"}}
}

func (AdminTelegramBinding) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("admin_id").Unique(),
		field.Int64("telegram_user_id").Unique(),
		field.Int64("chat_id"),
		field.String("telegram_username").MaxLen(64).Optional().Nillable(),
		field.Bool("enabled").Default(true),
		field.Bool("notify_new_ticket").Default(true),
		field.Bool("notify_user_reply").Default(true),
		field.Bool("notify_high_priority").Default(true),
		field.Time("bound_at").Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_success_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("last_error").SchemaType(map[string]string{dialect.Postgres: "text"}).Optional().Nillable(),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (AdminTelegramBinding) Indexes() []ent.Index {
	return []ent.Index{index.Fields("enabled"), index.Fields("chat_id")}
}
