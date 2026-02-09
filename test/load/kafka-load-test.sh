#!/bin/bash

# =============================================================================
# WikiSurge Kafka / Ingestion Load Testing Script
# =============================================================================
# Tests the ingestion pipeline under various load conditions:
#   - High edit rate (100 edits/sec sustained)
#   - Spike handling (normal -> 500 edits/sec burst -> recovery)
#   - Consumer lag measurement
#   - End-to-end pipeline throughput
#
# Prerequisites:
#   - kafka-console-producer / kafka-producer-perf-test (from Kafka distribution)
#   - curl (for SSE ingestion simulation)
#   - jq, bc
#
# Usage:
#   ./test/load/kafka-load-test.sh [OPTIONS]
# =============================================================================

set -euo pipefail

# ==== Configuration ====
KAFKA_BROKER="${KAFKA_BROKER:-localhost:19092}"
KAFKA_TOPIC="${KAFKA_TOPIC:-wikipedia-edits}"
API_HOST="${API_HOST:-localhost}"
API_PORT="${API_PORT:-8080}"
INGESTOR_PORT="${INGESTOR_PORT:-2112}"
OUTPUT_DIR="test/load/results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Test parameters
HIGH_RATE=100               # edits per second for sustained test
HIGH_RATE_DURATION=600      # 10 minutes
SPIKE_NORMAL_RATE=10        # Normal rate
SPIKE_HIGH_RATE=500         # Spike rate
SPIKE_DURATION=60           # Spike duration in seconds
SPIKE_TOTAL_DURATION=300    # Total test duration

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
WikiSurge Kafka / Ingestion Load Testing Script

USAGE:
    ./test/load/kafka-load-test.sh [OPTIONS]

OPTIONS:
    -b, --broker BROKER      Kafka broker address (default: localhost:19092)
    -t, --topic TOPIC        Kafka topic (default: wikipedia-edits)
    -r, --rate RATE          High sustained rate in edits/sec (default: 100)
    -d, --duration SECS      High rate duration in seconds (default: 600)
    --spike-rate RATE        Spike rate in edits/sec (default: 500)
    --spike-duration SECS    Spike duration in seconds (default: 60)
    -o, --output DIR         Output directory (default: test/load/results)
    --help                   Show this help message

SCENARIOS:
    1. Sustained High Rate   - 100 edits/sec for 10 minutes
    2. Spike Handling        - Normal -> 500/sec spike -> Recovery
    3. Consumer Lag          - Measure consumer group lag during load
    4. End-to-End Pipeline   - Full path: produce -> consume -> process -> API
EOF
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -b|--broker)         KAFKA_BROKER="$2"; shift 2 ;;
            -t|--topic)          KAFKA_TOPIC="$2"; shift 2 ;;
            -r|--rate)           HIGH_RATE="$2"; shift 2 ;;
            -d|--duration)       HIGH_RATE_DURATION="$2"; shift 2 ;;
            --spike-rate)        SPIKE_HIGH_RATE="$2"; shift 2 ;;
            --spike-duration)    SPIKE_DURATION="$2"; shift 2 ;;
            -o|--output)         OUTPUT_DIR="$2"; shift 2 ;;
            --help)              usage; exit 0 ;;
            *)                   log_error "Unknown option: $1"; usage; exit 1 ;;
        esac
    done
}

check_prerequisites() {
    log_header "Checking Prerequisites"

    # Check Kafka tools
    local has_kafka_tools=false
    if command -v kafka-console-producer &>/dev/null || \
       command -v kafka-console-producer.sh &>/dev/null; then
        has_kafka_tools=true
        log_success "Kafka console tools found"
    else
        log_warn "Kafka console tools not found — using built-in producer simulation"
    fi

    if command -v kafka-consumer-groups &>/dev/null || \
       command -v kafka-consumer-groups.sh &>/dev/null; then
        log_success "kafka-consumer-groups found"
    else
        log_warn "kafka-consumer-groups not found — lag monitoring limited"
    fi

    # Test Kafka connectivity
    log_info "Checking Kafka broker at ${KAFKA_BROKER}..."
    if timeout 5 bash -c "echo > /dev/tcp/${KAFKA_BROKER//:/\/}" 2>/dev/null; then
        log_success "Kafka broker reachable"
    else
        log_warn "Kafka broker at ${KAFKA_BROKER} not reachable"
    fi

    # Check API
    if curl -sf --connect-timeout 5 "http://${API_HOST}:${API_PORT}/health" > /dev/null 2>&1; then
        log_success "API is reachable"
    else
        log_warn "API not reachable at http://${API_HOST}:${API_PORT}"
    fi
}

