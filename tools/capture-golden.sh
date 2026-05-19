#!/usr/bin/env bash
# 启动 Java wren-engine:0.9.3 作为 oracle, 捕获 difftest golden, 然后销毁容器.
set -euo pipefail

IMAGE="ghcr.io/canner/wren-engine:0.9.3"
PORT="18080"
ETC_DIR="$(cd "$(dirname "$0")/oracle-etc" && pwd)"

cid="$(docker run -d -p "${PORT}:8080" -v "${ETC_DIR}:/usr/src/app/etc" "${IMAGE}")"
trap 'docker rm -f "${cid}" >/dev/null 2>&1 || true' EXIT

echo "等待 oracle 就绪 (容器 ${cid:0:12})..."
ready=""
for _ in $(seq 1 60); do
  if curl -sf "http://localhost:${PORT}/v1/config" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 2
done
if [ -z "${ready}" ]; then
  echo "oracle 启动失败, 容器日志末尾:" >&2
  docker logs --tail 40 "${cid}" >&2
  exit 1
fi

echo "oracle 就绪, 开始捕获..."
go run ./cmd/capture-golden -addr "http://localhost:${PORT}"
