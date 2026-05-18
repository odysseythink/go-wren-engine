package analyzer

// SessionContext holds session-level configuration.
type SessionContext struct {
	Catalog             string
	Schema              string
	EnableDynamicFields bool
}
