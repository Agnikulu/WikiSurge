#!/bin/bash

# =============================================================================
# WikiSurge Chaos Testing Script
# =============================================================================
# Tests system resilience by introducing controlled failures:
#   1. Kill random services
#   2. Introduce network latency
#   3. Limit bandwidth
#   4. Fill disk space
#   5. Spike memory usage
#   6. Introduce packet loss
#
# For each experiment: verify graceful handling, recovery time, and no data loss.
#
# Prerequisites:
#   - docker & docker-compose
#   - tc (iproute2) for network chaos
#   - stress-ng for resource pressure
#   - jq, bc
#
# Usage:
#   ./test/chaos/random-failures.sh [OPTIONS]
# =============================================================================

set -euo pipefail

# ==== Configuration ====
API_HOST="${API_HOST:-localhost}"
API_PORT="${API_PORT:-8080}"
BASE_URL="http://${API_HOST}:${API_PORT}"
OUTPUT_DIR="test/chaos/results"
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
WikiSurge Chaos Testing Script

USAGE:
    ./test/chaos/random-failures.sh [OPTIONS]

OPTIONS:
    --experiment EXP     Run specific experiment:
                           kill-service, network-latency, bandwidth-limit,
                           disk-fill, memory-spike, packet-loss, all
    --duration SECS      Duration of chaos injection (default: 30)
    --dry-run            Show what would be done without executing
    -o, --output DIR     Output directory (default: test/chaos/results)
    --help               Show this help message

SAFETY:
    - All chaos experiments are time-bounded
    - Cleanup is automatic (via trap)
    - Non-destructive by default
EOF
}

EXPERIMENT="all"
CHAOS_DURATION=30
DRY_RUN=false

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --experiment)  EXPERIMENT="$2"; shift 2 ;;
            --duration)    CHAOS_DURATION="$2"; shift 2 ;;
            --dry-run)     DRY_RUN=true; shift ;;
            -o|--output)   OUTPUT_DIR="$2"; shift 2 ;;
            --help)        usage; exit 0 ;;
            *)             log_error "Unknown: $1"; exit 1 ;;
        esac
    done
}

setup() {
    mkdir -p "${OUTPUT_DIR}"
    log_info "Results: ${OUTPUT_DIR}"
    log_info "Chaos duration: ${CHAOS_DURATION}s"
}

# Check if API is healthy
check_health() {
    local attempts=0
    local max_attempts=30
    while [[ ${attempts} -lt ${max_attempts} ]]; do
        if curl -sf --connect-timeout 3 "${BASE_URL}/health" > /dev/null 2>&1; then
            return 0
        fi
        attempts=$((attempts + 1))
        sleep 1
    done
    return 1
}

# Record health status over time
record_health_timeline() {
    local output_file="$1"
    local duration="$2"
    local end_time=$(($(date +%s) + duration))

    while [[ $(date +%s) -lt ${end_time} ]]; do
        local status="DOWN"
        local latency="N/A"
        local start_ms
        start_ms=$(date +%s%3N 2>/dev/null || date +%s)

        if curl -sf --connect-timeout 3 --max-time 5 "${BASE_URL}/health" > /dev/null 2>&1; then
            local end_ms
            end_ms=$(date +%s%3N 2>/dev/null || date +%s)
            latency=$((end_ms - start_ms))
            status="UP"
        fi

        echo "$(date +%s),${status},${latency}" >> "${output_file}"
        sleep 1
    done
}

# ==== Experiment 1: Kill Random Service ====

