package dto

// DateSpine represents a date spine configuration.
type DateSpine struct {
	Unit       TimeUnit          `json:"unit"`
	Start      string            `json:"start"`
	End        string            `json:"end"`
	Properties map[string]string `json:"properties,omitempty"`
}

// DefaultDateSpine returns the default date spine.
func DefaultDateSpine() DateSpine {
	return DateSpine{
		Unit:  TimeUnitDay,
		Start: "1970-01-01",
		End:   "2077-12-31",
	}
}
