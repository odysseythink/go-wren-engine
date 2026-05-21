# Phase 2 设计：config.properties 解析 + PATCH 持久化 + 配置 reload

> 子项目：Phase 2（drop-in replacement 路线图 5 阶段中的第 2 阶段）。
> 前置：P0–P6 全部、Phase 1（Dockerfile + entrypoint 设 `WREN_CONFIG_FILE` env）。
> 目标：让 WrenAI bootstrap 写入的 `etc/config.properties` 在 Go 引擎启动时**真正生效**，PATCH
> 写入持久化到磁盘并归档旧版本，与 Java `ConfigManager` 在配置生命周期上等价。

## 1. 背景

Phase 1 让 docker 镜像能起来，但 Go 当前**完全忽略** mount 进来的 `etc/config.properties`：

- `cmd/wren-engine/main.go:14-16` 只调 `configMgr.LoadFromEnv()`，从不读文件
- `internal/server/config_handler.go.Patch` 调用 `cm.Set(name, value)` **纯内存**改动；重启即丢
- `DELETE /v1/config` 调 `cm.Reset()` 同样不写盘

这造成三个 drop-in 阻塞行为：

| 行为 | Java | Go (Phase 1 后) |
|---|---|---|
| 启动时找不到 `etc/config.properties` | `WrenException(NOT_FOUND)` → 进程退出 | 不挂，使用内置默认 |
| WrenAI bootstrap 写的 `enable-dynamic-fields=true` | Java `ConfigManager` 读入 → 走 dynamic-field 分支 | Go 不读，永远走 static 分支（**语义偏差**） |
| `PATCH /v1/config` 后重启 | 上次改动持久（`syncFile` 写盘 + `archived/` 归档时间戳副本） | 内存丢失，回到默认 |

Phase 2 闭合这三个行为差。

**奇偶校验线（Phase 2）**：① 启动后 `GET /v1/config/<key>` 返回 file 里的值（如果 file 没写则返回 typed 默认）；
② PATCH 后磁盘 `etc/config.properties` 包含新键值，`etc/archived/config.properties.<timestamp>` 含旧版本；
③ 缺 file 启动 fatal；④ 现有 `baseline-config.json` 12 IDs 仍 pass（不破坏 P6 的成果）。

## 2. 范围

### IN（Phase 2 负责）

- `internal/config/properties.go`：手写 `.properties` 解析器（key=value、`#`/`!` 注释、空行、行续 `\`、转义 `\n`/`\r`/`\t`/`\\`/`\=`/`\:`/`\#`、UTF-8 主体 + 兼容 ISO-8859-1 fallback）
- `internal/config/config.go` 扩展：
  - `LoadFromFile(path string) error` —— 解析文件 → 已知 key 覆盖 `configs`、未知 key 存 `fileExtras`
  - `SyncToFile() error` —— 写回 `etc/config.properties`（用 Java `Properties.store()` 等价格式）
  - `Archive() error` —— 归档当前 file 到 `<dir>/archived/config.properties.<timestamp>`
  - `sync.RWMutex` 包住 Set/Reset/All/Get 等并发路径
- `internal/server/config_handler.go.Patch`：调用 `cm.Set` 成功后 → `cm.Archive()` → `cm.SyncToFile()` → 触发 reload hook
- `internal/server/config_handler.go.DeleteAll`：reset 后同样 `Archive` + `SyncToFile`
- `cmd/wren-engine/main.go`：启动序——`LoadFromFile(env) → LoadFromEnv()`（env 优先）；file 缺失 → `os.Exit(1)` + 错误信息
- Reload 钩子：`ConfigManager.OnChange(keys []string, fn func())` 注册器，PATCH 触发已 reload-flag 的 key 时调用
- 单元测试：properties 解析器（含转义边界）、Load/Sync 往返、Archive 时间戳唯一性
- 差分测试：`baseline-config.json` 12 IDs 不破

### OUT（不在 Phase 2）

- DuckDB metadata 实际 reload —— Java `SqlConverterManager.reload()` 仅检测 `datasource.type` 变化；
  Go 当前 DuckDB-only，**无 datasource 切换需求**，reload hook 留空实现。memory-limit / home-directory /
  temp-directory 改动要求 DuckDB 客户端重建，**Phase 2 仅在 risk 登记表记录**，由 Phase 4 / 后续阶段补
