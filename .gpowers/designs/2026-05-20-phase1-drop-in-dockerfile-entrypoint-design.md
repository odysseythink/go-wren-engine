# Phase 1 设计：Drop-in Dockerfile + entrypoint

> 子项目：Phase 1（drop-in replacement 路线图 5 阶段中的第 1 阶段）。
> 前置：P0–P6 全部已合入（HTTP API 表面已与 `wren-engine:0.9.3` 对齐）。
> 目标：产出 `go-wren-engine` 镜像，遵循 Java 镜像（`ghcr.io/canner/wren-engine:0.9.3` / 0.11.1）的
> 启动契约，使其能在 WrenAI 0.9.0 `docker-compose` 中**仅替换 `image:` 行**即可启动。

## 1. 背景

drop-in 路线图共 5 阶段：

1. **Phase 1（本 spec）**：Dockerfile + entrypoint
2. Phase 2：`etc/config.properties` 解析
3. Phase 3：TPC-H golden 补齐
4. Phase 4：envelope `go-error` 修复
5. Phase 5（即 P7）：dynamic-field + `WrenDataLineage`

仓库根目录已有 `Dockerfile` + `docker-compose.yaml`，但**当前实现无法 drop-in**：

| 当前问题 | 后果 |
|---|---|
| `FROM golang:1.26-alpine` 编译 + `FROM alpine:latest` 运行 | go-duckdb v1.7.0 静态打包的 `libduckdb.a` 是 glibc-bound，alpine 的 musl 链接器会 fail；即使编出来跑也可能 segfault |
| `WORKDIR /root/` | WrenAI bootstrap mount `data:/usr/src/app/etc`，Go 镜像 cwd 不在 `/usr/src/app` 则相对路径全错 |
| `CMD ["./wren-server"]` 不接收 heap 参数 | Java entrypoint 通过 `$2/$3` 接收 `MAX_HEAP_SIZE/MIN_HEAP_SIZE`，docker-compose 透传；Go 镜像必须保持相同 argv 形态 |
| `docker-compose.yaml` mount `./etc/mdl:/app/etc/mdl` | 与 Java 镜像 `/usr/src/app/etc` 完全不兼容；WrenAI bootstrap 写 `${data_path}/config.properties` 到 `/usr/src/app/etc/config.properties`，Go 镜像 mount 路径不一致 |
| 未安装 `postgresql-client-13` | Java 镜像安装这个包；如果有 sidecar 或运维脚本依赖 `psql` 可达，Go 镜像缺它就坏 |

**奇偶校验线（Phase 1）**：① `docker build` + `docker run` 成功，HTTP API 表面行为与 Java 镜像
等价（同端口、同路径、同 mount）；② WrenAI 0.9.0 `docker-compose` 直接替换 `image:` 行无需改其它字段
即可启动 wren-engine 服务并通过 `bootstrap` 的 init.sh 写盘。

注意：Phase 1 **不引入** Go 源码改动。只新增 / 修改 docker / 构建脚本类资产。

## 2. 范围

### IN（Phase 1 负责）

- `docker/Dockerfile`：多阶段构建（CGo + libduckdb 静态打包 + Debian-slim 运行时）
- `docker/entrypoint.sh`：接收 Java 兼容的 `$1=binary` `$2=maxHeap` `$3=minHeap` 三个位置参数
- 根目录 `Dockerfile`：thin wrapper（一行 `Dockerfile -> docker/Dockerfile` 软链或单行 `#include`-style）
  ——或直接物理迁移
- 根目录 `docker-compose.yaml`：mount 路径、env、ports 与 WrenAI compose 兼容
- `.dockerignore`：减少 build context 体积（排除 `.gpowers/`、`testdata/`、`*.md`、`.git/`）
- 镜像版本号约定：`go-wren-engine:<git-tag>` + `latest` 双标签；CI 触发条件文档化
- README 章节：「替换 WrenAI compose 中 wren-engine 镜像」操作步骤

