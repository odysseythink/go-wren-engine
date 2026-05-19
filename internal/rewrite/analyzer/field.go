package analyzer

import (
	"strings"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// Field is one resolvable column in a RelationType. Mirrors analyzer.Field.
type Field struct {
	relationAlias     *ast.QualifiedName
	tableName         CatalogSchemaTableName
	columnName        string
	name              *string
	sourceDatasetName *string
	sourceColumn      *dto.Column
}

func (f *Field) TableName() CatalogSchemaTableName    { return f.tableName }
func (f *Field) ColumnName() string                   { return f.columnName }
func (f *Field) Name() *string                        { return f.name }
func (f *Field) SourceColumn() *dto.Column            { return f.sourceColumn }

// MatchesPrefix mirrors Field.matchesPrefix: empty prefix matches; otherwise
// the relation alias (or table name) must have the prefix as a suffix.
func (f *Field) MatchesPrefix(prefix *ast.QualifiedName) bool {
	if prefix == nil {
		return true
	}
	scope := f.relationAlias
	if scope == nil {
		qn := tableNameToQualifiedName(f.tableName)
		scope = &qn
	}
	return hasSuffix(*scope, *prefix)
}

// CanResolve mirrors Field.canResolve.
func (f *Field) CanResolve(name *ast.QualifiedName) bool {
	if name == nil || f.name == nil {
		return false
	}
	if f.MatchesPrefix(qualifiedNamePrefix(name)) &&
		strings.EqualFold(*f.name, qualifiedNameSuffix(name)) {
		return true
	}
	if p := qualifiedNamePrefix(name); p != nil && p.String() == f.columnName {
		return true // struct type support
	}
	return false
}
