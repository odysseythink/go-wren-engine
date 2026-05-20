package decisionpoint

import "github.com/wren-engine/wren/internal/parser/ast"

type RelationType string

const (
	RelationTypeTable        RelationType = "TABLE"
	RelationTypeSubquery     RelationType = "SUBQUERY"
	RelationTypeInnerJoin    RelationType = "INNER_JOIN"
	RelationTypeLeftJoin     RelationType = "LEFT_JOIN"
	RelationTypeRightJoin    RelationType = "RIGHT_JOIN"
	RelationTypeFullJoin     RelationType = "FULL_JOIN"
	RelationTypeCrossJoin    RelationType = "CROSS_JOIN"
	RelationTypeImplicitJoin RelationType = "IMPLICIT_JOIN"
)

type RelationAnalysis struct {
	Type         RelationType
	Alias        string
	NodeLocation *ast.NodeLocation
	TableName    string
	Left         *RelationAnalysis
	Right        *RelationAnalysis
	Criteria     *JoinCriteria
	ExprSources  []ExprSource
	Body         []*QueryAnalysis
}

type JoinCriteria struct {
	Expression   string
	NodeLocation *ast.NodeLocation
}

func NewTableRelation(tableName, alias string, loc *ast.NodeLocation) *RelationAnalysis {
	return &RelationAnalysis{Type: RelationTypeTable, TableName: tableName, Alias: alias, NodeLocation: loc}
}
func NewJoinRelation(joinType RelationType, alias string, left, right *RelationAnalysis, criteria *JoinCriteria, exprSources []ExprSource, loc *ast.NodeLocation) *RelationAnalysis {
	return &RelationAnalysis{Type: joinType, Alias: alias, Left: left, Right: right, Criteria: criteria, ExprSources: exprSources, NodeLocation: loc}
}
func NewSubqueryRelation(alias string, body []*QueryAnalysis, loc *ast.NodeLocation) *RelationAnalysis {
	return &RelationAnalysis{Type: RelationTypeSubquery, Alias: alias, Body: body, NodeLocation: loc}
}