- properties 编辑界面 / API 增强（Java 0.9.3 也只有 PATCH，不补别的）
- 文件加密 / secrets 管理（不在 Java 行为里）
- properties 文件 watch（filesystem 通知重新加载）—— Java 不做，Go 不做
- node.environment 等非 11-key 配置项的 typed binding（继续作为 `fileExtras` 透传写回，不暴露 GET）

## 3. 架构与数据流

### 启动序

```
main.go
  configMgr = config.NewConfigManager()       ← typed 默认值
  path = os.Getenv("WREN_CONFIG_FILE")        ← Phase 1 entrypoint 设
  if path == "" {
      log.Fatalf("WREN_CONFIG_FILE env required (Java parity)")
  }
  if err := configMgr.LoadFromFile(path); err != nil {
      log.Fatalf("Config file not found: %v", err)   ← Java 等价: WrenException(NOT_FOUND)
  }
  configMgr.LoadFromEnv()                      ← env 在 file 之后覆盖
```

`LoadFromFile` 流程：

```
read file → parseProperties → for k, v in parsed:
    if k in configs (11 typed keys):
        configs[k] = v
    else:
        fileExtras[k] = v        ← 透传写回不暴露 GET
```

### PATCH 序

```
PATCH /v1/config  body=[{name,value},...]
  └─ Patch handler:
       cm.Lock()
       defer cm.Unlock()

       updated := []string{}
       for entry in body:
           value := strings.TrimSpace(entry.Value)   ← Java parity
           if cm.staticConfigs[name] { continue }    ← 静默 skip
           if !cm.knownKey(name) { → 404 NotFound }  ← 未知 key 拒绝
           cm.configs[name] = value
           cm.fileExtras[name] = value               ← 同步 mirror (任一可写回)
           updated = append(updated, name)

       if len(updated) == 0 { return 200 }           ← 全部 static，no-op

       if err := cm.Archive(); err != nil {          ← 先归档
           rollback cm.configs/fileExtras 修改
           → 500
       }
       if err := cm.SyncToFile(); err != nil {       ← 后写盘
           → 500 (archived 文件保留作为回退)
       }

       for _, k := range updated {
           if cm.requiredReload[k] {
               cm.fireReload(k)                       ← 调 OnChange 注册的 handler
           }
       }
       → 200
```

### 写盘格式

Java `Properties.store(writer, "sync with file")` 产出形如：

```
#sync with file
#Wed May 20 17:30:45 UTC 2026
key1=value1
key2=value2
key3=
```

特点：注释丢失（用户原始 `# Apache License` header **被覆盖**）、键按 HashMap 迭代顺序输出、值带特殊字符的转义、空值留 `key=`。

Go `SyncToFile` 镜像该格式：

- 第一行：`#sync with file`
- 第二行：`#<RFC1123-ish timestamp>` —— Java 用 `Date.toString()`，Go 用 `time.Now().UTC().Format(time.RFC1123)`，**格式不字节等价**但保持"双注释 header"结构
- 后续：所有 `configs` (11 keys) + `fileExtras` (file 里有但不在 11 keys 的) 一并写出
- 输出顺序：**alphabetic by name**（Go 默认排序，Java HashMap 不稳，Go 排序更确定 —— **结构等价但顺序更稳**）
- 转义规则：`=`、`:`、`#`、`!`、`\n`、`\r`、`\t`、`\\` 在 value 中转义；key 中额外转义空白

### 归档

Java `archiveConfigs`：

```
home = parentOf(configFile)                              ← etc/
archived = home/archived                                  ← etc/archived/
Files.copy(configFile, archived/configFile.<ts>)         ← 复制，不删原
```

`<ts>` 格式 `uuuuMMddHHmmssnnnn`（年月日时分秒 + 4 位 nano-of-second 前缀），如 `202605201730450012`。

Go `Archive`：

- `dir = filepath.Dir(path)`
- `archiveDir = filepath.Join(dir, "archived")`，不存在则 `os.MkdirAll(0o755)`
- ts = `time.Now().UTC().Format("20060102150405") + fmt.Sprintf("%04d", time.Now().Nanosecond()/100000)`
- `dst = filepath.Join(archiveDir, filepath.Base(path)+"."+ts)`
- `os.Link(path, dst)` 优先（POSIX hard link 原子）→ 失败回退 `copyFile`
- Java 用 `Files.copy`（非原子），Go 优先用 hardlink 提升原子性

## 4. 组件与文件结构

