package lineage

import (
	"testing"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
)

// buildSimpleModel creates a model with the given columns.
func buildSimpleModel(name, base string, cols ...dto.Column) dto.Model {
	return dto.Model{Name: name, BaseObject: base, PrimaryKey: "id", Columns: cols}
}

// TestAnalyze_ModelSelfReference verifies a non-calc column depends on itself.
func TestAnalyze_ModelSelfReference(t *testing.T) {
	m := buildSimpleModel("Orders", "orders_table",
		dto.Column{Name: "orderkey", Type: "integer"},
		dto.Column{Name: "price", Type: "double"},
	)
	manifest := dto.Manifest{
		Catalog: "wren", Schema: "wren",
		Models: []dto.Model{m},
	}
	l, err := Analyze(mdl.WrenMDLFromManifest(&manifest))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	src := l.SourceColumns(QualifiedName{Table: "Orders", Column: "orderkey"})
	if src == nil || len(src["Orders"]) != 1 || src["Orders"][0] != "orderkey" {
		t.Fatalf("expected self-reference, got %v", src)
	}
}

// TestAnalyze_CalcFieldWithRelationship verifies calc field across relationship.
func TestAnalyze_CalcFieldWithRelationship(t *testing.T) {
	customer := buildSimpleModel("Customer", "customer_table",
		dto.Column{Name: "custkey", Type: "integer"},
		dto.Column{Name: "name", Type: "varchar"},
	)
	orders := buildSimpleModel("Orders", "orders_table",
		dto.Column{Name: "orderkey", Type: "integer"},
		dto.Column{Name: "custkey", Type: "integer", Expression: "custkey"},
		dto.Column{Name: "Customer", Type: "Customer", Relationship: "o_c"},
		dto.Column{Name: "custname", Type: "varchar", IsCalculated: true, Expression: `Customer.name`},
	)
	rel := dto.Relationship{
		Name: "o_c", Models: []string{"Orders", "Customer"},
		Condition: `"Orders".custkey = "Customer".custkey`,
		JoinType:  "ONE_TO_MANY",
	}
	manifest := dto.Manifest{
		Catalog: "wren", Schema: "wren",
		Models:        []dto.Model{customer, orders},
		Relationships: []dto.Relationship{rel},
	}
	l, err := Analyze(mdl.WrenMDLFromManifest(&manifest))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	// custname's direct source is Customer.name
	src := l.SourceColumns(QualifiedName{Table: "Orders", Column: "custname"})
	if len(src["Customer"]) != 1 || src["Customer"][0] != "name" {
		t.Fatalf("expected Customer.name, got %v", src)
	}

	// RequiredFields from custname should include Customer (source) and Orders (self).
	req, err := l.RequiredFields([]QualifiedName{{Table: "Orders", Column: "custname"}})
	if err != nil {
		t.Fatalf("RequiredFields: %v", err)
	}
	if len(req) != 2 {
		t.Fatalf("expected 2 tables, got %d: %+v", len(req), req)
	}
	// topo order: Customer before Orders (Customer -> Orders edge)
	if req[0].Name != "Customer" || req[1].Name != "Orders" {
		t.Fatalf("expected [Customer, Orders], got %v", req)
	}
}

// TestAnalyze_CycleDetection verifies a circular calc field returns an error.
func TestAnalyze_CycleDetection(t *testing.T) {
	a := buildSimpleModel("A", "a_table",
		dto.Column{Name: "B", Type: "B", Relationship: "a_b"},
		dto.Column{Name: "x", Type: "integer", IsCalculated: true, Expression: "B.y"},
	)
	b := buildSimpleModel("B", "b_table",
		dto.Column{Name: "A", Type: "A", Relationship: "a_b"},
		dto.Column{Name: "y", Type: "integer", IsCalculated: true, Expression: "A.x"},
	)
	rel := dto.Relationship{
		Name: "a_b", Models: []string{"A", "B"},
		Condition: `"A".id = "B".id`, JoinType: "ONE_TO_ONE",
	}
	manifest := dto.Manifest{
		Catalog: "wren", Schema: "wren",
		Models:        []dto.Model{a, b},
		Relationships: []dto.Relationship{rel},
	}
	l, err := Analyze(mdl.WrenMDLFromManifest(&manifest))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	_, err = l.RequiredFields([]QualifiedName{{Table: "A", Column: "x"}})
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
}

// TestAnalyze_MetricOnModel verifies metric column sources resolve through base model.
func TestAnalyze_MetricOnModel(t *testing.T) {
	orders := buildSimpleModel("Orders", "orders_table",
		dto.Column{Name: "orderkey", Type: "integer"},
		dto.Column{Name: "totalprice", Type: "double"},
	)
	revenue := dto.Metric{
		Name: "Revenue", BaseObject: "Orders",
		Dimension: []dto.Column{
			{Name: "orderkey", Type: "integer"},
		},
		Measure: []dto.Column{
			{Name: "amount", Type: "double", Expression: "SUM(totalprice)"},
		},
	}
	manifest := dto.Manifest{
		Catalog: "wren", Schema: "wren",
		Models:  []dto.Model{orders},
		Metrics: []dto.Metric{revenue},
	}
	l, err := Analyze(mdl.WrenMDLFromManifest(&manifest))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	src := l.SourceColumns(QualifiedName{Table: "Revenue", Column: "amount"})
	if len(src["Orders"]) != 1 || src["Orders"][0] != "totalprice" {
		t.Fatalf("expected Orders.totalprice, got %v", src)
	}
}

// TestAnalyze_CumulativeMetric verifies cumulative metric pseudo-columns map to baseObject.
func TestAnalyze_CumulativeMetric(t *testing.T) {
	orders := buildSimpleModel("Orders", "orders_table",
		dto.Column{Name: "orderkey", Type: "integer"},
		dto.Column{Name: "totalprice", Type: "double"},
		dto.Column{Name: "orderdate", Type: "date"},
	)
	cum := dto.CumulativeMetric{
		Name: "CumRevenue", BaseObject: "Orders",
		Measure: dto.Measure{Name: "amount", RefColumn: "totalprice", Operator: "SUM"},
		Window:  dto.Window{Name: "window", RefColumn: "orderdate", TimeUnit: dto.TimeUnitDay, Start: "2023-01-01", End: "2023-12-31"},
	}
	manifest := dto.Manifest{
		Catalog: "wren", Schema: "wren",
		Models:             []dto.Model{orders},
		CumulativeMetrics: []dto.CumulativeMetric{cum},
	}
	l, err := Analyze(mdl.WrenMDLFromManifest(&manifest))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	src := l.SourceColumns(QualifiedName{Table: "CumRevenue", Column: "amount"})
	if len(src["Orders"]) != 1 || src["Orders"][0] != "totalprice" {
		t.Fatalf("expected Orders.totalprice, got %v", src)
	}
}
