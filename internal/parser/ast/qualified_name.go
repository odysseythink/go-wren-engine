package ast

import "strings"

// QualifiedName represents a dot-separated name (e.g., catalog.schema.table).
type QualifiedName struct {
	Parts         []string
	OriginalParts []Identifier
}

func QualifiedNameOf(parts ...string) QualifiedName {
	original := make([]Identifier, len(parts))
	for i, p := range parts {
		original[i] = Identifier{Value: p}
	}
	return QualifiedName{Parts: parts, OriginalParts: original}
}

func (q QualifiedName) String() string {
	return strings.Join(q.Parts, ".")
}

func (q QualifiedName) HasPrefix(prefix QualifiedName) bool {
	if len(prefix.Parts) > len(q.Parts) {
		return false
	}
	for i, p := range prefix.Parts {
		if p != q.Parts[i] {
			return false
		}
	}
	return true
}

func (q QualifiedName) Suffix(n int) QualifiedName {
	return QualifiedName{
		Parts:         q.Parts[n:],
		OriginalParts: q.OriginalParts[n:],
	}
}

func (q QualifiedName) Last() string {
	if len(q.Parts) == 0 {
		return ""
	}
	return q.Parts[len(q.Parts)-1]
}
