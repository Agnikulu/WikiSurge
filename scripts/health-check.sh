#!/usr/bin/env bash
# =============================================================================
# WikiSurge - Health Check Script
# =============================================================================
# Checks the health of all WikiSurge services.
#
# Exit Codes:
#   0 - All components healthy
#   1 - One or more components unhealthy
#
# Usage:
#   ./scripts/health-check.sh          # Check all services
#   ./scripts/health-check.sh --json   # JSON output
#   ./scripts/health-check.sh --quiet  # Exit code only
# =============================================================================

set -euo pipefail

# ---------- Configuration ----------
KAFKA_ADMIN_URL="${KAFKA_ADMIN_URL:-localhost:9644}"
REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"
ES_URL="${ES_URL:-http://localhost:9200}"
API_URL="${API_URL:-http://localhost:8080}"
METRICS_URL="${METRICS_URL:-http://localhost:2112}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:9090}"
GRAFANA_URL="${GRAFANA_URL:-http://localhost:3000}"

# ---------- Colors ----------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

# ---------- State ----------
OVERALL_STATUS=0
JSON_MODE=false
QUIET_MODE=false
declare -A RESULTS

# ---------- Parse Arguments ----------
for arg in "$@"; do
    case $arg in
        --json)   JSON_MODE=true ;;
        --quiet)  QUIET_MODE=true ;;
        --help|-h)
            echo "Usage: $0 [--json] [--quiet]"
            echo "  --json   Output results as JSON"
            echo "  --quiet  Suppress output, exit code only"
            exit 0
            ;;
    esac
done

# ---------- Helper Functions ----------
log() {
    if ! $QUIET_MODE && ! $JSON_MODE; then
        echo -e "$@"
    fi
}

check_service() {
    local name="$1"
    local check_cmd="$2"
    local start_time
    start_time=$(date +%s%N)

    if eval "$check_cmd" > /dev/null 2>&1; then
        local end_time
        end_time=$(date +%s%N)
        local latency_ms=$(( (end_time - start_time) / 1000000 ))
        RESULTS["$name"]="healthy:${latency_ms}ms"
        log "${GREEN}✓${NC} $name: healthy (${latency_ms}ms)"
    else
        RESULTS["$name"]="unhealthy"
        OVERALL_STATUS=1
        log "${RED}✗${NC} $name: unhealthy"
    fi
}

# ---------- Banner ----------
log ""
log "=========================================="
log "  WikiSurge Health Check"
log "  $(date '+%Y-%m-%d %H:%M:%S')"
log "=========================================="
log ""

# ---------- Service Checks ----------

# Kafka (Redpanda)
check_service "kafka" \
    "docker exec wikisurge-kafka rpk cluster health --api-urls ${KAFKA_ADMIN_URL} 2>/dev/null || curl -sf http://${KAFKA_ADMIN_URL}/v1/cluster/health_overview"

# Redis
check_service "redis" \
    "redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT} ping 2>/dev/null || docker exec wikisurge-redis redis-cli ping"

# Elasticsearch
check_service "elasticsearch" \
    "curl -sf '${ES_URL}/_cluster/health?wait_for_status=yellow&timeout=5s'"

# API
check_service "api" \
    "curl -sf '${API_URL}/health'"

# Metrics
check_service "metrics" \
    "curl -sf '${METRICS_URL}/metrics' | head -1"

# Prometheus
check_service "prometheus" \
    "curl -sf '${PROMETHEUS_URL}/-/healthy'"

# Grafana
check_service "grafana" \
    "curl -sf '${GRAFANA_URL}/api/health'"

# ---------- Summary ----------
log ""
if [ $OVERALL_STATUS -eq 0 ]; then
    log "${GREEN}All services healthy${NC}"
else
    log "${RED}One or more services unhealthy${NC}"
fi
log ""

# ---------- JSON Output ----------
if $JSON_MODE; then
    echo "{"
    echo "  \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\","
    echo "  \"overall\": \"$([ $OVERALL_STATUS -eq 0 ] && echo healthy || echo unhealthy)\","
    echo "  \"services\": {"
    local first=true
    for service in "${!RESULTS[@]}"; do
        if ! $first; then echo ","; fi
        first=false
        local status="${RESULTS[$service]}"
        local health="${status%%:*}"
        local latency="${status#*:}"
        if [ "$health" = "healthy" ]; then
            printf "    \"%s\": {\"status\": \"healthy\", \"latency\": \"%s\"}" "$service" "$latency"
        else
            printf "    \"%s\": {\"status\": \"unhealthy\"}" "$service"
        fi
    done
    echo ""
    echo "  }"
    echo "}"
fi

exit $OVERALL_STATUS
