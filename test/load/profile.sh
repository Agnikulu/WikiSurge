#!/bin/bash

# =============================================================================
# WikiSurge Profiling & Bottleneck Analysis Script
# =============================================================================
# Runs Go profiling tools and generates reports for CPU, memory, goroutine,
# and block profiles. Also covers frontend profiling guidance.
#
# Usage:
#   ./test/load/profile.sh [OPTIONS]
# =============================================================================

set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUTPUT_DIR="${PROJECT_ROOT}/test/load/results/profiles"
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
WikiSurge Profiling & Bottleneck Analysis

USAGE:
    ./test/load/profile.sh [OPTIONS]

OPTIONS:
    --cpu              Run CPU profiling on Go tests
    --mem              Run memory profiling on Go tests
    --goroutine        Capture goroutine profile from running API
    --block            Run block profiling on Go tests
    --all              Run all profiles (default)
    --port PORT        API pprof port (default: 6060)
    --bench-time SECS  Benchmark duration (default: 30s)
    -o, --output DIR   Output directory
    --help             Show this help message

PROFILING TARGETS:
    - internal/api: API handler hot paths
    - internal/processor: Edit processing pipeline
    - internal/storage: Redis/ES operations
    - test/benchmark: Dedicated benchmarks
EOF
}

RUN_CPU=false
RUN_MEM=false
RUN_GOROUTINE=false
RUN_BLOCK=false
RUN_ALL=true
PPROF_PORT=6060
BENCH_TIME="30s"

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --cpu)        RUN_CPU=true; RUN_ALL=false; shift ;;
            --mem)        RUN_MEM=true; RUN_ALL=false; shift ;;
            --goroutine)  RUN_GOROUTINE=true; RUN_ALL=false; shift ;;
            --block)      RUN_BLOCK=true; RUN_ALL=false; shift ;;
            --all)        RUN_ALL=true; shift ;;
            --port)       PPROF_PORT="$2"; shift 2 ;;
            --bench-time) BENCH_TIME="$2"; shift 2 ;;
            -o|--output)  OUTPUT_DIR="$2"; shift 2 ;;
            --help)       usage; exit 0 ;;
            *)            log_error "Unknown: $1"; exit 1 ;;
        esac
    done

    if [[ "$RUN_ALL" == "true" ]]; then
        RUN_CPU=true
        RUN_MEM=true
        RUN_GOROUTINE=true
        RUN_BLOCK=true
    fi
}

setup() {
    mkdir -p "${OUTPUT_DIR}"
    cd "${PROJECT_ROOT}"
    log_info "Project root: ${PROJECT_ROOT}"
    log_info "Output: ${OUTPUT_DIR}"
}

# ==== CPU Profiling ====

profile_cpu() {
    log_header "CPU Profiling"

    local packages=(
        "./internal/api/..."
        "./internal/processor/..."
        "./internal/storage/..."
        "./test/benchmark/..."
    )

    for pkg in "${packages[@]}"; do
        local pkg_name
        pkg_name=$(echo "$pkg" | sed 's|./||;s|/\.\.\.||;s|/|_|g')
        local profile="${OUTPUT_DIR}/cpu_${pkg_name}_${TIMESTAMP}.prof"
        local report="${OUTPUT_DIR}/cpu_${pkg_name}_${TIMESTAMP}.txt"

        log_info "CPU profiling: ${pkg}"

        if go test -cpuprofile="${profile}" -benchtime="${BENCH_TIME}" \
            -bench=. -run='^$' "${pkg}" > "${report}" 2>&1; then
            log_success "CPU profile saved: ${profile}"

            # Generate text report
            if [[ -f "${profile}" ]]; then
                go tool pprof -text "${profile}" > "${OUTPUT_DIR}/cpu_${pkg_name}_${TIMESTAMP}_top.txt" 2>&1 || true
                log_info "Top CPU consumers for ${pkg_name}:"
                head -20 "${OUTPUT_DIR}/cpu_${pkg_name}_${TIMESTAMP}_top.txt" 2>/dev/null || true

                # Generate SVG flame graph if graphviz available
                if command -v dot &>/dev/null; then
                    go tool pprof -svg "${profile}" > "${OUTPUT_DIR}/cpu_${pkg_name}_${TIMESTAMP}.svg" 2>&1 || true
                    log_info "Flame graph SVG saved"
                fi
            fi
        else
            log_warn "CPU profiling failed for ${pkg} (may have no benchmarks)"
            cat "${report}" 2>/dev/null | tail -5 || true
        fi
    done
}