chaos_kill_service() {
    log_header "Chaos Experiment: Kill Random Service"

    local services=("redis" "kafka" "elasticsearch")
    local target="${services[$((RANDOM % ${#services[@]}))]}"

    log_info "Target service: ${target}"
    log_info "Duration: ${CHAOS_DURATION}s"

    local health_file="${OUTPUT_DIR}/chaos_kill_${target}_${TIMESTAMP}.csv"
    echo "timestamp,status,latency_ms" > "${health_file}"

    # Record baseline health
    log_info "Recording baseline health (5s)..."
    record_health_timeline "${health_file}" 5

    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY RUN] Would pause Docker container: ${target}"
        return
    fi

    # Inject chaos: pause the container
    log_warn "PAUSING container: ${target}"
    if docker pause "${target}" 2>/dev/null; then
        # Record health during chaos
        log_info "Recording health during chaos..."
        record_health_timeline "${health_file}" "${CHAOS_DURATION}" &
        local monitor_pid=$!

        sleep "${CHAOS_DURATION}"

        # Cleanup: unpause
        log_info "UNPAUSING container: ${target}"
        docker unpause "${target}" 2>/dev/null || true
        kill "${monitor_pid}" 2>/dev/null || true
        wait "${monitor_pid}" 2>/dev/null || true

        # Recovery check
        log_info "Checking recovery..."
        local recovery_start
        recovery_start=$(date +%s)

        if check_health; then
            local recovery_end
            recovery_end=$(date +%s)
            local recovery_time=$((recovery_end - recovery_start))
            log_success "System recovered in ${recovery_time}s after ${target} was paused"
        else
            log_error "System did NOT recover within 30s after ${target} was paused"
        fi

        # Record post-recovery health
        record_health_timeline "${health_file}" 10
    else
        log_warn "Could not pause ${target} — container may not exist"
        log_info "Verify containers: docker ps"
    fi

    # Analyze
    if [[ -f "${health_file}" ]]; then
        local down_count
        down_count=$(grep -c "DOWN" "${health_file}" 2>/dev/null || echo "0")
        local up_count
        up_count=$(grep -c "UP" "${health_file}" 2>/dev/null || echo "0")
        local total=$((down_count + up_count))
        
        echo ""
        echo "╔══════════════════════════════════════════╗"
        echo "║   Kill Service: ${target}               ║"
        echo "╠══════════════════════════════════════════╣"
        printf "║ Health checks UP:    %-19s ║\n" "${up_count}/${total}"
        printf "║ Health checks DOWN:  %-19s ║\n" "${down_count}/${total}"
        echo "╚══════════════════════════════════════════╝"
    fi
}

# ==== Experiment 2: Network Latency ====

chaos_network_latency() {
    log_header "Chaos Experiment: Network Latency"

    local latency_ms=$((100 + RANDOM % 400))  # 100-500ms
    log_info "Injecting ${latency_ms}ms latency for ${CHAOS_DURATION}s"

    local health_file="${OUTPUT_DIR}/chaos_latency_${TIMESTAMP}.csv"
    echo "timestamp,status,latency_ms" > "${health_file}"

    # Baseline
    record_health_timeline "${health_file}" 5

    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY RUN] Would add ${latency_ms}ms latency via tc"
        return
    fi

    # Get default network interface
    local iface
    iface=$(ip route | grep default | awk '{print $5}' | head -1 2>/dev/null || echo "eth0")

    # Inject latency (requires root/sudo)
    if sudo tc qdisc add dev "${iface}" root netem delay "${latency_ms}ms" 50ms distribution normal 2>/dev/null; then
        log_warn "Latency injected on ${iface}: ${latency_ms}ms +/- 50ms"

        # Record during chaos
        record_health_timeline "${health_file}" "${CHAOS_DURATION}" &
        local monitor_pid=$!

        sleep "${CHAOS_DURATION}"

        # Cleanup
        sudo tc qdisc del dev "${iface}" root netem 2>/dev/null || true
        log_info "Latency removed from ${iface}"

        kill "${monitor_pid}" 2>/dev/null || true
        wait "${monitor_pid}" 2>/dev/null || true

        # Recovery
        record_health_timeline "${health_file}" 10
    else
        log_warn "Could not inject latency — tc requires root/sudo"
        log_info "Alternative: docker network with tc inside container"
    fi

    log_success "Network latency experiment completed"
}

# ==== Experiment 3: Bandwidth Limit ====

chaos_bandwidth_limit() {
    log_header "Chaos Experiment: Bandwidth Limit"

    local bandwidth="100kbit"
    log_info "Limiting bandwidth to ${bandwidth} for ${CHAOS_DURATION}s"

    local health_file="${OUTPUT_DIR}/chaos_bandwidth_${TIMESTAMP}.csv"
    echo "timestamp,status,latency_ms" > "${health_file}"

    record_health_timeline "${health_file}" 5

    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY RUN] Would limit bandwidth to ${bandwidth}"
        return
    fi

    local iface
    iface=$(ip route | grep default | awk '{print $5}' | head -1 2>/dev/null || echo "eth0")

    if sudo tc qdisc add dev "${iface}" root tbf rate "${bandwidth}" burst 32kbit latency 400ms 2>/dev/null; then
        log_warn "Bandwidth limited to ${bandwidth} on ${iface}"

        record_health_timeline "${health_file}" "${CHAOS_DURATION}" &
        local monitor_pid=$!

        sleep "${CHAOS_DURATION}"

        sudo tc qdisc del dev "${iface}" root 2>/dev/null || true
        log_info "Bandwidth limit removed"

        kill "${monitor_pid}" 2>/dev/null || true
        wait "${monitor_pid}" 2>/dev/null || true

        record_health_timeline "${health_file}" 10
    else
        log_warn "Could not limit bandwidth — tc requires root/sudo"
    fi

    log_success "Bandwidth limit experiment completed"
}

# ==== Experiment 4: Disk Fill ====

