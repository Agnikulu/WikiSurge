#!/bin/bash

# =============================================================================
# WikiSurge WebSocket Load Testing Script
# =============================================================================
# Tests WebSocket endpoints under various connection and message load patterns.
# Measures connection handling, message throughput, and memory usage.
#
# Prerequisites:
#   - websocat: https://github.com/vi/websocat
#   - jq: for JSON processing
#
# Usage:
#   ./test/load/websocket-load-test.sh [OPTIONS]
# =============================================================================

set -euo pipefail

# ==== Configuration ====
WS_HOST="${WS_HOST:-localhost}"
WS_PORT="${WS_PORT:-8080}"
WS_FEED_URL="ws://${WS_HOST}:${WS_PORT}/ws/feed"
WS_ALERTS_URL="ws://${WS_HOST}:${WS_PORT}/ws/alerts"
OUTPUT_DIR="test/load/results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Test parameters
MAX_CONNECTIONS=100
KEEPALIVE_DURATION=300  # 5 minutes
PING_INTERVAL=30
HIGH_MSG_CONNECTIONS=50
HIGH_MSG_COUNT=1000
HIGH_MSG_DURATION=60

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
WikiSurge WebSocket Load Testing Script

USAGE:
    ./test/load/websocket-load-test.sh [OPTIONS]

OPTIONS:
    -h, --host HOST          WebSocket host (default: localhost)
    -p, --port PORT          WebSocket port (default: 8080)
    -c, --connections N      Max connections for Test 1 (default: 100)
    -d, --duration SECS      Keepalive duration in seconds (default: 300)
    -m, --messages N         Messages per connection for Test 2 (default: 1000)
    -o, --output DIR         Output directory (default: test/load/results)
    --help                   Show this help message

TESTS:
    1. Connection Handling   - Open N connections, keep alive, verify messaging
    2. High Message Rate     - 50 connections receiving 1000 messages in 60s
    3. Connection Churn      - Rapid connect/disconnect cycles
    4. Filter Performance    - Test with various filter configurations
EOF
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--host)        WS_HOST="$2"; shift 2 ;;
            -p|--port)        WS_PORT="$2"; shift 2 ;;
            -c|--connections) MAX_CONNECTIONS="$2"; shift 2 ;;
            -d|--duration)    KEEPALIVE_DURATION="$2"; shift 2 ;;
            -m|--messages)    HIGH_MSG_COUNT="$2"; shift 2 ;;
            -o|--output)      OUTPUT_DIR="$2"; shift 2 ;;
            --help)           usage; exit 0 ;;
            *)                log_error "Unknown option: $1"; usage; exit 1 ;;
        esac
    done
    WS_FEED_URL="ws://${WS_HOST}:${WS_PORT}/ws/feed"
    WS_ALERTS_URL="ws://${WS_HOST}:${WS_PORT}/ws/alerts"
}

check_prerequisites() {
    log_header "Checking Prerequisites"

    local has_websocat=false
    if command -v websocat &>/dev/null; then
        has_websocat=true
        log_success "websocat found"
    else
        log_warn "websocat not found. Install: cargo install websocat"
        log_info "Falling back to curl-based WebSocket testing (limited)"
    fi

    if ! command -v jq &>/dev/null; then
        log_warn "jq not found — some analysis will be limited"
    fi

    # Check WS endpoint
    log_info "Checking API health at http://${WS_HOST}:${WS_PORT}/health ..."
    if curl -sf --connect-timeout 5 "http://${WS_HOST}:${WS_PORT}/health" > /dev/null 2>&1; then
        log_success "API is reachable"
    else
        log_warn "API not reachable — tests may fail"
    fi
}

setup_output() {
    mkdir -p "${OUTPUT_DIR}"
}

# ==== Memory Monitoring ====