# ==== Memory Profiling ====

profile_memory() {
    log_header "Memory Profiling"

    local packages=(
        "./internal/api/..."
        "./internal/processor/..."
        "./internal/storage/..."
        "./test/benchmark/..."
    )

    for pkg in "${packages[@]}"; do
        local pkg_name
        pkg_name=$(echo "$pkg" | sed 's|./||;s|/\.\.\.||;s|/|_|g')
        local profile="${OUTPUT_DIR}/mem_${pkg_name}_${TIMESTAMP}.prof"
        local report="${OUTPUT_DIR}/mem_${pkg_name}_${TIMESTAMP}.txt"

        log_info "Memory profiling: ${pkg}"

        if go test -memprofile="${profile}" -benchmem -benchtime="${BENCH_TIME}" \
            -bench=. -run='^$' "${pkg}" > "${report}" 2>&1; then
            log_success "Memory profile saved: ${profile}"

            if [[ -f "${profile}" ]]; then
                # Allocation report
                go tool pprof -text -alloc_space "${profile}" > \
                    "${OUTPUT_DIR}/mem_${pkg_name}_${TIMESTAMP}_allocs.txt" 2>&1 || true
                log_info "Top memory allocators for ${pkg_name}:"
                head -20 "${OUTPUT_DIR}/mem_${pkg_name}_${TIMESTAMP}_allocs.txt" 2>/dev/null || true

                # In-use memory
                go tool pprof -text -inuse_space "${profile}" > \
                    "${OUTPUT_DIR}/mem_${pkg_name}_${TIMESTAMP}_inuse.txt" 2>&1 || true
            fi

            # Show allocation stats from benchmark
            log_info "Benchmark allocation stats:"
            grep -E "allocs/op|B/op" "${report}" 2>/dev/null | head -10 || true
        else
            log_warn "Memory profiling failed for ${pkg}"
        fi
    done
}

# ==== Goroutine Profiling ====