### OUT（不在 Phase 1）

- `etc/config.properties` 解析（Phase 2）
- Postgres wire protocol（端口 7432）—— Java 默认开启，Go 无任何实现；归入 **drop-in gap 登记表**，
  Phase 1 不修复但 entrypoint 启动时**打印 warning**告知缺口
- 镜像注册到 ghcr.io / docker hub（CI/CD 推送）——交付到本地 `docker images` 即视为完成
- ARM/AMD 多架构构建（先 amd64 单架构；多架构留 Phase 1.5）
- 健康检查端点（Java 镜像也没有 HEALTHCHECK 指令；保持等价）

## 3. 架构与数据流

```
docker build .                                 ← multi-stage
  ├─ builder: golang:1.26-bookworm (Debian)
  │    apt install build-essential
  │    CGO_ENABLED=1 go build -ldflags='-s -w' -o wren-server ./cmd/wren-server
  │    → 产出静态链接 libduckdb 的 ~50-60 MB ELF (linux/amd64)
  │
  └─ runtime: debian:stable-slim
       apt install postgresql-client-13 ca-certificates  ← 维持 Java 镜像 parity
       WORKDIR /usr/src/app
       COPY --from=builder /build/wren-server ./
       COPY docker/entrypoint.sh ./
       CMD ./entrypoint.sh wren-server ${MAX_HEAP_SIZE:-512m} ${MIN_HEAP_SIZE:-64m}

docker run -v ${data}:/usr/src/app/etc go-wren-engine
  └─ entrypoint.sh
       echo "GOMEMLIMIT=${MAX_HEAP_SIZE}iB"  ← parse "512m"→"512MiB"
       export WREN_CONFIG_FILE=/usr/src/app/etc/config.properties  ← Phase 2 才解析，
                                                                     Phase 1 仅设 env
       warn "Postgres wire protocol (7432) not supported by go-wren-engine" if [ "$WARN_DROP_IN_GAPS" != "0" ]
       exec ./wren-server
```

构建走多阶段：builder 用完整 Go + gcc，runtime 用裸 debian-slim + 必要包。
运行时 binary 静态包含 `libduckdb`（go-duckdb v1.7.0 在 `deps/linux_amd64/libduckdb.a` 提供静态库），
对 libc 仍有动态依赖（glibc），所以**不能用 distroless-static / scratch**，可以用 distroless-base 但
Java 用的是 eclipse-temurin（Debian-based）—— 选 `debian:stable-slim` 保 Java parity。

## 4. 组件与文件结构

| 文件 | 操作 | 说明 |
|---|---|---|
| `docker/Dockerfile` | 新建 | 多阶段构建文件 |
| `docker/entrypoint.sh` | 新建 | 启动脚本（位置参数 + heap 转 GOMEMLIMIT + drop-in gap warning） |
| `Dockerfile`（仓库根） | 重写 | 单行 `# See docker/Dockerfile` + `include` 等价语法不存在 → 实际写一份引用 `docker/Dockerfile` 同内容或者用 `--file docker/Dockerfile` build 选项 |
| `docker-compose.yaml`（仓库根） | 重写 | build context + mount `./etc:/usr/src/app/etc` + ports 8080:8080 + env |
| `.dockerignore` | 新建 | 排除 build context |
| `docker/README.md` | 新建 | 「如何替换 WrenAI compose 中 wren-engine 镜像」3 步操作 |
| `Makefile`（仓库根，如果不存在） | 新建可选 | `make image` / `make image-run` / `make image-test` 三个 target |

文件布局**镜像 Java 仓库**（`wren-engine-0.9.3/docker/Dockerfile` + `wren-engine-0.9.3/docker/entrypoint.sh`），便于维护者交叉比对。

## 5. 实施切片（方案 C：增量切片）

每个切片：实现 → 验证 → commit。

### 切片 1 — 多阶段 Dockerfile

