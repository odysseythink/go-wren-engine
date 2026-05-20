package mdl

import (
	"testing"

	"github.com/wren-engine/wren/internal/dto"
)

func TestRenderJinjaSimpleMacro(t *testing.T) {
	manifest := &dto.Manifest{
		Models: []dto.Model{
			{Name: "orders", Columns: []dto.Column{
				{Name: "total", Expression: "{{ add_one(orderkey) }}", Type: "INTEGER"},
			}},
		},
		Macros: []dto.Macro{
			{Name: "add_one", Definition: "add_one(x) => x + 1"},
		},
	}
	result := RenderJinja(manifest)
	expr := result.Models[0].Columns[0].Expression
	if expr != "orderkey + 1" {
		t.Fatalf("expected 'orderkey + 1', got %q", expr)
	}
}

func TestRenderJinjaNoArgs(t *testing.T) {
	manifest := &dto.Manifest{
		Models: []dto.Model{
			{Name: "orders", Columns: []dto.Column{
				{Name: "now", Expression: "{{ current_time }}", Type: "TIMESTAMP"},
			}},
		},
		Macros: []dto.Macro{
			{Name: "current_time", Definition: "current_time() => now()"},
		},
	}
	result := RenderJinja(manifest)
	expr := result.Models[0].Columns[0].Expression
	if expr != "now()" {
		t.Fatalf("expected 'now()', got %q", expr)
	}
}

func TestRenderJinjaUnknownMacroLeftAsIs(t *testing.T) {
	manifest := &dto.Manifest{
		Models: []dto.Model{
			{Name: "orders", Columns: []dto.Column{
				{Name: "x", Expression: "{{ unknown() }}", Type: "INTEGER"},
			}},
		},
		Macros: []dto.Macro{
			{Name: "other", Definition: "other() => 1"},
		},
	}
	result := RenderJinja(manifest)
	expr := result.Models[0].Columns[0].Expression
	if expr != "{{ unknown() }}" {
		t.Fatalf("expected unchanged unknown macro, got %q", expr)
	}
}

func TestRenderJinjaNilManifest(t *testing.T) {
	// Risk #16: must not panic.
	if RenderJinja(nil) != nil {
		t.Fatalf("expected nil result for nil input")
	}
}

func TestRenderJinjaNoMacros(t *testing.T) {
	manifest := &dto.Manifest{
		Models: []dto.Model{
			{Name: "orders", Columns: []dto.Column{
				{Name: "orderkey", Expression: "orderkey", Type: "INTEGER"},
			}},
		},
	}
	result := RenderJinja(manifest)
	if result.Models[0].Columns[0].Expression != "orderkey" {
		t.Fatalf("expected unchanged expression")
	}
}
