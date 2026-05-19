package analyzer

import (
	"strings"

	"github.com/wren-engine/wren/internal/parser/ast"
)

// tableNameToQualifiedName converts a CatalogSchemaTableName to a QualifiedName.
func tableNameToQualifiedName(cstn CatalogSchemaTableName) ast.QualifiedName {
	parts := []string{}
	if cstn.Catalog != "" {
		parts = append(parts, cstn.Catalog)
	}
	if cstn.Schema != "" {
		parts = append(parts, cstn.Schema)
	}
	parts = append(parts, cstn.Table)
	return ast.QualifiedNameOf(parts...)
}

// hasSuffix reports whether qn ends with suffix (by Parts comparison).
func hasSuffix(qn, suffix ast.QualifiedName) bool {
	if len(suffix.Parts) > len(qn.Parts) {
		return false
	}
	offset := len(qn.Parts) - len(suffix.Parts)
	for i, p := range suffix.Parts {
		if !strings.EqualFold(p, qn.Parts[offset+i]) {
			return false
		}
	}
	return true
}

// qualifiedNamePrefix returns all but the last part of a QualifiedName,
// or nil if there are fewer than 2 parts.
func qualifiedNamePrefix(qn *ast.QualifiedName) *ast.QualifiedName {
	if qn == nil || len(qn.Parts) < 2 {
		return nil
	}
	prefix := ast.QualifiedName{
		Parts:         qn.Parts[:len(qn.Parts)-1],
		OriginalParts: qn.OriginalParts[:len(qn.OriginalParts)-1],
	}
	return &prefix
}

// qualifiedNameSuffix returns the last part of a QualifiedName.
func qualifiedNameSuffix(qn *ast.QualifiedName) string {
	if qn == nil || len(qn.Parts) == 0 {
		return ""
	}
	return qn.Parts[len(qn.Parts)-1]
}
