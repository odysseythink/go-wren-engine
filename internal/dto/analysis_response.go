package dto

type NodeLocationDto struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type QueryAnalysisDto struct {
	SelectItems     []ColumnAnalysisDto   `json:"selectItems"`
	Relation        *RelationAnalysisDto  `json:"relation,omitempty"`
	Filter          *FilterAnalysisDto    `json:"filter,omitempty"`
	GroupByKeys     [][]GroupByKeyDto     `json:"groupByKeys,omitempty"`
	Sortings        []SortItemAnalysisDto `json:"sortings,omitempty"`
	IsSubqueryOrCte bool                  `json:"isSubqueryOrCte"`
}

type ColumnAnalysisDto struct {
	// Alias is *string + omitempty so nil → absent (matches Java Jackson
	// class-level @JsonInclude(NON_NULL) skipping Optional.empty()).
	Alias *string `json:"alias,omitempty"`
	Expression string `json:"expression"`
	// Properties is always emitted (Java emits {} for an empty Map; class-level
	// NON_NULL only skips null, not empty). Mapper layer must replace nil with
	// an empty map before serialization — see risk #9.
	Properties   map[string]string `json:"properties"`
	NodeLocation *NodeLocationDto  `json:"nodeLocation,omitempty"`
	ExprSources  []ExprSourceDto   `json:"exprSources,omitempty"`
}

type RelationAnalysisDto struct {
	Type         string               `json:"type"`
	Alias        string               `json:"alias,omitempty"`
	Left         *RelationAnalysisDto `json:"left,omitempty"`
	Right        *RelationAnalysisDto `json:"right,omitempty"`
	Criteria     *JoinCriteriaDto     `json:"criteria,omitempty"`
	TableName    string               `json:"tableName,omitempty"`
	Body         []QueryAnalysisDto   `json:"body,omitempty"`
	ExprSources  []ExprSourceDto      `json:"exprSources,omitempty"`
	NodeLocation *NodeLocationDto     `json:"nodeLocation,omitempty"`
}

type JoinCriteriaDto struct {
	Expression   string           `json:"expression"`
	NodeLocation *NodeLocationDto `json:"nodeLocation,omitempty"`
}

type FilterAnalysisDto struct {
	Type         string            `json:"type"`
	Left         *FilterAnalysisDto `json:"left,omitempty"`
	Right        *FilterAnalysisDto `json:"right,omitempty"`
	Node         string            `json:"node,omitempty"`
	NodeLocation *NodeLocationDto  `json:"nodeLocation,omitempty"`
	ExprSources  []ExprSourceDto   `json:"exprSources,omitempty"`
}

type SortItemAnalysisDto struct {
	Expression   string           `json:"expression"`
	Ordering     string           `json:"ordering"`
	NodeLocation *NodeLocationDto `json:"nodeLocation,omitempty"`
	ExprSources  []ExprSourceDto  `json:"exprSources,omitempty"`
}

type GroupByKeyDto struct {
	Expression   string           `json:"expression"`
	NodeLocation *NodeLocationDto `json:"nodeLocation,omitempty"`
	ExprSources  []ExprSourceDto  `json:"exprSources,omitempty"`
}

type ExprSourceDto struct {
	Expression    string           `json:"expression"`
	SourceDataset string           `json:"sourceDataset"`
	SourceColumn  *string          `json:"sourceColumn,omitempty"`
	NodeLocation  *NodeLocationDto `json:"nodeLocation,omitempty"`
}
