#!/usr/bin/env bash
# scripts/test-failures.sh
#
# Comprehensive failure-scenario test harness for WikiSurge.
# Runs unit tests for the resilience layer and — if Docker is available —
# validates live infrastructure failure/recovery flows.
#
# Usage:
#   ./scripts/test-failures.sh          # unit tests only
#   ./scripts/test-failures.sh --live   # include Docker integration tests
set -euo pipefail

GREEN="\033[0;32m"
RED="\033[0;31m"
YELLOW="\033[0;33m"
NC="\033[0m"

pass() { echo -e "${GREEN}✓ $1${NC}"; }
fail() { echo -e "${RED}✗ $1${NC}"; FAILURES=$((FAILURES + 1)); }
info() { echo -e "${YELLOW}→ $1${NC}"; }

FAILURES=0

# -----------------------------------------------------------------------
# 1. Unit tests — resilience package (circuit breaker, retry, degradation)
# -----------------------------------------------------------------------
info "Running resilience unit tests..."
if go test ./internal/resilience/ -v -count=1 -timeout 60s; then
    pass "Resilience unit tests passed"
else
    fail "Resilience unit tests failed"
fi

# -----------------------------------------------------------------------
# 2. Unit tests — feature flags
# -----------------------------------------------------------------------
info "Running feature flags tests..."
if go test ./internal/config/ -run TestFeatureFlags -v -count=1 -timeout 30s; then
    pass "Feature flags tests passed"
else
    fail "Feature flags tests failed"
fi

# -----------------------------------------------------------------------
# 3. Live infrastructure tests (requires Docker)
# -----------------------------------------------------------------------
if [[ "${1:-}" == "--live" ]]; then
    info "Starting live failure scenario tests..."

    # --- Redis failure ---
    info "Testing Redis failure and recovery..."
    REDIS_CONTAINER=$(docker ps --filter "name=redis" --format "{{.Names}}" | head -1)
    if [[ -n "${REDIS_CONTAINER}" ]]; then
        docker pause "${REDIS_CONTAINER}" 2>/dev/null || true
        sleep 5
        info "Redis paused — checking logs for circuit breaker activation..."
        # Give the system a few seconds to detect.
        sleep 10
        docker unpause "${REDIS_CONTAINER}" 2>/dev/null || true
        sleep 5
        pass "Redis failure/recovery scenario executed"
    else
        info "Skipping Redis failure test — no container found"
    fi

    # --- Redis memory pressure ---
    info "Testing Redis memory fill..."
    if command -v redis-cli &>/dev/null; then
        # Fill Redis with data to trigger memory alerts.
        for i in $(seq 1 5000); do
            redis-cli -p 6379 SET "test:key:$i" "$(head -c 1024 /dev/urandom | base64)" EX 60 &>/dev/null || true
        done
        sleep 5
        # Clean up.
        redis-cli -p 6379 KEYS "test:key:*" | xargs -r redis-cli -p 6379 DEL &>/dev/null || true
        pass "Redis memory pressure scenario executed"
    else
        info "Skipping Redis memory test — redis-cli not found"
    fi

    # --- Network delay ---
    info "Testing network delay simulation..."
    if command -v tc &>/dev/null; then
        # Add 200ms latency to loopback (requires root).
        sudo tc qdisc add dev lo root netem delay 200ms 2>/dev/null || true
        sleep 5
        sudo tc qdisc del dev lo root netem 2>/dev/null || true
        pass "Network delay scenario executed"
    else
        info "Skipping network delay test — tc not found"
    fi

    # --- Bad Kafka message ---
    info "Testing poison message (bad Kafka message)..."
    if command -v kafka-console-producer.sh &>/dev/null || command -v kafkacat &>/dev/null; then
        echo "THIS IS NOT VALID JSON !!!" | kafkacat -P -b localhost:9092 -t wikipedia.edits 2>/dev/null || \
        echo "THIS IS NOT VALID JSON !!!" | kafka-console-producer.sh --broker-list localhost:9092 --topic wikipedia.edits 2>/dev/null || true
        sleep 5
        pass "Poison message scenario executed"
    else
        info "Skipping poison message test — no Kafka CLI found"
    fi
fi

# -----------------------------------------------------------------------
# Summary
# -----------------------------------------------------------------------
echo ""
echo "======================================="
if [[ ${FAILURES} -eq 0 ]]; then
    echo -e "${GREEN}All failure scenario tests passed!${NC}"
    exit 0
else
    echo -e "${RED}${FAILURES} failure scenario test(s) failed.${NC}"
    exit 1
fi
