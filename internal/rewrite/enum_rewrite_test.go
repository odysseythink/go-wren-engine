package rewrite

import (
	"os"
	"strings"
	"testing"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

// viewenumMDL loads the synthetic view/enum MDL. Shared across P3c tests.
func viewenumMDL(t *testing.T) *mdl.WrenMDL {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/difftest/cases/viewenum/mdl.json")
	if err != nil {
		t.Fatalf("read viewenum mdl: %v", err)
	}
	wrenMDL, err := mdl.WrenMDLFromJSON(string(raw))
	if err != nil {
		t.Fatalf("parse viewenum mdl: %v", err)
	}
	return wrenMDL
}

func TestEnumRewrite_ReplacesEnumInWhere(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	stmt, err := parser.ParseSQL("SELECT orderkey FROM Orders WHERE orderstatus = Status.O")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := (&EnumRewrite{}).Apply(stmt, nil, mdl.NewAnalyzedMDL(wrenMDL))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := formatter.FormatSQL(out)
	if !strings.Contains(got, "'O'") {
		t.Errorf("missing 'O' literal:\n%s", got)
	}
	if strings.Contains(got, "Status.O") {
		t.Errorf("Status.O not replaced:\n%s", got)
	}
}

func TestEnumRewrite_ThreePartDereferenceUntouched(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	// x.Status.O is 3-part: not an enum match, base recurses but Status alone
	// is also not 2-part-enum since base is x.Status not just Status.
	stmt, _ := parser.ParseSQL("SELECT x.Status.O FROM t")
	out, err := (&EnumRewrite{}).Apply(stmt, nil, mdl.NewAnalyzedMDL(wrenMDL))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := formatter.FormatSQL(out)
	if !strings.Contains(got, "Status") || !strings.Contains(got, "O") {
		t.Errorf("3-part dereference should be untouched:\n%s", got)
	}
}

func TestEnumRewrite_UnknownEnumNameLeftAlone(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	stmt, _ := parser.ParseSQL("SELECT NotAnEnum.X FROM t")
	out, err := (&EnumRewrite{}).Apply(stmt, nil, mdl.NewAnalyzedMDL(wrenMDL))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := formatter.FormatSQL(out)
	if !strings.Contains(got, "NotAnEnum") {
		t.Errorf("non-enum dereference should be untouched:\n%s", got)
	}
}

func TestEnumRewrite_MissingEnumValueErrors(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	stmt, _ := parser.ParseSQL("SELECT Status.NOPE FROM t")
	_, err := (&EnumRewrite{}).Apply(stmt, nil, mdl.NewAnalyzedMDL(wrenMDL))
	if err == nil {
		t.Fatal("expected error for unknown enum value, got nil")
	}
	if !strings.Contains(err.Error(), "NOPE") || !strings.Contains(err.Error(), "Status") {
		t.Errorf("error should mention NOPE and Status, got: %v", err)
	}
}
