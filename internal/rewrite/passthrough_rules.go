package rewrite

import (
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// GenerateViewRewrite is a P3a pass-through stub for Java GenerateViewRewrite.
// Real implementation lands in P3c.
type GenerateViewRewrite struct{}

func (r *GenerateViewRewrite) Apply(root ast.Statement, _ *base.SessionContext, _ *mdl.AnalyzedMDL) (ast.Statement, error) {
	return root, nil
}
