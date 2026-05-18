package dto

// TimeGrain represents a time grain for metrics.
type TimeGrain struct {
	Name      string   `json:"name"`
	RefColumn string   `json:"refColumn"`
	DateParts []TimeUnit `json:"dateParts,omitempty"`
}

// Measure represents a measure in a cumulative metric.
type Measure struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Operator   string            `json:"operator"`
	RefColumn  string            `json:"refColumn"`
	Properties map[string]string `json:"properties,omitempty"`
}

// Window represents a time window for cumulative metrics.
type Window struct {
	Name       string            `json:"name"`
	RefColumn  string            `json:"refColumn"`
	TimeUnit   TimeUnit          `json:"timeUnit"`
	Start      string            `json:"start"`
	End        string            `json:"end"`
	Properties map[string]string `json:"properties,omitempty"`
}

// Metric represents a semantic metric.
type Metric struct {
	Name        string            `json:"name"`
	BaseObject  string            `json:"baseObject"`
	Dimension   []Column          `json:"dimension"`
	Measure     []Column          `json:"measure"`
	TimeGrain   []TimeGrain       `json:"timeGrain,omitempty"`
	Cached      bool              `json:"cached"`
	RefreshTime string            `json:"refreshTime,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
}

func (m Metric) IsCached() bool      { return m.Cached }
func (m Metric) GetColumns() []Column { return append(m.Dimension, m.Measure...) }
func (m Metric) GetBaseObject() string { return m.BaseObject }

// CumulativeMetric represents a cumulative metric.
type CumulativeMetric struct {
	Name        string            `json:"name"`
	BaseObject  string            `json:"baseObject"`
	Measure     Measure           `json:"measure"`
	Window      Window            `json:"window"`
	Cached      bool              `json:"cached"`
	RefreshTime string            `json:"refreshTime,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
}

func (c CumulativeMetric) IsCached() bool { return c.Cached }
