package mdl

import (
	"regexp"
	"strings"

	"github.com/wren-engine/wren/internal/dto"
)

// RenderJinja substitutes macro references in column expressions.
// Best-effort positional-argument substitution; full Jinjava parity is deferred
// to P7 (default-config MDLs contain no macros, so this is gap-with-acceptance).
func RenderJinja(manifest *dto.Manifest) *dto.Manifest {
	if manifest == nil || len(manifest.Macros) == 0 {
		return manifest // risk #16 — nil-safe fast path
	}
	macros := make(map[string]*dto.Macro, len(manifest.Macros))
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

// macroCallRegex matches {{ macro_name }} or {{ macro_name(arg, arg) }} with optional whitespace.
var macroCallRegex = regexp.MustCompile(`\{\{\s*([A-Za-z_]\w*)\s*(?:\(([^)]*)\))?\s*\}\}`)

func expandMacros(expression string, macros map[string]*dto.Macro) string {
	return macroCallRegex.ReplaceAllStringFunc(expression, func(match string) string {
		groups := macroCallRegex.FindStringSubmatch(match)
		if groups == nil {
			return match
		}
		macroName := groups[1]
		argStr := groups[2]

		macro, ok := macros[macroName]
		if !ok {
			return match // unknown macro left as-is (Java parity for unmatched tags)
		}

		body := macro.GetBody()
		paramNames := macro.GetParameters()
		args := parseArgs(argStr)

		for i, name := range paramNames {
			if i < len(args) {
				body = strings.ReplaceAll(body, name, strings.TrimSpace(args[i]))
			}
		}
		return body
	})
}

func parseArgs(argStr string) []string {
	if strings.TrimSpace(argStr) == "" {
		return nil
	}
	return strings.Split(argStr, ",")
}
