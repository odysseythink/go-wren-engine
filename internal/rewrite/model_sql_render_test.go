package rewrite

import (
	"strings"
	"testing"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

func TestModelSqlRender_PlainModel(t *testing.T) {
	wrenMDL := loadTPCHForRewrite(t)
	part, _ := wrenMDL.GetModel("Part")
	info, err := relationInfoOfModel(part, wrenMDL)
	if err != nil {
		t.Fatalf("render Part: %v", err)
	}
	if len(info.RequiredObjects()) != 0 {
		t.Fatalf("Part RequiredObjects = %v, want []", info.RequiredObjects())
	}
	got := formatter.FormatSQL(info.Query())
	for _, want := range []string{`"Part"."partkey"`, `"partkey"`, `"Part"."name"`, `p_partkey`, `p_name`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered Part SQL missing %q:\n%s", want, got)
		}
	}
}

func syntheticToOneMDL(t *testing.T) *mdl.WrenMDL {
	t.Helper()
	manifest := dto.Manifest{
		Catalog: "c", Schema: "s",
		Models: []dto.Model{
			{Name: "A", RefSql: "select * from a", PrimaryKey: "id", Columns: []dto.Column{
				{Name: "id", Type: "int4", Expression: "a_id"},
				{Name: "bkey", Type: "int4", Expression: "a_bkey"},
				{Name: "b", Type: "B", Relationship: "AB"},
				{Name: "b_name", Type: "varchar", IsCalculated: true, Expression: "b.name"},
			}},
			{Name: "B", RefSql: "select * from b", PrimaryKey: "id", Columns: []dto.Column{
				{Name: "id", Type: "int4", Expression: "b_id"},
				{Name: "name", Type: "varchar", Expression: "b_name"},
			}},
		},
		Relationships: []dto.Relationship{
			{Name: "AB", Models: []string{"A", "B"}, JoinType: dto.JoinTypeManyToOne, Condition: "A.bkey = B.id"},
		},
	}
	return mdl.WrenMDLFromManifest(&manifest)
}

func TestModelSqlRender_ToOneRelationship(t *testing.T) {
	wrenMDL := syntheticToOneMDL(t)
	a, _ := wrenMDL.GetModel("A")
	info, err := relationInfoOfModel(a, wrenMDL)
	if err != nil {
		t.Fatalf("render A: %v", err)
	}
	if !contains(info.RequiredObjects(), "B") {
		t.Fatalf("RequiredObjects = %v, want to contain B", info.RequiredObjects())
	}
	got := formatter.FormatSQL(info.Query())
	for _, want := range []string{`LEFT JOIN`, `A_relationsub`, `"b_name"`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered A SQL missing %q:\n%s", want, got)
		}
	}
}

func syntheticToManyMDL(t *testing.T) *mdl.WrenMDL {
	t.Helper()
	manifest := dto.Manifest{
		Catalog: "c", Schema: "s",
		Models: []dto.Model{
			{Name: "A", RefSql: "select * from a", PrimaryKey: "id", Columns: []dto.Column{
				{Name: "id", Type: "int4", Expression: "a_id"},
				{Name: "bkey", Type: "int4", Expression: "a_bkey"},
				{Name: "b", Type: "B", Relationship: "AB"},
				{Name: "b_count", Type: "int4", IsCalculated: true, Expression: "count(b.id)"},
			}},
			{Name: "B", RefSql: "select * from b", PrimaryKey: "id", Columns: []dto.Column{
				{Name: "id", Type: "int4", Expression: "b_id"},
				{Name: "name", Type: "varchar", Expression: "b_name"},
			}},
		},
		Relationships: []dto.Relationship{
			{Name: "AB", Models: []string{"A", "B"}, JoinType: dto.JoinTypeOneToMany, Condition: "A.bkey = B.id"},
		},
	}
	return mdl.WrenMDLFromManifest(&manifest)
}

func TestModelSqlRender_ToManyRelationship(t *testing.T) {
	wrenMDL := syntheticToManyMDL(t)
	a, _ := wrenMDL.GetModel("A")
	info, err := relationInfoOfModel(a, wrenMDL)
	if err != nil {
		t.Fatalf("render A: %v", err)
	}
	if !contains(info.RequiredObjects(), "B") {
		t.Fatalf("RequiredObjects = %v, want to contain B", info.RequiredObjects())
	}
	got := formatter.FormatSQL(info.Query())
	for _, want := range []string{`GROUP BY 1`, `"b_count"`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered A SQL missing %q:\n%s", want, got)
		}
	}
}