| 文件 | 操作 | 说明 |
|---|---|---|
| `internal/config/properties.go` | 新建 | `parseProperties(io.Reader) (map[string]string, error)` + `writeProperties(io.Writer, map[string]string, header string) error` |
| `internal/config/properties_test.go` | 新建 | 解析器 / writer 单测，涵盖转义 / 行续 / `!` 注释 / 空值 / Unicode |
| `internal/config/config.go` | 修改 | 加 `fileExtras map[string]string`、`filePath string`、`mu sync.RWMutex`、`requiredReload map[string]bool`、`reloadHooks map[string][]func()`，新增 `LoadFromFile/SyncToFile/Archive/OnChange/RequiredReload` |
| `internal/config/config_test.go` | 修改 | 加 LoadFromFile + Sync 往返 + Archive 测试 |
| `internal/server/config_handler.go` | 修改 | Patch / DeleteAll 加 Archive + SyncToFile 调用 + reload hook |
| `internal/server/config_handler_test.go` | 修改 | 加端到端测试：临时 file → PATCH → 校验 file 内容 + archived 副本 |
| `cmd/wren-engine/main.go` | 修改 | 启动序：env 取 `WREN_CONFIG_FILE` → LoadFromFile → fatal-if-missing |
| `internal/difftest/config_diff_test.go` | 可能微调 | 现有 12 IDs 测的是 in-memory 默认；Phase 2 后改成 LoadFromFile-from-mock-file 路径，**核心断言不变** |

## 5. 实施切片（方案 C：增量切片）

### 切片 1 — properties 解析器

- 实现 `internal/config/properties.go.parseProperties`
- 覆盖 Java `.properties` 格式所有规则：
  - 分隔符：`=`、`:`、whitespace（任意一个）
  - 注释：`#` / `!` 在行首（前导 whitespace OK）
  - 行续：行尾 `\` 接 newline → 下一行 trim leading whitespace 拼接
  - 转义：`\n`/`\r`/`\t`/`\\`/`\=`/`\:`/`\#`/`\!`/`\ `/`\u<hex4>`
  - 空值：`key=` → value `""`
- 写单元测试 covering 12+ 边界用例
- 验证：跑 `parseProperties` 解 `wren-engine-0.9.3/example/duckdb-tpch-example/etc/config.properties` 得 6 个 key

### 切片 2 — ConfigManager Load / Sync / Archive

- 加 `fileExtras / filePath / mu` 字段
- 新增方法：`LoadFromFile / SyncToFile / Archive / RequiredReload(keys ...) / OnChange`
- 给 11 个 typed key 标 reload-flag：`wren.datasource.type`、`duckdb.memory-limit`、`duckdb.home-directory`、`duckdb.temp-directory`（与 Java `requiredReload` 集合完全一致）
- 实现 `writeProperties(io.Writer, ...)`：双 # header + alphabetic sort + 转义
- 单测：
  - LoadFromFile + SyncToFile 往返不丢键
  - PATCH 一次 → Archive 后 `etc/archived/` 含 1 个文件
  - PATCH 两次 → Archive 后 `etc/archived/` 含 2 个文件
  - Concurrent PATCH 不 race（go test -race）

### 切片 3 — Handler 接线 + 启动 fatal

- `config_handler.go.Patch`：调用 `Archive → SyncToFile → fireReload`
- `config_handler.go.DeleteAll`：同样接线
- `cmd/wren-engine/main.go`：`os.Getenv("WREN_CONFIG_FILE") → LoadFromFile → log.Fatalf` if missing
- 启动失败信息：`Config file not found: <path>` （字符串 100% 匹配 Java `WrenException` message 减去 `WrenException` 前缀）
- 端到端测试 (`config_handler_test.go`)：临时目录 + 临时 file → POST PATCH → 文件内容含新键 + archived/ 存原版

### 切片 4 — 差分基线 + 文档

- 检查 `internal/difftest/config_diff_test.go` 是否需要调整：当前 12 IDs 用的是 `config.NewConfigManager()` 直接读默认，Phase 2 后该调用路径不变（只是 `main.go` 多一步 LoadFromFile），baseline 应当 trivially pass
- 测试 PATCH 持久化：临时 file → PATCH → 重启（new ConfigManager + LoadFromFile）→ GET 看到新值
- 测试 `make image-test` (Phase 1 引入的 target)：Phase 2 后 entrypoint 缺 `etc/config.properties` mount 应当让容器**立即 fail 退出**（与 Java parity）

