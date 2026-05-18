package mdl

import (
	"testing"

	"github.com/wren-engine/wren/internal/dto"
)

func TestWrenMDLFromManifest(t *testing.T) {
	manifest := &dto.Manifest{
		Catalog: "wren",
		Schema:  "public",
		Models: []dto.Model{
			{Name: "orders", RefSql: "SELECT * FROM raw_orders"},
		},
	}
	mdl := WrenMDLFromManifest(manifest)
	if mdl.Catalog() != "wren" {
		t.Errorf("Expected catalog wren, got %s", mdl.Catalog())
	}
	model, ok := mdl.GetModel("orders")
	if !ok {
		t.Fatal("Expected to find orders model")
	}
	if model.Name != "orders" {
		t.Errorf("Expected model name orders, got %s", model.Name)
	}
}

func TestIsObjectExist(t *testing.T) {
	manifest := &dto.Manifest{
		Models: []dto.Model{{Name: "orders"}},
		Metrics: []dto.Metric{{Name: "total_sales"}},
	}
	mdl := WrenMDLFromManifest(manifest)
	if !mdl.IsObjectExist("orders") {
		t.Error("Expected orders to exist")
	}
	if !mdl.IsObjectExist("total_sales") {
		t.Error("Expected total_sales to exist")
	}
	if mdl.IsObjectExist("nonexistent") {
		t.Error("Expected nonexistent to not exist")
	}
}