# Monitor system memory usage during tests
start_memory_monitor() {
    local monitor_file="${OUTPUT_DIR}/ws_memory_${TIMESTAMP}.csv"
    echo "timestamp,rss_kb,vsz_kb,connections" > "${monitor_file}"

    (
        while true; do
            local api_pid
            api_pid=$(pgrep -f "wikisurge.*api\|cmd/api" 2>/dev/null | head -1 || echo "")
            if [[ -n "$api_pid" ]]; then
                local rss vsz
                rss=$(ps -o rss= -p "$api_pid" 2>/dev/null || echo "0")
                vsz=$(ps -o vsz= -p "$api_pid" 2>/dev/null || echo "0")
                echo "$(date +%s),${rss},${vsz},${CURRENT_CONNECTIONS:-0}" >> "${monitor_file}"
            else
                echo "$(date +%s),0,0,${CURRENT_CONNECTIONS:-0}" >> "${monitor_file}"
            fi
            sleep 2
        done
    ) &
    MEMORY_MONITOR_PID=$!
    log_info "Memory monitor started (PID: ${MEMORY_MONITOR_PID})"
}

stop_memory_monitor() {
    if [[ -n "${MEMORY_MONITOR_PID:-}" ]]; then
        kill "${MEMORY_MONITOR_PID}" 2>/dev/null || true
        wait "${MEMORY_MONITOR_PID}" 2>/dev/null || true
        log_info "Memory monitor stopped"
    fi
}

# ==== Test 1: Connection Handling ====

