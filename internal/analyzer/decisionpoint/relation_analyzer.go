package decisionpoint

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/parser/formatter"
	rewriteAnalyzer "github.com/wren-engine/wren/internal/rewrite/analyzer"
)

func AnalyzeRelation(relation ast.Relation, sessionContext *analyzer.SessionContext, wrenMDL *mdl.WrenMDL, analysis *rewriteAnalyzer.Analysis) *RelationAnalysis {
	return analyzeRelationNode(relation, sessionContext, wrenMDL, analysis)
}

func analyzeRelationNode(node ast.Node, sessionContext *analyzer.SessionContext, wrenMDL *mdl.WrenMDL, analysis *rewriteAnalyzer.Analysis) *RelationAnalysis {
	switch n := node.(type) {
	case *ast.Table:
		return NewTableRelation(n.Name.String(), "", n.GetLocation())
	case *ast.Join:
		left := analyzeRelationNode(n.Left, sessionContext, wrenMDL, analysis)
		right := analyzeRelationNode(n.Right, sessionContext, wrenMDL, analysis)
		scope := analysis.GetScope(n)
		var exprSources []ExprSource
		var criteria *JoinCriteria
		if n.Criteria != nil {
			exprSources = analyzeJoinCriteria(n.Criteria, scope)
			criteria, _ = formatJoinCriteria(n.Criteria)
		}
		joinType := RelationType(fmt.Sprintf("%s_JOIN", n.JoinType))
		return NewJoinRelation(joinType, "", left, right, criteria, exprSources, n.GetLocation())
	case *ast.AliasedRelation:
		inner := analyzeRelationNode(n.Relation, sessionContext, wrenMDL, analysis)
		alias := ""
		if n.Alias != nil {
			alias = n.Alias.Value
		}
		switch inner.Type {
		case RelationTypeTable:
			return NewTableRelation(inner.TableName, alias, n.GetLocation())
		case RelationTypeSubquery:
			return NewSubqueryRelation(alias, inner.Body, n.GetLocation())
		default:
			return &RelationAnalysis{Type: inner.Type, Alias: alias, Left: inner.Left, Right: inner.Right, Criteria: inner.Criteria, ExprSources: inner.ExprSources, NodeLocation: n.GetLocation()}
		}
	case *ast.TableSubquery:
		// Forward-reference into slice 4's Analyze. Allowed in Go because we
		// share package `decisionpoint`. Compiling slice 2 alone (before
		// slice 4) will fail with `undefined: Analyze` — expected; finish
		// slice 4 before re-running this package's tests.
		queries := Analyze(n.Query, sessionContext, wrenMDL)
		// Force isSubqueryOrCte=true on every returned QueryAnalysis (Java
		// QueryAnalysis.Builder.from(...).setSubqueryOrCte(true).build()).
		for i, q := range queries {
			b := NewQueryAnalysisBuilder()
			b.selectItems = q.SelectItems
			b.relation = q.Relation
			b.filter = q.Filter
			b.groupByKeys = q.GroupByKeys
			b.sortings = q.Sortings
			b.isSubqueryOrCte = true
			queries[i] = b.Build()
		}
		return NewSubqueryRelation("", queries, n.GetLocation())
	case *ast.QuerySpecification:
		// Java RelationAnalyzer.visitQuerySpecification falls through to
		// super.visitQuerySpecification (a no-op for relations). We mirror by
		// returning nil and letting the outer DP visitor handle it.
		return nil
	default:
		// Mirror Java throw UnsupportedOperationException for unsupported
		// relation types — never silently return nil (would let bad input
		// produce empty Relation in DTO).
		panic(fmt.Sprintf("Analyze %T is not supported yet", n))
	}
}

func analyzeJoinCriteria(criteria ast.JoinCriteria, scope *rewriteAnalyzer.Scope) []ExprSource {
	switch c := criteria.(type) {
	case *ast.JoinOn:
		return expressionSourceAnalyze(c.Expression, scope)
	case *ast.JoinUsing:
		var out []ExprSource
		for i := range c.Columns {
			out = append(out, expressionSourceAnalyze(&c.Columns[i], scope)...)
		}
		return out
	case *ast.NaturalJoin:
		return nil
	default:
		return nil
	}
}

