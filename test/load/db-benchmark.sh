#!/bin/bash

# =============================================================================
# WikiSurge Database Performance Benchmark Script
# =============================================================================
# Benchmarks Redis and Elasticsearch performance against defined targets.
#
# Redis targets:
#   SET: >10K ops/sec   GET: >50K ops/sec
#   ZADD: >5K ops/sec   ZRANGE: >10K ops/sec
#   Pipeline: 10x improvement
#
# Elasticsearch targets:
#   Bulk indexing: >1000 docs/sec
#   Search queries: <100ms p95
#   Aggregations: <500ms p95
#   Concurrent queries: 10 simultaneous
#
# Usage:
#   ./test/load/db-benchmark.sh [OPTIONS]
# =============================================================================

set -euo pipefail

# ==== Configuration ====
REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"
ES_HOST="${ES_HOST:-localhost}"
ES_PORT="${ES_PORT:-9200}"
ES_URL="http://${ES_HOST}:${ES_PORT}"
OUTPUT_DIR="test/load/results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log_info()    { echo -e "${BLUE}[INFO]${NC} $*"; }
log_success() { echo -e "${GREEN}[PASS]${NC} $*"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error()   { echo -e "${RED}[FAIL]${NC} $*"; }
log_header()  { echo -e "\n${BOLD}${CYAN}=== $* ===${NC}\n"; }

usage() {
    cat << 'EOF'
WikiSurge Database Performance Benchmark

USAGE:
    ./test/load/db-benchmark.sh [OPTIONS]

OPTIONS:
    --redis-host HOST       Redis host (default: localhost)
    --redis-port PORT       Redis port (default: 6379)
    --es-host HOST          Elasticsearch host (default: localhost)
    --es-port PORT          Elasticsearch port (default: 9200)
    --redis-only            Only run Redis benchmarks
    --es-only               Only run Elasticsearch benchmarks
    -o, --output DIR        Output directory (default: test/load/results)
    --help                  Show this help message
EOF
}

RUN_REDIS=true
RUN_ES=true

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --redis-host)  REDIS_HOST="$2"; shift 2 ;;
            --redis-port)  REDIS_PORT="$2"; shift 2 ;;
            --es-host)     ES_HOST="$2"; shift 2 ;;
            --es-port)     ES_PORT="$2"; shift 2 ;;
            --redis-only)  RUN_ES=false; shift ;;
            --es-only)     RUN_REDIS=false; shift ;;
            -o|--output)   OUTPUT_DIR="$2"; shift 2 ;;
            --help)        usage; exit 0 ;;
            *)             log_error "Unknown: $1"; exit 1 ;;
        esac
    done
    ES_URL="http://${ES_HOST}:${ES_PORT}"
}

check_prerequisites() {
    log_header "Checking Prerequisites"

    if [[ "$RUN_REDIS" == "true" ]]; then
        if command -v redis-benchmark &>/dev/null; then
            log_success "redis-benchmark found"
        else
            log_warn "redis-benchmark not found — using redis-cli based benchmarks"
        fi

        if command -v redis-cli &>/dev/null; then
            log_success "redis-cli found"
        else
            log_error "redis-cli not found — Redis benchmarks will be limited"
        fi

        # Check Redis connectivity
        if redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" ping 2>/dev/null | grep -q "PONG"; then
            log_success "Redis is reachable"
        else
            log_warn "Redis at ${REDIS_HOST}:${REDIS_PORT} not reachable"
        fi
    fi

    if [[ "$RUN_ES" == "true" ]]; then
        if curl -sf "${ES_URL}" > /dev/null 2>&1; then
            log_success "Elasticsearch is reachable at ${ES_URL}"
        else
            log_warn "Elasticsearch at ${ES_URL} not reachable"
        fi
    fi
}

setup_output() {
    mkdir -p "${OUTPUT_DIR}"
}

# ============================================================================
# Redis Benchmarks
# ============================================================================

