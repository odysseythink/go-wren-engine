package parser

import "testing"

func TestAstBuilder_NilContextDoesNotPanic(t *testing.T) {
	b := &AstBuilder{}
	tests := []struct {
		name string
		fn   func()
	}{
		{"VisitAliasedRelation nil", func() { b.VisitAliasedRelation(nil) }},
		{"VisitTableName nil", func() { b.VisitTableName(nil) }},
		{"VisitSubqueryRelation nil", func() { b.VisitSubqueryRelation(nil) }},
		{"VisitParenthesizedRelation nil", func() { b.VisitParenthesizedRelation(nil) }},
		{"VisitColumnReference nil", func() { b.VisitColumnReference(nil) }},
		{"VisitDereference nil", func() { b.VisitDereference(nil) }},
		{"VisitCast nil", func() { b.VisitCast(nil) }},
		{"VisitExists nil", func() { b.VisitExists(nil) }},
		{"VisitParenthesizedExpression nil", func() { b.VisitParenthesizedExpression(nil) }},
		{"VisitLateral nil", func() { b.VisitLateral(nil) }},
		{"VisitPatternRecognition nil", func() { b.VisitPatternRecognition(nil) }},
		{"VisitFunctionRelation nil", func() { b.VisitFunctionRelation(nil) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic: %v", r)
				}
			}()
			tc.fn()
		})
	}
}
