// Package difftest provides a golden-snapshot differential testing harness
// that compares the Go rewrite engine against the Java wren-engine:0.9.3.
package difftest

import (
	"strings"

	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/generated"
)

// Normalize tokenizes sql and returns a canonical token sequence. Two SQL
// strings are considered equivalent iff their normalized sequences are equal.
//
// Whitespace and comments are dropped. Keywords, unquoted identifiers,
// numbers and operators are upper-cased (SQL treats them case-insensitively).
// String literals and quoted identifiers keep their original text verbatim,
// since they are case-sensitive.
func Normalize(sql string) ([]string, error) {
	toks := parser.LexTokens(sql)
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		out = append(out, normalizeToken(t))
	}
	return out, nil
}

func normalizeToken(t parser.LexToken) string {
	switch t.Type {
	case generated.SqlBaseLexerSTRING,
		generated.SqlBaseLexerUNICODE_STRING,
		generated.SqlBaseLexerQUOTED_IDENTIFIER,
		generated.SqlBaseLexerBACKQUOTED_IDENTIFIER:
		return t.Text
	default:
		return strings.ToUpper(t.Text)
	}
}
