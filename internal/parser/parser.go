package parser

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/wren-engine/wren/internal/parser/ast"
	generated "github.com/wren-engine/wren/internal/parser/generated"
)

// caseInsensitiveStream wraps an antlr.InputStream and uppercases characters
// returned by LA(), making the lexer treat lowercase letters as uppercase.
// String literal text remains unchanged because GetText() reads from the
// original underlying data.
type caseInsensitiveStream struct {
	*antlr.InputStream
}

func newCaseInsensitiveStream(data string) *caseInsensitiveStream {
	return &caseInsensitiveStream{
		InputStream: antlr.NewInputStream(data),
	}
}

func (c *caseInsensitiveStream) LA(offset int) int {
	r := c.InputStream.LA(offset)
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}

// ParseSQL parses a SQL string into an AST Statement.
func ParseSQL(sql string) (ast.Statement, error) {
	lexer := generated.NewSqlBaseLexer(newCaseInsensitiveStream(sql))
	stream := antlr.NewCommonTokenStream(lexer, 0)
	p := generated.NewSqlBaseParser(stream)
	tree := p.SingleStatement()

	builder := &AstBuilder{BaseSqlBaseVisitor: &generated.BaseSqlBaseVisitor{}}
	result := tree.Accept(builder)
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
	lexer := generated.NewSqlBaseLexer(newCaseInsensitiveStream(sql))
	stream := antlr.NewCommonTokenStream(lexer, 0)
	p := generated.NewSqlBaseParser(stream)
	tree := p.StandaloneExpression()

	builder := &AstBuilder{BaseSqlBaseVisitor: &generated.BaseSqlBaseVisitor{}}
	result := tree.Accept(builder)
	if result == nil {
		return nil, fmt.Errorf("failed to parse expression: %s", sql)
	}
	expr, ok := result.(ast.Expression)
	if !ok {
		return nil, fmt.Errorf("parse result is not an Expression: %T", result)
	}
	return expr, nil
}