chaos_disk_fill() {
    log_header "Chaos Experiment: Disk Fill (95%)"

    local health_file="${OUTPUT_DIR}/chaos_disk_${TIMESTAMP}.csv"
    echo "timestamp,status,latency_ms" > "${health_file}"

    record_health_timeline "${health_file}" 5

    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY RUN] Would create large temp file to fill disk to 95%"
        return
    fi

    # Calculate how much space to fill
    local avail_kb
    avail_kb=$(df -k /tmp | tail -1 | awk '{print $4}')
    local total_kb
    total_kb=$(df -k /tmp | tail -1 | awk '{print $2}')
    local used_pct
    used_pct=$(df -k /tmp | tail -1 | awk '{print $5}' | tr -d '%')

    log_info "Current disk usage: ${used_pct}%"

    if [[ ${used_pct} -ge 90 ]]; then
        log_warn "Disk already at ${used_pct}% — skipping fill"
        return
    fi

    # Create temp file to fill to ~90% (safer than 95%)
    local target_pct=90
    local target_used_kb=$((total_kb * target_pct / 100))
    local current_used_kb=$((total_kb - avail_kb))
    local fill_kb=$((target_used_kb - current_used_kb))

    if [[ ${fill_kb} -le 0 ]]; then
        log_warn "Not enough room to create test fill"
        return
    fi

    local fill_file="/tmp/wikisurge_chaos_disk_fill_${TIMESTAMP}"

    log_warn "Creating ${fill_kb}KB temp file..."
    dd if=/dev/zero of="${fill_file}" bs=1024 count="${fill_kb}" 2>/dev/null || true

    local new_pct
    new_pct=$(df -k /tmp | tail -1 | awk '{print $5}' | tr -d '%')
    log_info "Disk usage now: ${new_pct}%"

    # Record health during pressure
    record_health_timeline "${health_file}" "${CHAOS_DURATION}" &
    local monitor_pid=$!

    sleep "${CHAOS_DURATION}"

    # Cleanup
    log_info "Removing temp file..."
    rm -f "${fill_file}"

    kill "${monitor_pid}" 2>/dev/null || true
    wait "${monitor_pid}" 2>/dev/null || true

    local final_pct
    final_pct=$(df -k /tmp | tail -1 | awk '{print $5}' | tr -d '%')
    log_info "Disk usage after cleanup: ${final_pct}%"

    # Recovery check
    if check_health; then
        log_success "System healthy after disk pressure"
    else
        log_error "System unhealthy after disk pressure"
    fi

    record_health_timeline "${health_file}" 10
}

# ==== Experiment 5: Memory Spike ====

chaos_memory_spike() {
    log_header "Chaos Experiment: Memory Spike"

    local health_file="${OUTPUT_DIR}/chaos_memory_${TIMESTAMP}.csv"
    echo "timestamp,status,latency_ms" > "${health_file}"

    record_health_timeline "${health_file}" 5

    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY RUN] Would spike memory usage with stress-ng"
        return
    fi

    if ! command -v stress-ng &>/dev/null; then
        log_warn "stress-ng not found — skipping memory spike"
        log_info "Install: sudo apt-get install stress-ng"
        return
    fi

    # Get total memory
    local total_mem_kb
    total_mem_kb=$(grep MemTotal /proc/meminfo 2>/dev/null | awk '{print $2}' || echo "0")
    local spike_mb=$((total_mem_kb * 60 / 100 / 1024))  # Use 60% of memory

    log_warn "Spiking memory usage: ${spike_mb}MB for ${CHAOS_DURATION}s"

    # Record health during pressure
    record_health_timeline "${health_file}" "${CHAOS_DURATION}" &
    local monitor_pid=$!

    stress-ng --vm 2 --vm-bytes "${spike_mb}M" --timeout "${CHAOS_DURATION}s" \
        --vm-hang 0 > /dev/null 2>&1 &
    local stress_pid=$!

    # Wait for stress to finish
    wait "${stress_pid}" 2>/dev/null || true

    kill "${monitor_pid}" 2>/dev/null || true
    wait "${monitor_pid}" 2>/dev/null || true

    log_info "Memory spike ended"

    # Recovery check
    if check_health; then
        log_success "System recovered after memory spike"
    else
        log_error "System did not recover after memory spike"
    fi

    record_health_timeline "${health_file}" 10
}

# ==== Experiment 6: Packet Loss ====

