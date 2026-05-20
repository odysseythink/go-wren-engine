package dto

import (
	"fmt"
	"strings"
)

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

// ToQualifiedName returns a qualified name string for the table reference.
func (t *TableReference) ToQualifiedName() string {
	if t.Catalog != "" && t.Schema != "" {
		return t.Catalog + "." + t.Schema + "." + t.Table
	}
	if t.Schema != "" {
		return t.Schema + "." + t.Table
	}
	return t.Table
}

// CacheInfo is implemented by objects that support caching.
type CacheInfo interface {
	IsCached() bool
}

// Relationable is implemented by objects that have columns.
type Relationable interface {
	GetColumns() []Column
}

// IntervalExpression returns the SQL INTERVAL literal for this unit.
// Mirrors Java io.wren.base.dto.TimeUnit.getIntervalExpression.
func (t TimeUnit) IntervalExpression() string {
	switch t {
	case TimeUnitYear:
		return "INTERVAL '1' YEAR"
	case TimeUnitQuarter:
		return "INTERVAL '3' MONTH"
	case TimeUnitMonth:
		return "INTERVAL '1' MONTH"
	case TimeUnitWeek:
		return "INTERVAL '7' DAY"
	case TimeUnitDay:
		return "INTERVAL '1' DAY"
	case TimeUnitHour:
		return "INTERVAL '1' HOUR"
	case TimeUnitMinute:
		return "INTERVAL '1' MINUTE"
	case TimeUnitSecond:
		return "INTERVAL '1' SECOND"
	default:
		return ""
	}
}

// ParseTimeUnit resolves a case-insensitive name to a TimeUnit.
// Mirrors Java TimeUnit.timeUnit.
func ParseTimeUnit(name string) (TimeUnit, error) {
	u := TimeUnit(strings.ToUpper(name))
	switch u {
	case TimeUnitYear, TimeUnitQuarter, TimeUnitMonth, TimeUnitWeek,
		TimeUnitDay, TimeUnitHour, TimeUnitMinute, TimeUnitSecond:
		return u, nil
	default:
		return "", fmt.Errorf("no enum constant TimeUnit.%s", name)
	}
}
