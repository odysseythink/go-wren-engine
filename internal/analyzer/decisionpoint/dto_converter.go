package decisionpoint

import (
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/parser/ast"
)

func (a *QueryAnalysis) ToDto() dto.QueryAnalysisDto {
	groupByKeys := toGroupKeyDtos(a.GroupByKeys)
	if groupByKeys == nil {
		groupByKeys = [][]dto.GroupByKeyDto{}
	}
	sortings := toSortDtos(a.Sortings)
	if sortings == nil {
		sortings = []dto.SortItemAnalysisDto{}
	}
	return dto.QueryAnalysisDto{
		SelectItems:     toColumnDtos(a.SelectItems),
		Relation:        toRelationDto(a.Relation),
		Filter:          toFilterDto(a.Filter),
		GroupByKeys:     groupByKeys,
		Sortings:        sortings,
		IsSubqueryOrCte: a.IsSubqueryOrCte,
	}
}

func toColumnDtos(items []ColumnAnalysis) []dto.ColumnAnalysisDto {
	out := make([]dto.ColumnAnalysisDto, len(items))
	for i, c := range items {
		// Risk #9: Properties must always be a non-nil map so JSON emits {}
		// rather than null, matching Java's empty-Map behavior.
		props := c.Properties
		if props == nil {
			props = map[string]string{}
		}
		out[i] = dto.ColumnAnalysisDto{
			Alias:        c.AliasName,
			Expression:   c.Expression,
			Properties:   props,
			NodeLocation: toLocDto(c.NodeLocation),
			ExprSources:  toSrcDtos(c.ExprSources),
		}
	}
	return out
}

func toRelationDto(r *RelationAnalysis) *dto.RelationAnalysisDto {
	if r == nil {
		return nil
	}
	var exprSources *[]dto.ExprSourceDto
	// Java only emits exprSources for JOIN relations; TABLE/SUBQUERY omit it.
	if r.Type == RelationTypeInnerJoin || r.Type == RelationTypeLeftJoin || r.Type == RelationTypeRightJoin ||
		r.Type == RelationTypeFullJoin || r.Type == RelationTypeCrossJoin || r.Type == RelationTypeImplicitJoin {
		srcs := toSrcDtos(r.ExprSources)
		exprSources = &srcs
	}
	var body *[]dto.QueryAnalysisDto
	if r.Type == RelationTypeSubquery {
		b := toQueryDtos(r.Body)
		body = &b
	}
	return &dto.RelationAnalysisDto{Type: string(r.Type), Alias: r.Alias, Left: toRelationDto(r.Left), Right: toRelationDto(r.Right), Criteria: toCriteriaDto(r.Criteria), TableName: r.TableName, Body: body, ExprSources: exprSources, NodeLocation: toLocDto(r.NodeLocation)}
}

func toCriteriaDto(c *JoinCriteria) *dto.JoinCriteriaDto {
	if c == nil {
		return nil
	}
	return &dto.JoinCriteriaDto{Expression: c.Expression, NodeLocation: toLocDto(c.NodeLocation)}
}

func toFilterDto(f *FilterAnalysis) *dto.FilterAnalysisDto {
	if f == nil {
		return nil
	}
	var exprSources *[]dto.ExprSourceDto
	// Java only emits exprSources for EXPR filters; AND/OR omit it.
	if f.Type == FilterTypeExpr {
		srcs := toSrcDtos(f.ExprSources)
		exprSources = &srcs
	}
	return &dto.FilterAnalysisDto{Type: string(f.Type), Left: toFilterDto(f.Left), Right: toFilterDto(f.Right), Node: f.Node, NodeLocation: toLocDto(f.NodeLocation), ExprSources: exprSources}
}

func toSortDtos(items []SortItemAnalysis) []dto.SortItemAnalysisDto {
	out := make([]dto.SortItemAnalysisDto, len(items))
	for i, s := range items {
		// Risk #3: emit Java's SortItem.Ordering.name() form ("ASCENDING" /
		// "DESCENDING"), not Go's ast.OrderingAsc/Desc constants ("ASC" /
		// "DESC").
		out[i] = dto.SortItemAnalysisDto{
			Expression:   s.Expression,
			Ordering:     orderingToJavaName(s.Ordering),
			NodeLocation: toLocDto(s.NodeLocation),
			ExprSources:  toSrcDtos(s.ExprSources),
		}
	}
	return out
}

func toGroupKeyDtos(groups [][]GroupByKey) [][]dto.GroupByKeyDto {
	out := make([][]dto.GroupByKeyDto, len(groups))
	for i, g := range groups {
		out[i] = make([]dto.GroupByKeyDto, len(g))
		for j, k := range g {
			out[i][j] = dto.GroupByKeyDto{Expression: k.Expression, NodeLocation: toLocDto(k.NodeLocation), ExprSources: toSrcDtos(k.ExprSources)}
		}
	}
	return out
}

func toSrcDtos(sources []ExprSource) []dto.ExprSourceDto {
	out := make([]dto.ExprSourceDto, len(sources))
	for i, s := range sources {
		out[i] = dto.ExprSourceDto{Expression: s.Expression, SourceDataset: s.SourceDataset, SourceColumn: s.SourceColumn, NodeLocation: toLocDto(s.NodeLocation)}
	}
	return out
}

func toQueryDtos(analyses []*QueryAnalysis) []dto.QueryAnalysisDto {
	out := make([]dto.QueryAnalysisDto, len(analyses))
	for i, a := range analyses {
		out[i] = a.ToDto()
	}
	return out
}

func toLocDto(loc *ast.NodeLocation) *dto.NodeLocationDto {
	if loc == nil {
		return nil
	}
	return &dto.NodeLocationDto{Line: loc.Line, Column: loc.CharPosition}
}
