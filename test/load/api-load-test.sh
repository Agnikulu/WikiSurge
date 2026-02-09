#!/bin/bash

# =============================================================================
# WikiSurge API Load Testing Script
# =============================================================================
# Tests API endpoints under various load scenarios using Vegeta or curl-based
# fallback. Measures latency distribution, error rate, and throughput.
#
# Prerequisites:
#   - Vegeta: go install github.com/tsenart/vegeta@latest
#   - jq: for JSON processing
#   - bc: for math calculations
#
# Usage:
#   ./test/load/api-load-test.sh [OPTIONS]
#
# Options:
#   -h, --host HOST        API host (default: localhost)
#   -p, --port PORT        API port (default: 8080)
#   -s, --scenario SCENARIO  Scenario: trending|search|stats|mixed|all (default: all)
#   -d, --duration SECS    Duration in seconds (default: 60)
#   -o, --output DIR       Output directory (default: test/load/results)
#   --skip-vegeta          Use curl-based fallback
#   --help                 Show this help message
# =============================================================================

set -euo pipefail

# ==== Configuration ====
API_HOST="${API_HOST:-localhost}"
API_PORT="${API_PORT:-8080}"
BASE_URL="http://${API_HOST}:${API_PORT}"
DURATION=60
SCENARIO="all"
OUTPUT_DIR="test/load/results"
SKIP_VEGETA=false
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# ==== Helper Functions ====
log_info()    { echo -e "${BLUE}[INFO]${NC} $*"; }
log_success() { echo -e "${GREEN}[PASS]${NC} $*"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error()   { echo -e "${RED}[FAIL]${NC} $*"; }
log_header()  { echo -e "\n${BOLD}${CYAN}=== $* ===${NC}\n"; }

usage() {
    cat << 'EOF'
WikiSurge API Load Testing Script

USAGE:
    ./test/load/api-load-test.sh [OPTIONS]

OPTIONS:
    -h, --host HOST         API host (default: localhost)
    -p, --port PORT         API port (default: 8080)
    -s, --scenario SCENE    Scenario: trending|search|stats|mixed|all (default: all)
    -d, --duration SECS     Duration in seconds (default: 60)
    -o, --output DIR        Output directory (default: test/load/results)
    --skip-vegeta           Use curl-based fallback instead of Vegeta
    --help                  Show this help message

SCENARIOS:
    trending    GET /api/trending at 100 req/s for 60s (p99 < 100ms)
    search      GET /api/search?q=... at 50 req/s for 60s (p99 < 200ms)
    stats       GET /api/stats at 200 req/s for 60s (p99 < 50ms)
    mixed       All endpoints proportionally at 200 req/s for 300s
    all         Run all individual scenarios sequentially

EXAMPLES:
    ./test/load/api-load-test.sh
    ./test/load/api-load-test.sh --scenario trending --duration 30
    ./test/load/api-load-test.sh --host api.example.com --port 443
EOF
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--host)     API_HOST="$2"; shift 2 ;;
            -p|--port)     API_PORT="$2"; shift 2 ;;
            -s|--scenario) SCENARIO="$2"; shift 2 ;;
            -d|--duration) DURATION="$2"; shift 2 ;;
            -o|--output)   OUTPUT_DIR="$2"; shift 2 ;;
            --skip-vegeta) SKIP_VEGETA=true; shift ;;
            --help)        usage; exit 0 ;;
            *)             log_error "Unknown option: $1"; usage; exit 1 ;;
        esac
    done
    BASE_URL="http://${API_HOST}:${API_PORT}"
}

# Check prerequisites
check_prerequisites() {
    log_header "Checking Prerequisites"

    local has_vegeta=false
    if command -v vegeta &>/dev/null; then
        has_vegeta=true
        log_success "Vegeta found: $(vegeta --version 2>&1 || echo 'installed')"
    else
        log_warn "Vegeta not found. Install: go install github.com/tsenart/vegeta@latest"
        if [[ "$SKIP_VEGETA" == "false" ]]; then
            log_info "Falling back to curl-based load testing"
            SKIP_VEGETA=true
        fi
    fi

    if ! command -v jq &>/dev/null; then
        log_warn "jq not found — some reports will be limited"
    fi

    if ! command -v bc &>/dev/null; then
        log_warn "bc not found — percentile calculations will be limited"
    fi

    # Check API is reachable
    log_info "Checking API at ${BASE_URL}/health ..."
    if curl -sf --connect-timeout 5 "${BASE_URL}/health" > /dev/null 2>&1; then
        log_success "API is reachable"
    else
        log_warn "API at ${BASE_URL} is not reachable — tests may fail"
        log_info "Continuing anyway (tests will report connection errors)"
    fi
}

