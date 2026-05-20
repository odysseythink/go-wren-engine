#!/bin/bash
set -euo pipefail
PORT="${PORT:-18080}"
go run ./cmd/capture-envelope-golden -addr "http://localhost:${PORT}"
