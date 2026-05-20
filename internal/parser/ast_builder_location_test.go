package parser

import (
	"testing"
)

func TestLocationOnBasicNodes(t *testing.T) {
	stmt, err := ParseSQL("SELECT a FROM t WHERE a > 1")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if loc := stmt.GetLocation(); loc == nil {
		t.Fatal("Query.Location nil; expected (1, 1)")
	} else if loc.Line != 1 || loc.CharPosition != 1 {
		t.Errorf("Query loc = (%d,%d); want (1,1)", loc.Line, loc.CharPosition)
	}
}
