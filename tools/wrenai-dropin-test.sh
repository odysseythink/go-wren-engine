#!/usr/bin/env bash
# End-to-end drop-in test: replace wren-engine.image in WrenAI 0.9.0 compose
# with the local go-wren-engine image, run smoke queries, then restore.
set -euo pipefail

WRENAI_DIR="${WRENAI_DIR:-../WrenAI}"
IMAGE_NAME="${IMAGE_NAME:-go-wren-engine:latest}"
COMPOSE_FILE="$WRENAI_DIR/docker/docker-compose.yaml"

if [ ! -f "$COMPOSE_FILE" ]; then
    echo "ERROR: WrenAI compose file not found at $COMPOSE_FILE" >&2
    echo "Set WRENAI_DIR to the WrenAI 0.9.0 checkout." >&2
    exit 1
fi

# Build local image if not present
if ! docker image inspect "$IMAGE_NAME" >/dev/null 2>&1; then
    echo "Building local image $IMAGE_NAME..."
    make image IMAGE_NAME="$(echo "$IMAGE_NAME" | cut -d: -f1)" IMAGE_TAG="$(echo "$IMAGE_NAME" | cut -d: -f2)"
fi

BACKUP="$COMPOSE_FILE.bak.$(date +%s)"
cp "$COMPOSE_FILE" "$BACKUP"

cleanup() {
    echo "==> Restoring docker-compose.yaml..."
    cp "$BACKUP" "$COMPOSE_FILE"
    echo "==> Shutting down compose..."
    (cd "$WRENAI_DIR/docker" && docker compose down) >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Replace image line
perl -pi -e "s|ghcr\.io/canner/wren-engine:.*|$IMAGE_NAME|" "$COMPOSE_FILE"

echo "==> Starting WrenAI compose (bootstrap + wren-engine + ibis-server)..."
cd "$WRENAI_DIR/docker"
docker compose up -d bootstrap wren-engine ibis-server

echo "==> Waiting for services..."
for i in $(seq 1 60); do
    if curl -sf --max-time 5 http://localhost:8080/v1/config >/dev/null 2>&1; then
        break
    fi
    sleep 2
done

if ! curl -sf --max-time 5 http://localhost:8080/v1/config >/dev/null 2>&1; then
    echo "FAIL: wren-engine not reachable" >&2
    exit 1
fi
echo "PASS: wren-engine /v1/config reachable"

# Dry-plan smoke with a minimal MDL
SMOKE_MANIFEST='{"catalog":"wren","schema":"wren","models":[{"name":"Orders","baseObject":"orders","primaryKey":"orderkey","columns":[{"name":"orderkey","type":"integer"},{"name":"totalprice","type":"double"}]}]}'
PLAN_RESULT=$(curl -sf --max-time 10 -X POST http://localhost:8080/v1/mdl/dry-plan \
    -H "Content-Type: application/json" \
    -d "{\"manifest\":$SMOKE_MANIFEST,\"sql\":\"SELECT orderkey FROM Orders\"}" || true)

if [ -z "$PLAN_RESULT" ]; then
    echo "FAIL: dry-plan returned empty" >&2
    exit 1
fi

if echo "$PLAN_RESULT" | grep -q '"sql"'; then
    echo "PASS: dry-plan returned SQL"
else
    echo "FAIL: dry-plan response missing 'sql' field" >&2
    echo "$PLAN_RESULT" >&2
    exit 1
fi

echo "==> Drop-in test PASSED"
