package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamMembership 保存邀请、正式成员和退出待审批状态。
type TeamMembership struct {
	ent.Schema
}

func (TeamMembership) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "team_memberships"}}
}

func (TeamMembership) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (TeamMembership) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("team_id"),
		field.Int64("user_id"),
		field.Int64("invited_by"),
		field.String("remark").MaxLen(100).Default(""),
		field.String("status").MaxLen(20).Default("invited"),
		field.Time("joined_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("exit_requested_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (TeamMembership) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("team", Team.Type).
			Ref("memberships").
			Field("team_id").
			Required().
			Unique(),
		edge.From("user", User.Type).
			Ref("team_membership").
			Field("user_id").
			Required().
			Unique(),
	}
}

func (TeamMembership) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").Unique(),
		index.Fields("team_id", "user_id").Unique(),
		index.Fields("team_id", "status"),
	}
}