- 删除根目录 alpine-based `Dockerfile`
- 新增 `docker/Dockerfile` 多阶段（Debian-bookworm + Debian-stable-slim）
- 新增 `.dockerignore`
- 验证：`docker build -t go-wren-engine:phase1 -f docker/Dockerfile .` 成功
- 验证：`docker images go-wren-engine:phase1` size < 150 MB（debian-slim base ~30 MB + binary ~60 MB + postgresql-client-13 ~40 MB ≈ 130 MB）

### 切片 2 — entrypoint.sh + heap 参数

- 新增 `docker/entrypoint.sh`，接收 `$1=binary $2=maxHeap $3=minHeap`
- maxHeap 解析：`512m → 512MiB`, `4g → 4GiB`，落到 `GOMEMLIMIT` env
- minHeap 在 Go 下**不可表达**（无类似 -Xms 的初始 heap 概念）；entrypoint 打印 info-level 提示并忽略
- 启动时 echo 一行 drop-in gap 提示：`[INFO] Postgres wire protocol (port 7432) not supported by this go-wren-engine build`，用户可设 `WARN_DROP_IN_GAPS=0` 静默
- Dockerfile CMD 改为 `./entrypoint.sh wren-server ${MAX_HEAP_SIZE:-512m} ${MIN_HEAP_SIZE:-64m}`
- 验证：`docker run --rm go-wren-engine:phase1` 不立即退出；`docker logs` 显示 binary 启动 + 端口监听
- 验证：`docker run -e MAX_HEAP_SIZE=1g --rm go-wren-engine:phase1` 进程 env 含 `GOMEMLIMIT=1GiB`

### 切片 3 — docker-compose + drop-in 验证

- 重写仓库根 `docker-compose.yaml`：
  - `build: { context: ., dockerfile: docker/Dockerfile }`
  - `volumes: [./etc:/usr/src/app/etc]`（mount path 与 Java 镜像、WrenAI compose 一致）
  - `ports: ["8080:8080"]`
- 准备最小 `etc/config.properties` + `etc/mdl/sample.json` 示例
- 验证：`docker compose up` 后 `curl http://localhost:8080/v1/config` 返回 200 + 11 个 Java parity config entries
- 验证：`curl http://localhost:8080/v1/mdl/dry-plan` 带最小 manifest 返回 200 + 重写 SQL
- 验证：把 WrenAI 0.9.0 `docker/docker-compose.yaml` 里 `wren-engine: image: ghcr.io/canner/wren-engine:0.11.1` 改为 `wren-engine: build: ../go-wren-engine`（或本地 image 引用），`docker compose up` 后 wren-engine 服务 healthy 且 bootstrap-init.sh 能写盘

### 切片 4 — Makefile + README 文档

- 根 `Makefile` 新增 `image` / `image-run` / `image-test` / `image-clean` 四个 target
- `docker/README.md`：3 步替换 WrenAI compose 镜像的操作 + 已知 drop-in gap 清单链接
- 验证：`make image-test` 一键跑完 build + 容器内 smoke test

## 6. 风险登记表

