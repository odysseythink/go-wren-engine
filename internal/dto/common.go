package dto

// TimeUnit represents a time unit for date spine and time grains.
type TimeUnit string

const (
	TimeUnitYear    TimeUnit = "YEAR"
	TimeUnitQuarter TimeUnit = "QUARTER"
	TimeUnitMonth   TimeUnit = "MONTH"
	TimeUnitWeek    TimeUnit = "WEEK"
	TimeUnitDay     TimeUnit = "DAY"
	TimeUnitHour    TimeUnit = "HOUR"
	TimeUnitMinute  TimeUnit = "MINUTE"
	TimeUnitSecond  TimeUnit = "SECOND"
)

// TableReference represents a reference to a physical table.
type TableReference struct {
	Catalog string `json:"catalog,omitempty"`
	Schema  string `json:"schema,omitempty"`
	Table   string `json:"table"`
}

// CacheInfo is implemented by objects that support caching.
type CacheInfo interface {
	IsCached() bool
}

// Relationable is implemented by objects that have columns.
type Relationable interface {
	GetColumns() []Column
}
