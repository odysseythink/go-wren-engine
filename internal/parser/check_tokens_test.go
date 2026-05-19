package parser

import (
	"fmt"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	generated "github.com/wren-engine/wren/internal/parser/generated"
)

func TestTokens(t *testing.T) {
	for _, sql := range []string{"SELECT 1", "SELECT a", "SELECT a, b FROM t"} {
		lexer := generated.NewSqlBaseLexer(antlr.NewInputStream(sql))
		stream := antlr.NewCommonTokenStream(lexer, 0)
		stream.Fill()
		fmt.Printf("SQL: %s\n", sql)
		for i := 0; i < stream.Size(); i++ {
			tok := stream.Get(i)
			if tok.GetTokenType() == antlr.TokenEOF {
				fmt.Printf("  EOF\n")
				continue
			}
			names := lexer.GetSymbolicNames()
			name := "?"
			if tok.GetTokenType() < len(names) {
				name = names[tok.GetTokenType()]
			}
			fmt.Printf("  %s: %q\n", name, tok.GetText())
		}
		fmt.Println()
	}
}