## 6. 风险登记表

| # | 风险 | 说明 |
|---|---|---|
| 1 | **Properties.store 注释丢失行为** | Java 写盘 = 覆写整个文件，用户原始注释（License header、自定义说明）**会丢失**。Go 必须镜像该行为以保持 byte 等价 —— 但**用户感知层面是 regression**。需在 `docker/README.md` 标红：「PATCH 后注释丢失，请用 git 管理 properties 文件」 |
| 2 | **archived 文件名 nano-of-second 不字节等价** | Java `nnnn` = 4 位 nano-of-second prefix，Go 用 `time.Nanosecond()/100000` 模拟，**值域相同但偶发 0-pad 差异**。归档时序仍可单调读取；不影响功能。属 §1 honest deviation 类 |
| 3 | **未知 key 的 GET 暴露问题** | Java GET `/v1/config` 仅返回 `configs` (11 typed) 而**不**返回 `setConfigs` 里多出来的 key（如 `node.environment`）。Go 必须同样仅返回 11 keys 不混入 fileExtras |
| 4 | **memory-limit / home-directory / temp-directory 改动需重建 DuckDB metadata** | Java `requiredReload` 标这 3 个 key，但 `SqlConverterManager.reload` 只处理 datasource.type 变化（其它 3 个由 DuckDB Client lifecycle 处理 —— 未细究）。Go 当前没有 DuckDB metadata 重建机制。**Phase 2 仅触发空 reload hook**，留 Phase 4/5 补 |
| 5 | **datasource.type 切换不可能** | Java 4 种 datasource 实现都 deprecated（只 DUCKDB 活），Go 仅实现 DUCKDB。Reload hook 即使调用也无事可做。OnChange 注册保留 API 但实现 NOOP；记入文档 |
| 6 | **PATCH 中途 crash** | archive 成功 → SyncToFile 失败 → file 半写。POSIX `os.Rename`（实际写法：写 `.tmp` + atomic rename）做到 file 要么旧版要么新版，永不半写。crash 后 archived 还有最新备份，可手动 recover |
| 7 | **PATCH 并发 race** | 多个 PATCH 请求同时改 file。`sync.RWMutex` + `Archive → SyncToFile` 在 lock 内串行化。Go `-race` 必须 clean |
| 8 | **WREN_CONFIG_FILE env 未设** | Phase 1 entrypoint 已设；若用户直接 `go run cmd/wren-engine` 启动会 fatal。本地开发应当 `export WREN_CONFIG_FILE=etc/config.properties`，README 说明 |
| 9 | **node.environment 等 5 个非典型 key 解析正确性** | TPC-H example 含 `node.environment=production` 这类 airlift framework key，Go 必须**接受但不报错**。fileExtras 透传写回是关键 |
| 10 | **行续 + 转义边界** | Java `loadPropertiesFrom` 走 airlift configuration loader 也是 wrap `java.util.Properties`，规则不简单。手写解析器**必须**单测 12+ 转义边界 |
| 11 | **archive 目录权限** | mount 进来的 `etc/` 可能是只读，archive 子目录创建失败。`Archive()` 失败时**整个 PATCH 应当 abort**（Java 等价：throw IOException → 500），原子保持 |
| 12 | **timestamp 第二行 header 难字节等价** | Java `Properties.store` 第二行注释格式：`#Wed May 20 17:30:45 UTC 2026`（locale-dependent!）。Go 用 RFC1123 `Wed, 20 May 2026 17:30:45 UTC` —— 带逗号、零填充day。**结构上是 `#<timestamp>`，但格式细节不字节等价**。属 §1 honest deviation |

## 7. 错误处理

- **WREN_CONFIG_FILE 缺失**：`log.Fatalf("WREN_CONFIG_FILE env required (Java parity: -Dconfig must be set)")`
- **config file 不存在**：`log.Fatalf("Config file not found: %s", path)` —— message 与 Java `WrenException(NOT_FOUND, "Config file not found")` 文字等价（去 Java exception 前缀）
- **config file 解析错（语法错）**：fatal，附 line + 列号
- **PATCH archive 失败**：500 + `WrenError{Code:65536, Type:GenericUserError, Message:"archive failed: " + err}`；不修改 configs map
- **PATCH SyncToFile 失败**：500 + `WrenError{...Message:"sync to file failed: ..."}`；archive 已生成可作回退
- **PATCH 未知 key**：404 + `WrenError{NotFound, "Config not found: " + key}`（Phase P6 已实现，Phase 2 不破）
- **DELETE 同上**：失败路径同 PATCH

