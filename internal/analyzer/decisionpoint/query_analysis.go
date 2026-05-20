package decisionpoint

import "github.com/wren-engine/wren/internal/parser/ast"

type QueryAnalysis struct {
	SelectItems     []ColumnAnalysis
	Relation        *RelationAnalysis
	Filter          *FilterAnalysis
	GroupByKeys     [][]GroupByKey
	Sortings        []SortItemAnalysis
	IsSubqueryOrCte bool
}

type ColumnAnalysis struct {
	AliasName    *string
	Expression   string
	Properties   map[string]string
	NodeLocation *ast.NodeLocation
	ExprSources  []ExprSource
}

type SortItemAnalysis struct {
	Expression   string
	Ordering     ast.Ordering
	NodeLocation *ast.NodeLocation
	ExprSources  []ExprSource
}

type GroupByKey struct {
	Expression   string
	NodeLocation *ast.NodeLocation
	ExprSources  []ExprSource
}

type QueryAnalysisBuilder struct {
	selectItems     []ColumnAnalysis
	relation        *RelationAnalysis
	filter          *FilterAnalysis
	groupByKeys     [][]GroupByKey
	sortings        []SortItemAnalysis
	isSubqueryOrCte bool
}

func NewQueryAnalysisBuilder() *QueryAnalysisBuilder { return &QueryAnalysisBuilder{} }

func (b *QueryAnalysisBuilder) AddSelectItem(item ColumnAnalysis) *QueryAnalysisBuilder {
	b.selectItems = append(b.selectItems, item)
	return b
}
func (b *QueryAnalysisBuilder) SetRelation(r *RelationAnalysis) *QueryAnalysisBuilder {
	b.relation = r
	return b
}
func (b *QueryAnalysisBuilder) SetFilter(f *FilterAnalysis) *QueryAnalysisBuilder {
	b.filter = f
	return b
}
func (b *QueryAnalysisBuilder) SetGroupByKeys(keys [][]GroupByKey) *QueryAnalysisBuilder {
	b.groupByKeys = keys
	return b
}
func (b *QueryAnalysisBuilder) SetSortings(s []SortItemAnalysis) *QueryAnalysisBuilder {
	b.sortings = s
	return b
}
func (b *QueryAnalysisBuilder) SetSubqueryOrCte(v bool) *QueryAnalysisBuilder {
	b.isSubqueryOrCte = v
	return b
}
func (b *QueryAnalysisBuilder) GetSelectItems() []ColumnAnalysis { return b.selectItems }
func (b *QueryAnalysisBuilder) Build() *QueryAnalysis {
	return &QueryAnalysis{
		SelectItems: b.selectItems, Relation: b.relation, Filter: b.filter,
		GroupByKeys: b.groupByKeys, Sortings: b.sortings, IsSubqueryOrCte: b.isSubqueryOrCte,
	}
}