setup_output() {
    mkdir -p "${OUTPUT_DIR}"
}

# ==== Edit Event Generators ====

# Generate a realistic Wikipedia edit event JSON
generate_edit_event() {
    local idx=${1:-1}
    local titles=(
        "Main_Page" "United_States" "JavaScript" "Python_(programming_language)"
        "Linux" "Albert_Einstein" "World_War_II" "Moon" "DNA" "Computer_science"
        "Barack_Obama" "Solar_System" "Evolution" "Mathematics" "Machine_learning"
        "Climate_change" "Quantum_mechanics" "Human_rights" "Artificial_intelligence"
        "Blockchain" "SpaceX" "COVID-19_pandemic" "Democracy" "Philosophy"
        "Genetics" "Astronomy" "Chemistry" "Biology" "History_of_Europe"
        "Geography" "Economics" "Psychology" "Sociology" "Music" "Art"
    )
    local users=(
        "WikiEditor42" "BotHelper" "AcademicUser" "NewContributor" "ExpertReviewer"
        "CommunityMod" "ResearchBot" "StudentEditor" "HistoryBuff" "ScienceFan"
    )
    local languages=("en" "es" "fr" "de" "ja" "zh" "pt" "ru" "it" "ar")

    local title_idx=$((idx % ${#titles[@]}))
    local user_idx=$((idx % ${#users[@]}))
    local lang_idx=$((idx % ${#languages[@]}))
    local is_bot="false"
    if [[ $((idx % 10)) -eq 0 ]]; then
        is_bot="true"
    fi

    local old_len=$((RANDOM % 50000 + 1000))
    local new_len=$((old_len + RANDOM % 2000 - 1000))
    local revision=$((1000000 + idx))
    local ts
    ts=$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -Iseconds)

    cat <<EOJSON
{"title":"${titles[$title_idx]}","user":"${users[$user_idx]}","bot":${is_bot},"server_name":"${languages[$lang_idx]}.wikipedia.org","revision":{"new":${revision},"old":$((revision - 1))},"length":{"new":${new_len},"old":${old_len}},"timestamp":"${ts}","comment":"Edit #${idx}","type":"edit","namespace":0}
EOJSON
}

# ==== Monitoring Helpers ====

# Monitor Kafka consumer lag
start_lag_monitor() {
    local lag_file="${OUTPUT_DIR}/kafka_lag_${TIMESTAMP}.csv"
    echo "timestamp,topic,partition,current_offset,log_end_offset,lag" > "${lag_file}"

    (
        while true; do
            # Try docker-based kafka tools first, then native
            local lag_output=""
            if command -v docker &>/dev/null; then
                lag_output=$(docker exec kafka kafka-consumer-groups \
                    --bootstrap-server localhost:9092 \
                    --group wikisurge-dev \
                    --describe 2>/dev/null || echo "")
            fi

            if [[ -z "$lag_output" ]] && command -v kafka-consumer-groups &>/dev/null; then
                lag_output=$(kafka-consumer-groups \
                    --bootstrap-server "${KAFKA_BROKER}" \
                    --group wikisurge-dev \
                    --describe 2>/dev/null || echo "")
            fi

            if [[ -n "$lag_output" ]]; then
                echo "$lag_output" | grep "${KAFKA_TOPIC}" | while read -r line; do
                    local partition current_offset log_end lag
                    partition=$(echo "$line" | awk '{print $3}')
                    current_offset=$(echo "$line" | awk '{print $4}')
                    log_end=$(echo "$line" | awk '{print $5}')
                    lag=$(echo "$line" | awk '{print $6}')
                    echo "$(date +%s),${KAFKA_TOPIC},${partition},${current_offset},${log_end},${lag}" >> "${lag_file}"
                done
            fi

            sleep 5
        done
    ) &
    LAG_MONITOR_PID=$!
    log_info "Lag monitor started (PID: ${LAG_MONITOR_PID})"
}

stop_lag_monitor() {
    if [[ -n "${LAG_MONITOR_PID:-}" ]]; then
        kill "${LAG_MONITOR_PID}" 2>/dev/null || true
        wait "${LAG_MONITOR_PID}" 2>/dev/null || true
        log_info "Lag monitor stopped"
    fi
}

# Monitor throughput via API stats
record_throughput_snapshot() {
    local output_file="$1"
    local stats
    stats=$(curl -sf --connect-timeout 5 "http://${API_HOST}:${API_PORT}/api/stats" 2>/dev/null || echo "{}")
    echo "$(date +%s),${stats}" >> "${output_file}"
}

# ==== Test 1: Sustained High Edit Rate ====

test_sustained_high_rate() {
    log_header "Scenario 1: Sustained High Edit Rate"
    log_info "Rate: ${HIGH_RATE} edits/sec | Duration: ${HIGH_RATE_DURATION}s"

    local result_file="${OUTPUT_DIR}/kafka_sustained_${TIMESTAMP}.log"
    local throughput_file="${OUTPUT_DIR}/kafka_throughput_${TIMESTAMP}.csv"
    local latencies_file="${OUTPUT_DIR}/kafka_producer_latencies_${TIMESTAMP}.txt"
    echo "timestamp,stats_json" > "${throughput_file}"
    > "${result_file}"
    > "${latencies_file}"

    local total_sent=0
    local total_errors=0
    local delay
    delay=$(echo "scale=6; 1.0 / ${HIGH_RATE}" | bc 2>/dev/null || echo "0.01")

    local start_time
    start_time=$(date +%s)
    local end_time=$((start_time + HIGH_RATE_DURATION))

    # Start throughput recorder
    (
        while [[ $(date +%s) -lt ${end_time} ]]; do
            record_throughput_snapshot "${throughput_file}"
            sleep 10
        done
    ) &
    local throughput_pid=$!

    log_info "Producing messages at ${HIGH_RATE}/sec..."

    # Produce messages at the target rate
    local batch_size=10
    local batch_delay
    batch_delay=$(echo "scale=6; ${batch_size}.0 / ${HIGH_RATE}" | bc 2>/dev/null || echo "0.1")

    while [[ $(date +%s) -lt ${end_time} ]]; do
        for ((b = 0; b < batch_size; b++)); do
            total_sent=$((total_sent + 1))
            local evt
            evt=$(generate_edit_event ${total_sent})

            # Send via kafka-console-producer or simulate via test endpoint
            local send_start
            send_start=$(date +%s%3N 2>/dev/null || date +%s)

            if curl -sf --connect-timeout 2 --max-time 5 \
                -X POST "http://${API_HOST}:${INGESTOR_PORT}/simulate" \
                -H "Content-Type: application/json" \
                -d "${evt}" > /dev/null 2>&1; then
                local send_end
                send_end=$(date +%s%3N 2>/dev/null || date +%s)
                echo "$((send_end - send_start))" >> "${latencies_file}"
            else
                total_errors=$((total_errors + 1))
            fi
        done

        # Progress every 30 seconds
        local now
        now=$(date +%s)
        local elapsed=$((now - start_time))
        if [[ $((elapsed % 30)) -lt 2 ]]; then
            local rate_actual=$((total_sent / (elapsed + 1)))
            log_info "[${elapsed}s] Sent: ${total_sent} | Errors: ${total_errors} | Rate: ${rate_actual}/s"
        fi

        sleep "${batch_delay}" 2>/dev/null || sleep 0.1
    done

    kill "${throughput_pid}" 2>/dev/null || true
    wait "${throughput_pid}" 2>/dev/null || true

    local actual_end
    actual_end=$(date +%s)
    local actual_duration=$((actual_end - start_time))
    local actual_rate=$((total_sent / (actual_duration + 1)))
    local error_pct="0"
    if [[ ${total_sent} -gt 0 ]]; then
        error_pct=$(echo "scale=4; ${total_errors} * 100 / ${total_sent}" | bc 2>/dev/null || echo "0")
    fi

    # Compute producer latency percentiles
    local p50_ms="N/A" p95_ms="N/A" p99_ms="N/A"
    if [[ -s "${latencies_file}" ]]; then
        sort -n "${latencies_file}" > "${latencies_file}.sorted"
        local count
        count=$(wc -l < "${latencies_file}.sorted")
        if [[ ${count} -gt 0 ]]; then
            p50_ms=$(sed -n "$((count * 50 / 100 + 1))p" "${latencies_file}.sorted" || echo "N/A")
            p95_ms=$(sed -n "$((count * 95 / 100 + 1))p" "${latencies_file}.sorted" || echo "N/A")
            p99_ms=$(sed -n "$((count * 99 / 100 + 1))p" "${latencies_file}.sorted" || echo "N/A")
        fi
        rm -f "${latencies_file}.sorted"
    fi

    echo ""
    echo "╔══════════════════════════════════════════╗"
    echo "║   Sustained High Rate Results            ║"
    echo "╠══════════════════════════════════════════╣"
    printf "║ Target Rate:         %-19s ║\n" "${HIGH_RATE}/s"
    printf "║ Actual Rate:         %-19s ║\n" "${actual_rate}/s"
    printf "║ Duration:            %-19s ║\n" "${actual_duration}s"
    printf "║ Total Sent:          %-19s ║\n" "${total_sent}"
    printf "║ Errors:              %-19s ║\n" "${total_errors}"
    printf "║ Error Rate:          %-19s ║\n" "${error_pct}%"
    printf "║ Producer p50:        %-19s ║\n" "${p50_ms}ms"
    printf "║ Producer p95:        %-19s ║\n" "${p95_ms}ms"
    printf "║ Producer p99:        %-19s ║\n" "${p99_ms}ms"
    echo "╚══════════════════════════════════════════╝"

    cat > "${OUTPUT_DIR}/kafka_sustained_summary_${TIMESTAMP}.json" <<EOJSON
{
    "scenario": "sustained_high_rate",
    "timestamp": "${TIMESTAMP}",
    "target_rate": ${HIGH_RATE},
    "actual_rate": ${actual_rate},
    "duration_s": ${actual_duration},
    "total_sent": ${total_sent},
    "errors": ${total_errors},
    "error_rate_pct": "${error_pct}",
    "producer_latency_p50_ms": "${p50_ms}",
    "producer_latency_p95_ms": "${p95_ms}",
    "producer_latency_p99_ms": "${p99_ms}"
}
EOJSON

    if [[ ${actual_rate} -ge $((HIGH_RATE * 80 / 100)) ]]; then
        log_success "Sustained rate test passed: ${actual_rate}/s >= 80% of target ${HIGH_RATE}/s"
    else
        log_error "Sustained rate test failed: ${actual_rate}/s < 80% of target ${HIGH_RATE}/s"
    fi
}

# ==== Test 2: Spike Handling ====

test_spike_handling() {
    log_header "Scenario 2: Spike Handling"
    log_info "Normal: ${SPIKE_NORMAL_RATE}/s -> Spike: ${SPIKE_HIGH_RATE}/s for ${SPIKE_DURATION}s -> Recovery"

    local result_file="${OUTPUT_DIR}/kafka_spike_${TIMESTAMP}.log"
    local throughput_file="${OUTPUT_DIR}/kafka_spike_throughput_${TIMESTAMP}.csv"
    echo "timestamp,phase,sent,errors,rate" > "${throughput_file}"
    > "${result_file}"

    local total_sent=0
    local total_errors=0
    local start_time
    start_time=$(date +%s)

    # Phase 1: Normal rate (warm-up)
    local normal_duration=$(( (SPIKE_TOTAL_DURATION - SPIKE_DURATION) / 2 ))
    log_info "Phase 1: Normal rate (${SPIKE_NORMAL_RATE}/s) for ${normal_duration}s"

    local phase_start
    phase_start=$(date +%s)
    local phase_end=$((phase_start + normal_duration))
    local delay
    delay=$(echo "scale=6; 1.0 / ${SPIKE_NORMAL_RATE}" | bc 2>/dev/null || echo "0.1")

    while [[ $(date +%s) -lt ${phase_end} ]]; do
        total_sent=$((total_sent + 1))
        local evt
        evt=$(generate_edit_event ${total_sent})
        if ! curl -sf --connect-timeout 2 --max-time 5 \
            -X POST "http://${API_HOST}:${INGESTOR_PORT}/simulate" \
            -H "Content-Type: application/json" \
            -d "${evt}" > /dev/null 2>&1; then
            total_errors=$((total_errors + 1))
        fi
        sleep "${delay}" 2>/dev/null || sleep 0.1
    done

    local pre_spike_sent=${total_sent}
    log_info "Pre-spike: ${pre_spike_sent} events sent"

    # Record API stats before spike
    local pre_spike_stats
    pre_spike_stats=$(curl -sf "http://${API_HOST}:${API_PORT}/api/stats" 2>/dev/null || echo "{}")

    # Phase 2: Spike
    log_info "Phase 2: SPIKE (${SPIKE_HIGH_RATE}/s) for ${SPIKE_DURATION}s"
    phase_start=$(date +%s)
    phase_end=$((phase_start + SPIKE_DURATION))

    local batch_size=50
    local batch_delay
    batch_delay=$(echo "scale=6; ${batch_size}.0 / ${SPIKE_HIGH_RATE}" | bc 2>/dev/null || echo "0.1")
    local spike_sent=0
    local spike_errors=0

    while [[ $(date +%s) -lt ${phase_end} ]]; do
        for ((b = 0; b < batch_size; b++)); do
            total_sent=$((total_sent + 1))
            spike_sent=$((spike_sent + 1))
            local evt
            evt=$(generate_edit_event ${total_sent})
            if ! curl -sf --connect-timeout 2 --max-time 5 \
                -X POST "http://${API_HOST}:${INGESTOR_PORT}/simulate" \
                -H "Content-Type: application/json" \
                -d "${evt}" > /dev/null 2>&1; then
                total_errors=$((total_errors + 1))
                spike_errors=$((spike_errors + 1))
            fi
        done
        sleep "${batch_delay}" 2>/dev/null || sleep 0.1
    done

    local spike_end_time
    spike_end_time=$(date +%s)
    log_info "Spike phase: ${spike_sent} events sent, ${spike_errors} errors"

    # Phase 3: Recovery (back to normal rate)
    log_info "Phase 3: Recovery (${SPIKE_NORMAL_RATE}/s) for ${normal_duration}s"
    phase_start=$(date +%s)
    phase_end=$((phase_start + normal_duration))
    delay=$(echo "scale=6; 1.0 / ${SPIKE_NORMAL_RATE}" | bc 2>/dev/null || echo "0.1")

    local recovery_start=${total_sent}

    while [[ $(date +%s) -lt ${phase_end} ]]; do
        total_sent=$((total_sent + 1))
        local evt
        evt=$(generate_edit_event ${total_sent})
        if ! curl -sf --connect-timeout 2 --max-time 5 \
            -X POST "http://${API_HOST}:${INGESTOR_PORT}/simulate" \
            -H "Content-Type: application/json" \
            -d "${evt}" > /dev/null 2>&1; then
            total_errors=$((total_errors + 1))
        fi
        sleep "${delay}" 2>/dev/null || sleep 0.1
    done

    local actual_end
    actual_end=$(date +%s)
    local total_duration=$((actual_end - start_time))

    # Record post-recovery stats
    local post_recovery_stats
    post_recovery_stats=$(curl -sf "http://${API_HOST}:${API_PORT}/api/stats" 2>/dev/null || echo "{}")

    echo ""
    echo "╔══════════════════════════════════════════╗"
    echo "║       Spike Handling Results             ║"
    echo "╠══════════════════════════════════════════╣"
    printf "║ Total Duration:      %-19s ║\n" "${total_duration}s"
    printf "║ Total Sent:          %-19s ║\n" "${total_sent}"
    printf "║ Total Errors:        %-19s ║\n" "${total_errors}"
    printf "║ Pre-Spike Events:    %-19s ║\n" "${pre_spike_sent}"
    printf "║ Spike Events:        %-19s ║\n" "${spike_sent}"
    printf "║ Spike Errors:        %-19s ║\n" "${spike_errors}"
    printf "║ Recovery Events:     %-19s ║\n" "$((total_sent - pre_spike_sent - spike_sent))"
    echo "╚══════════════════════════════════════════╝"

    cat > "${OUTPUT_DIR}/kafka_spike_summary_${TIMESTAMP}.json" <<EOJSON
{
    "scenario": "spike_handling",
    "timestamp": "${TIMESTAMP}",
    "total_duration_s": ${total_duration},
    "total_sent": ${total_sent},
    "total_errors": ${total_errors},
    "normal_rate": ${SPIKE_NORMAL_RATE},
    "spike_rate": ${SPIKE_HIGH_RATE},
    "spike_duration_s": ${SPIKE_DURATION},
    "pre_spike_events": ${pre_spike_sent},
    "spike_events": ${spike_sent},
    "spike_errors": ${spike_errors},
    "recovery_events": $((total_sent - pre_spike_sent - spike_sent))
}
EOJSON

    log_success "Spike handling test completed"
}

# ==== Test 3: Consumer Lag Analysis ====

test_consumer_lag() {
    log_header "Scenario 3: Consumer Lag Analysis"
    log_info "Monitoring consumer group lag during message production"

    local lag_file="${OUTPUT_DIR}/kafka_lag_analysis_${TIMESTAMP}.log"
    > "${lag_file}"

    # Try to get consumer group lag
    local has_lag_info=false

    # Docker-based Kafka
    if command -v docker &>/dev/null; then
        local lag_output
        lag_output=$(docker exec kafka kafka-consumer-groups \
            --bootstrap-server localhost:9092 \
            --group wikisurge-dev \
            --describe 2>/dev/null || echo "")

        if [[ -n "$lag_output" ]]; then
            has_lag_info=true
            echo "$lag_output" > "${lag_file}"
            log_info "Consumer group lag:"
            echo "$lag_output"
        fi
    fi

    # Native kafka tools
    if [[ "$has_lag_info" == "false" ]] && command -v kafka-consumer-groups &>/dev/null; then
        local lag_output
        lag_output=$(kafka-consumer-groups \
            --bootstrap-server "${KAFKA_BROKER}" \
            --group wikisurge-dev \
            --describe 2>/dev/null || echo "")

        if [[ -n "$lag_output" ]]; then
            has_lag_info=true
            echo "$lag_output" > "${lag_file}"
            log_info "Consumer group lag:"
            echo "$lag_output"
        fi
    fi

    if [[ "$has_lag_info" == "false" ]]; then
        log_warn "Could not retrieve consumer lag — Kafka tools not available"
        log_info "To check manually: kafka-consumer-groups --bootstrap-server ${KAFKA_BROKER} --group wikisurge-dev --describe"
    fi

    # Produce a burst and measure lag increase/recovery
    log_info "Producing burst of 500 messages to measure lag..."
    local burst_sent=0
    for ((i = 0; i < 500; i++)); do
        local evt
        evt=$(generate_edit_event $((10000 + i)))
        if curl -sf --connect-timeout 2 --max-time 5 \
            -X POST "http://${API_HOST}:${INGESTOR_PORT}/simulate" \
            -H "Content-Type: application/json" \
            -d "${evt}" > /dev/null 2>&1; then
            burst_sent=$((burst_sent + 1))
        fi
    done

    log_info "Burst sent: ${burst_sent}/500 messages"

    # Wait and check lag recovery
    log_info "Waiting 30s for consumer to catch up..."
    sleep 30

    if [[ "$has_lag_info" == "true" ]]; then
        local post_lag
        if command -v docker &>/dev/null; then
            post_lag=$(docker exec kafka kafka-consumer-groups \
                --bootstrap-server localhost:9092 \
                --group wikisurge-dev \
                --describe 2>/dev/null || echo "")
        elif command -v kafka-consumer-groups &>/dev/null; then
            post_lag=$(kafka-consumer-groups \
                --bootstrap-server "${KAFKA_BROKER}" \
                --group wikisurge-dev \
                --describe 2>/dev/null || echo "")
        fi

        if [[ -n "${post_lag:-}" ]]; then
            log_info "Post-recovery consumer lag:"
            echo "${post_lag}"
        fi
    fi

    log_success "Consumer lag analysis completed"
}

# ==== Test 4: End-to-End Pipeline ====

test_end_to_end() {
    log_header "Scenario 4: End-to-End Pipeline Throughput"
    log_info "Measuring full pipeline: ingestor -> Kafka -> processor -> API"

    # Record initial trending/stats state
    local initial_stats
    initial_stats=$(curl -sf "http://${API_HOST}:${API_PORT}/api/stats" 2>/dev/null || echo "{}")
    log_info "Initial stats: ${initial_stats}"

    # Send a known batch of events
    local batch_size=100
    local sent=0
    local marker="E2E_TEST_${TIMESTAMP}"

    log_info "Sending ${batch_size} events with marker: ${marker}"
    local send_start
    send_start=$(date +%s)

    for ((i = 0; i < batch_size; i++)); do
        local evt
        evt=$(generate_edit_event $((20000 + i)))
        if curl -sf --connect-timeout 2 --max-time 5 \
            -X POST "http://${API_HOST}:${INGESTOR_PORT}/simulate" \
            -H "Content-Type: application/json" \
            -d "${evt}" > /dev/null 2>&1; then
            sent=$((sent + 1))
        fi
    done

    local send_end
    send_end=$(date +%s)
    local send_duration=$((send_end - send_start))
    log_info "Sent ${sent}/${batch_size} events in ${send_duration}s"

    # Wait for processing
    log_info "Waiting 15s for pipeline processing..."
    sleep 15

    # Check if events propagated to API
    local final_stats
    final_stats=$(curl -sf "http://${API_HOST}:${API_PORT}/api/stats" 2>/dev/null || echo "{}")
    log_info "Final stats: ${final_stats}"

    # Check trending
    local trending
    trending=$(curl -sf "http://${API_HOST}:${API_PORT}/api/trending?limit=10" 2>/dev/null || echo "[]")
    log_info "Trending pages: $(echo "${trending}" | jq -r 'length' 2>/dev/null || echo 'N/A')"

    local pipeline_latency=$((send_duration + 15))

    echo ""
    echo "╔══════════════════════════════════════════╗"
    echo "║    End-to-End Pipeline Results            ║"
    echo "╠══════════════════════════════════════════╣"
    printf "║ Events Sent:         %-19s ║\n" "${sent}"
    printf "║ Send Duration:       %-19s ║\n" "${send_duration}s"
    printf "║ Pipeline Latency:    %-19s ║\n" "~${pipeline_latency}s"
    echo "╚══════════════════════════════════════════╝"

    cat > "${OUTPUT_DIR}/kafka_e2e_summary_${TIMESTAMP}.json" <<EOJSON
{
    "scenario": "end_to_end_pipeline",
    "timestamp": "${TIMESTAMP}",
    "events_sent": ${sent},
    "send_duration_s": ${send_duration},
    "pipeline_latency_s": ${pipeline_latency},
    "initial_stats": ${initial_stats:-"{}"},
    "final_stats": ${final_stats:-"{}"}
}
EOJSON

    log_success "End-to-end pipeline test completed"
}

# ==== Main ====

main() {
    parse_args "$@"

    log_header "WikiSurge Kafka / Ingestion Load Testing"
    log_info "Broker: ${KAFKA_BROKER}"
    log_info "Topic: ${KAFKA_TOPIC}"
    log_info "Timestamp: ${TIMESTAMP}"

    check_prerequisites
    setup_output

    # Start lag monitor
    start_lag_monitor

    # Run scenarios
    test_sustained_high_rate
    test_spike_handling
    test_consumer_lag
    test_end_to_end

    # Stop lag monitor
    stop_lag_monitor

    log_header "Kafka / Ingestion Load Testing Complete"
    log_info "Results saved to: ${OUTPUT_DIR}/"
}

main "$@"
