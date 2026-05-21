# Go Wren Engine Rewrite Design

**Date**: 2026-05-18
**Status**: Approved
**Source**: `ghcr.io/canner/wren-engine:0.9.3` (Java, source at `D:\workspace\kb_work\wren-engine-0.9.3`)
**Target**: `D:\workspace\kb_work\go-wren-engine` (Go, from scratch)

## 1. Overview

Wren Engine is a semantic SQL engine for LLMs. It accepts SQL queries combined with a Modeling Definition Language (MDL) manifest, semantically rewrites the SQL based on model/metric/view definitions, and executes the rewritten SQL against data sources (DuckDB, PostgreSQL).

This document specifies a 100% Go rewrite of the core Java engine modules (trino-parser, wren-base, wren-main, wren-engine), excluding the Python ibis-server and Rust wren-modeling-rs components.

### Key Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| SQL Parser | ANTLR4 Go target + focused AST (~45 node types) | Best balance of Trino SQL compatibility and implementation effort |
| Data Sources | DuckDB + PostgreSQL | User requirement; DuckDB is primary |
| API Compatibility | Full REST API compatibility with Java version | Existing clients (Wren AI) can switch without changes |
| Macro/Jinja | Yes, implement via gonja | Required for MDL macro expression rendering |
| DI Framework | None (explicit Go constructor injection) | Go idiom; no Guice equivalent needed |
| HTTP Framework | chi router + net/http | Lightweight, stdlib-compatible |

## 2. Project Structure

```
go-wren-engine/
├── cmd/
│   └── wren-engine/              # Server entry point (main.go)
├── internal/
│   ├── parser/                   # ANTLR4-generated Go parser + focused AST
│   │   ├── generated/            # ANTLR4 Go output (lexer, parser, listener)
│   │   ├── ast/                  # Hand-written Go AST node types (~45)
│   │   ├── visitor/              # AST visitor interface & base visitor
│   │   ├── formatter/            # SQL formatter (AST → SQL string)
│   │   └── parser.go             # Public API: ParseSQL(sql) → Statement
│   ├── dto/                      # Data Transfer Objects
│   ├── mdl/                      # WrenMDL, AnalyzedMDL, macro/Jinja rendering
│   ├── rewrite/                  # SQL rewrite engine
│   │   ├── planner.go            # WrenPlanner (orchestrates rewrite rules)
│   │   ├── rule.go               # WrenRule interface
│   │   ├── view_rewrite.go       # GenerateViewRewrite
│   │   ├── metric_rollup.go      # MetricRollupRewrite
│   │   ├── wren_sql.go           # WrenSqlRewrite (model/metric → CTE)
│   │   ├── enum_rewrite.go       # EnumRewrite
│   │   ├── with_rewriter.go      # WithRewriter (adds CTEs)
│   │   └── descriptor.go         # QueryDescriptor, RelationInfo, etc.
│   ├── analyzer/                 # SQL analysis
│   │   ├── statement.go          # StatementAnalyzer
│   │   ├── expression.go         # ExpressionAnalyzer
│   │   ├── scope.go              # Scope, ScopeAnalyzer
│   │   ├── decisionpoint/        # DecisionPointAnalyzer (for /v1/analysis, /v2/analysis)
│   │   └── analysis.go           # Analysis struct
│   ├── connector/                # Data source connectors
│   │   ├── connector.go          # Client interface
│   │   ├── duckdb/               # DuckDB connector (using go-duckdb)
│   │   └── postgres/             # PostgreSQL connector (using pgx)
│   ├── converter/                # SQL dialect converters
│   │   ├── converter.go          # SqlConverter interface
│   │   ├── duckdb.go             # DuckDBSqlConverter (Trino SQL → DuckDB dialect)
│   │   └── postgres.go           # PostgreSQL converter
│   ├── config/                   # Configuration management
│   ├── service/                  # Business services
│   │   ├── preview.go            # PreviewService
│   │   └── validation.go         # ValidationService
│   └── server/                   # HTTP server & API handlers
│       ├── server.go             # Server setup
│       ├── mdl_handler.go        # /v1/mdl, /v2/mdl endpoints
│       ├── analysis_handler.go   # /v1/analysis, /v2/analysis endpoints
│       ├── duckdb_handler.go     # /v1/data-source/duckdb endpoints
│       └── config_handler.go     # /v1/config endpoints
├── go.mod
├── go.sum
└── Makefile
```

## 3. SQL Parser & AST Layer

### 3.1 ANTLR4 Pipeline

