package dto

import "testing"

func TestTimeUnit_IntervalExpression(t *testing.T) {
	cases := map[TimeUnit]string{
		TimeUnitYear:    "INTERVAL '1' YEAR",
		TimeUnitQuarter: "INTERVAL '3' MONTH",
		TimeUnitMonth:   "INTERVAL '1' MONTH",
		TimeUnitWeek:    "INTERVAL '7' DAY",
		TimeUnitDay:     "INTERVAL '1' DAY",
		TimeUnitHour:    "INTERVAL '1' HOUR",
		TimeUnitMinute:  "INTERVAL '1' MINUTE",
		TimeUnitSecond:  "INTERVAL '1' SECOND",
	}
	for unit, want := range cases {
		if got := unit.IntervalExpression(); got != want {
			t.Errorf("%s.IntervalExpression() = %q, want %q", unit, got, want)
		}
	}
}

func TestParseTimeUnit(t *testing.T) {
	got, err := ParseTimeUnit("year")
	if err != nil || got != TimeUnitYear {
		t.Fatalf("ParseTimeUnit(year) = %v, %v; want YEAR, nil", got, err)
	}
	if _, err := ParseTimeUnit("decade"); err == nil {
		t.Fatal("ParseTimeUnit(decade): want error, got nil")
	}
}

func TestMetric_GetTimeGrain(t *testing.T) {
	m := Metric{TimeGrain: []TimeGrain{{Name: "orderdate", RefColumn: "orderdate"}}}
	tg, ok := m.GetTimeGrain("orderdate")
	if !ok || tg.RefColumn != "orderdate" {
		t.Fatalf("GetTimeGrain(orderdate) = %v, %v", tg, ok)
	}
	if _, ok := m.GetTimeGrain("missing"); ok {
		t.Fatal("GetTimeGrain(missing): want ok=false")
	}
}