| # | 风险 | 说明 |
|---|---|---|
| 1 | **go-duckdb v1.7.0 CGo + alpine musl 不兼容** | 必须用 Debian-based runtime；不能贪图 alpine 体积小。预期镜像 ~130 MB 与 Java image (~400 MB JVM-based) 已经大幅瘦身 |
| 2 | **Postgres wire 协议 (port 7432) 完全缺失** | Java 镜像默认监听 `0.0.0.0:7432`；Go 完全没实现。WrenAI 0.9.0 `docker-compose.yaml` 同时 `expose: [8080, 7432]`。如有客户端连 7432 走 Postgres 协议，drop-in 直接失败。**Phase 1 仅 warning，不修复**；记入 drop-in gap 登记表 |
| 3 | **postgresql-client-13 安装与否** | Java 安装这个包；grep `wren-engine-0.9.3` 源码无 `Runtime.exec("psql")` 调用，**理论上可去**。但 WrenAI ecosystem 的 bootstrap / ibis-server 可能依赖。**默认安装**保最大兼容 |
| 4 | **maxHeap → GOMEMLIMIT 单位转换** | Java 接受 `512m`/`4g`/`512MB`/`512`；Go GOMEMLIMIT 接受 `512MiB`/`4GiB`/`512000000`。entrypoint 用 sed/case 解析；不支持的格式 fallback 不设 GOMEMLIMIT 并 warn |
| 5 | **minHeap 在 Go 下不可表达** | Go runtime 没有 `-Xms` 等价概念。entrypoint 接收 `$3` 但 info-log 「ignored under Go runtime」。docker-compose / WrenAI 透传该参数也不报错 |
| 6 | **多架构 (linux/arm64)** | Apple Silicon 用户 + ARM K8s 集群常见。go-duckdb 也提供 arm64 `libduckdb.a`。但 Phase 1 先 amd64 单架构；arm64 留 Phase 1.5（buildx + QEMU） |
| 7 | **WrenAI 实际用 0.11.1，源码我们对的是 0.9.3** | brainstorming 之初基线是 0.9.3。但 WrenAI 0.9.0 `.env.example` 设 `WREN_ENGINE_VERSION=0.11.1`。HTTP API 表面在 0.9.3 → 0.11.1 是否有破坏性变化未审计。**Phase 1 不解决**；记入 gap，但 docker-compose 替换测试目标以 0.11.1 image 行为为准 |
| 8 | **Java 镜像不 EXPOSE 端口** | Dockerfile 不写 `EXPOSE 8080`（与 Java parity）；compose 透过 `ports:` 显式映射。文档要点 |
| 9 | **build cache 失效** | go mod download 应单独 COPY 进 layer 加速 cache；改源码不应重新下载依赖。已在 Dockerfile 模板里处理 |
| 10 | **CGO_ENABLED=1 编译失败** | builder 必须装 `build-essential`（gcc/g++）。golang:1.26-bookworm 自带 gcc，golang:1.26-alpine 没有 →故意避开 alpine builder |
| 11 | **entrypoint.sh 跨平台换行符** | Windows CRLF 提交进 git 会让 sh 找不到 `\r` 结尾的解释器。`.gitattributes` 设 `*.sh text eol=lf`；新增此文件 |
| 12 | **镜像默认 root 用户** | Java 镜像默认 root（Dockerfile 无 USER 指令）。保持 parity 不引入 USER nobody；运维侧由 K8s SecurityContext 收 |

## 7. 错误处理

- **docker build 失败**：CGo 编译 error / libduckdb 缺架构 / postgres-client 仓库换 key —— `make image-test` 必须捕获 build 失败的 exit code，CI 红
- **docker run 立即退出**：典型原因为 `etc/config.properties` 缺失（Phase 2 才补这条解析；Phase 1 阶段 Go 服务**不依赖**该文件即可启动 —— 这是 Go 当前行为 vs Java fatal 的不等价点，Phase 2 收尾）
- **entrypoint heap 解析错**：fallback 不设 GOMEMLIMIT + stderr warn；不阻塞启动
- **端口 8080 占用**：Go server.go 启动失败 → 立即退出非零；用户从 logs 看到典型 `bind: address already in use`
- **drop-in gap 提示**：默认 entrypoint 启动时打印一行 `[INFO] Postgres wire (7432) / config.properties parsing not yet supported in this build`，可由 `WARN_DROP_IN_GAPS=0` 静默；不阻塞启动

## 8. 测试与验收

### 验收标准

