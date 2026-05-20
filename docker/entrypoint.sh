#!/usr/bin/env bash
set -euo pipefail

# Usage: ./entrypoint.sh <binary> <maxHeap> <minHeap>
# Mirrors Java image entrypoint argv convention.

BINARY="${1:-wren-server}"
MAX_HEAP="${MAX_HEAP_SIZE:-${2:-512m}}"
MIN_HEAP="${MIN_HEAP_SIZE:-${3:-64m}}"

# ── GOMEMLIMIT conversion ───────────────────────────────────────
# Java heap args: 512m, 4g, 512MB, 512
# Go GOMEMLIMIT: 512MiB, 4GiB, 512000000
# Pure numbers are treated as bytes and passed through unchanged.

convert_heap_to_gomemlimit() {
    local input="$1"
    local num unit

    # Strip trailing 'B' / 'b' (e.g. 512MB -> 512M)
    input="${input%[Bb]}"

    # Extract trailing unit letter if present
    if [[ "$input" =~ ^([0-9]+)([mMgG])$ ]]; then
        num="${BASH_REMATCH[1]}"
        unit="${BASH_REMATCH[2]}"
        case "$unit" in
            m|M) echo "${num}MiB" ;;
            g|G) echo "${num}GiB" ;;
        esac
        return 0
    elif [[ "$input" =~ ^[0-9]+$ ]]; then
        # Pure number — bytes. Go accepts this directly.
        echo "$input"
        return 0
    else
        return 1
    fi
}

GOMEMLIMIT=""
if converted="$(convert_heap_to_gomemlimit "$MAX_HEAP")"; then
    GOMEMLIMIT="$converted"
    export GOMEMLIMIT
    echo "[INFO] GOMEMLIMIT set to ${GOMEMLIMIT}"
else
    echo "[WARN] Unrecognized MAX_HEAP_SIZE format '${MAX_HEAP}'. GOMEMLIMIT not set." >&2
fi

# ── minHeap ─────────────────────────────────────────────────────
# Go runtime has no equivalent to Java's -Xms (initial heap size).
# We accept the argument for compatibility but log that it is ignored.
if [[ -n "$MIN_HEAP" && "$MIN_HEAP" != "0" ]]; then
    echo "[INFO] MIN_HEAP_SIZE=${MIN_HEAP} ignored under Go runtime (no -Xms equivalent)"
fi

# ── Config file env (Phase 2 will implement parsing) ────────────
export WREN_CONFIG_FILE="/usr/src/app/etc/config.properties"

# ── Drop-in gap warnings ────────────────────────────────────────
if [[ "${WARN_DROP_IN_GAPS:-1}" != "0" ]]; then
    echo "[INFO] Postgres wire protocol (port 7432) not supported by this go-wren-engine build"
fi

# ── Launch binary ───────────────────────────────────────────────
echo "[INFO] Starting ${BINARY}..."
exec "./${BINARY}"
