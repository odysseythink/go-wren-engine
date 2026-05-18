package analyzer

import (
	"testing"

	"github.com/wren-engine/wren/internal/dto"
)

func TestAnalysis(t *testing.T) {
	a := &Analysis{}
	a.AddTable("orders")
	a.AddModel("customers")
	a.AddColumn(dto.Column{Name: "id", Type: "INTEGER"})

	if len(a.Tables) != 1 || a.Tables[0] != "orders" {
		t.Error("Table not added correctly")
	}
	if len(a.Models) != 1 || a.Models[0] != "customers" {
		t.Error("Model not added correctly")
	}
	if len(a.CollectedColumns) != 1 {
		t.Error("Column not added correctly")
	}
}