test_connection_handling() {
    log_header "Test 1: Connection Handling"
    log_info "Opening ${MAX_CONNECTIONS} WebSocket connections"
    log_info "Keep alive for ${KEEPALIVE_DURATION}s with ${PING_INTERVAL}s ping interval"

    local result_file="${OUTPUT_DIR}/ws_connections_${TIMESTAMP}.log"
    local timing_file="${OUTPUT_DIR}/ws_connection_times_${TIMESTAMP}.txt"
    local pids=()
    local CURRENT_CONNECTIONS=0
    export CURRENT_CONNECTIONS

    > "${result_file}"
    > "${timing_file}"

    local test_start
    test_start=$(date +%s%3N 2>/dev/null || date +%s)

    # Open connections gradually (10 per second to avoid thundering herd)
    for ((i = 1; i <= MAX_CONNECTIONS; i++)); do
        local conn_start
        conn_start=$(date +%s%3N 2>/dev/null || date +%s)

        if command -v websocat &>/dev/null; then
            (
                local msg_count=0
                local conn_id="conn-${i}"

                # Connect and read messages for the duration
                timeout "${KEEPALIVE_DURATION}" websocat -t "${WS_FEED_URL}" 2>/dev/null | \
                while IFS= read -r line; do
                    msg_count=$((msg_count + 1))
                    if [[ $((msg_count % 100)) -eq 0 ]]; then
                        echo "${conn_id}: received ${msg_count} messages" >> "${result_file}"
                    fi
                done

                echo "${conn_id}: total=${msg_count}" >> "${result_file}"
            ) &
            pids+=($!)
        else
            # Curl-based WebSocket (very limited)
            (
                curl -sf --connect-timeout 5 --max-time "${KEEPALIVE_DURATION}" \
                    -H "Upgrade: websocket" \
                    -H "Connection: Upgrade" \
                    -H "Sec-WebSocket-Key: $(openssl rand -base64 16 2>/dev/null || echo 'dGhlIHNhbXBsZSBub25jZQ==')" \
                    -H "Sec-WebSocket-Version: 13" \
                    "http://${WS_HOST}:${WS_PORT}/ws/feed" \
                    -o /dev/null 2>/dev/null
                echo "conn-${i}: completed" >> "${result_file}"
            ) &
            pids+=($!)
        fi

        CURRENT_CONNECTIONS=$i

        local conn_end
        conn_end=$(date +%s%3N 2>/dev/null || date +%s)
        local elapsed=$((conn_end - conn_start))
        echo "${elapsed}" >> "${timing_file}"

        # Progress output every 10 connections
        if [[ $((i % 10)) -eq 0 ]]; then
            log_info "Opened ${i}/${MAX_CONNECTIONS} connections"
        fi

        # Rate limit connection creation
        if [[ $((i % 10)) -eq 0 ]]; then
            sleep 1
        fi
    done

    local all_connected
    all_connected=$(date +%s%3N 2>/dev/null || date +%s)
    local setup_time=$(( (all_connected - test_start) ))

    log_success "All ${MAX_CONNECTIONS} connections opened in ${setup_time}ms"
    log_info "Waiting for keepalive period (${KEEPALIVE_DURATION}s)..."

    # Wait for keepalive period or until connections close
    local remaining=$((KEEPALIVE_DURATION + 30))
    sleep "${remaining}" 2>/dev/null &
    local sleep_pid=$!

    # Check connections periodically
    local check_interval=30
    local elapsed=0
    while [[ ${elapsed} -lt ${KEEPALIVE_DURATION} ]]; do
        sleep "${check_interval}" 2>/dev/null || true
        elapsed=$((elapsed + check_interval))

        # Count alive processes
        local alive=0
        for pid in "${pids[@]}"; do
            if kill -0 "$pid" 2>/dev/null; then
                alive=$((alive + 1))
            fi
        done
        log_info "Active connections: ${alive}/${MAX_CONNECTIONS} (${elapsed}s elapsed)"
    done

    kill "${sleep_pid}" 2>/dev/null || true

    # Cleanup
    for pid in "${pids[@]}"; do
        kill "$pid" 2>/dev/null || true
    done
    wait 2>/dev/null || true

    # Analyze results
    log_info "Analyzing connection test results..."

    local total_messages=0
    local completed=0
    if [[ -f "${result_file}" ]]; then
        completed=$(grep -c "total=" "${result_file}" 2>/dev/null || echo "0")
        total_messages=$(grep "total=" "${result_file}" | \
            sed 's/.*total=\([0-9]*\)/\1/' | \
            awk '{s+=$1} END {print s+0}' 2>/dev/null || echo "0")
    fi

    local avg_conn_time="N/A"
    if [[ -f "${timing_file}" && -s "${timing_file}" ]]; then
        avg_conn_time=$(awk '{s+=$1; n++} END {if(n>0) printf "%.1f", s/n; else print "N/A"}' "${timing_file}")
    fi

    echo ""
    echo "╔══════════════════════════════════════════╗"
    echo "║     Connection Handling Results          ║"
    echo "╠══════════════════════════════════════════╣"
    printf "║ Total Connections:   %-19s ║\n" "${MAX_CONNECTIONS}"
    printf "║ Setup Time:          %-19s ║\n" "${setup_time}ms"
    printf "║ Avg Conn Time:       %-19s ║\n" "${avg_conn_time}ms"
    printf "║ Completed:           %-19s ║\n" "${completed}"
    printf "║ Total Messages Rcvd: %-19s ║\n" "${total_messages}"
    echo "╚══════════════════════════════════════════╝"

    # Save summary
    cat > "${OUTPUT_DIR}/ws_connection_summary_${TIMESTAMP}.json" <<EOJSON
{
    "test": "connection_handling",
    "timestamp": "${TIMESTAMP}",
    "max_connections": ${MAX_CONNECTIONS},
    "setup_time_ms": ${setup_time},
    "avg_connection_time_ms": "${avg_conn_time}",
    "completed": ${completed},
    "total_messages": ${total_messages},
    "keepalive_duration_s": ${KEEPALIVE_DURATION}
}
EOJSON

    if [[ ${completed} -ge $((MAX_CONNECTIONS * 80 / 100)) ]]; then
        log_success "Connection handling test passed (${completed}/${MAX_CONNECTIONS} completed)"
    else
        log_error "Connection handling test failed (${completed}/${MAX_CONNECTIONS} completed)"
    fi
}

# ==== Test 2: High Message Rate ====

