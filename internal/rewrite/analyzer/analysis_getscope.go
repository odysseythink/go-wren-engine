package analyzer

import (
	"fmt"
	"github.com/wren-engine/wren/internal/parser/ast"
)

func (a *Analysis) GetScope(n ast.Node) *Scope {
	s, ok := a.TryGetScope(n)
	if !ok {
		panic(fmt.Sprintf("Analysis does not contain information for node: %T", n))
	}
	return s
}
