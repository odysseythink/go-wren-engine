package rewrite

import (
	"strings"
	"testing"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

func TestCreateDateSpineQuery(t *testing.T) {
	q, err := createDateSpineQuery(dto.DefaultDateSpine())
	if err != nil {
		t.Fatalf("createDateSpineQuery: %v", err)
	}
	got := formatter.FormatSQL(q)
	for _, want := range []string{"GENERATE_TIMESTAMP_ARRAY", "metric_time", "INTERVAL '1 DAY'"} {
		if !strings.Contains(got, want) {
			t.Errorf("date spine SQL missing %q:\n%s", want, got)
		}
	}
}

func TestDateSpineInfo(t *testing.T) {
	d, err := dateSpineInfoGet(dto.DefaultDateSpine())
	if err != nil {
		t.Fatalf("dateSpineInfoGet: %v", err)
	}
	if d.Name() != "date_spine" {
		t.Fatalf("Name() = %q, want date_spine", d.Name())
	}
	if d.RequiredObjects() != nil {
		t.Fatalf("RequiredObjects() = %v, want nil", d.RequiredObjects())
	}
}

func TestCumulativeMetricInfo(t *testing.T) {
	wrenMDL := metricMDL(t) // from metric_sql_render_test.go (same package)
	cm, _ := wrenMDL.GetCumulativeMetric("WeeklyRevenue")
	info, err := cumulativeMetricInfoGet(cm, wrenMDL)
	if err != nil {
		t.Fatalf("cumulativeMetricInfoGet: %v", err)
	}
	if info.Name() != "WeeklyRevenue" {
		t.Fatalf("Name() = %q, want WeeklyRevenue", info.Name())
	}
	if !contains(info.RequiredObjects(), "Orders") || !contains(info.RequiredObjects(), "date_spine") {
		t.Fatalf("RequiredObjects() = %v, want to contain Orders and date_spine", info.RequiredObjects())
	}
	got := formatter.FormatSQL(info.Query())
	for _, want := range []string{"date_trunc('WEEK'", `date_spine`, "group by 1", "order by 1"} {
		if !strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
			t.Errorf("cumulative metric SQL missing %q:\n%s", want, got)
		}
	}
}
