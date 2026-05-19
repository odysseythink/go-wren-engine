#!/usr/bin/env bash
# 把 Java SqlFormatter 的输出冻结成 P2 差分测试的 golden 文件.
# 需要本机有 java(17+) 与 mvn, 且 ../wren-engine-0.9.3 源码可用.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WREN_JAVA="${WREN_JAVA:-${REPO_ROOT}/../wren-engine-0.9.3}"

if [ ! -d "${WREN_JAVA}/trino-parser" ]; then
  echo "找不到 trino-parser 源码: ${WREN_JAVA}/trino-parser" >&2
  echo "设置 WREN_JAVA 环境变量指向 wren-engine-0.9.3 检出目录" >&2
  exit 1
fi

# 1) 把 io.wren:trino-parser:0.9.3 装进本地 m2 (幂等; 已装则很快).
echo "安装 trino-parser 到本地 Maven 仓库..."
( cd "${WREN_JAVA}" && mvn -q -pl trino-parser -am -DskipTests install )

# 2) 编译并运行 oracle, 工作目录 = 仓库根, 使 testdata 相对路径生效.
echo "运行 format oracle..."
( cd "${REPO_ROOT}/tools/format-oracle" && mvn -q compile )
( cd "${REPO_ROOT}" && mvn -q -f tools/format-oracle/pom.xml exec:java )