# Setup output directory
setup_output() {
    mkdir -p "${OUTPUT_DIR}"
    log_info "Results will be saved to: ${OUTPUT_DIR}"
}

# ==== Realistic Query Generators ====

# Generate search queries that mimic real user patterns
generate_search_queries() {
    local count=${1:-100}
    local queries=(
        "Wikipedia" "United+States" "JavaScript" "Python" "Linux"
        "Albert+Einstein" "World+War+II" "Moon" "DNA" "Computer"
        "Barack+Obama" "Solar+System" "Evolution" "Mathematics"
        "Machine+Learning" "Climate+Change" "Quantum+Physics"
        "Human+Rights" "Artificial+Intelligence" "Blockchain"
        "SpaceX" "COVID-19" "Democracy" "Philosophy" "Genetics"
        "Astronomy" "Chemistry" "Biology" "History" "Geography"
        "Economics" "Psychology" "Sociology" "Music" "Art"
        "Literature" "Engineering" "Medicine" "Law" "Politics"
        "Religion" "Science" "Technology" "Education" "Culture"
        "Language" "Sports" "Football" "Basketball" "Cricket"
    )
    for ((i = 0; i < count; i++)); do
        local idx=$((RANDOM % ${#queries[@]}))
        echo "GET ${BASE_URL}/api/search?q=${queries[$idx]}&limit=$((RANDOM % 20 + 5))"
    done
}

# Generate trending requests with varying params
generate_trending_targets() {
    local count=${1:-100}
    for ((i = 0; i < count; i++)); do
        local limit=$((RANDOM % 50 + 5))
        local lang_options=("" "en" "es" "fr" "de")
        local lang_idx=$((RANDOM % ${#lang_options[@]}))
        local lang="${lang_options[$lang_idx]}"
        if [[ -n "$lang" ]]; then
            echo "GET ${BASE_URL}/api/trending?limit=${limit}&language=${lang}"
        else
            echo "GET ${BASE_URL}/api/trending?limit=${limit}"
        fi
    done
}

# Generate stats requests
generate_stats_targets() {
    local count=${1:-100}
    for ((i = 0; i < count; i++)); do
        echo "GET ${BASE_URL}/api/stats"
    done
}

# Generate mixed workload targets
generate_mixed_targets() {
    local count=${1:-1000}
    for ((i = 0; i < count; i++)); do
        local r=$((RANDOM % 100))
        if [[ $r -lt 40 ]]; then
            # 40% stats (lightweight)
            echo "GET ${BASE_URL}/api/stats"
        elif [[ $r -lt 70 ]]; then
            # 30% trending
            local limit=$((RANDOM % 50 + 5))
            echo "GET ${BASE_URL}/api/trending?limit=${limit}"
        elif [[ $r -lt 85 ]]; then
            # 15% search
            local queries=("Wikipedia" "Python" "Linux" "Einstein" "History" "Science")
            local idx=$((RANDOM % ${#queries[@]}))
            echo "GET ${BASE_URL}/api/search?q=${queries[$idx]}"
        elif [[ $r -lt 93 ]]; then
            # 8% alerts
            echo "GET ${BASE_URL}/api/alerts"
        else
            # 7% edit-wars
            echo "GET ${BASE_URL}/api/edit-wars"
        fi
    done
}

# ==== Vegeta-based Load Tests ====

run_vegeta_test() {
    local name="$1"
    local rate="$2"
    local duration="$3"
    local target_file="$4"
    local expected_p99_ms="$5"

    local result_file="${OUTPUT_DIR}/${name}_${TIMESTAMP}.bin"
    local report_file="${OUTPUT_DIR}/${name}_${TIMESTAMP}_report.txt"
    local json_file="${OUTPUT_DIR}/${name}_${TIMESTAMP}.json"
    local hist_file="${OUTPUT_DIR}/${name}_${TIMESTAMP}_histogram.txt"

    log_header "Scenario: ${name}"
    log_info "Rate: ${rate} req/s | Duration: ${duration}s | Target p99: <${expected_p99_ms}ms"

    # Run Vegeta attack
    log_info "Starting load test..."
    vegeta attack \
        -targets="${target_file}" \
        -rate="${rate}" \
        -duration="${duration}s" \
        -timeout=30s \
        -workers=50 \
        -max-workers=100 \
        -connections=100 \
        > "${result_file}" 2>/dev/null

    # Generate reports
    log_info "Generating reports..."
    vegeta report "${result_file}" > "${report_file}" 2>&1
    vegeta report -type=json "${result_file}" > "${json_file}" 2>&1
    vegeta report -type='hist[0,10ms,25ms,50ms,75ms,100ms,200ms,500ms,1s,5s]' \
        "${result_file}" > "${hist_file}" 2>&1

    # Display results
    echo ""
    cat "${report_file}"
    echo ""
    echo "Latency Distribution:"
    cat "${hist_file}"
    echo ""

    # Analyze results
    analyze_vegeta_results "${name}" "${json_file}" "${expected_p99_ms}"
}

analyze_vegeta_results() {
    local name="$1"
    local json_file="$2"
    local expected_p99_ms="$3"

    if ! command -v jq &>/dev/null; then
        log_warn "jq not available — skipping detailed analysis"
        return
    fi

    local success_rate
    success_rate=$(jq -r '.success' "${json_file}" 2>/dev/null || echo "0")
    local p99_ns
    p99_ns=$(jq -r '.latencies."99th"' "${json_file}" 2>/dev/null || echo "0")
    local p50_ns
    p50_ns=$(jq -r '.latencies."50th"' "${json_file}" 2>/dev/null || echo "0")
    local p95_ns
    p95_ns=$(jq -r '.latencies."95th"' "${json_file}" 2>/dev/null || echo "0")
    local throughput
    throughput=$(jq -r '.throughput' "${json_file}" 2>/dev/null || echo "0")
    local total_requests
    total_requests=$(jq -r '.requests' "${json_file}" 2>/dev/null || echo "0")
    local error_count
    error_count=$(jq -r '[.status_codes | to_entries[] | select(.key | test("^[45]")) | .value] | add // 0' "${json_file}" 2>/dev/null || echo "0")

    # Convert ns to ms
    local p99_ms p50_ms p95_ms
    p99_ms=$(echo "scale=2; ${p99_ns} / 1000000" | bc 2>/dev/null || echo "N/A")
    p50_ms=$(echo "scale=2; ${p50_ns} / 1000000" | bc 2>/dev/null || echo "N/A")
    p95_ms=$(echo "scale=2; ${p95_ns} / 1000000" | bc 2>/dev/null || echo "N/A")

    echo "╔══════════════════════════════════════════╗"
    echo "║           ${name} Results                ║"
    echo "╠══════════════════════════════════════════╣"
    printf "║ Total Requests:    %-20s ║\n" "${total_requests}"
    printf "║ Throughput:        %-20s ║\n" "${throughput} req/s"
    printf "║ Success Rate:      %-20s ║\n" "${success_rate}"
    printf "║ Error Count:       %-20s ║\n" "${error_count}"
    printf "║ Latency p50:       %-20s ║\n" "${p50_ms}ms"
    printf "║ Latency p95:       %-20s ║\n" "${p95_ms}ms"
    printf "║ Latency p99:       %-20s ║\n" "${p99_ms}ms"
    echo "╚══════════════════════════════════════════╝"

    # Validate against targets
    local passed=true
    if [[ "$p99_ms" != "N/A" ]] && (( $(echo "${p99_ms} <= ${expected_p99_ms}" | bc -l 2>/dev/null || echo "0") )); then
        log_success "p99 latency ${p99_ms}ms <= ${expected_p99_ms}ms target"
    else
        log_error "p99 latency ${p99_ms}ms > ${expected_p99_ms}ms target"
        passed=false
    fi

    local error_pct
    if [[ "$total_requests" != "0" && "$total_requests" != "null" ]]; then
        error_pct=$(echo "scale=4; ${error_count} * 100 / ${total_requests}" | bc 2>/dev/null || echo "N/A")
    else
        error_pct="N/A"
    fi

    if [[ "$error_pct" != "N/A" ]] && (( $(echo "${error_pct} < 0.1" | bc -l 2>/dev/null || echo "0") )); then
        log_success "Error rate ${error_pct}% < 0.1% target"
    else
        log_error "Error rate ${error_pct}% >= 0.1% target"
        passed=false
    fi

    if [[ "$passed" == "true" ]]; then
        log_success "Scenario ${name}: ALL CHECKS PASSED"
    else
        log_error "Scenario ${name}: SOME CHECKS FAILED"
    fi

    # Save summary
    cat > "${OUTPUT_DIR}/${name}_${TIMESTAMP}_summary.json" <<EOJSON
{
    "scenario": "${name}",
    "timestamp": "${TIMESTAMP}",
    "total_requests": ${total_requests:-0},
    "throughput": ${throughput:-0},
    "success_rate": ${success_rate:-0},
    "error_count": ${error_count:-0},
    "error_rate_pct": "${error_pct}",
    "latency_p50_ms": "${p50_ms}",
    "latency_p95_ms": "${p95_ms}",
    "latency_p99_ms": "${p99_ms}",
    "target_p99_ms": ${expected_p99_ms},
    "passed": ${passed}
}
EOJSON
}

# ==== Curl-based Fallback Tests ====

run_curl_test() {
    local name="$1"
    local rate="$2"
    local duration="$3"
    local url_pattern="$4"
    local expected_p99_ms="$5"

    log_header "Scenario: ${name} (curl-based)"
    log_info "Rate: ${rate} req/s | Duration: ${duration}s | Target p99: <${expected_p99_ms}ms"

    local latencies_file="${OUTPUT_DIR}/${name}_${TIMESTAMP}_latencies.txt"
    local result_file="${OUTPUT_DIR}/${name}_${TIMESTAMP}_curl_results.txt"
    local total_requests=0
    local errors=0
    local delay
    delay=$(echo "scale=6; 1.0 / ${rate}" | bc 2>/dev/null || echo "0.01")

    local start_time
    start_time=$(date +%s)
    local end_time=$((start_time + duration))

    > "${latencies_file}"
    > "${result_file}"

    log_info "Running curl-based load test for ${duration}s..."

    while [[ $(date +%s) -lt ${end_time} ]]; do
        # Generate URL based on pattern
        local url
        case "$name" in
            trending)
                local limit=$((RANDOM % 50 + 5))
                url="${BASE_URL}/api/trending?limit=${limit}"
                ;;
            search)
                local queries=("Wikipedia" "Python" "Linux" "Einstein" "History")
                local idx=$((RANDOM % ${#queries[@]}))
                url="${BASE_URL}/api/search?q=${queries[$idx]}"
                ;;
            stats)
                url="${BASE_URL}/api/stats"
                ;;
            *)
                url="${BASE_URL}/api/stats"
                ;;
        esac

        # Fire request in background and record timing
        (
            local resp_time
            resp_time=$(curl -sf -o /dev/null -w "%{time_total}" \
                --connect-timeout 5 --max-time 10 "${url}" 2>/dev/null || echo "ERROR")
            if [[ "$resp_time" == "ERROR" ]]; then
                echo "ERROR" >> "${result_file}"
            else
                # Convert to ms
                local ms
                ms=$(echo "${resp_time} * 1000" | bc 2>/dev/null || echo "0")
                echo "${ms}" >> "${latencies_file}"
                echo "OK ${ms}ms" >> "${result_file}"
            fi
        ) &

        total_requests=$((total_requests + 1))

        # Rate limiting
        sleep "${delay}" 2>/dev/null || sleep 0.01
    done

    # Wait for all background requests
    wait 2>/dev/null || true

    # Analyze results
    errors=$(grep -c "ERROR" "${result_file}" 2>/dev/null || echo "0")
    local ok_count=$((total_requests - errors))

    if [[ -s "${latencies_file}" ]]; then
        # Sort latencies and compute percentiles
        sort -n "${latencies_file}" > "${latencies_file}.sorted"

        local line_count
        line_count=$(wc -l < "${latencies_file}.sorted")

        if [[ ${line_count} -gt 0 ]]; then
            local p50_line=$((line_count * 50 / 100))
            local p95_line=$((line_count * 95 / 100))
            local p99_line=$((line_count * 99 / 100))
            [[ ${p50_line} -lt 1 ]] && p50_line=1
            [[ ${p95_line} -lt 1 ]] && p95_line=1
            [[ ${p99_line} -lt 1 ]] && p99_line=1

            local p50_ms p95_ms p99_ms
            p50_ms=$(sed -n "${p50_line}p" "${latencies_file}.sorted")
            p95_ms=$(sed -n "${p95_line}p" "${latencies_file}.sorted")
            p99_ms=$(sed -n "${p99_line}p" "${latencies_file}.sorted")

            local error_pct="0"
            if [[ ${total_requests} -gt 0 ]]; then
                error_pct=$(echo "scale=4; ${errors} * 100 / ${total_requests}" | bc 2>/dev/null || echo "0")
            fi

            echo ""
            echo "╔══════════════════════════════════════════╗"
            echo "║   ${name} Results (curl-based)           ║"
            echo "╠══════════════════════════════════════════╣"
            printf "║ Total Requests:    %-20s ║\n" "${total_requests}"
            printf "║ Successful:        %-20s ║\n" "${ok_count}"
            printf "║ Errors:            %-20s ║\n" "${errors}"
            printf "║ Error Rate:        %-20s ║\n" "${error_pct}%"
            printf "║ Latency p50:       %-20s ║\n" "${p50_ms}ms"
            printf "║ Latency p95:       %-20s ║\n" "${p95_ms}ms"
            printf "║ Latency p99:       %-20s ║\n" "${p99_ms}ms"
            echo "╚══════════════════════════════════════════╝"

            # Validate
            if (( $(echo "${p99_ms} <= ${expected_p99_ms}" | bc -l 2>/dev/null || echo "0") )); then
                log_success "p99 latency ${p99_ms}ms <= ${expected_p99_ms}ms target"
            else
                log_error "p99 latency ${p99_ms}ms > ${expected_p99_ms}ms target"
            fi

            if (( $(echo "${error_pct} < 0.1" | bc -l 2>/dev/null || echo "0") )); then
                log_success "Error rate ${error_pct}% < 0.1% target"
            else
                log_error "Error rate ${error_pct}% >= 0.1% target"
            fi

            # Save summary
            cat > "${OUTPUT_DIR}/${name}_${TIMESTAMP}_summary.json" <<EOJSON
{
    "scenario": "${name}",
    "timestamp": "${TIMESTAMP}",
    "total_requests": ${total_requests},
    "successful": ${ok_count},
    "errors": ${errors},
    "error_rate_pct": "${error_pct}",
    "latency_p50_ms": "${p50_ms}",
    "latency_p95_ms": "${p95_ms}",
    "latency_p99_ms": "${p99_ms}",
    "target_p99_ms": ${expected_p99_ms},
    "method": "curl"
}
EOJSON
        fi

        rm -f "${latencies_file}.sorted"
    else
        log_error "No successful responses recorded"
    fi
}

# ==== Scenario Runners ====

run_scenario_trending() {
    local targets_file="${OUTPUT_DIR}/trending_targets.txt"
    generate_trending_targets 10000 > "${targets_file}"

    if [[ "$SKIP_VEGETA" == "true" ]]; then
        run_curl_test "trending" 100 "${DURATION}" "trending" 100
    else
        run_vegeta_test "trending" 100 "${DURATION}" "${targets_file}" 100
    fi
}

run_scenario_search() {
    local targets_file="${OUTPUT_DIR}/search_targets.txt"
    generate_search_queries 10000 > "${targets_file}"

    if [[ "$SKIP_VEGETA" == "true" ]]; then
        run_curl_test "search" 50 "${DURATION}" "search" 200
    else
        run_vegeta_test "search" 50 "${DURATION}" "${targets_file}" 200
    fi
}

run_scenario_stats() {
    local targets_file="${OUTPUT_DIR}/stats_targets.txt"
    generate_stats_targets 10000 > "${targets_file}"

    if [[ "$SKIP_VEGETA" == "true" ]]; then
        run_curl_test "stats" 200 "${DURATION}" "stats" 50
    else
        run_vegeta_test "stats" 200 "${DURATION}" "${targets_file}" 50
    fi
}

run_scenario_mixed() {
    local targets_file="${OUTPUT_DIR}/mixed_targets.txt"
    local mixed_duration=${DURATION}
    if [[ "${SCENARIO}" == "all" || "${SCENARIO}" == "mixed" ]]; then
        mixed_duration=300
    fi
    generate_mixed_targets 100000 > "${targets_file}"

    if [[ "$SKIP_VEGETA" == "true" ]]; then
        log_header "Scenario: mixed (curl-based)"
        log_info "Mixed workload runs all endpoints proportionally"
        log_info "Running for ${mixed_duration}s at ~200 req/s total..."

        # Simplified mixed test with curl
        local start_time end_time total_requests=0 errors=0
        start_time=$(date +%s)
        end_time=$((start_time + mixed_duration))
        local latencies_file="${OUTPUT_DIR}/mixed_${TIMESTAMP}_latencies.txt"
        > "${latencies_file}"

        while [[ $(date +%s) -lt ${end_time} ]]; do
            local r=$((RANDOM % 100))
            local url
            if [[ $r -lt 40 ]]; then
                url="${BASE_URL}/api/stats"
            elif [[ $r -lt 70 ]]; then
                url="${BASE_URL}/api/trending?limit=$((RANDOM % 50 + 5))"
            elif [[ $r -lt 85 ]]; then
                local queries=("Wikipedia" "Python" "Linux")
                url="${BASE_URL}/api/search?q=${queries[$((RANDOM % 3))]}"
            else
                url="${BASE_URL}/api/alerts"
            fi

            (
                local t
                t=$(curl -sf -o /dev/null -w "%{time_total}" --connect-timeout 5 \
                    --max-time 10 "${url}" 2>/dev/null || echo "ERROR")
                if [[ "$t" != "ERROR" ]]; then
                    echo "$(echo "${t} * 1000" | bc 2>/dev/null || echo 0)" >> "${latencies_file}"
                fi
            ) &
            total_requests=$((total_requests + 1))
            sleep 0.005
        done
        wait 2>/dev/null || true
        log_success "Mixed workload completed: ${total_requests} requests sent"
    else
        run_vegeta_test "mixed" 200 "${mixed_duration}" "${targets_file}" 200
    fi
}

# ==== Report Generation ====

generate_final_report() {
    log_header "Generating Final Report"

    local report_file="${OUTPUT_DIR}/load_test_report_${TIMESTAMP}.md"

    cat > "${report_file}" <<EOF
# WikiSurge API Load Test Report

**Date:** $(date -Iseconds)
**Host:** ${BASE_URL}
**Tool:** $(if [[ "$SKIP_VEGETA" == "true" ]]; then echo "curl-based"; else echo "Vegeta"; fi)

## Test Environment
- **OS:** $(uname -s) $(uname -r)
- **CPU:** $(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo "unknown") cores
- **Memory:** $(free -h 2>/dev/null | awk '/Mem:/ {print $2}' || echo "unknown")

## Scenarios

EOF

    # Collect summaries
    for summary in "${OUTPUT_DIR}"/*_${TIMESTAMP}_summary.json; do
        if [[ -f "$summary" ]]; then
            local scenario_name
            scenario_name=$(jq -r '.scenario' "$summary" 2>/dev/null || echo "unknown")
            cat >> "${report_file}" <<EOF
### ${scenario_name}

| Metric | Value |
|--------|-------|
$(jq -r 'to_entries | map("| \(.key) | \(.value) |") | .[]' "$summary" 2>/dev/null || echo "| N/A | N/A |")

EOF
        fi
    done

    cat >> "${report_file}" <<EOF

## Targets

| Endpoint | Rate | Expected p99 |
|----------|------|-------------|
| GET /api/trending | 100 req/s | <100ms |
| GET /api/search | 50 req/s | <200ms |
| GET /api/stats | 200 req/s | <50ms |
| Mixed workload | 200 req/s | <200ms |

## Conclusion

See individual scenario results above for pass/fail status.
Review the raw results in \`${OUTPUT_DIR}/\` for detailed latency distributions.
EOF

    log_success "Report saved to: ${report_file}"
    echo ""
    cat "${report_file}"
}

# ==== Main ====

main() {
    parse_args "$@"

    log_header "WikiSurge API Load Testing"
    log_info "Target: ${BASE_URL}"
    log_info "Scenario: ${SCENARIO}"
    log_info "Duration: ${DURATION}s per scenario"
    log_info "Timestamp: ${TIMESTAMP}"

    check_prerequisites
    setup_output

    case "${SCENARIO}" in
        trending)
            run_scenario_trending
            ;;
        search)
            run_scenario_search
            ;;
        stats)
            run_scenario_stats
            ;;
        mixed)
            run_scenario_mixed
            ;;
        all)
            run_scenario_trending
            run_scenario_search
            run_scenario_stats
            run_scenario_mixed
            ;;
        *)
            log_error "Unknown scenario: ${SCENARIO}"
            usage
            exit 1
            ;;
    esac

    generate_final_report

    log_header "Load Testing Complete"
    log_info "Results saved to: ${OUTPUT_DIR}/"
}

main "$@"
