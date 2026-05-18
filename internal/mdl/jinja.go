package mdl

import (
	"fmt"
	"strings"

	"github.com/wren-engine/wren/internal/dto"
)

// RenderJinja processes Jinja templates in column expressions.
// TODO: Replace with full gonja implementation when available.
func RenderJinja(manifest *dto.Manifest) *dto.Manifest {
	// Minimal implementation: replace macro calls with their bodies
	macros := make(map[string]*dto.Macro)
	for i := range manifest.Macros {
		macros[manifest.Macros[i].Name] = &manifest.Macros[i]
	}

	for i := range manifest.Models {
		for j := range manifest.Models[i].Columns {
			col := &manifest.Models[i].Columns[j]
			if col.Expression != "" {
				col.Expression = expandMacros(col.Expression, macros)
			}
		}
	}

	return manifest
}

func expandMacros(expression string, macros map[string]*dto.Macro) string {
	for name, macro := range macros {
		// Simple substitution: {{ macro_name(args) }} -> body
		// This is a very basic placeholder implementation.
		placeholder := fmt.Sprintf("{{%s}}", name)
		expression = strings.ReplaceAll(expression, placeholder, macro.GetBody())
	}
	return expression
}
