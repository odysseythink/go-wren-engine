package dto

// JoinType represents the join cardinality between models.
type JoinType string

const (
	JoinTypeManyToMany JoinType = "MANY_TO_MANY"
	JoinTypeOneToOne   JoinType = "ONE_TO_ONE"
	JoinTypeManyToOne  JoinType = "MANY_TO_ONE"
	JoinTypeOneToMany  JoinType = "ONE_TO_MANY"
)

// Reverse returns the reversed join type.
func ReverseJoinType(j JoinType) JoinType {
	switch j {
	case JoinTypeOneToOne:
		return JoinTypeOneToOne
	case JoinTypeOneToMany:
		return JoinTypeManyToOne
	case JoinTypeManyToOne:
		return JoinTypeOneToMany
	default:
		return j
	}
}

// GenericJoinType returns the generic direction.
func GenericJoinType(j JoinType) string {
	switch j {
	case JoinTypeOneToOne, JoinTypeManyToOne:
		return "TO_ONE"
	case JoinTypeOneToMany, JoinTypeManyToMany:
		return "TO_MANY"
	default:
		return "TO_MANY"
	}
}

// SortKey represents a sort key for the many side of a relationship.
type SortKey struct {
	Name        string `json:"name"`
	Ordering    string `json:"ordering,omitempty"`
	IsDescending bool  `json:"isDescending"`
}

// Relationship represents a relationship between models.
type Relationship struct {
	Name             string            `json:"name"`
	Models           []string          `json:"models"`
	JoinType         JoinType          `json:"joinType"`
	Condition        string            `json:"condition"`
	ManySideSortKeys []SortKey         `json:"manySideSortKeys,omitempty"`
	Properties       map[string]string `json:"properties,omitempty"`
	IsReverse        bool              `json:"isReverse,omitempty"`
}
