package ast

import "testing"

func TestNodeLocation(t *testing.T) {
	loc := NodeLocation{Line: 1, CharPosition: 5}
	if loc.Line != 1 || loc.CharPosition != 5 {
		t.Errorf("NodeLocation not set correctly: %+v", loc)
	}
}

func TestNodeInterface(t *testing.T) {
	var _ Node = (*Query)(nil)
	var _ Node = (*Table)(nil)
	var _ Node = (*Identifier)(nil)
	var _ Node = (*LongLiteral)(nil)
	var _ Node = (*StringLiteral)(nil)
}
