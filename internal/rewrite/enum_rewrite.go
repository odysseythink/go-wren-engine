package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// EnumRewrite replaces "EnumName.Value" dereferences with a string literal.
// Mirrors Java io.wren.base.sqlrewrite.EnumRewrite.
type EnumRewrite struct{}

// Apply rewrites every 2-part DereferenceExpression whose first part names an
// MDL enum into a StringLiteral of the matched EnumValue's value. Mirrors
// EnumRewrite.apply + the inner Rewriter.visitDereferenceExpression.
func (r *EnumRewrite) Apply(root ast.Statement, _ *base.SessionContext, analyzedMDL *mdl.AnalyzedMDL) (ast.Statement, error) {
	wrenMDL := analyzedMDL.WrenMDL()
	var rewriteErr error
	out := RewriteNode(root, func(n ast.Node) (ast.Node, bool) {
		d, ok := n.(*ast.DereferenceExpression)
		if !ok {
			return nil, false // descend everything else
		}
		repl, matched, err := rewriteEnumIfNeed(d, wrenMDL)
		if err != nil {
			rewriteErr = err
			return d, true // stop descent on error path
		}
		if matched {
			return repl, true
		}
		return nil, false // not an enum: let RewriteNode recurse into Base
	})
	if rewriteErr != nil {
		return nil, rewriteErr
	}
	return out.(ast.Statement), nil
}

// rewriteEnumIfNeed returns (literal, true, nil) when node is a 2-part
// dereference whose first part is an MDL enum name; (node, false, nil) when
// it's not an enum match; or (node, false, err) when the enum name matches
// but the value doesn't (mirrors Java's IllegalArgumentException).
func rewriteEnumIfNeed(node *ast.DereferenceExpression, wrenMDL *mdl.WrenMDL) (ast.Expression, bool, error) {
	qn := ast.GetQualifiedName(node)
	if qn == nil || len(qn.OriginalParts) != 2 { // risk #4
		return node, false, nil
	}
	enumName := qn.OriginalParts[0].Value
	enumDef, ok := wrenMDL.GetEnumDefinition(enumName)
	if !ok {
		return node, false, nil
	}
	valueName := qn.OriginalParts[1].Value
	ev := enumDef.ValueOf(valueName) // risk #3: strict equality
	if ev == nil {
		return node, false, fmt.Errorf("Enum value '%s' not found in enum '%s'", qn.Parts[1], qn.Parts[0])
	}
	return &ast.StringLiteral{Value: ev.GetValue()}, true, nil // risk #5
}
