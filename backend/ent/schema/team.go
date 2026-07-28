package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Team 定义团队及其主账号。
type Team struct {
	ent.Schema
}

func (Team) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "teams"}}
}

func (Team) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (Team) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(100).NotEmpty(),
		field.Int64("owner_id"),
		field.String("status").MaxLen(20).Default("active"),
	}
}

func (Team) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("owned_team").
			Field("owner_id").
			Required().
			Unique(),
		edge.To("memberships", TeamMembership.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("transactions", TeamTransaction.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Team) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("owner_id").Unique(),
		index.Fields("status"),
	}
}
