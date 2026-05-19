package rewrite

import (
	"testing"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"

	base "github.com/wren-engine/wren/internal/analyzer"
)

var dtoEmptyManifest = dto.Manifest{Catalog: "wren", Schema: "public"}

func TestRewritePassThrough(t *testing.T) {
	ctx := &base.SessionContext{Catalog: "wren", Schema: "public"}
	analyzed := mdl.NewAnalyzedMDL(mdl.WrenMDLFromManifest(&dtoEmptyManifest))
	got, err := Rewrite("SELECT 1", ctx, analyzed)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if got != "SELECT 1\n\n" {
		t.Fatalf("got %q, want %q", got, "SELECT 1\n\n")
	}
}
