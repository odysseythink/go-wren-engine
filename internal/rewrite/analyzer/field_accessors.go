package analyzer

import "github.com/wren-engine/wren/internal/parser/ast"

func (f *Field) SourceDatasetName() *string {
	return f.sourceDatasetName
}

func (f *Field) RelationAlias() *ast.QualifiedName {
	return f.relationAlias
}
