package mdl

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/wren-engine/wren/internal/dto"
)

func TestRenderJinja_TpchManifest(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/difftest/cases/tpch/mdl.json")
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest dto.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	RenderJinja(&manifest)

	for _, model := range manifest.Models {
		for _, col := range model.Columns {
			if strings.Contains(col.Expression, "{{") || strings.Contains(col.Expression, "}}") {
				t.Errorf("%s.%s still contains jinja: %q", model.Name, col.Name, col.Expression)
			}
		}
	}
}
