package parser

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// locOf extracts a 1-based (line, column) NodeLocation from any ANTLR
// ParserRuleContext. Mirrors Java parsing's `new NodeLocation(token.getLine(),
// token.getCharPositionInLine() + 1)`. ANTLR's `GetColumn()` is 0-based; we
// add 1 to align with Java's 1-based column.
func locOf(ctx antlr.ParserRuleContext) *ast.NodeLocation {
	if ctx == nil {
		return nil
	}
	tok := ctx.GetStart()
	if tok == nil {
		return nil
	}
	return &ast.NodeLocation{Line: tok.GetLine(), CharPosition: tok.GetColumn() + 1}
}

// locOfTerminal extracts (line, column) from an ANTLR TerminalNode (for token
// references, e.g. Identifier built directly from an ID token without a wrapping
// rule context).
func locOfTerminal(node antlr.TerminalNode) *ast.NodeLocation {
	if node == nil {
		return nil
	}
	tok := node.GetSymbol()
	if tok == nil {
		return nil
	}
	return &ast.NodeLocation{Line: tok.GetLine(), CharPosition: tok.GetColumn() + 1}
}
