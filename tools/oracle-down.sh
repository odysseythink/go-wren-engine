#!/usr/bin/env bash
set -euo pipefail
docker rm -f wren-oracle >/dev/null 2>&1 || true
echo "Oracle stopped"