1. Port the existing `SqlBase.g4` grammar from `trino-parser/src/main/antlr4/` to Go via ANTLR4's Go target
2. ANTLR4 produces Go lexer (`SqlBaseLexer`) and parser (`SqlBaseParser`) in `internal/parser/generated/`
3. Hand-write an `AstBuilder` (Go) that walks the ANTLR4 parse tree and produces the focused Go AST — same pattern as Java's `AstBuilder.java`

### 3.2 Focused AST Node Types

Only ~45 key node types needed by the rewrite engine (vs 235 in full Trino parser):

**Statements** (~5):
- `Statement`, `Query`, `QuerySpecification`, `Insert`, `CreateTableAsSelect`

**Relations** (~8):
- `Table`, `AliasedRelation`, `Join`, `JoinOn`, `JoinUsing`, `TableSubquery`, `Unnest`, `Values`

**Expressions** (~15):
- `Expression`, `DereferenceExpression`, `Identifier`, `QualifiedName`
- `Literal` (Long, String, Double, Boolean, Null)
- `ComparisonExpression`, `ArithmeticBinaryExpression`, `LogicalBinaryExpression`
- `FunctionCall`, `Cast`, `CoalesceExpression`, `InPredicate`, `BetweenPredicate`, `AtTimeZone`, `SubqueryExpression`

**Query parts** (~8):
- `Select`, `SelectItem`, `SingleColumn`, `AllColumns`, `With`, `WithQuery`, `SortItem`, `Window`

**DDL/misc** (~5):
- `ColumnDefinition`, `DataType`, `Node`, `NodeLocation`, `NodeRef`

### 3.3 Visitor Pattern

```go
// AstVisitor interface with double-dispatch
type AstVisitor interface {
    Visit(node Node) any
    VisitTable(node *Table) any
    VisitQuery(node *Query) any
    VisitDereferenceExpression(node *DereferenceExpression) any
    // ... all node visit methods
}

// BaseVisitor provides default traversal (visits all children)
type BaseVisitor struct{}

// Rewriters extend BaseVisitor and override specific visit methods
```

### 3.4 SQL Formatter

Port the `SqlFormatter` from Trino to Go. Converts AST back to SQL string. Supports:
- Standard SQL output (default)
- DuckDB dialect output (e.g., `ARRAY[1,2,3]` → `array_value(1,2,3)`)

### 3.5 Public Parser API

```go
func ParseSQL(sql string) (Statement, error)
func FormatSQL(statement Statement) string
func FormatSQLDialect(statement Statement, dialect Dialect) string
```

## 4. MDL (Modeling Definition Language)

### 4.1 DTOs

Port all DTOs from `wren-base/dto/` with JSON struct tags matching the Java field names:

- `Manifest` — Top-level container: catalog, schema, models, relationships, enumDefinitions, metrics, cumulativeMetrics, views, macros, dateSpine
- `Model` — name, refSql, baseObject, tableReference, columns, primaryKey, isCached, refreshTime, properties
- `Metric` — name, baseObject, dimension, measure, timeGrain, isCached, refreshTime, properties
- `CumulativeMetric` — name, measure, window, baseObject
- `View` — name, sql
- `Column` — name, type, relationship, isCalculated, isNotNull, expression, properties
- `Relationship` — name, models, joinType, condition
- `EnumDefinition` — name, values
- `EnumValue` — name, value
- `Macro` — name, parameters, returnType, expression
- `DateSpine` — unit, startDate, endDate
- `TimeGrain` — name, refColumn, timeUnit, refColumnTimeGrain
- `Measure` — name, type, expression
- `Window` — name, refColumn
- `JoinType` — enum (ONE_TO_ONE, ONE_TO_MANY, MANY_TO_ONE, MANY_TO_MANY)
- `TableReference` — catalog, schema, table

### 4.2 WrenMDL

Central MDL representation, ported from `wren-base/WrenMDL.java`:

```go
type WrenMDL struct {
    catalog            string
    schema             string
    manifest           *Manifest
    models             map[string]*Model
    metrics            map[string]*Metric
    cumulativeMetrics  map[string]*CumulativeMetric
    relationships      map[string]*Relationship
}

func WrenMDLFromManifest(manifest *Manifest) *WrenMDL
func WrenMDLFromJSON(jsonStr string) (*WrenMDL, error)
```

Key behaviors:
- Parse from JSON using `encoding/json`
- Index models/metrics/relationships by name for O(1) lookup
- Macro/Jinja rendering: process macro expressions in column definitions before SQL rewrite using gonja

### 4.3 Macro/Jinja Rendering

