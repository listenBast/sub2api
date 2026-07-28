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

// TeamTransaction 是不可变的团队操作与资金流水。
type TeamTransaction struct {
	ent.Schema
}

func (TeamTransaction) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "team_transactions"}}
}

func (TeamTransaction) Fields() []ent.Field {
	decimal := map[string]string{dialect.Postgres: "decimal(20,8)"}
	return []ent.Field{
		field.Int64("team_id"),
		field.Int64("operator_id"),
		field.Int64("member_id").Optional().Nillable(),
		field.String("action").MaxLen(40).NotEmpty(),
		field.Float("amount").SchemaType(decimal).Default(0),
		field.Float("owner_balance_before").SchemaType(decimal).Default(0),
		field.Float("owner_balance_after").SchemaType(decimal).Default(0),
		field.Float("member_balance_before").SchemaType(decimal).Optional().Nillable(),
		field.Float("member_balance_after").SchemaType(decimal).Optional().Nillable(),
		field.String("note").MaxLen(500).Default(""),
		field.Time("created_at").
			Default(time.Now).
			Immutable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (TeamTransaction) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("team", Team.Type).
			Ref("transactions").
			Field("team_id").
			Required().
			Unique(),
	}
}

func (TeamTransaction) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("team_id", "created_at"),
		index.Fields("member_id", "created_at"),
		index.Fields("action"),
	}
}
