package rewrite

import (
	"testing"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/mdl"
)

func TestRewriteNoRules(t *testing.T) {
	ctx := &analyzer.SessionContext{Catalog: "wren", Schema: "public"}
	analyzed := mdl.NewAnalyzedMDL(nil)
	// AstBuilder not yet implemented, so parsing returns error.
	_, err := Rewrite("SELECT 1", ctx, analyzed)
	if err == nil {
		t.Fatal("Expected parse error since AstBuilder is not implemented")
	}
}
