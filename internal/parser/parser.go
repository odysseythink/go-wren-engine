package parser

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/wren-engine/wren/internal/parser/ast"
	generated "github.com/wren-engine/wren/internal/parser/generated"
)

// ParseSQL parses a SQL string into an AST Statement.
func ParseSQL(sql string) (ast.Statement, error) {
	lexer := generated.NewSqlBaseLexer(antlr.NewInputStream(sql))
	stream := antlr.NewCommonTokenStream(lexer, 0)
	p := generated.NewSqlBaseParser(stream)
	tree := p.Statements()

	builder := &AstBuilder{}
	result := builder.Visit(tree)
	if result == nil {
		return nil, fmt.Errorf("failed to parse SQL: %s", sql)
	}
	stmt, ok := result.(ast.Statement)
	if !ok {
		return nil, fmt.Errorf("parse result is not a Statement: %T", result)
	}
	return stmt, nil
}

// ParseExpression parses a SQL expression into an AST Expression.
func ParseExpression(sql string) (ast.Expression, error) {
	lexer := generated.NewSqlBaseLexer(antlr.NewInputStream(sql))
	stream := antlr.NewCommonTokenStream(lexer, 0)
	p := generated.NewSqlBaseParser(stream)
	tree := p.Expression()

	builder := &AstBuilder{}
	result := builder.Visit(tree)
	if result == nil {
		return nil, fmt.Errorf("failed to parse expression: %s", sql)
	}
	expr, ok := result.(ast.Expression)
	if !ok {
		return nil, fmt.Errorf("parse result is not an Expression: %T", result)
	}
	return expr, nil
}

// AstBuilder walks the ANTLR4 parse tree and produces Go AST nodes.
type AstBuilder struct{}

// Visit dispatches to the appropriate method based on the parse tree node type.
func (b *AstBuilder) Visit(tree antlr.ParseTree) any {
	return nil // Will be filled in incrementally as grammar rules are mapped
}
