package dto

// View represents a semantic view.
type View struct {
	Name       string            `json:"name"`
	Statement  string            `json:"statement"`
	Properties map[string]string `json:"properties,omitempty"`
}
