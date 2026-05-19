package analyzer

import "github.com/wren-engine/wren/internal/dto"

// MetricRollupInfo describes a roll_up(metric, timeColumn, timeUnit) call.
// Mirrors Java io.wren.base.sqlrewrite.analyzer.MetricRollupInfo.
type MetricRollupInfo struct {
	Metric    *dto.Metric
	TimeGrain dto.TimeGrain
	TimeUnit  dto.TimeUnit // Java getDatePart()
}