func formatJoinCriteria(criteria ast.JoinCriteria) (*JoinCriteria, *ast.NodeLocation) {
	switch c := criteria.(type) {
	case *ast.JoinOn:
		expr := formatter.FormatExpression(c.Expression)
		loc := ExpressionLocationAnalyze(c.Expression)
		return &JoinCriteria{Expression: "ON " + expr, NodeLocation: loc}, loc
	case *ast.JoinUsing:
		cols := make([]string, len(c.Columns))
		for i := range c.Columns {
			cols[i] = c.Columns[i].Value
		}
		expr := "USING (" + strings.Join(cols, ", ") + ")"
		var loc *ast.NodeLocation
		if len(c.Columns) > 0 {
			loc = c.Columns[0].GetLocation()
		}
		return &JoinCriteria{Expression: expr, NodeLocation: loc}, loc
	case *ast.NaturalJoin:
		return nil, nil
	default:
		return nil, nil
	}
}

func expressionSourceAnalyze(expr ast.Expression, scope *rewriteAnalyzer.Scope) []ExprSource {
	if expr == nil || scope == nil {
		return nil
	}
	v := &exprSourceVisitor{scope: scope}
	v.walk(expr)
	return sortAndDedupExprSources(v.sources)
}

// sortAndDedupExprSources normalizes the ExprSource slice — risk #2. Java
// builds a HashSet then ImmutableList.copyOf; the resulting list order depends
// on JVM HashSet hash distribution and is not reproducible across runtimes.
// We dedup (Expression, SourceDataset, SourceColumn, line, column) tuples and
// sort by (line, column, expression) so Go and Java goldens (both normalized
// in capture-golden) line up.
func sortAndDedupExprSources(in []ExprSource) []ExprSource {
	if len(in) == 0 {
		return nil
	}
	type key struct {
		expr, ds, col string
		line, column  int
	}
	seen := map[key]bool{}
	out := make([]ExprSource, 0, len(in))
	for _, s := range in {
		var line, col int
		if s.NodeLocation != nil {
			line, col = s.NodeLocation.Line, s.NodeLocation.CharPosition
		}
		var srcCol string
		if s.SourceColumn != nil {
			srcCol = *s.SourceColumn
		}
		k := key{s.Expression, s.SourceDataset, srcCol, line, col}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		li, lj := 0, 0
		ci, cj := 0, 0
		if out[i].NodeLocation != nil {
			li, ci = out[i].NodeLocation.Line, out[i].NodeLocation.CharPosition
		}
		if out[j].NodeLocation != nil {
			lj, cj = out[j].NodeLocation.Line, out[j].NodeLocation.CharPosition
		}
		if li != lj {
			return li < lj
		}
		if ci != cj {
			return ci < cj
		}
		return out[i].Expression < out[j].Expression
	})
	return out
}

type exprSourceVisitor struct {
	scope   *rewriteAnalyzer.Scope
	sources []ExprSource
}

func (v *exprSourceVisitor) walk(node ast.Node) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *ast.Identifier:
		qn := &ast.QualifiedName{Parts: []string{n.Value}}
		for _, f := range v.scope.RelationType().ResolveFields(qn) {
			v.sources = append(v.sources, ExprSource{Expression: n.Value, SourceDataset: safeString(f.SourceDatasetName()), SourceColumn: safeColumnName(f.SourceColumn()), NodeLocation: n.GetLocation()})
		}
	case *ast.DereferenceExpression:
		qn := ast.GetQualifiedName(n)
		if qn != nil {
			for _, f := range v.scope.RelationType().ResolveFields(qn) {
				v.sources = append(v.sources, ExprSource{Expression: qn.String(), SourceDataset: safeString(f.SourceDatasetName()), SourceColumn: safeColumnName(f.SourceColumn()), NodeLocation: n.GetLocation()})
			}
		} else {
			v.walk(n.Base)
		}
	default:
		for _, child := range node.GetChildren() {
			v.walk(child)
		}
	}
}

func safeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func safeColumnName(c *dto.Column) *string {
	if c == nil {
		return nil
	}
	return &c.Name
}