Use [gonja](https://github.com/noirbizarre/gonja) (Go Jinja2 template engine) to replicate Java's Jinjava behavior:

1. Collect macro definitions from the manifest
2. For each column with an expression, render Jinja templates (macro tags + expression)
3. Replace column expressions with rendered results in the processed manifest

### 4.4 AnalyzedMDL

```go
type AnalyzedMDL struct {
    wrenMDL         *WrenMDL
    wrenDataLineage *WrenDataLineage  // column-level dependency tracking
}
```

## 5. SQL Rewrite Engine

### 5.1 Data Flow

```
Input SQL
    ↓ ParseSQL()
AST Statement
    ↓ WrenPlanner.rewrite(sql, sessionContext, analyzedMDL)
Rewritten SQL
    ↓ SqlConverter.convert(plannedSQL, sessionContext)
Dialect-specific SQL (DuckDB/PostgreSQL)
    ↓ connector.Query()
Query Results
```

### 5.2 WrenPlanner

Applies 4 rewrite rules sequentially. Between each rule, the AST is formatted to SQL and re-parsed to avoid inter-rule interference:

```go
var AllRules = []WrenRule{
    GenerateViewRewrite,
    MetricRollupRewrite,
    WrenSqlRewrite,
    EnumRewrite,
}

func Rewrite(sql string, ctx *SessionContext, mdl *AnalyzedMDL) string {
    stmt := ParseSQL(sql)
    for _, rule := range AllRules {
        sql = FormatSQL(stmt)
        stmt = rule.Apply(ParseSQL(sql), ctx, mdl)
    }
    return FormatSQL(stmt)
}
```

### 5.3 Rewrite Rules

**GenerateViewRewrite**: Expands View references into CTEs. Builds a DAG of view dependencies and topologically orders them using `dominikbraun/graph`.

**MetricRollupRewrite**: Handles metric time-grain rollups. Rewrites cumulative metric references with appropriate time windowing.

**WrenSqlRewrite**: The main rule. Core logic:
1. `StatementAnalyzer` analyzes which tables/models/metrics are referenced and collects column usage
2. Builds `QueryDescriptor` for each referenced object (RelationInfo for models, MetricRollupInfo for metrics, CumulativeMetricInfo for cumulative metrics)
3. Builds DAG of dependencies between objects (models referencing other models via relationships)
4. Topologically sorts descriptors
5. `WithRewriter` prepends CTEs (one per descriptor) to the query's WITH clause
6. `Rewriter` (extends BaseVisitor) replaces Table nodes with CTE names, strips catalog/schema prefixes from DereferenceExpressions

**EnumRewrite**: Replaces enum column comparisons with `IN (...)` expressions based on EnumDefinition values.

### 5.4 Analyzer Subsystem

Used by rewrite rules to understand the query structure:

- **StatementAnalyzer**: Walks AST to find referenced tables, models, metrics, views; collects column references; identifies scope boundaries
- **ExpressionAnalyzer**: Analyzes expression types and relationships
- **ScopeAnalyzer**: Manages scoping for subqueries and CTEs
- **DecisionPointAnalyzer**: For the analysis API endpoints (separate from rewrite). Provides column/relation/filter analysis for AI consumption

### 5.5 DAG Dependency

Uses `github.com/dominikbraun/graph` for:
- Topological sorting of model/metric/view dependencies
- Cycle detection (returns error if circular dependencies found)

## 6. Connectors

### 6.1 Client Interface

```go
type Client interface {
    Query(ctx context.Context, sql string) (RecordIterator, error)
    QueryWithParams(ctx context.Context, sql string, params []Parameter) (RecordIterator, error)
    Describe(ctx context.Context, sql string) ([]Column, error)
    ExecuteDDL(ctx context.Context, sql string) error
    Close() error
}

type RecordIterator interface {
    Next() bool
    Get() []any
    Columns() []Column
    Close() error
}

// Metadata provides query execution on the underlying data source.
// Implemented by connector packages (duckdb, postgres).
type Metadata interface {
    DirectQuery(ctx context.Context, sql string, params []Parameter) (RecordIterator, error)
    DescribeQuery(ctx context.Context, sql string, params []Parameter) ([]Column, error)
    DirectDDL(ctx context.Context, sql string) error
}
```

### 6.2 DuckDB Connector

- Uses `github.com/marcboeker/go-duckdb` driver
- Connection pool via `database/sql.DB`
- Supports init SQL, session SQL, memory limit, temp directory settings
- Implements DuckDB-specific SQL rewrites (array syntax, function mapping)

### 6.3 PostgreSQL Connector

- Uses `github.com/jackc/pgx/v5` driver
- Connection pool via `pgxpool`
- Standard PostgreSQL SQL output from converter

## 7. SQL Dialect Converters

### 7.1 Converter Interface

```go
type SqlConverter interface {
    Convert(sql string, ctx *SessionContext) string
}
```

### 7.2 DuckDBSqlConverter

Applies DuckDB-specific SQL rewrites after the semantic rewrite:
1. Parse SQL into AST
2. Apply `RewriteArray` (Trino `ARRAY[1,2,3][1]` → DuckDB `array_value(1,2,3)[1]`)
3. Apply `RewriteFunction` (Trino function names → DuckDB equivalents)
4. Format AST with DuckDB dialect

### 7.3 PostgresSqlConverter

Applies PostgreSQL-specific SQL rewrites:
1. Parse SQL into AST
2. Rewrite Trino-specific syntax to PostgreSQL equivalents
3. Format AST with PostgreSQL dialect

## 8. Services

### 8.1 PreviewService

```go
type PreviewService struct {
    metadata     Metadata
    sqlConverter SqlConverter
    configMgr    *ConfigManager
    sem           *semaphore.Weighted  // bounded concurrency via golang.org/x/sync
}

func (s *PreviewService) Preview(ctx context.Context, mdl *WrenMDL, sql string, limit int64) (*QueryResultDto, error)
func (s *PreviewService) DryPlan(ctx context.Context, mdl *WrenMDL, sql string, modelingOnly bool) (string, error)
func (s *PreviewService) DryRun(ctx context.Context, mdl *WrenMDL, sql string) ([]Column, error)
```

Flow for Preview:
1. Build SessionContext from WrenMDL + config
2. `WrenPlanner.Rewrite(sql, sessionContext, analyzedMDL)` → planned SQL
3. `sqlConverter.Convert(plannedSQL, sessionContext)` → dialect SQL
4. `metadata.DirectQuery(convertedSQL)` → execute and return results

### 8.2 ValidationService

```go
type ValidationService struct {
    rules map[string]ValidationRule
}

func (s *ValidationService) Validate(ctx context.Context, ruleName string, params map[string]any, mdl *AnalyzedMDL) ([]ValidationResult, error)
```

Initially supports `column_is_valid` rule only. Extensible for future rules.

## 9. REST API

### 9.1 Endpoints (full compatibility with Java version)

Using `github.com/go-chi/chi/v5` router:

| Method | Path | Request Body | Response |
|--------|------|-------------|----------|
| GET | `/v1/mdl/preview` | PreviewDto (JSON body on GET) | QueryResultDto |
| GET | `/v1/mdl/dry-plan` | DryPlanDto | string |
| GET | `/v1/mdl/dry-run` | PreviewDto | []Column |
| POST | `/v1/mdl/validate/{ruleName}` | ValidateDto | []ValidationResult |
| GET | `/v2/mdl/dry-plan` | DryPlanDtoV2 (base64 manifest) | string |
| GET | `/v1/analysis/sql` | SqlAnalysisInputDto | []QueryAnalysisDto |
| GET | `/v2/analysis/sql` | SqlAnalysisInputDtoV2 | []QueryAnalysisDto |
| GET | `/v2/analysis/sqls` | SqlAnalysisInputBatchDto | [][]QueryAnalysisDto |
| POST | `/v1/data-source/duckdb/query` | string (raw SQL) | QueryResultDto |
| GET | `/v1/data-source/duckdb/settings/init-sql` | — | string |
| PUT | `/v1/data-source/duckdb/settings/init-sql` | string | 200 |
| PATCH | `/v1/data-source/duckdb/settings/init-sql` | string | 200 |
| GET | `/v1/data-source/duckdb/settings/session-sql` | — | string |
| PUT | `/v1/data-source/duckdb/settings/session-sql` | string | 200 |
| PATCH | `/v1/data-source/duckdb/settings/session-sql` | string | 200 |
| GET | `/v1/config` | — | []ConfigEntry |
| GET | `/v1/config/{configName}` | — | ConfigEntry |
| DELETE | `/v1/config` | — | 200 |
| PATCH | `/v1/config` | []ConfigEntry | 200 |

**Note**: The Java version uses GET with JSON body for some endpoints (preview, dry-plan, etc.) which is unconventional. The Go version maintains this for compatibility.

### 9.2 JSON Request/Response DTOs

All DTOs match the Java version's JSON field names exactly for compatibility:

```go
type PreviewDto struct {
    Manifest *Manifest `json:"manifest"`
    SQL      string    `json:"sql"`
    Limit    *int64    `json:"limit,omitempty"`
}

type QueryResultDto struct {
    Columns []Column   `json:"columns"`
    Data    [][]any    `json:"data"`
}

type DryPlanDto struct {
    Manifest      *Manifest `json:"manifest"`
    SQL           string    `json:"sql"`
    ModelingOnly  bool      `json:"modelingOnly"`
}

type DryPlanDtoV2 struct {
    ManifestStr string `json:"manifestStr"` // base64-encoded JSON
    SQL         string `json:"sql"`
}

type ValidateDto struct {
    Manifest   *Manifest       `json:"manifest"`
    Parameters map[string]any  `json:"parameters"`
}
```

## 10. Configuration

### 10.1 Config File

YAML at `etc/config.yaml` (or via `--config` flag):

```yaml
server:
  port: 8080
wren:
  mdl_directory: etc/mdl
  datasource_type: duckdb
  enable_dynamic_fields: true
duckdb:
  max_concurrent_tasks: 4
  memory_limit: 4GB
  temp_directory: /tmp/wren
  home_directory: .
postgres:
  host: localhost
  port: 5432
  database: wren
  user: wren
  password: ""
```

### 10.2 Environment Variables

Override config values with `WREN_` prefix:
- `WREN_PORT`
- `WREN_MDL_DIRECTORY`
- `WREN_DATASOURCE_TYPE`
- `WREN_DUCKDB_MEMORY_LIMIT`
- `WREN_PG_HOST`, `WREN_PG_PORT`, `WREN_PG_DATABASE`, `WREN_PG_USER`, `WREN_PG_PASSWORD`

### 10.3 Hot Reload

Config PATCH API updates in-memory config and persists to file. Reloading the SQL converter may be needed when datasource config changes.

## 11. Error Handling

### 11.1 Error Types

```go
type ErrorType string

const (
    SyntaxError     ErrorType = "SYNTAX_ERROR"
    SemanticError   ErrorType = "SEMANTIC_ERROR"
    TypeMismatch    ErrorType = "TYPE_MISMATCH"
    NotFound        ErrorType = "NOT_FOUND"
    GenericUserError ErrorType = "GENERIC_USER_ERROR"
)

type WrenError struct {
    Code    ErrorCode
    Type    ErrorType
    Message string
    Cause   error
}
```

### 11.2 HTTP Error Mapping

`WrenError` → HTTP response:
- `SYNTAX_ERROR` → 400 Bad Request
- `SEMANTIC_ERROR` → 400 Bad Request
- `NOT_FOUND` → 404 Not Found
- `GENERIC_USER_ERROR` → 500 Internal Server Error
- Default → 500 Internal Server Error

Response format matches Java's `ErrorMessageDto`:
```json
{
    "errorCode": 65536,
    "errorType": "GENERIC_USER_ERROR",
    "message": "error description"
}
```

## 12. Key Go Dependencies

| Purpose | Library | Version |
|---------|---------|---------|
| SQL Parser (ANTLR4 runtime) | `github.com/antlr4-go/antlr/v4` | v4 |
| HTTP Router | `github.com/go-chi/chi/v5` | v5 |
| DuckDB Driver | `github.com/marcboeker/go-duckdb` | latest |
| PostgreSQL Driver | `github.com/jackc/pgx/v5` | v5 |
| Jinja2 Templates | `github.com/noirbizarre/gonja` | v2 |
| DAG/Topological Sort | `github.com/dominikbraun/graph` | latest |
| Config (env vars) | `github.com/caarlos0/env` | latest |
| YAML parsing | `gopkg.in/yaml.v3` | v3 |
| Logging | `log/slog` (stdlib) | — |
| JSON | `encoding/json` (stdlib) | — |
| Worker pool | `golang.org/x/sync/semaphore` or custom | — |

## 13. Implementation Phases

### Phase 1: Foundation
- Go module initialization + project structure
- ANTLR4 grammar port + Go code generation
- Focused AST layer (~45 node types)
- AST visitor pattern
- SQL formatter

### Phase 2: Core MDL & Rewrite
- DTO structs + JSON parsing
- WrenMDL + macro/Jinja rendering
- Analyzer subsystem (StatementAnalyzer, ExpressionAnalyzer, ScopeAnalyzer)
- WrenPlanner + 4 rewrite rules
- DAG dependency resolution

### Phase 3: Connectors & Converters
- Client interface + DuckDB connector
- PostgreSQL connector
- DuckDB SQL converter
- PostgreSQL SQL converter

### Phase 4: Services & Server
- PreviewService, ValidationService
- REST API handlers (all endpoints)
- Configuration management
- Error handling + HTTP error mapping

### Phase 5: Testing & Hardening
- Unit tests for parser, rewriter, analyzer
- Integration tests matching Java test suite
- End-to-end API compatibility tests
