#!/usr/bin/env bash
set -euo pipefail

# Integration smoke test for UKPS
# Prerequisites: docker, go, curl
# Starts DBs via compose-dev.yml, builds & runs each service, runs HTTP tests.

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COMPOSE_DEV="$ROOT/compose-dev.yml"

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

PASS=0
FAIL=0

ok()   { PASS=$((PASS+1)); echo -e "  ${GREEN}✓${NC} $1"; }
fail() { FAIL=$((FAIL+1)); echo -e "  ${RED}✗${NC} $1"; }

cleanup() {
    echo ""
    echo "=== Cleaning up ==="
    # Kill service processes
    for pid in "$CHAPS_PID" "$FPS_PID" "$BACS_PID"; do
        [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
    done
    # Stop docker containers
    docker compose -f "$COMPOSE_DEV" down -v 2>/dev/null || true
    wait 2>/dev/null || true
    echo "Done."
}
trap cleanup EXIT INT TERM

echo "=== UKPS Integration Smoke Tests ==="
echo "Root: $ROOT"
echo ""

# 1. Check prerequisites
echo "--- Checking prerequisites ---"
command -v docker >/dev/null 2>&1 && ok "docker available" || { fail "docker not found"; exit 1; }
command -v go >/dev/null 2>&1 && ok "go available" || { fail "go not found"; exit 1; }
command -v curl >/dev/null 2>&1 && ok "curl available" || { fail "curl not found"; exit 1; }

# 2. Start databases
echo ""
echo "--- Starting databases ---"
docker compose -f "$COMPOSE_DEV" up -d 2>&1
ok "docker compose up"

echo ""
echo "--- Waiting for databases to be ready ---"
MAX_WAIT=60
wait_for_db() {
    local name=$1 port=$2 user=$3 db=$4
    local waited=0
    while [ $waited -lt $MAX_WAIT ]; do
        if docker exec "$name" pg_isready -U "$user" -d "$db" >/dev/null 2>&1; then
            ok "$name ready after ${waited}s"
            return 0
        fi
        sleep 2
        waited=$((waited+2))
    done
    fail "$name not ready after ${MAX_WAIT}s"
    return 1
}

# Containers may not exist immediately; wait a beat
sleep 3
wait_for_db "chaps_db_dev" 5432 chaps_admin chaps_ledger
wait_for_db "fps_db_dev" 5433 fps_admin fps_ledger
wait_for_db "bacs_db_dev" 5434 bacs_admin bacs_ledger

# 3. Build and start services
echo ""
echo "--- Building services ---"
build_and_start() {
    local name=$1 dir=$2 port=$3 db_url=$4
    echo "  Building $name..."
    go build -o "$ROOT/test/$name-server" "$dir/cmd/server/main.go" 2>&1
    echo "  Starting $name on port $port..."
    DATABASE_URL="$db_url" "$ROOT/test/$name-server" &
    local pid=$!
    eval "${name}_PID=\$pid"
    # Give it a moment to start
    sleep 1
    if kill -0 "$pid" 2>/dev/null; then
        ok "$name started (PID $pid)"
    else
        fail "$name failed to start"
    fi
}

build_and_start "chaps" "$ROOT/chaps-service" 8080 "postgres://chaps_admin:password123@127.0.0.1:5432/chaps_ledger"
build_and_start "fps" "$ROOT/fps-service" 8081 "postgres://fps_admin:password123@127.0.0.1:5433/fps_ledger"
build_and_start "bacs" "$ROOT/bacs-service" 8082 "postgres://bacs_admin:password123@127.0.0.1:5434/bacs_ledger"

# 4. Smoke tests
echo ""
echo "--- Running smoke tests ---"

expect_http() {
    local desc=$1 method=$2 url=$3 expected_code=$4 data=${5:-}
    local code
    if [ -n "$data" ]; then
        code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" -H "Content-Type: application/json" -d "$data" "$url")
    else
        code=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" "$url")
    fi
    if [ "$code" = "$expected_code" ]; then
        ok "$desc ($code)"
    else
        fail "$desc — got $code, expected $expected_code"
    fi
}

expect_body() {
    local desc=$1 method=$2 url=$3 expected_pattern=$4 data=${5:-}
    local body
    if [ -n "$data" ]; then
        body=$(curl -s -X "$method" -H "Content-Type: application/json" -d "$data" "$url")
    else
        body=$(curl -s -X "$method" "$url")
    fi
    if echo "$body" | grep -q "$expected_pattern"; then
        ok "$desc (matches '$expected_pattern')"
    else
        fail "$desc — body did not contain '$expected_pattern': $(echo "$body" | head -c 200)"
    fi
}

# Give services a moment to connect to DB
sleep 2

# CHAPS smoke tests
echo "  --- CHAPS ---"
expect_http "CHAPS: list participants" GET "http://127.0.0.1:8080/v1/participants/chaps" 200
expect_body "CHAPS: list has seeded banks" GET "http://127.0.0.1:8080/v1/participants/chaps" "SNDRUK22"
expect_http "CHAPS: get single participant" GET "http://127.0.0.1:8080/v1/participants/chaps/SNDRUK22" 200
expect_http "CHAPS: get non-existent" GET "http://127.0.0.1:8080/v1/participants/chaps/XXXXXXXX" 404

# FPS smoke tests
echo "  --- FPS ---"
expect_http "FPS: list participants" GET "http://127.0.0.1:8081/v1/participants/fps" 200
expect_body "FPS: list has seeded banks" GET "http://127.0.0.1:8081/v1/participants/fps" "SNDRUK22"
expect_http "FPS: get single participant" GET "http://127.0.0.1:8081/v1/participants/fps/SNDRUK22" 200
expect_http "FPS: get DNS cycles" GET "http://127.0.0.1:8081/v1/fps/cycles" 200

# BACS smoke tests
echo "  --- BACS ---"
expect_http "BACS: list participants" GET "http://127.0.0.1:8082/v1/participants/bacs" 200
expect_body "BACS: list has seeded banks" GET "http://127.0.0.1:8082/v1/participants/bacs" "SNDRUK22"
expect_http "BACS: get cycles" GET "http://127.0.0.1:8082/v1/bacs/cycles" 200

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ $FAIL -eq 0 ] && exit 0 || exit 1
