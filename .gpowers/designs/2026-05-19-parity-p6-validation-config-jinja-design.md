# P6 设计：校验、配置与 jinja 收尾

> 子项目：P6（七阶段拆解中的第六、收官阶段）。
> 前置：P0–P5 全部。
> 目标：补齐校验、配置端点与 jinja 宏渲染的剩余缺口，使 Go 引擎对默认配置 100% 替代
> `wren-engine:0.9.3`。

## 1. 背景

P0–P5 覆盖了重写、执行、分析三大链路。P6 收尾镜像剩余的端点与子系统：

- **校验**：`/v1/mdl/validate/{ruleName}` —— 按规则名校验 MDL（如 `ColumnIsValid` 检查列是否有效）。
- **配置**：`/v1/config`、`/v1/config/{configName}` —— 读取 / 修改服务配置。
- **jinja 收尾**：`internal/mdl/jinja.go` 已存在（早期提交），P6 补齐宏渲染的边界用例，与 Java 对齐。

**奇偶校验线（P6）**：校验结果、配置的 JSON 输出与 Java **结构化等价**；jinja 渲染后的 SQL 经
重写链路（P3）+ difftest 验证。

Go 现状：`internal/service/validation.go`（51 行）为桩、`internal/config/config.go` 与
`internal/server/config_handler.go` 部分实现、`internal/mdl/jinja.go` 已有。

## 2. 范围

### IN（P6 负责）

- 校验子系统：
  - `ValidationRule` 接口、`ValidationResult` DTO
  - `ColumnIsValid` 规则（对 DuckDB 执行列检查 —— 依赖 P4）
  - `/v1/mdl/validate/{ruleName}` 端点接线
- 配置子系统：`/v1/config`（GET）、`/v1/config/{configName}`（GET / PATCH）
- jinja 收尾：`internal/mdl/jinja.go` 宏渲染补齐与 Java `MacroService` / jinja 行为对齐
- 健康检查及其余杂项端点

### OUT（不在 P6）

- `WrenSqlRewrite` 动态字段路径 + `WrenDataLineage`（见 §9 残留说明）

## 3. 架构与数据流

```
POST /v1/mdl/validate/{ruleName}  (manifest + parameters)
  └─> analyzedMDL = AnalyzedMDL(WrenMDL.fromManifest(manifest))
      rule = lookupRule(ruleName)              ← 如 ColumnIsValid
      ValidationResult = rule.validate(parameters, analyzedMDL)
        ColumnIsValid：对 DuckDB 执行检查语句（依赖 P4 连接器）
      → JSON 响应

GET /v1/config            → 全部配置项 JSON
GET /v1/config/{name}     → 单项
PATCH /v1/config/{name}   → 修改

jinja：MDL 中的 macro / 模板表达式 → jinja 渲染 → 进入重写链路（P3）
```

校验与配置是基础设施 —— 惯用 Go 实现；jinja 渲染语义须与 Java 对齐 —— 按 Java 行为核对。

## 4. 组件与文件结构

| Go 文件 | 对应 Java | 操作 |
|---|---|---|
| `internal/service/validation.go` | `ValidationService` | 重写 |
| `internal/service/validation_rule.go` | `ValidationRule` 接口 | 新建 |
| `internal/service/column_is_valid.go` | `ColumnIsValid` | 新建 |
| `internal/dto/validation_result.go` | `ValidationResult` | 新建 |
| `internal/config/config.go` | 配置模型 | 修改 |
| `internal/server/config_handler.go` | `ConfigResource` | 修改 |
| `internal/server/mdl_handler.go` | `MDLResource.validate` 接线 | 修改 |
| `internal/mdl/jinja.go` | jinja / `MacroService` 收尾 | 修改 |

## 5. 实施切片（方案 C：增量切片）

每个切片：实现 / 移植 → golden 差分 / 单测 → baseline 接受 fail→pass。

- **切片 1 — 配置端点**：`/v1/config` GET、`/v1/config/{name}` GET / PATCH；配置模型与 Java 对齐。
- **切片 2 — 校验框架**：`ValidationRule` 接口、`ValidationResult` DTO、`/v1/mdl/validate/{ruleName}`
  端点接线、规则查找。
- **切片 3 — ColumnIsValid 规则**：对 DuckDB 执行列检查（复用 P4 连接器）。
- **切片 4 — jinja 收尾**：核对 `internal/mdl/jinja.go` 宏渲染与 Java 的边界用例差异并补齐。

## 6. 风险登记表

| # | 风险 | 说明 |
|---|---|---|
| 1 | jinja 宏渲染语义 | Go 的 jinja 实现与 Java `MacroService` 的宏展开语义可能有边界差异；以重写后 SQL 经 difftest 校验。 |
| 2 | `ColumnIsValid` 的 DuckDB 依赖 | 校验规则需对 DuckDB 执行检查语句 —— 依赖 P4 正确。 |
| 3 | 配置项集合 | `/v1/config` 返回的配置项名称、默认值须与 Java 一致。 |
| 4 | 校验结果 JSON | `ValidationResult` 的字段、状态枚举须与 Java 结构化等价。 |

## 7. 错误处理

- 未知规则名 / manifest 缺失 / 校验执行失败：按 Java `WrenExceptionMapper` 格式返回错误 JSON。
- 不静默吞错。

## 8. 测试与验收

- **端到端差分测试**：golden 快照 —— 捕获 Java `validate` / `config` 端点 JSON，结构化等价比对。
- **jinja 验证**：含 macro 的 MDL 经 P3 重写链路 + difftest 校验渲染结果。
- **单元测试**：每切片组件级单测。
- **baseline 计分板**：沿用 P1。
- **验收标准**：
  - 校验 / 配置端点输出结构化等价。
  - 含 macro 的 MDL 重写后 SQL 与 Java 一致。
  - `go build ./...` 通过、`go vet ./...` 干净、`gofmt -l` 无输出。

## 9. 与其他阶段的关系与收官

- **依赖 P0–P5**：`ColumnIsValid` 需对 DuckDB 执行 → 依赖 P4；`validate` 端点分析 manifest →
  依赖 P3 的 analyzer / MDL。
- **收官**：P6 完成即七阶段（P0–P6）全部完成 —— Go 引擎对**默认配置**的
  `wren-engine:0.9.3` 实现 100% 替代。

### 明确残留：动态字段路径

`WrenSqlRewrite` 的动态字段路径（`sessionContext.isEnableDynamicField()`）+ `WrenDataLineage`
（约 530 行）在 P3a 中已标为范围外。该路径在 Java 中由 `enable-dynamic-fields` 配置控制、**默认关闭**，
且官方源码注释标注其为 "experimental and buggy"。

因此 P0–P6 完成 = 对**默认配置**的镜像 100% 替代。若需覆盖动态字段路径（严格意义上的「全部功能」），
应作为 P6 之后的独立增强子项目（P7：动态字段 + 数据血缘），走同样的 spec → plan → 实现流程。
是否纳入由用户决定。

- 本 spec 完成后进入 writing-plans，产出逐任务实施计划。
