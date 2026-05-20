package rewrite

import (
	"strings"
	"testing"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

func metricMDL(t *testing.T) *mdl.WrenMDL {
	t.Helper()
	manifest := dto.Manifest{
		Catalog: "wren", Schema: "test",
		Models: []dto.Model{
			{Name: "Orders", RefSql: "select * from orders", PrimaryKey: "orderkey", Columns: []dto.Column{
				{Name: "orderkey", Type: "int4", Expression: "o_orderkey"},
				{Name: "custkey", Type: "int4", Expression: "o_custkey"},
				{Name: "totalprice", Type: "float8", Expression: "o_totalprice"},
				{Name: "orderdate", Type: "date", Expression: "o_orderdate"},
			}},
		},
		Metrics: []dto.Metric{
			{Name: "Revenue", BaseObject: "Orders",
				Dimension: []dto.Column{{Name: "custkey", Type: "int4", Expression: "custkey"}},
				Measure:   []dto.Column{{Name: "totalprice", Type: "int4", Expression: "sum(totalprice)"}},
				TimeGrain: []dto.TimeGrain{{Name: "orderdate", RefColumn: "orderdate", DateParts: []dto.TimeUnit{dto.TimeUnitYear, dto.TimeUnitMonth}}}},
			{Name: "RevenueByCustomer", BaseObject: "Revenue",
				Dimension: []dto.Column{{Name: "custkey", Type: "int4", Expression: "custkey"}},
				Measure:   []dto.Column{{Name: "totalprice", Type: "int4", Expression: "sum(totalprice)"}}},
		},
		CumulativeMetrics: []dto.CumulativeMetric{
			{Name: "WeeklyRevenue", BaseObject: "Orders",
				Measure: dto.Measure{Name: "totalprice", Type: "int4", Operator: "sum", RefColumn: "totalprice"},
				Window:  dto.Window{Name: "orderdate", RefColumn: "orderdate", TimeUnit: dto.TimeUnitWeek, Start: "1994-01-01", End: "1994-12-31"}},
		},
	}
	return mdl.WrenMDLFromManifest(&manifest)
}

func TestMetricSqlRender_OnModel(t *testing.T) {
	wrenMDL := metricMDL(t)
	revenue, _ := wrenMDL.GetMetric("Revenue")
	info, err := relationInfoOfMetric(revenue, wrenMDL)
	if err != nil {
		t.Fatalf("render Revenue: %v", err)
	}
	if !contains(info.RequiredObjects(), "Orders") {
		t.Fatalf("RequiredObjects = %v, want to contain Orders", info.RequiredObjects())
	}
	got := formatter.FormatSQL(info.Query())
	for _, want := range []string{`"Orders"."custkey"`, `sum("Orders"."totalprice")`, `GROUP BY 1`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered Revenue SQL missing %q:\n%s", want, got)
		}
	}
}

func TestMetricSqlRender_OnMetric(t *testing.T) {
	wrenMDL := metricMDL(t)
	m, _ := wrenMDL.GetMetric("RevenueByCustomer")
	info, err := relationInfoOfMetric(m, wrenMDL)
	if err != nil {
		t.Fatalf("render RevenueByCustomer: %v", err)
	}
	if got := info.RequiredObjects(); len(got) != 1 || got[0] != "Revenue" {
		t.Fatalf("RequiredObjects = %v, want [Revenue]", got)
	}
	got := formatter.FormatSQL(info.Query())
	for _, want := range []string{"FROM\n  Revenue", `GROUP BY 1`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered RevenueByCustomer SQL missing %q:\n%s", want, got)
		}
	}
}

func TestMetricSqlRender_OnCumulative(t *testing.T) {
	manifest := dto.Manifest{
		Catalog: "wren", Schema: "test",
		Models: []dto.Model{
			{Name: "Orders", RefSql: "select * from orders", PrimaryKey: "orderkey", Columns: []dto.Column{
				{Name: "totalprice", Type: "float8", Expression: "o_totalprice"},
				{Name: "orderdate", Type: "date", Expression: "o_orderdate"},
			}},
		},
		CumulativeMetrics: []dto.CumulativeMetric{
			{Name: "WeeklyRevenue", BaseObject: "Orders",
				Measure: dto.Measure{Name: "totalprice", Type: "int4", Operator: "sum", RefColumn: "totalprice"},
				Window:  dto.Window{Name: "orderdate", RefColumn: "orderdate", TimeUnit: dto.TimeUnitWeek, Start: "1994-01-01", End: "1994-12-31"}},
		},
		Metrics: []dto.Metric{
			{Name: "RevenueOnCumulative", BaseObject: "WeeklyRevenue",
				Dimension: []dto.Column{{Name: "orderdate", Type: "date", Expression: "orderdate"}},
				Measure:   []dto.Column{{Name: "totalprice", Type: "int4", Expression: "sum(totalprice)"}}},
		},
	}
	wrenMDL := mdl.WrenMDLFromManifest(&manifest)
	m, _ := wrenMDL.GetMetric("RevenueOnCumulative")
	info, err := relationInfoOfMetric(m, wrenMDL)
	if err != nil {
		t.Fatalf("render RevenueOnCumulative: %v", err)
	}
	if got := info.RequiredObjects(); len(got) != 1 || got[0] != "WeeklyRevenue" {
		t.Fatalf("RequiredObjects = %v, want [WeeklyRevenue]", got)
	}
}

func TestMetricSqlRender_RelationshipDimension(t *testing.T) {
	manifest := dto.Manifest{
		Catalog: "wren", Schema: "test",
		Models: []dto.Model{
			{Name: "A", RefSql: "select * from a", PrimaryKey: "id", Columns: []dto.Column{
				{Name: "id", Type: "int4", Expression: "a_id"},
				{Name: "bkey", Type: "int4", Expression: "a_bkey"},
				{Name: "b", Type: "B", Relationship: "AB"},
			}},
			{Name: "B", RefSql: "select * from b", PrimaryKey: "id", Columns: []dto.Column{
				{Name: "id", Type: "int4", Expression: "b_id"},
				{Name: "name", Type: "varchar", Expression: "b_name"},
			}},
		},
		Relationships: []dto.Relationship{
			{Name: "AB", Models: []string{"A", "B"}, JoinType: dto.JoinTypeManyToOne, Condition: "A.bkey = B.id"},
		},
		Metrics: []dto.Metric{
			{Name: "M", BaseObject: "A",
				Dimension: []dto.Column{{Name: "b_name", Type: "varchar", Expression: "b.name"}},
				Measure:   []dto.Column{{Name: "cnt", Type: "int4", Expression: "count(id)"}}},
		},
	}
	wrenMDL := mdl.WrenMDLFromManifest(&manifest)
	m, _ := wrenMDL.GetMetric("M")
	info, err := relationInfoOfMetric(m, wrenMDL)
	if err != nil {
		t.Fatalf("render M: %v", err)
	}
	if !contains(info.RequiredObjects(), "B") {
		t.Fatalf("RequiredObjects = %v, want to contain B", info.RequiredObjects())
	}
	got := formatter.FormatSQL(info.Query())
	for _, want := range []string{`LEFT JOIN`, `A_relationsub`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered M SQL missing %q:\n%s", want, got)
		}
	}
}
