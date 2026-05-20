package decisionpoint

import "github.com/wren-engine/wren/internal/parser/ast"

type ExprSource struct {
	Expression    string
	SourceDataset string
	SourceColumn  *string
	NodeLocation  *ast.NodeLocation
}