test_high_message_rate() {
    log_header "Test 2: High Message Rate"
    log_info "${HIGH_MSG_CONNECTIONS} connections, targeting ${HIGH_MSG_COUNT} messages in ${HIGH_MSG_DURATION}s"

    local result_dir="${OUTPUT_DIR}/ws_high_rate_${TIMESTAMP}"
    mkdir -p "${result_dir}"
    local pids=()

    for ((i = 1; i <= HIGH_MSG_CONNECTIONS; i++)); do
        (
            local msg_file="${result_dir}/conn_${i}.log"
            local count=0
            local start_time
            start_time=$(date +%s)

            if command -v websocat &>/dev/null; then
                # Send a filter to only get high-rate messages
                echo '{"type":"filter","exclude_bots":false}' | \
                timeout "${HIGH_MSG_DURATION}" websocat -t "${WS_FEED_URL}" 2>/dev/null | \
                while IFS= read -r line; do
                    count=$((count + 1))
                    if [[ $count -ge ${HIGH_MSG_COUNT} ]]; then
                        break
                    fi
                done
            else
                # Fallback: just curl
                timeout "${HIGH_MSG_DURATION}" curl -sf --max-time "${HIGH_MSG_DURATION}" \
                    -H "Upgrade: websocket" -H "Connection: Upgrade" \
                    "http://${WS_HOST}:${WS_PORT}/ws/feed" -o /dev/null 2>/dev/null || true
                count=0
            fi

            local end_time
            end_time=$(date +%s)
            local elapsed=$((end_time - start_time))
            echo "${i},${count},${elapsed}" > "${msg_file}"
        ) &
        pids+=($!)

        # Stagger connections
        if [[ $((i % 10)) -eq 0 ]]; then
            sleep 0.5
        fi
    done

    log_info "Waiting for high message rate test to complete (${HIGH_MSG_DURATION}s)..."
    
    # Wait for all connections
    for pid in "${pids[@]}"; do
        wait "$pid" 2>/dev/null || true
    done

    # Analyze results
    local total_msgs=0
    local conn_count=0
    local total_duration=0

    for f in "${result_dir}"/conn_*.log; do
        if [[ -f "$f" ]]; then
            local msgs elapsed
            msgs=$(cut -d',' -f2 "$f" 2>/dev/null || echo "0")
            elapsed=$(cut -d',' -f3 "$f" 2>/dev/null || echo "0")
            total_msgs=$((total_msgs + msgs))
            total_duration=$((total_duration + elapsed))
            conn_count=$((conn_count + 1))
        fi
    done

    local avg_msgs_per_conn=0
    if [[ ${conn_count} -gt 0 ]]; then
        avg_msgs_per_conn=$((total_msgs / conn_count))
    fi

    local msg_rate="N/A"
    if [[ ${total_duration} -gt 0 && ${conn_count} -gt 0 ]]; then
        local avg_duration=$((total_duration / conn_count))
        if [[ ${avg_duration} -gt 0 ]]; then
            msg_rate=$((total_msgs / avg_duration))
        fi
    fi

    echo ""
    echo "╔══════════════════════════════════════════╗"
    echo "║     High Message Rate Results            ║"
    echo "╠══════════════════════════════════════════╣"
    printf "║ Connections:         %-19s ║\n" "${conn_count}"
    printf "║ Total Messages:      %-19s ║\n" "${total_msgs}"
    printf "║ Avg Msgs/Conn:       %-19s ║\n" "${avg_msgs_per_conn}"
    printf "║ Overall Rate:        %-19s ║\n" "${msg_rate} msg/s"
    echo "╚══════════════════════════════════════════╝"

    cat > "${OUTPUT_DIR}/ws_high_rate_summary_${TIMESTAMP}.json" <<EOJSON
{
    "test": "high_message_rate",
    "timestamp": "${TIMESTAMP}",
    "connections": ${conn_count},
    "total_messages": ${total_msgs},
    "avg_messages_per_connection": ${avg_msgs_per_conn},
    "message_rate": "${msg_rate}",
    "duration_s": ${HIGH_MSG_DURATION}
}
EOJSON

    log_success "High message rate test completed"
}

# ==== Test 3: Connection Churn ====

