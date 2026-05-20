#!/bin/bash
set -euo pipefail
PORT="${PORT:-18080}"
go run ./cmd/capture-duckdb-golden -addr "http://localhost:${PORT}"
