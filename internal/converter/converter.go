package converter

import "github.com/wren-engine/wren/internal/analyzer"

// SqlConverter converts SQL between dialects.
type SqlConverter interface {
	Convert(sql string, ctx *analyzer.SessionContext) string
}
