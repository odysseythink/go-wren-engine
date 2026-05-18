package rewrite

// QueryDescriptor describes a CTE to be generated.
type QueryDescriptor struct {
	Name     string
	SQL      string
	Requires []string
}