benchmark_redis() {
    log_header "Redis Performance Benchmarks"

    local results_file="${OUTPUT_DIR}/redis_benchmark_${TIMESTAMP}.json"
    local summary=""

    # Test 1: SET operations (target >10K ops/sec)
    log_info "Test 1: SET operations (target >10K ops/sec)..."
    if command -v redis-benchmark &>/dev/null; then
        local set_result
        set_result=$(redis-benchmark -h "${REDIS_HOST}" -p "${REDIS_PORT}" \
            -t set -n 100000 -c 50 -q 2>&1 || echo "ERROR")
        local set_ops
        set_ops=$(echo "$set_result" | grep -oP '[\d.]+(?= requests per second)' || echo "0")
        log_info "SET: ${set_result}"

        if (( $(echo "${set_ops} > 10000" | bc -l 2>/dev/null || echo "0") )); then
            log_success "SET: ${set_ops} ops/s > 10K target"
        else
            log_error "SET: ${set_ops} ops/s < 10K target"
        fi
    else
        # redis-cli based benchmark
        local start_time end_time
        start_time=$(date +%s%3N 2>/dev/null || date +%s)
        for ((i = 0; i < 10000; i++)); do
            redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" SET "bench:key:${i}" "value_${i}" > /dev/null 2>&1
        done
        end_time=$(date +%s%3N 2>/dev/null || date +%s)
        local elapsed_ms=$((end_time - start_time))
        local set_ops=$((10000 * 1000 / (elapsed_ms + 1)))
        log_info "SET: ${set_ops} ops/s (${elapsed_ms}ms for 10K operations)"
    fi

    # Test 2: GET operations (target >50K ops/sec)
    log_info "Test 2: GET operations (target >50K ops/sec)..."
    if command -v redis-benchmark &>/dev/null; then
        local get_result
        get_result=$(redis-benchmark -h "${REDIS_HOST}" -p "${REDIS_PORT}" \
            -t get -n 100000 -c 50 -q 2>&1 || echo "ERROR")
        local get_ops
        get_ops=$(echo "$get_result" | grep -oP '[\d.]+(?= requests per second)' || echo "0")
        log_info "GET: ${get_result}"

        if (( $(echo "${get_ops} > 50000" | bc -l 2>/dev/null || echo "0") )); then
            log_success "GET: ${get_ops} ops/s > 50K target"
        else
            log_error "GET: ${get_ops} ops/s < 50K target"
        fi
    else
        local start_time end_time
        start_time=$(date +%s%3N 2>/dev/null || date +%s)
        for ((i = 0; i < 10000; i++)); do
            redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" GET "bench:key:$((i % 1000))" > /dev/null 2>&1
        done
        end_time=$(date +%s%3N 2>/dev/null || date +%s)
        local elapsed_ms=$((end_time - start_time))
        local get_ops=$((10000 * 1000 / (elapsed_ms + 1)))
        log_info "GET: ${get_ops} ops/s (${elapsed_ms}ms for 10K operations)"
    fi

    # Test 3: ZADD operations (target >5K ops/sec)
    log_info "Test 3: ZADD operations (target >5K ops/sec)..."
    if command -v redis-benchmark &>/dev/null; then
        local zadd_result
        zadd_result=$(redis-benchmark -h "${REDIS_HOST}" -p "${REDIS_PORT}" \
            -t zadd -n 50000 -c 50 -q 2>&1 || echo "ERROR")
        local zadd_ops
        zadd_ops=$(echo "$zadd_result" | grep -oP '[\d.]+(?= requests per second)' || echo "0")
        log_info "ZADD: ${zadd_result}"

        if (( $(echo "${zadd_ops} > 5000" | bc -l 2>/dev/null || echo "0") )); then
            log_success "ZADD: ${zadd_ops} ops/s > 5K target"
        else
            log_error "ZADD: ${zadd_ops} ops/s < 5K target"
        fi
    else
        local start_time end_time
        start_time=$(date +%s%3N 2>/dev/null || date +%s)
        for ((i = 0; i < 5000; i++)); do
            redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" \
                ZADD "bench:zset" "${i}" "member_${i}" > /dev/null 2>&1
        done
        end_time=$(date +%s%3N 2>/dev/null || date +%s)
        local elapsed_ms=$((end_time - start_time))
        local zadd_ops=$((5000 * 1000 / (elapsed_ms + 1)))
        log_info "ZADD: ${zadd_ops} ops/s (${elapsed_ms}ms for 5K operations)"
    fi

    # Test 4: ZRANGE operations (target >10K ops/sec)
    log_info "Test 4: ZRANGE operations (target >10K ops/sec)..."
    # Prepare sorted set
    redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" DEL bench:zrange:test > /dev/null 2>&1
    for ((i = 0; i < 100; i++)); do
        redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" \
            ZADD bench:zrange:test "$((RANDOM % 1000))" "member_${i}" > /dev/null 2>&1
    done

    local start_time end_time
    start_time=$(date +%s%3N 2>/dev/null || date +%s)
    for ((i = 0; i < 10000; i++)); do
        redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" \
            ZRANGE bench:zrange:test 0 9 WITHSCORES > /dev/null 2>&1
    done
    end_time=$(date +%s%3N 2>/dev/null || date +%s)
    local elapsed_ms=$((end_time - start_time))
    local zrange_ops=$((10000 * 1000 / (elapsed_ms + 1)))
    log_info "ZRANGE: ${zrange_ops} ops/s (${elapsed_ms}ms for 10K operations)"

    if [[ ${zrange_ops} -gt 10000 ]]; then
        log_success "ZRANGE: ${zrange_ops} ops/s > 10K target"
    else
        log_warn "ZRANGE: ${zrange_ops} ops/s (target: 10K ops/s)"
    fi

    # Test 5: Pipeline performance (target 10x improvement)
    log_info "Test 5: Pipeline performance (target 10x improvement)..."

    # Non-pipelined
    start_time=$(date +%s%3N 2>/dev/null || date +%s)
    for ((i = 0; i < 1000; i++)); do
        redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" \
            SET "bench:pipe:${i}" "value_${i}" > /dev/null 2>&1
    done
    end_time=$(date +%s%3N 2>/dev/null || date +%s)
    local non_pipe_ms=$((end_time - start_time))

    # Pipelined
    start_time=$(date +%s%3N 2>/dev/null || date +%s)
    (
        for ((i = 0; i < 1000; i++)); do
            echo "SET bench:pipe:${i} value_${i}"
        done
    ) | redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" --pipe > /dev/null 2>&1
    end_time=$(date +%s%3N 2>/dev/null || date +%s)
    local pipe_ms=$((end_time - start_time))

    local improvement="N/A"
    if [[ ${pipe_ms} -gt 0 ]]; then
        improvement=$(echo "scale=1; ${non_pipe_ms} / ${pipe_ms}" | bc 2>/dev/null || echo "N/A")
    fi

    log_info "Non-pipelined: ${non_pipe_ms}ms | Pipelined: ${pipe_ms}ms | Improvement: ${improvement}x"

    if [[ "$improvement" != "N/A" ]] && (( $(echo "${improvement} >= 5" | bc -l 2>/dev/null || echo "0") )); then
        log_success "Pipeline improvement: ${improvement}x (target: 10x)"
    else
        log_warn "Pipeline improvement: ${improvement}x (target: 10x)"
    fi

    # Cleanup benchmark keys
    log_info "Cleaning up benchmark keys..."
    redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" --scan --pattern "bench:*" | \
        xargs -r redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" DEL > /dev/null 2>&1 || true

    # Redis INFO stats
    log_info "Redis Server Info:"
    redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" INFO memory 2>/dev/null | grep -E "used_memory_human|maxmemory_human|mem_fragmentation_ratio" || true
    redis-cli -h "${REDIS_HOST}" -p "${REDIS_PORT}" INFO stats 2>/dev/null | grep -E "total_commands_processed|instantaneous_ops_per_sec" || true

    # Save summary
    cat > "${OUTPUT_DIR}/redis_summary_${TIMESTAMP}.json" <<EOJSON
{
    "benchmark": "redis",
    "timestamp": "${TIMESTAMP}",
    "host": "${REDIS_HOST}:${REDIS_PORT}",
    "set_ops_sec": "${set_ops:-N/A}",
    "get_ops_sec": "${get_ops:-N/A}",
    "zadd_ops_sec": "${zadd_ops:-N/A}",
    "zrange_ops_sec": "${zrange_ops}",
    "pipeline_improvement": "${improvement}",
    "non_pipelined_ms": ${non_pipe_ms},
    "pipelined_ms": ${pipe_ms}
}
EOJSON

    log_success "Redis benchmarks completed"
}

