package dto

import (
	"encoding/json"
	"testing"
)

func TestManifestJSONRoundTrip(t *testing.T) {
	original := &Manifest{
		Catalog: "wren",
		Schema:  "public",
		Models: []Model{
			{Name: "orders", RefSql: "SELECT * FROM raw_orders", Columns: []Column{{Name: "id", Type: "INTEGER"}}},
		},
		Relationships: []Relationship{
			{Name: "orders_customer", Models: []string{"orders", "customers"}, JoinType: JoinTypeManyToMany, Condition: "orders.customer_id = customers.id"},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var parsed Manifest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if parsed.Catalog != "wren" {
		t.Errorf("Catalog mismatch: got %s", parsed.Catalog)
	}
	if len(parsed.Models) != 1 {
		t.Errorf("Expected 1 model, got %d", len(parsed.Models))
	}
}

func TestManifestEmptyDefaults(t *testing.T) {
	jsonStr := `{"catalog": "c", "schema": "s"}`
	var m Manifest
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if m.Models == nil {
		t.Error("Models should default to empty slice")
	}
}
