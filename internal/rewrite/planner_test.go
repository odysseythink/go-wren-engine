package rewrite

import (
	"testing"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/mdl"
)

func TestRewriteNoRules(t *testing.T) {
	ctx := &analyzer.SessionContext{Catalog: "wren", Schema: "public"}
	analyzed := mdl.NewAnalyzedMDL(nil)
	result, err := Rewrite("SELECT 1", ctx, analyzed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "SELECT 1\n\n" {
		t.Fatalf("expected 'SELECT 1\\n\\n', got '%s'", result)
	}
}