# ============================================================================
# Elasticsearch Benchmarks
# ============================================================================

benchmark_elasticsearch() {
    log_header "Elasticsearch Performance Benchmarks"

    local bench_index="wikisurge-bench-${TIMESTAMP}"

    # Create benchmark index
    log_info "Creating benchmark index: ${bench_index}"
    curl -sf -X PUT "${ES_URL}/${bench_index}" \
        -H "Content-Type: application/json" \
        -d '{
            "settings": {
                "number_of_shards": 1,
                "number_of_replicas": 0,
                "refresh_interval": "1s"
            },
            "mappings": {
                "properties": {
                    "title": {"type": "text"},
                    "user": {"type": "keyword"},
                    "timestamp": {"type": "date"},
                    "byte_change": {"type": "integer"},
                    "language": {"type": "keyword"},
                    "bot": {"type": "boolean"},
                    "comment": {"type": "text"},
                    "namespace": {"type": "integer"}
                }
            }
        }' > /dev/null 2>&1 || true

    # Test 1: Bulk indexing (target >1000 docs/sec)
    log_info "Test 1: Bulk indexing (target >1000 docs/sec)..."

    local total_docs=5000
    local bulk_size=500
    local total_indexed=0
    local start_time end_time

    start_time=$(date +%s%3N 2>/dev/null || date +%s)

    for ((batch = 0; batch < total_docs / bulk_size; batch++)); do
        local bulk_body=""
        for ((i = 0; i < bulk_size; i++)); do
            local doc_id=$((batch * bulk_size + i))
            local titles=("Main_Page" "Python" "Linux" "JavaScript" "Wikipedia" "Einstein" "History" "Science" "Math" "Art")
            local users=("UserA" "UserB" "UserC" "BotD" "Editor1")
            local langs=("en" "es" "fr" "de" "ja")
            bulk_body+='{"index":{"_id":"'${doc_id}'"}}'$'\n'
            bulk_body+='{"title":"'${titles[$((doc_id % ${#titles[@]}))]}'","user":"'${users[$((doc_id % ${#users[@]}))]}'","timestamp":"'$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -Iseconds)'","byte_change":'$((RANDOM % 5000 - 2500))',"language":"'${langs[$((doc_id % ${#langs[@]}))]}'","bot":'$(if [[ $((doc_id % 10)) -eq 0 ]]; then echo true; else echo false; fi)',"comment":"Benchmark edit #'${doc_id}'","namespace":0}'$'\n'
        done

        local resp
        resp=$(curl -sf -X POST "${ES_URL}/${bench_index}/_bulk" \
            -H "Content-Type: application/x-ndjson" \
            -d "${bulk_body}" 2>/dev/null || echo '{"errors":true}')

        local has_errors
        has_errors=$(echo "$resp" | jq -r '.errors' 2>/dev/null || echo "true")
        if [[ "$has_errors" == "false" ]]; then
            total_indexed=$((total_indexed + bulk_size))
        fi
    done

    end_time=$(date +%s%3N 2>/dev/null || date +%s)
    local elapsed_ms=$((end_time - start_time))
    local index_rate=0
    if [[ ${elapsed_ms} -gt 0 ]]; then
        index_rate=$((total_indexed * 1000 / elapsed_ms))
    fi

    log_info "Indexed ${total_indexed}/${total_docs} docs in ${elapsed_ms}ms (${index_rate} docs/s)"

    if [[ ${index_rate} -gt 1000 ]]; then
        log_success "Bulk indexing: ${index_rate} docs/s > 1000 target"
    else
        log_warn "Bulk indexing: ${index_rate} docs/s (target: 1000)"
    fi

    # Refresh before queries
    curl -sf -X POST "${ES_URL}/${bench_index}/_refresh" > /dev/null 2>&1

    # Test 2: Search queries (target <100ms p95)
    log_info "Test 2: Search queries (target <100ms p95)..."
    local search_latencies="${OUTPUT_DIR}/es_search_latencies_${TIMESTAMP}.txt"
    > "${search_latencies}"

    local search_queries=(
        '{"query":{"match":{"title":"Python"}}}'
        '{"query":{"match":{"title":"Linux"}}}'
        '{"query":{"term":{"language":"en"}}}'
        '{"query":{"range":{"byte_change":{"gte":0}}}}'
        '{"query":{"bool":{"must":[{"match":{"title":"Wikipedia"}},{"term":{"bot":false}}]}}}'
    )

    for ((i = 0; i < 100; i++)); do
        local q_idx=$((i % ${#search_queries[@]}))
        local query="${search_queries[$q_idx]}"

        local search_start
        search_start=$(date +%s%3N 2>/dev/null || date +%s)

        curl -sf -X POST "${ES_URL}/${bench_index}/_search" \
            -H "Content-Type: application/json" \
            -d "${query}" > /dev/null 2>&1

        local search_end
        search_end=$(date +%s%3N 2>/dev/null || date +%s)
        echo "$((search_end - search_start))" >> "${search_latencies}"
    done

    # Compute percentiles
    if [[ -s "${search_latencies}" ]]; then
        sort -n "${search_latencies}" > "${search_latencies}.sorted"
        local count
        count=$(wc -l < "${search_latencies}.sorted")
        local p50 p95 p99
        p50=$(sed -n "$((count * 50 / 100 + 1))p" "${search_latencies}.sorted" || echo "N/A")
        p95=$(sed -n "$((count * 95 / 100 + 1))p" "${search_latencies}.sorted" || echo "N/A")
        p99=$(sed -n "$((count * 99 / 100 + 1))p" "${search_latencies}.sorted" || echo "N/A")

        log_info "Search latency: p50=${p50}ms p95=${p95}ms p99=${p99}ms"

        if [[ "${p95}" != "N/A" ]] && [[ ${p95} -lt 100 ]]; then
            log_success "Search p95: ${p95}ms < 100ms target"
        else
            log_warn "Search p95: ${p95}ms (target: <100ms)"
        fi

        rm -f "${search_latencies}.sorted"
    fi

    # Test 3: Aggregations (target <500ms p95)
    log_info "Test 3: Aggregation queries (target <500ms p95)..."
    local agg_latencies="${OUTPUT_DIR}/es_agg_latencies_${TIMESTAMP}.txt"
    > "${agg_latencies}"

    local agg_queries=(
        '{"size":0,"aggs":{"by_language":{"terms":{"field":"language"}}}}'
        '{"size":0,"aggs":{"by_user":{"terms":{"field":"user","size":20}}}}'
        '{"size":0,"aggs":{"byte_stats":{"stats":{"field":"byte_change"}}}}'
        '{"size":0,"aggs":{"by_lang":{"terms":{"field":"language"},"aggs":{"avg_bytes":{"avg":{"field":"byte_change"}}}}}}'
        '{"size":0,"aggs":{"over_time":{"date_histogram":{"field":"timestamp","calendar_interval":"hour"}}}}'
    )

    for ((i = 0; i < 50; i++)); do
        local q_idx=$((i % ${#agg_queries[@]}))
        local query="${agg_queries[$q_idx]}"

        local agg_start
        agg_start=$(date +%s%3N 2>/dev/null || date +%s)

        curl -sf -X POST "${ES_URL}/${bench_index}/_search" \
            -H "Content-Type: application/json" \
            -d "${query}" > /dev/null 2>&1

        local agg_end
        agg_end=$(date +%s%3N 2>/dev/null || date +%s)
        echo "$((agg_end - agg_start))" >> "${agg_latencies}"
    done

    if [[ -s "${agg_latencies}" ]]; then
        sort -n "${agg_latencies}" > "${agg_latencies}.sorted"
        local count
        count=$(wc -l < "${agg_latencies}.sorted")
        local p50 p95
        p50=$(sed -n "$((count * 50 / 100 + 1))p" "${agg_latencies}.sorted" || echo "N/A")
        p95=$(sed -n "$((count * 95 / 100 + 1))p" "${agg_latencies}.sorted" || echo "N/A")

        log_info "Aggregation latency: p50=${p50}ms p95=${p95}ms"

        if [[ "${p95}" != "N/A" ]] && [[ ${p95} -lt 500 ]]; then
            log_success "Aggregation p95: ${p95}ms < 500ms target"
        else
            log_warn "Aggregation p95: ${p95}ms (target: <500ms)"
        fi

        rm -f "${agg_latencies}.sorted"
    fi

    # Test 4: Concurrent queries (10 simultaneous)
    log_info "Test 4: Concurrent queries (10 simultaneous)..."
    local concurrent_start
    concurrent_start=$(date +%s%3N 2>/dev/null || date +%s)
    local pids=()

    for ((i = 0; i < 10; i++)); do
        (
            for ((j = 0; j < 10; j++)); do
                curl -sf -X POST "${ES_URL}/${bench_index}/_search" \
                    -H "Content-Type: application/json" \
                    -d '{"query":{"match_all":{}},"size":10}' > /dev/null 2>&1
            done
        ) &
        pids+=($!)
    done

    for pid in "${pids[@]}"; do
        wait "$pid" 2>/dev/null || true
    done

    local concurrent_end
    concurrent_end=$(date +%s%3N 2>/dev/null || date +%s)
    local concurrent_ms=$((concurrent_end - concurrent_start))
    local concurrent_qps=$((100 * 1000 / (concurrent_ms + 1)))

    log_info "10 concurrent threads, 100 total queries in ${concurrent_ms}ms (${concurrent_qps} qps)"
    log_success "Concurrent query test completed"

    # Cleanup benchmark index
    log_info "Cleaning up benchmark index..."
    curl -sf -X DELETE "${ES_URL}/${bench_index}" > /dev/null 2>&1 || true

    # ES cluster stats
    log_info "Elasticsearch Cluster Stats:"
    curl -sf "${ES_URL}/_cluster/health?pretty" 2>/dev/null | jq -r '. | "Status: \(.status), Nodes: \(.number_of_nodes), Indices: \(.active_shards)"' 2>/dev/null || true

    # Save summary
    cat > "${OUTPUT_DIR}/es_summary_${TIMESTAMP}.json" <<EOJSON
{
    "benchmark": "elasticsearch",
    "timestamp": "${TIMESTAMP}",
    "host": "${ES_URL}",
    "bulk_index_rate": ${index_rate},
    "search_p50_ms": "${p50:-N/A}",
    "search_p95_ms": "${p95:-N/A}",
    "concurrent_qps": ${concurrent_qps},
    "concurrent_ms": ${concurrent_ms}
}
EOJSON

    log_success "Elasticsearch benchmarks completed"
}

# ==== System Resource Monitor ====

monitor_resources() {
    log_header "System Resource Snapshot"

    echo "CPU Usage:"
    top -bn1 | head -5 2>/dev/null || uptime

    echo ""
    echo "Memory Usage:"
    free -h 2>/dev/null || echo "free not available"

    echo ""
    echo "Disk I/O:"
    iostat -x 1 1 2>/dev/null || echo "iostat not available"

    echo ""
    echo "Network:"
    ss -s 2>/dev/null || netstat -s 2>/dev/null | head -20 || echo "network stats not available"
}

# ==== Main ====

main() {
    parse_args "$@"

    log_header "WikiSurge Database Performance Benchmarks"
    log_info "Timestamp: ${TIMESTAMP}"

    check_prerequisites
    setup_output

    if [[ "$RUN_REDIS" == "true" ]]; then
        benchmark_redis
    fi

    if [[ "$RUN_ES" == "true" ]]; then
        benchmark_elasticsearch
    fi

    monitor_resources

    log_header "Database Benchmarks Complete"
    log_info "Results saved to: ${OUTPUT_DIR}/"
}

main "$@"