profile_goroutines() {
    log_header "Goroutine Profiling"

    local pprof_url="http://localhost:${PPROF_PORT}/debug/pprof"

    # Check if pprof endpoint is available
    if curl -sf "${pprof_url}/goroutine?debug=1" > /dev/null 2>&1; then
        log_success "pprof endpoint available at ${pprof_url}"

        # Goroutine dump
        log_info "Capturing goroutine profile..."
        curl -sf "${pprof_url}/goroutine?debug=2" > \
            "${OUTPUT_DIR}/goroutine_dump_${TIMESTAMP}.txt" 2>/dev/null || true
        
        local goroutine_count
        goroutine_count=$(curl -sf "${pprof_url}/goroutine?debug=1" 2>/dev/null | \
            head -1 | grep -oP '\d+' || echo "N/A")
        log_info "Active goroutines: ${goroutine_count}"

        # Download binary profile for pprof analysis
        curl -sf "${pprof_url}/goroutine" > \
            "${OUTPUT_DIR}/goroutine_${TIMESTAMP}.prof" 2>/dev/null || true

        if [[ -f "${OUTPUT_DIR}/goroutine_${TIMESTAMP}.prof" ]]; then
            go tool pprof -text "${OUTPUT_DIR}/goroutine_${TIMESTAMP}.prof" > \
                "${OUTPUT_DIR}/goroutine_${TIMESTAMP}_top.txt" 2>&1 || true
            log_info "Top goroutine stacks:"
            head -30 "${OUTPUT_DIR}/goroutine_${TIMESTAMP}_top.txt" 2>/dev/null || true
        fi

        # Check for potential goroutine leaks
        log_info "Checking for goroutine leaks..."
        sleep 10
        local goroutine_count2
        goroutine_count2=$(curl -sf "${pprof_url}/goroutine?debug=1" 2>/dev/null | \
            head -1 | grep -oP '\d+' || echo "N/A")
        log_info "Goroutines after 10s: ${goroutine_count2} (was: ${goroutine_count})"

        if [[ "$goroutine_count" != "N/A" && "$goroutine_count2" != "N/A" ]]; then
            local diff=$((goroutine_count2 - goroutine_count))
            if [[ $diff -le 5 ]]; then
                log_success "No goroutine leak detected (diff: ${diff})"
            else
                log_warn "Possible goroutine leak: ${diff} new goroutines in 10s"
            fi
        fi

        # Heap profile
        log_info "Capturing heap profile..."
        curl -sf "${pprof_url}/heap" > \
            "${OUTPUT_DIR}/heap_${TIMESTAMP}.prof" 2>/dev/null || true

        if [[ -f "${OUTPUT_DIR}/heap_${TIMESTAMP}.prof" ]]; then
            go tool pprof -text -inuse_space "${OUTPUT_DIR}/heap_${TIMESTAMP}.prof" > \
                "${OUTPUT_DIR}/heap_${TIMESTAMP}_inuse.txt" 2>&1 || true
            log_info "Heap in-use:"
            head -15 "${OUTPUT_DIR}/heap_${TIMESTAMP}_inuse.txt" 2>/dev/null || true
        fi

    else
        log_warn "pprof endpoint not available at ${pprof_url}"
        log_info "To enable: import _ \"net/http/pprof\" and start debug server on port ${PPROF_PORT}"
        log_info "Running goroutine tests via go test instead..."

        # Run tests that detect goroutine leaks
        go test -v -run="TestGoroutine|TestLeak" ./internal/... 2>&1 | tail -20 || true
    fi
}

# ==== Block Profiling ====

profile_block() {
    log_header "Block Profiling (Contention)"

    local packages=(
        "./internal/api/..."
        "./internal/processor/..."
        "./internal/storage/..."
    )

    for pkg in "${packages[@]}"; do
        local pkg_name
        pkg_name=$(echo "$pkg" | sed 's|./||;s|/\.\.\.||;s|/|_|g')
        local profile="${OUTPUT_DIR}/block_${pkg_name}_${TIMESTAMP}.prof"
        local report="${OUTPUT_DIR}/block_${pkg_name}_${TIMESTAMP}.txt"

        log_info "Block profiling: ${pkg}"

        if go test -blockprofile="${profile}" -benchtime="${BENCH_TIME}" \
            -bench=. -run='^$' "${pkg}" > "${report}" 2>&1; then
            log_success "Block profile saved: ${profile}"

            if [[ -f "${profile}" ]]; then
                go tool pprof -text "${profile}" > \
                    "${OUTPUT_DIR}/block_${pkg_name}_${TIMESTAMP}_top.txt" 2>&1 || true
                log_info "Top contention points for ${pkg_name}:"
                head -15 "${OUTPUT_DIR}/block_${pkg_name}_${TIMESTAMP}_top.txt" 2>/dev/null || true
            fi
        else
            log_warn "Block profiling failed for ${pkg}"
        fi
    done

    # Also check for mutex contention from pprof if available
    local pprof_url="http://localhost:${PPROF_PORT}/debug/pprof"
    if curl -sf "${pprof_url}/mutex" > /dev/null 2>&1; then
        curl -sf "${pprof_url}/mutex" > \
            "${OUTPUT_DIR}/mutex_${TIMESTAMP}.prof" 2>/dev/null || true
        log_info "Mutex profile captured from runtime"
    fi
}

# ==== Frontend Profiling Guide ====