test_connection_churn() {
    log_header "Test 3: Connection Churn"
    log_info "Rapidly connecting and disconnecting to test cleanup"

    local churn_count=200
    local churn_duration=60
    local result_file="${OUTPUT_DIR}/ws_churn_${TIMESTAMP}.log"
    > "${result_file}"

    local start_time
    start_time=$(date +%s)
    local end_time=$((start_time + churn_duration))
    local connections_made=0
    local failures=0

    while [[ $(date +%s) -lt ${end_time} && ${connections_made} -lt ${churn_count} ]]; do
        (
            local conn_start
            conn_start=$(date +%s%3N 2>/dev/null || date +%s)

            if command -v websocat &>/dev/null; then
                # Connect for 1-5 seconds then disconnect
                local hold=$((RANDOM % 5 + 1))
                timeout "${hold}" websocat -t "${WS_FEED_URL}" > /dev/null 2>&1 || true
            else
                local hold=$((RANDOM % 5 + 1))
                timeout "${hold}" curl -sf --max-time "${hold}" \
                    -H "Upgrade: websocket" -H "Connection: Upgrade" \
                    "http://${WS_HOST}:${WS_PORT}/ws/feed" -o /dev/null 2>/dev/null || true
            fi

            local conn_end
            conn_end=$(date +%s%3N 2>/dev/null || date +%s)
            echo "$((conn_end - conn_start))" >> "${result_file}"
        ) &

        connections_made=$((connections_made + 1))

        # 3-5 connections per second
        sleep 0.25
    done

    wait 2>/dev/null || true

    local actual_end
    actual_end=$(date +%s)
    local total_time=$((actual_end - start_time))

    echo ""
    echo "╔══════════════════════════════════════════╗"
    echo "║     Connection Churn Results             ║"
    echo "╠══════════════════════════════════════════╣"
    printf "║ Connections Made:    %-19s ║\n" "${connections_made}"
    printf "║ Duration:            %-19s ║\n" "${total_time}s"
    printf "║ Rate:                %-19s ║\n" "$((connections_made / (total_time + 1)))/s"
    echo "╚══════════════════════════════════════════╝"

    cat > "${OUTPUT_DIR}/ws_churn_summary_${TIMESTAMP}.json" <<EOJSON
{
    "test": "connection_churn",
    "timestamp": "${TIMESTAMP}",
    "connections_made": ${connections_made},
    "duration_s": ${total_time},
    "rate_per_second": $((connections_made / (total_time + 1)))
}
EOJSON

    log_success "Connection churn test completed"
}

# ==== Test 4: WebSocket Filter Performance ====

test_filter_performance() {
    log_header "Test 4: Filter Performance"
    log_info "Testing various filter configurations"

    local filters=(
        '{"type":"filter","languages":["en"],"exclude_bots":true}'
        '{"type":"filter","languages":["en","es","fr"],"exclude_bots":false}'
        '{"type":"filter","page_pattern":".*Wikipedia.*","exclude_bots":true}'
        '{"type":"filter","min_byte_change":100}'
        '{"type":"filter","languages":["de"],"min_byte_change":50,"exclude_bots":true}'
    )

    local filter_names=(
        "english_no_bots"
        "multi_lang"
        "pattern_match"
        "min_bytes"
        "combined"
    )

    for ((i = 0; i < ${#filters[@]}; i++)); do
        local filter="${filters[$i]}"
        local name="${filter_names[$i]}"
        local duration=15

        log_info "Testing filter: ${name}"

        if command -v websocat &>/dev/null; then
            local msg_count=0
            local start_time
            start_time=$(date +%s)

            msg_count=$(echo "${filter}" | \
                timeout "${duration}" websocat -t "${WS_FEED_URL}" 2>/dev/null | \
                wc -l || echo "0")

            local end_time
            end_time=$(date +%s)
            local elapsed=$((end_time - start_time))

            log_info "  Filter '${name}': ${msg_count} messages in ${elapsed}s"
        else
            log_info "  Filter '${name}': skipped (no websocat)"
        fi
    done

    log_success "Filter performance test completed"
}

# ==== Main ====

main() {
    parse_args "$@"

    log_header "WikiSurge WebSocket Load Testing"
    log_info "Target: ${WS_FEED_URL}"
    log_info "Timestamp: ${TIMESTAMP}"

    check_prerequisites
    setup_output

    # Start memory monitor
    start_memory_monitor

    # Run tests
    test_connection_handling
    test_high_message_rate
    test_connection_churn
    test_filter_performance

    # Stop memory monitor
    stop_memory_monitor

    # Summary
    log_header "WebSocket Load Testing Complete"

    echo ""
    echo "Metrics Summary:"
    echo "  - Connection setup time: see ws_connection_times_${TIMESTAMP}.txt"
    echo "  - Message latency: see ws_high_rate_summary_${TIMESTAMP}.json"
    echo "  - Memory per connection: see ws_memory_${TIMESTAMP}.csv"
    echo "  - CPU usage: monitor externally via top/htop"
    echo "  - Max concurrent connections tested: ${MAX_CONNECTIONS}"
    echo ""
    log_info "Results saved to: ${OUTPUT_DIR}/"
}

main "$@"