## 8. 测试与验收

### 验收标准

- ✅ `cmd/wren-engine` 启动时 `WREN_CONFIG_FILE` env 未设 → 进程退出 1 + stderr 含 `WREN_CONFIG_FILE env required`
- ✅ env 设但文件不存在 → 进程退出 1 + stderr 含 `Config file not found:`
- ✅ 文件存在但非法（语法错）→ 进程退出 1 + stderr 含具体行号
- ✅ 文件含 `wren.experimental-enable-dynamic-fields=false`，启动后 `GET /v1/config/wren.experimental-enable-dynamic-fields` 返回 `{"name":"...","value":"false"}`
- ✅ 文件含 `node.environment=production`（非 11-key），启动 OK，`GET /v1/config` 仍只返 11 entries
- ✅ `PATCH /v1/config` body=`[{"name":"wren.datasource.type","value":"POSTGRES"}]` → 200 + 磁盘 file 含 `wren.datasource.type=POSTGRES` + `etc/archived/config.properties.<ts>` 含上一版本
- ✅ 连续两次 PATCH → `etc/archived/` 含 2 个文件，时间戳单调递增
- ✅ PATCH static key (`duckdb.max-concurrent-tasks=99`) → 200 + 文件**未改**（static 不写）+ archived **未生成**
- ✅ `DELETE /v1/config` → 200 + 磁盘恢复到默认 + archived 含删除前版本
- ✅ `go test ./internal/config/... ./internal/server/... -race` clean
- ✅ `baseline-config.json` 12 IDs 仍 pass

### 测试方式

- **单元**：`internal/config/properties_test.go` 覆盖 12+ 解析边界（含 Apache License header 解析、行续、Unicode `é`、空值、`!` 注释）
- **单元**：`internal/config/config_test.go` 加 Load/Sync 往返 + Archive 单调时间戳 + concurrent PATCH race
- **HTTP**：`internal/server/config_handler_test.go` 加临时 file PATCH e2e
- **集成**：`make image-test`（Phase 1 引入）在 Phase 2 后 mount 真 properties → 容器启动 → `curl /v1/config` 验证 file 值生效
- **差分**：`internal/difftest/config_diff_test.go` 跑现有 12 IDs 不破

## 9. 与其他阶段的关系

- **依赖 P0–P6**：ConfigManager 的 11-key in-memory 模型由 P6 引入，Phase 2 在其上加文件层
- **依赖 Phase 1**：entrypoint.sh 设的 `WREN_CONFIG_FILE` env 被 Phase 2 消费；Phase 1 entrypoint 启动时打印的「config.properties parsing not yet supported」warning **本 Phase 删除**
- **承接 Phase 3** (TPC-H golden 补齐)：Phase 2 完成后，capture-golden 工具可以**真实模拟 WrenAI 部署环境**（mount config.properties → 启 Java oracle → 抓 golden），抓出的 baseline 才是真正可信的 drop-in 基线
- **不影响 Phase 4/5**：envelope bug 修复与 P7 dynamic-field 都是 rewrite chain 层，与 config-file 解耦

### Drop-in gap 登记表（Phase 2 后状态）

| Gap | Phase 1 后 | Phase 2 后 |
|---|---|---|
| `etc/config.properties` 不被解析 | ❌ ignore | ✅ 闭合 |
| 缺 config 文件不 fatal | ❌ 不挂 | ✅ 闭合（fatal） |
| PATCH 不写盘 | ❌ 内存改 | ✅ 闭合（archive + sync） |
| 注释保留 | n/a | ⚠ 故意丢失（Java parity，文档化） |
| DuckDB metadata reload (memory-limit 等) | ❌ 无机制 | ⚠ Hook 空实现，待 Phase 4 / 5 补 |
| Postgres wire protocol (7432) | ❌ 全缺 | ❌ 仍缺（Phase 6 评估） |
| `WrenSqlRewrite` dynamic-field 分支 | ❌ 全缺 | ❌ 仍缺（Phase 5 / P7） |
| WrenAI 实际锁 0.11.1 vs Go 对齐 0.9.3 | ❌ 未审计 | ❌ 未审计（待评估补 P8） |

---

- 本 spec 完成后进入 `gpowers:writing-plans`，产出 Phase 2 逐任务实施计划。