print_frontend_profiling_guide() {
    log_header "Frontend Profiling Guide"

    cat <<'EOF'
Frontend profiling should be done manually using browser tools:

1. Chrome DevTools Performance:
   - Open DevTools (F12) > Performance tab
   - Click Record, interact with the app, click Stop
   - Analyze: Main thread activity, Long tasks, Layout shifts
   - Target: No tasks > 100ms, FCP < 1.5s, LCP < 2.5s

2. React DevTools Profiler:
   - Install React DevTools browser extension
   - Open React DevTools > Profiler tab
   - Record a session, look for:
     - Components with frequent re-renders
     - Slow commits (>16ms)
     - Unnecessary renders

3. Bundle Size Analysis:
   Run from web/ directory:
     npx vite-bundle-visualizer
   Or:
     npm run build -- --report

   Check:
   - Total bundle size (target: <500KB gzipped)
   - Largest chunks
   - Unused dependencies

4. Lighthouse Audit:
   - DevTools > Lighthouse tab
   - Run audit for Performance
   - Target score: >90

5. Key Optimizations:
   - Code splitting with React.lazy() and Suspense
   - Memoize expensive components with React.memo()
   - Use useMemo/useCallback for expensive computations
   - Virtual scrolling for long lists (react-window)
   - Image optimization (WebP, lazy loading)
   - Service worker for caching static assets
EOF
}

# ==== Analysis Summary ====

generate_analysis_summary() {
    log_header "Profiling Analysis Summary"

    local summary="${OUTPUT_DIR}/profiling_summary_${TIMESTAMP}.md"

    cat > "${summary}" <<EOF
# WikiSurge Profiling Summary

**Date:** $(date -Iseconds)

## Profiles Generated

### CPU Profiles
$(ls -la "${OUTPUT_DIR}"/cpu_*${TIMESTAMP}*.prof 2>/dev/null | awk '{print "- " $NF}' || echo "- None generated")

### Memory Profiles
$(ls -la "${OUTPUT_DIR}"/mem_*${TIMESTAMP}*.prof 2>/dev/null | awk '{print "- " $NF}' || echo "- None generated")

### Goroutine Profiles
$(ls -la "${OUTPUT_DIR}"/goroutine_*${TIMESTAMP}*.prof 2>/dev/null | awk '{print "- " $NF}' || echo "- None generated")

### Block Profiles
$(ls -la "${OUTPUT_DIR}"/block_*${TIMESTAMP}*.prof 2>/dev/null | awk '{print "- " $NF}' || echo "- None generated")

## How to Analyze

### Interactive Analysis
\`\`\`bash
# CPU
go tool pprof -http=:8082 <profile>.prof

# Top functions
go tool pprof -top <profile>.prof

# Flame graph (requires graphviz)
go tool pprof -svg <profile>.prof > flame.svg
\`\`\`

### Key Things to Look For
1. **CPU Hot Paths**: Functions consuming >10% of CPU time
2. **Memory Allocations**: Frequent small allocations in hot loops
3. **Goroutine Leaks**: Goroutine count growing over time
4. **Lock Contention**: Long block/mutex wait times
5. **JSON Marshaling**: Often a hidden CPU cost

## Recommendations

Based on common patterns in Go HTTP services:
- Use \`sync.Pool\` for frequently allocated objects
- Pre-allocate slices when size is known
- Use \`json.Encoder\` instead of \`json.Marshal\` for HTTP responses
- Batch Redis commands using pipelines
- Cache hot data at the application level
- Use connection pooling for all external services
EOF

    log_success "Summary saved: ${summary}"
}

# ==== Main ====

main() {
    parse_args "$@"
    setup

    log_header "WikiSurge Profiling & Bottleneck Analysis"
    log_info "Timestamp: ${TIMESTAMP}"

    [[ "$RUN_CPU" == "true" ]] && profile_cpu
    [[ "$RUN_MEM" == "true" ]] && profile_memory
    [[ "$RUN_GOROUTINE" == "true" ]] && profile_goroutines
    [[ "$RUN_BLOCK" == "true" ]] && profile_block

    print_frontend_profiling_guide
    generate_analysis_summary

    log_header "Profiling Complete"
    log_info "Results: ${OUTPUT_DIR}/"
    log_info "Analyze with: go tool pprof -http=:8082 <profile>.prof"
}

main "$@"