- ✅ `docker build -t go-wren-engine:phase1 -f docker/Dockerfile .` exit 0
- ✅ 镜像 size ≤ 150 MB
- ✅ `docker run -d -p 8080:8080 -v $PWD/etc:/usr/src/app/etc go-wren-engine:phase1` 进程持续运行 ≥ 10 秒
- ✅ `curl -s localhost:8080/v1/config | jq 'length'` = 11
- ✅ `curl -s localhost:8080/v1/config/wren.datasource.type | jq -r .value` = `"DUCKDB"`
- ✅ `curl -s -X POST localhost:8080/v1/mdl/validate/column_is_valid -d '{...}'` 返回结构化 JSON（不挂）
- ✅ 把 WrenAI 0.9.0 `docker/docker-compose.yaml` 中 `wren-engine.image` 行替换为本地构建的镜像 ref（或 build:），`docker compose up bootstrap wren-engine` 两个服务都进入 healthy 状态，`bootstrap` 把 `config.properties` 写入 mount volume
- ✅ `docker logs wren-engine` 含 `[INFO] Postgres wire (7432) not supported` 一行
- ✅ `MAX_HEAP_SIZE=1g docker run ... go-wren-engine:phase1` 容器内 `cat /proc/1/environ | tr '\0' '\n' | grep GOMEMLIMIT` 输出 `GOMEMLIMIT=1GiB`
- ✅ `go build ./...` `go vet ./...` `gofmt -l .` 三者无新增警告（Phase 1 不改 Go 源码，应当 trivially 通过）

### 测试方式

- **build 测试**：`make image-test` 一键脚本：`docker build` → `docker run -d` → `sleep 5` → `curl localhost:8080/v1/config` → `docker kill`
- **drop-in 测试**：手动在 `../WrenAI-0.9.0/docker/` 复制一份 compose 并改 `image:` 行，跑 `docker compose up bootstrap wren-engine`；2 分钟内目测两个服务 healthy
- **size 检查**：CI 加一行 `docker images go-wren-engine:phase1 --format '{{.Size}}'` 断言 ≤ 150 MB
- **heap env 检查**：CI 用 `docker run --rm -e MAX_HEAP_SIZE=2g go-wren-engine:phase1 sh -c 'env | grep GOMEMLIMIT'` 断言含 `GOMEMLIMIT=2GiB`

## 9. 与其他阶段的关系

- **依赖 P0–P6**：Phase 1 仅打包现有 Go 二进制，需要 Go server 能正常起来。P0-P6 已合并即满足
- **承接 Phase 2**：Phase 1 entrypoint 仅设 `WREN_CONFIG_FILE` env，**不实际解析**；Phase 2 才在 `main.go` 加 `LoadFromProperties(env_path)` 并满足 Java 「缺文件即 fatal」语义
- **不影响 Phase 3/4/5**：TPC-H golden 补齐、envelope bug 修复、P7 dynamic-field 都在源码层；Phase 1 完成只是让"打个镜像跑起来"成为可重复动作

### Drop-in gap 登记表（跨阶段）

Phase 1 不修复但要文档化的 drop-in 缺口：

| Gap | 涉及 | 影响 | 后续阶段 |
|---|---|---|---|
| `etc/config.properties` 不被解析 | main.go | mount 的 properties 文件被忽略；Java 默认行为不可控制 | Phase 2 |
| Postgres wire protocol（端口 7432）未实现 | internal/wire/* 整个新模块 | 任何通过 PG 协议连 wren-engine 的客户端 100% 失败 | 待评估，可能 Phase 6 |
| `PATCH /v1/config` 不写盘 | config_handler.go + config.go | Java 持久化到 `etc/config.properties`，Go 仅改内存 → 重启丢失 | Phase 2 一并补 |
| `WrenSqlRewrite` 动态字段分支 | internal/rewrite/* | WrenAI bootstrap 强制 `enable-dynamic-fields=true`，Go 当前走静态分支 = 语义偏差 | Phase 5 (P7) |
| WrenAI 0.9.0 实际锁 0.11.1 image | 整体 | 0.9.3 → 0.11.1 间的 HTTP API 兼容性未审计 | 评估后决定是否补 P8 |

---

- 本 spec 完成后进入 `gpowers:writing-plans`，产出 Phase 1 逐任务实施计划。
