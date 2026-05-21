#!/usr/bin/env bash
set -euo pipefail

IMAGE="${WREN_ORACLE_IMAGE:-ghcr.io/canner/wren-engine:0.9.3}"
PORT="${WREN_ORACLE_PORT:-18080}"
MOUNT_DIR="${WREN_ORACLE_MOUNT:-}"

# Derive mount dir from sibling wren-engine-0.9.3 checkout if not set
if [ -z "$MOUNT_DIR" ]; then
    SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
    # Try relative to project root (tools/.. is project root)
    CANDIDATE="$(cd "$SCRIPT_DIR/.." 2>/dev/null && cd ../wren-engine-0.9.3/example/duckdb-tpch-example/etc 2>/dev/null && pwd)" || true
    if [ -n "$CANDIDATE" ] && [ -d "$CANDIDATE" ]; then
        MOUNT_DIR="$CANDIDATE"
    fi
fi

if [ -z "$MOUNT_DIR" ] || [ ! -d "$MOUNT_DIR" ]; then
    echo "ERROR: oracle mount directory not found." >&2
    echo "Please clone canner/wren-engine tag 0.9.3 into a sibling directory:" >&2
    echo "  cd .. && git clone --branch 0.9.3 https://github.com/canner/wren-engine.git wren-engine-0.9.3" >&2
    echo "Or set WREN_ORACLE_MOUNT to the path containing etc/config.properties." >&2
    exit 1
fi

# Check if already running
if docker ps --format '{{.Names}}' | grep -q '^wren-oracle$'; then
    echo "wren-oracle already running on port $PORT"
    exit 0
fi

echo "Pulling oracle image: $IMAGE"
if ! docker pull "$IMAGE" 2>&1; then
    echo "WARNING: docker pull failed." >&2
    echo "If you're behind a firewall, configure a Docker registry mirror," >&2
    echo "or pre-load the image with: docker load -i wren-engine-0.9.3.tar" >&2
    exit 1
fi

echo "Starting oracle container (name=wren-oracle, port=$PORT)..."
cid=$(docker run -d \
    --name wren-oracle \
    -p "${PORT}:8080" \
    -v "${MOUNT_DIR}:/usr/src/app/etc:ro" \
    -e MAX_HEAP_SIZE=2g \
    -e MIN_HEAP_SIZE=512m \
    "$IMAGE")

echo "Waiting for oracle readiness (container ${cid:0:12})..."
ready=""
for i in $(seq 1 30); do
    if curl -sf --max-time 2 "http://localhost:${PORT}/v1/config" >/dev/null 2>&1; then
        ready=1
        break
    fi
    sleep 2
done

if [ -z "${ready}" ]; then
    echo "ERROR: oracle failed to start within 60s" >&2
    docker logs --tail 30 wren-oracle >&2
    docker rm -f wren-oracle >/dev/null 2>&1 || true
    exit 1
fi

count=$(curl -sf --max-time 5 "http://localhost:${PORT}/v1/config" | grep -o '"name"' | wc -l | tr -d ' ')
echo "Oracle ready: http://localhost:${PORT} ($count config entries)"
