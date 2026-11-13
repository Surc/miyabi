package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// LocalScanEntry records the last successful import of a media path and its sidecars.
type LocalScanEntry struct{ ent.Schema }

func (LocalScanEntry) Fields() []ent.Field {
	return []ent.Field{
		field.String("root").NotEmpty(),
		field.String("path").NotEmpty(),
		field.String("fingerprint").NotEmpty(),
		field.Int("file_id"),
	}
}

func (LocalScanEntry) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("file", File.Type).Field("file_id").Unique().Required().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (LocalScanEntry) Indexes() []ent.Index {
	return []ent.Index{index.Fields("root", "path").Unique(), index.Fields("file_id")}
}