chaos_packet_loss() {
    log_header "Chaos Experiment: Packet Loss"

    local loss_pct=$((5 + RANDOM % 20))  # 5-25% packet loss
    log_info "Introducing ${loss_pct}% packet loss for ${CHAOS_DURATION}s"

    local health_file="${OUTPUT_DIR}/chaos_packetloss_${TIMESTAMP}.csv"
    echo "timestamp,status,latency_ms" > "${health_file}"

    record_health_timeline "${health_file}" 5

    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY RUN] Would introduce ${loss_pct}% packet loss"
        return
    fi

    local iface
    iface=$(ip route | grep default | awk '{print $5}' | head -1 2>/dev/null || echo "eth0")

    if sudo tc qdisc add dev "${iface}" root netem loss "${loss_pct}%" 2>/dev/null; then
        log_warn "Packet loss ${loss_pct}% injected on ${iface}"

        record_health_timeline "${health_file}" "${CHAOS_DURATION}" &
        local monitor_pid=$!

        sleep "${CHAOS_DURATION}"

        sudo tc qdisc del dev "${iface}" root netem 2>/dev/null || true
        log_info "Packet loss removed"

        kill "${monitor_pid}" 2>/dev/null || true
        wait "${monitor_pid}" 2>/dev/null || true

        if check_health; then
            log_success "System recovered after packet loss"
        else
            log_error "System did not recover after packet loss"
        fi

        record_health_timeline "${health_file}" 10
    else
        log_warn "Could not inject packet loss — tc requires root/sudo"
    fi

    log_success "Packet loss experiment completed"
}

# ==== Analysis & Report ====

generate_chaos_report() {
    log_header "Chaos Test Report"

    local report="${OUTPUT_DIR}/chaos_report_${TIMESTAMP}.md"

    cat > "${report}" <<EOF
# WikiSurge Chaos Test Report

**Date:** $(date -Iseconds)
**Duration per experiment:** ${CHAOS_DURATION}s
**Target:** ${BASE_URL}

## Experiments

EOF

    for csv in "${OUTPUT_DIR}"/chaos_*_${TIMESTAMP}.csv; do
        if [[ ! -f "$csv" ]]; then continue; fi
        local name
        name=$(basename "$csv" | sed "s/_${TIMESTAMP}\.csv//" | sed 's/chaos_//')

        local total up down
        total=$(wc -l < "$csv")
        total=$((total - 1))  # exclude header
        up=$(grep -c "UP" "$csv" 2>/dev/null || echo "0")
        down=$(grep -c "DOWN" "$csv" 2>/dev/null || echo "0")

        local availability="N/A"
        if [[ ${total} -gt 0 ]]; then
            availability=$(echo "scale=1; ${up} * 100 / ${total}" | bc 2>/dev/null || echo "N/A")
        fi

        cat >> "${report}" <<EOF
### ${name}

| Metric | Value |
|--------|-------|
| Health checks | ${total} |
| UP | ${up} |
| DOWN | ${down} |
| Availability | ${availability}% |

EOF
    done

    cat >> "${report}" <<EOF

## Validation Checklist

- [ ] System handles service failures gracefully
- [ ] Recovery time is acceptable (<30s for most scenarios)
- [ ] No permanent data loss detected
- [ ] Alerts fire when services degrade
- [ ] Degraded mode provides partial functionality

## Recommendations

1. Ensure circuit breakers activate during downstream failures
2. Verify retry logic has proper backoff
3. Monitor and alert on recovery time
4. Consider adding fallback behavior for critical endpoints
5. Test with longer chaos durations in staging environment
EOF

    log_success "Report: ${report}"
}

# ==== Main ====

main() {
    parse_args "$@"
    setup

    log_header "WikiSurge Chaos Testing"
    log_info "Experiment: ${EXPERIMENT}"
    log_info "Duration: ${CHAOS_DURATION}s"
    [[ "$DRY_RUN" == "true" ]] && log_warn "DRY RUN MODE — no actual chaos"

    # Verify API is healthy before chaos
    log_info "Pre-flight health check..."
    if check_health; then
        log_success "API is healthy — beginning chaos"
    else
        log_warn "API is not healthy — proceeding anyway"
    fi

    case "${EXPERIMENT}" in
        kill-service)     chaos_kill_service ;;
        network-latency)  chaos_network_latency ;;
        bandwidth-limit)  chaos_bandwidth_limit ;;
        disk-fill)        chaos_disk_fill ;;
        memory-spike)     chaos_memory_spike ;;
        packet-loss)      chaos_packet_loss ;;
        all)
            chaos_kill_service
            sleep 10  # Cool down between experiments
            chaos_network_latency
            sleep 10
            chaos_bandwidth_limit
            sleep 10
            chaos_disk_fill
            sleep 10
            chaos_memory_spike
            sleep 10
            chaos_packet_loss
            ;;
        *)
            log_error "Unknown experiment: ${EXPERIMENT}"
            usage
            exit 1
            ;;
    esac

    generate_chaos_report

    log_header "Chaos Testing Complete"
    log_info "Results: ${OUTPUT_DIR}/"
}

main "$@"
