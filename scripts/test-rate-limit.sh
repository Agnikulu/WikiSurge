#!/usr/bin/env bash
# test-rate-limit.sh – smoke-test rate limiting, security headers, and request validation
# against a running WikiSurge API server (default: localhost:8080).
set -euo pipefail

BASE_URL="${1:-http://localhost:8080}"
PASS=0
FAIL=0

green()  { printf "\033[32m%s\033[0m\n" "$*"; }
red()    { printf "\033[31m%s\033[0m\n" "$*"; }
yellow() { printf "\033[33m%s\033[0m\n" "$*"; }

check() {
  local desc="$1"; shift
  if "$@"; then
    green "  ✓ $desc"
    ((PASS++))
  else
    red "  ✗ $desc"
    ((FAIL++))
  fi
}

# ─────────────────────────────────────────────────────────────────────────────
# 1. Security headers
# ─────────────────────────────────────────────────────────────────────────────
echo ""
yellow "=== Security Headers ==="

HEADERS=$(curl -sI "${BASE_URL}/api/stats")

check "X-Content-Type-Options present" \
  grep -qi "X-Content-Type-Options: nosniff" <<< "$HEADERS"

check "X-Frame-Options present" \
  grep -qi "X-Frame-Options: DENY" <<< "$HEADERS"

check "X-XSS-Protection present" \
  grep -qi "X-XSS-Protection:" <<< "$HEADERS"

check "Strict-Transport-Security present" \
  grep -qi "Strict-Transport-Security:" <<< "$HEADERS"

check "Content-Security-Policy present" \
  grep -qi "Content-Security-Policy:" <<< "$HEADERS"

check "X-Request-ID present" \
  grep -qi "X-Request-ID:" <<< "$HEADERS"

# ─────────────────────────────────────────────────────────────────────────────
# 2. Rate limit headers on normal request
# ─────────────────────────────────────────────────────────────────────────────
echo ""
yellow "=== Rate Limit Headers ==="

HEADERS=$(curl -sI "${BASE_URL}/api/stats")

check "X-RateLimit-Limit present" \
  grep -qi "X-RateLimit-Limit:" <<< "$HEADERS"

check "X-RateLimit-Remaining present" \
  grep -qi "X-RateLimit-Remaining:" <<< "$HEADERS"

check "X-RateLimit-Reset present" \
  grep -qi "X-RateLimit-Reset:" <<< "$HEADERS"

# ─────────────────────────────────────────────────────────────────────────────
# 3. Rate limiting (send requests until we get 429)
# ─────────────────────────────────────────────────────────────────────────────
echo ""
yellow "=== Rate Limiting (sending requests to /api/search – limit 100) ==="

GOT_429=false
for i in $(seq 1 150); do
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${BASE_URL}/api/search?q=test")
  if [ "$STATUS" = "429" ]; then
    GOT_429=true
    echo "  → Got 429 after $i requests"
    break
  fi
done

check "Rate limit enforced (429 received)" $GOT_429

# Verify 429 body
if $GOT_429; then
  BODY=$(curl -s "${BASE_URL}/api/search?q=test")
  check "429 body contains RATE_LIMIT code" \
    grep -q "RATE_LIMIT" <<< "$BODY"

  RETRY_AFTER=$(curl -sI "${BASE_URL}/api/search?q=test" | grep -i "Retry-After:" | tr -d '\r')
  check "Retry-After header present on 429" \
    [ -n "$RETRY_AFTER" ]
fi

# ─────────────────────────────────────────────────────────────────────────────
# 4. Request validation
# ─────────────────────────────────────────────────────────────────────────────
echo ""
yellow "=== Request Validation ==="

# Method not allowed
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "${BASE_URL}/api/stats")
check "DELETE returns 405" [ "$STATUS" = "405" ]

# Path traversal
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${BASE_URL}/api/../etc/passwd")
check "Path traversal returns 400" [ "$STATUS" = "400" ]

# ─────────────────────────────────────────────────────────────────────────────
# 5. Redis keys
# ─────────────────────────────────────────────────────────────────────────────
echo ""
yellow "=== Redis Rate Limit Keys ==="
KEYS=$(redis-cli KEYS 'ratelimit:*' 2>/dev/null || echo "(redis-cli not available)")
echo "  $KEYS"

# ─────────────────────────────────────────────────────────────────────────────
# Summary
# ─────────────────────────────────────────────────────────────────────────────
echo ""
yellow "=== Summary ==="
green "  Passed: $PASS"
if [ "$FAIL" -gt 0 ]; then
  red "  Failed: $FAIL"
  exit 1
else
  green "  All checks passed!"
fi
