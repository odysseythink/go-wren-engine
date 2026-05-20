package rewrite

import (
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"
)

// orderedMap preserves insertion order. Replaces Java LinkedHashMap (risk #2).
type orderedMap struct {
	keys []string
	m    map[string]string
}

func newOrderedMap() *orderedMap { return &orderedMap{m: map[string]string{}} }
func (o *orderedMap) put(k, v string) {
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}
func (o *orderedMap) entries() []struct{ K, V string } {
	out := make([]struct{ K, V string }, len(o.keys))
	for i, k := range o.keys {
		out[i] = struct{ K, V string }{k, o.m[k]}
	}
	return out
}

// calculatedFieldRelationshipInfo mirrors RelationableSqlRender.CalculatedFieldRelationshipInfo.
type calculatedFieldRelationshipInfo struct {
	column                 *dto.Column
	expressionRelationship []*analyzer.ExpressionRelationshipInfo
	isAggregated           bool
}

func newCalculatedFieldRelationshipInfo(column *dto.Column, infos []*analyzer.ExpressionRelationshipInfo) *calculatedFieldRelationshipInfo {
	agg := false
	for _, info := range infos {
		for _, rel := range info.Relationships() {
			if dto.IsToMany(rel.JoinType) {
				agg = true
			}
		}
	}
	return &calculatedFieldRelationshipInfo{column: column, expressionRelationship: infos, isAggregated: agg}
}

func (c *calculatedFieldRelationshipInfo) alias() string { return c.column.Name }

// subQueryJoinInfo mirrors RelationableSqlRender.SubQueryJoinInfo.
type subQueryJoinInfo struct {
	sql           string
	subqueryAlias string
	joinCriteria  string
}

// relationableSqlRender holds the shared render state. Mirrors the abstract
// RelationableSqlRender fields; embedded by modelSqlRender.
type relationableSqlRender struct {
	relationable                        *dto.Model
	mdl                                 *mdl.WrenMDL
	refSql                              string
	requiredObjects                     map[string]bool
	selectItems                         []string
	calculatedRequiredRelationshipInfos []*calculatedFieldRelationshipInfo
	calculatedScopeSelectItems          *orderedMap
}

// getRelationableAlias mirrors RelationableSqlRender.getRelationableAlias.
func getRelationableAlias(baseModelName string) string { return baseModelName + "_relationsub" }
