package difftest

import (
	"slices"
	"testing"
)

func eq(t *testing.T, a, b string, want bool) {
	t.Helper()
	na, err := Normalize(a)
	if err != nil {
		t.Fatalf("Normalize(%q): %v", a, err)
	}
	nb, err := Normalize(b)
	if err != nil {
		t.Fatalf("Normalize(%q): %v", b, err)
	}
	got := slices.Equal(na, nb)
	if got != want {
		t.Errorf("equal(%q, %q) = %v, want %v\n  a=%v\n  b=%v", a, b, got, want, na, nb)
	}
}

func TestNormalize_IgnoresWhitespaceAndKeywordCase(t *testing.T) {
	eq(t, "select a from t", "SELECT   a\nFROM  t", true)
}

func TestNormalize_IgnoresUnquotedIdentifierCase(t *testing.T) {
	eq(t, "select Col from T", "select col from t", true)
}

func TestNormalize_StringLiteralIsCaseSensitive(t *testing.T) {
	eq(t, "select 'A'", "select 'a'", false)
}

func TestNormalize_QuotedIdentifierIsCaseSensitive(t *testing.T) {
	eq(t, `select "Col"`, `select "col"`, false)
}

func TestNormalize_StructuralDifferenceDetected(t *testing.T) {
	eq(t, "select a from t", "select a, b from t", false)
}
