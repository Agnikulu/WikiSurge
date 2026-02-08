#!/bin/bash

# WikiSurge Load Testing Script
# Simulates realistic Wikipedia edit events for testing ingestion pipeline
# Usage: ./simulate_sse.sh [options]

set -e

# Default configuration
DEFAULT_RATE=10
DEFAULT_DURATION=300  # 5 minutes in seconds
DEFAULT_HOST="localhost"
DEFAULT_PORT=2112
DEFAULT_ENDPOINT="/simulate"
DEFAULT_OUTPUT_DIR="./load_test_results"
DEFAULT_SCENARIO="normal"

# Configuration variables
RATE=${DEFAULT_RATE}
DURATION=${DEFAULT_DURATION}
HOST=${DEFAULT_HOST}
PORT=${DEFAULT_PORT}
ENDPOINT=${DEFAULT_ENDPOINT}
OUTPUT_DIR=${DEFAULT_OUTPUT_DIR}
SCENARIO=${DEFAULT_SCENARIO}
VERBOSE=false
DRY_RUN=false

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Print usage information
usage() {
    cat << EOF
WikiSurge Load Testing Script

USAGE:
    ./simulate_sse.sh [OPTIONS]

OPTIONS:
    -r, --rate RATE           Events per second (default: ${DEFAULT_RATE})
    -d, --duration DURATION   Test duration in seconds (default: ${DEFAULT_DURATION})
    -s, --scenario SCENARIO   Test scenario (normal|spike|sustained|bursty) (default: ${DEFAULT_SCENARIO})
    -h, --host HOST          Target host (default: ${DEFAULT_HOST})
    -p, --port PORT          Target port (default: ${DEFAULT_PORT})
    -e, --endpoint ENDPOINT   API endpoint (default: ${DEFAULT_ENDPOINT})
    -o, --output DIR         Output directory for results (default: ${DEFAULT_OUTPUT_DIR})
    -v, --verbose            Enable verbose logging
    --dry-run                Show what would be done without executing
    --help                   Show this help message

SCENARIOS:
    normal      Normal load: constant rate for entire duration
    spike       Spike load: ramp from 5 to 50 eps over 2 minutes
    sustained   Sustained high load: 30 eps for entire duration  
    bursty      Bursty load: alternate between 5 and 50 eps every 30 seconds

EXAMPLES:
    ./simulate_sse.sh --rate=20 --duration=300
    ./simulate_sse.sh --scenario=spike --duration=600
    ./simulate_sse.sh --rate=50 --duration=180 --verbose

EOF
}

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -r|--rate)
                RATE="$2"
                shift 2
                ;;
            -d|--duration) 
                DURATION="$2"
                shift 2
                ;;
            -s|--scenario)
                SCENARIO="$2"
                shift 2
                ;;
            -h|--host)
                HOST="$2"
                shift 2
                ;;
            -p|--port)
                PORT="$2"
                shift 2
                ;;
            -e|--endpoint)
                ENDPOINT="$2"
                shift 2
                ;;
            -o|--output)
                OUTPUT_DIR="$2"
                shift 2
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            --help)
                usage
                exit 0
                ;;
            *)
                echo "Unknown option: $1"
                usage
                exit 1
                ;;
        esac
    done
}

# Logging functions
log() {
    echo -e "${BLUE}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')] SUCCESS:${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[$(date +'%Y-%m-%d %H:%M:%S')] WARNING:${NC} $1"
}

log_error() {
    echo -e "${RED}[$(date +'%Y-%m-%d %H:%M:%S')] ERROR:${NC} $1"
}

log_verbose() {
    if [[ "$VERBOSE" == "true" ]]; then
        echo -e "${BLUE}[$(date +'%Y-%m-%d %H:%M:%S')] VERBOSE:${NC} $1"
    fi
}

# Validate dependencies
check_dependencies() {
    log "Checking dependencies..."
    
    local deps=("curl" "jq" "bc" "ps" "kill")
    for dep in "${deps[@]}"; do
        if ! command -v "$dep" &> /dev/null; then
            log_error "Required dependency '$dep' not found"
            exit 1
        fi
    done
    
    log_success "All dependencies found"
}

# Validate configuration
validate_config() {
    log "Validating configuration..."
    
    # Validate rate
    if ! [[ "$RATE" =~ ^[0-9]+$ ]] || [[ "$RATE" -lt 1 ]] || [[ "$RATE" -gt 1000 ]]; then
        log_error "Rate must be a number between 1 and 1000"
        exit 1
    fi
    
    # Validate duration
    if ! [[ "$DURATION" =~ ^[0-9]+$ ]] || [[ "$DURATION" -lt 10 ]]; then
        log_error "Duration must be a number >= 10 seconds"
        exit 1
    fi
    
    # Validate scenario
    if [[ ! "$SCENARIO" =~ ^(normal|spike|sustained|bursty)$ ]]; then
        log_error "Scenario must be one of: normal, spike, sustained, bursty"
        exit 1
    fi
    
    # Validate host and port
    if ! [[ "$PORT" =~ ^[0-9]+$ ]] || [[ "$PORT" -lt 1 ]] || [[ "$PORT" -gt 65535 ]]; then
        log_error "Port must be a number between 1 and 65535"
        exit 1
    fi
    
    log_success "Configuration validated"
}

# Setup output directory
setup_output_dir() {
    log "Setting up output directory: $OUTPUT_DIR"
    
    if [[ "$DRY_RUN" == "false" ]]; then
        mkdir -p "$OUTPUT_DIR"
        
        # Create subdirectories
        mkdir -p "$OUTPUT_DIR/logs"
        mkdir -p "$OUTPUT_DIR/metrics"
        mkdir -p "$OUTPUT_DIR/results"
    fi
    
    log_success "Output directory ready"
}

# Generate realistic Wikipedia edit event
generate_edit_event() {
    local id=$1
    local timestamp=$(date +%s)
    
    # Arrays of realistic data
    local wikis=("enwiki" "eswiki" "frwiki" "dewiki" "jawiki" "ruwiki" "itwiki" "ptwiki")
    local users=("User1" "EditBot" "AutoBot" "Maintainer" "NewEditor" "ExperiencedEditor" "AdminUser" "ContentBot")
    local titles=("Climate_change" "Artificial_intelligence" "World_War_II" "Mathematics" "Physics" "Biology" "Chemistry" "History" "Geography" "Literature" "Music" "Art" "Sports" "Technology" "Science" "Culture")
    local types=("edit" "edit" "edit" "edit" "new" "log")  # Weighted towards edit
    
    # Select random elements
    local wiki=${wikis[$((RANDOM % ${#wikis[@]}))]}
    local user=${users[$((RANDOM % ${#users[@]}))]}
    local title=${titles[$((RANDOM % ${#titles[@]}))]}
    local edit_type=${types[$((RANDOM % ${#types[@]}))]}
    
    # Determine if bot (30% chance for names with "Bot")
    local is_bot="false"
    if [[ "$user" == *"Bot"* ]]; then
        if (( RANDOM % 10 < 3 )); then
            is_bot="true"
        fi
    fi
    
    # Generate length changes (realistic distribution)
    local old_length=$((RANDOM % 5000 + 100))
    local change=$((RANDOM % 1000 - 500))  # -500 to +500
    local new_length=$((old_length + change))
    if [[ $new_length -lt 0 ]]; then
        new_length=0
    fi
    
    # Generate revision numbers
    local old_rev=$((1000000 + RANDOM % 9000000))
    local new_rev=$((old_rev + 1))
    
    # Create JSON event
    cat << EOF
{
    "id": ${id},
    "type": "${edit_type}",
    "title": "${title}",
    "user": "${user}",
    "bot": ${is_bot},
    "wiki": "${wiki}",
    "server_url": "${wiki:0:2}.wikipedia.org",
    "timestamp": ${timestamp},
    "length": {
        "old": ${old_length},
        "new": ${new_length}
    },
    "revision": {
        "old": ${old_rev},
        "new": ${new_rev}
    },
    "comment": "Load test edit ${id} - ${title}"
}
EOF
}

# Send single event to ingestion endpoint
send_event() {
    local event_json="$1"
    local event_id="$2"
    
    local url="http://${HOST}:${PORT}${ENDPOINT}"
    
    log_verbose "Sending event ${event_id} to ${url}"
    
    if [[ "$DRY_RUN" == "false" ]]; then
        # Send HTTP POST with event data
        local response
        local http_code
        
        response=$(curl -s -w "HTTP_CODE:%{http_code}" \
            -X POST \
            -H "Content-Type: application/json" \
            -d "$event_json" \
            "$url" 2>/dev/null)
        
        http_code=$(echo "$response" | sed -n 's/.*HTTP_CODE:\([0-9]*\)$/\1/p')
        
        if [[ "$http_code" != "200" ]] && [[ "$http_code" != "201" ]] && [[ "$http_code" != "202" ]]; then
            log_warn "Event ${event_id} failed with HTTP ${http_code}"
            return 1
        else
            log_verbose "Event ${event_id} sent successfully (HTTP ${http_code})"
            return 0
        fi
    else
        log_verbose "DRY RUN: Would send event ${event_id}"
        return 0
    fi
}

# Monitor system metrics during test
start_monitoring() {
    local pid_file="$OUTPUT_DIR/monitor.pid"
    
    log "Starting metrics monitoring..."
    
    if [[ "$DRY_RUN" == "false" ]]; then
        {
            while true; do
                local timestamp=$(date +%s)
                
                # Get metrics from the ingestion service
                local metrics_url="http://${HOST}:${PORT}/metrics"
                local metrics_response
                metrics_response=$(curl -s "$metrics_url" 2>/dev/null || echo "")
                
                if [[ -n "$metrics_response" ]]; then
                    # Parse key metrics
                    local ingestion_rate=$(echo "$metrics_response" | grep "edits_ingested_total" | tail -1 | awk '{print $2}' || echo "0")
                    local production_rate=$(echo "$metrics_response" | grep "messages_produced_total" | tail -1 | awk '{print $2}' || echo "0")
                    local error_count=$(echo "$metrics_response" | grep "produce_errors_total" | tail -1 | awk '{print $2}' || echo "0")
                    local dropped_count=$(echo "$metrics_response" | grep "dropped_messages_total" | tail -1 | awk '{print $2}' || echo "0")
                    
                    # Write to metrics file
                    echo "${timestamp},${ingestion_rate},${production_rate},${error_count},${dropped_count}" >> "$OUTPUT_DIR/metrics/metrics.csv"
                fi
                
                # Get system resources
                local cpu_usage=$(top -bn1 | grep "Cpu(s)" | awk '{print $2}' | cut -d'%' -f1 || echo "0")
                local mem_usage=$(free | grep Mem | awk '{printf "%.1f", $3/$2 * 100.0}')
                
                # Write to system metrics file
                echo "${timestamp},${cpu_usage},${mem_usage}" >> "$OUTPUT_DIR/metrics/system.csv"
                
                sleep 5
            done
        } &
        
        local monitor_pid=$!
        echo "$monitor_pid" > "$pid_file"
        
        log_success "Monitoring started with PID ${monitor_pid}"
    fi
}

# Stop monitoring
stop_monitoring() {
    local pid_file="$OUTPUT_DIR/monitor.pid"
    
    if [[ -f "$pid_file" ]]; then
        local monitor_pid
        monitor_pid=$(cat "$pid_file")
        
        if kill -0 "$monitor_pid" 2>/dev/null; then
            log "Stopping monitoring (PID ${monitor_pid})..."
            kill "$monitor_pid" 2>/dev/null || true
            rm -f "$pid_file"
            log_success "Monitoring stopped"
        fi
    fi
}

# Execute normal load scenario
run_normal_scenario() {
    log "Running normal load scenario: ${RATE} eps for ${DURATION} seconds"
    
    local interval
    interval=$(echo "scale=3; 1.0 / $RATE" | bc)
    
    local event_id=1
    local start_time=$(date +%s)
    local end_time=$((start_time + DURATION))
    local success_count=0
    local error_count=0
    
    # Initialize metrics file
    if [[ "$DRY_RUN" == "false" ]]; then
        echo "timestamp,event_id,success,latency_ms" > "$OUTPUT_DIR/results/events.csv"
        echo "timestamp,ingestion_rate,production_rate,error_count,dropped_count" > "$OUTPUT_DIR/metrics/metrics.csv"
        echo "timestamp,cpu_usage,memory_usage" > "$OUTPUT_DIR/metrics/system.csv"
    fi
    
    while [[ $(date +%s) -lt $end_time ]]; do
        local event_start=$(date +%s%3N)
        
        # Generate and send event
        local event_json
        event_json=$(generate_edit_event "$event_id")
        
        local success=0
        if send_event "$event_json" "$event_id"; then
            success=1
            ((success_count++))
        else
            ((error_count++))
        fi
        
        local event_end=$(date +%s%3N)
        local latency=$((event_end - event_start))
        
        # Log event result
        if [[ "$DRY_RUN" == "false" ]]; then
            echo "$(date +%s),${event_id},${success},${latency}" >> "$OUTPUT_DIR/results/events.csv"
        fi
        
        # Progress update every 100 events
        if (( event_id % 100 == 0 )); then
            local elapsed=$(($(date +%s) - start_time))
            local remaining=$((end_time - $(date +%s)))
            log "Progress: ${event_id} events sent, ${elapsed}s elapsed, ${remaining}s remaining (Success: ${success_count}, Errors: ${error_count})"
        fi
        
        ((event_id++))
        
        # Wait for next interval
        sleep "$interval" 2>/dev/null || true
    done
    
    log_success "Normal scenario completed: ${success_count} successful, ${error_count} failed"
}

# Execute spike load scenario
run_spike_scenario() {
    log "Running spike load scenario: ramp from 5 to 50 eps over ${DURATION} seconds"
    
    local start_rate=5
    local end_rate=50
    local ramp_duration=$DURATION
    
    local event_id=1
    local start_time=$(date +%s)
    local end_time=$((start_time + DURATION))
    local success_count=0
    local error_count=0
    
    # Initialize metrics file
    if [[ "$DRY_RUN" == "false" ]]; then
        echo "timestamp,event_id,success,latency_ms,current_rate" > "$OUTPUT_DIR/results/events.csv"
        echo "timestamp,ingestion_rate,production_rate,error_count,dropped_count" > "$OUTPUT_DIR/metrics/metrics.csv"
        echo "timestamp,cpu_usage,memory_usage" > "$OUTPUT_DIR/metrics/system.csv"
    fi
    
    while [[ $(date +%s) -lt $end_time ]]; do
        local current_time=$(date +%s)
        local elapsed=$((current_time - start_time))
        local progress
        progress=$(echo "scale=3; $elapsed / $ramp_duration" | bc)
        
        # Calculate current rate (linear ramp)
        local current_rate
        current_rate=$(echo "scale=0; $start_rate + ($end_rate - $start_rate) * $progress" | bc)
        
        # Ensure rate is at least 1
        if [[ "$current_rate" -lt 1 ]]; then
            current_rate=1
        fi
        
        local interval
        interval=$(echo "scale=3; 1.0 / $current_rate" | bc)
        
        local event_start=$(date +%s%3N)
        
        # Generate and send event
        local event_json
        event_json=$(generate_edit_event "$event_id")
        
        local success=0
        if send_event "$event_json" "$event_id"; then
            success=1
            ((success_count++))
        else
            ((error_count++))
        fi
        
        local event_end=$(date +%s%3N)
        local latency=$((event_end - event_start))
        
        # Log event result
        if [[ "$DRY_RUN" == "false" ]]; then
            echo "$(date +%s),${event_id},${success},${latency},${current_rate}" >> "$OUTPUT_DIR/results/events.csv"
        fi
        
        # Progress update every 50 events
        if (( event_id % 50 == 0 )); then
            log "Progress: ${event_id} events sent, current rate: ${current_rate} eps (Success: ${success_count}, Errors: ${error_count})"
        fi
        
        ((event_id++))
        
        # Wait for next interval
        sleep "$interval" 2>/dev/null || true
    done
    
    log_success "Spike scenario completed: ${success_count} successful, ${error_count} failed"
}

# Execute bursty load scenario  
run_bursty_scenario() {
    log "Running bursty load scenario: alternate between 5 and 50 eps every 30 seconds for ${DURATION} seconds"
    
    local low_rate=5
    local high_rate=50
    local burst_interval=30
    
    local event_id=1
    local start_time=$(date +%s)
    local end_time=$((start_time + DURATION))
    local success_count=0
    local error_count=0
    
    # Initialize metrics file
    if [[ "$DRY_RUN" == "false" ]]; then
        echo "timestamp,event_id,success,latency_ms,current_rate" > "$OUTPUT_DIR/results/events.csv"
        echo "timestamp,ingestion_rate,production_rate,error_count,dropped_count" > "$OUTPUT_DIR/metrics/metrics.csv"
        echo "timestamp,cpu_usage,memory_usage" > "$OUTPUT_DIR/metrics/system.csv"
    fi
    
    while [[ $(date +%s) -lt $end_time ]]; do
        local current_time=$(date +%s)
        local elapsed=$((current_time - start_time))
        
        # Determine if we're in high or low burst period  
        local cycle_position=$((elapsed % (burst_interval * 2)))
        local current_rate
        if [[ $cycle_position -lt $burst_interval ]]; then
            current_rate=$low_rate
        else
            current_rate=$high_rate
        fi
        
        local interval
        interval=$(echo "scale=3; 1.0 / $current_rate" | bc)
        
        local event_start=$(date +%s%3N)
        
        # Generate and send event
        local event_json
        event_json=$(generate_edit_event "$event_id")
        
        local success=0
        if send_event "$event_json" "$event_id"; then
            success=1
            ((success_count++))
        else
            ((error_count++))
        fi
        
        local event_end=$(date +%s%3N)
        local latency=$((event_end - event_start))
        
        # Log event result
        if [[ "$DRY_RUN" == "false" ]]; then
            echo "$(date +%s),${event_id},${success},${latency},${current_rate}" >> "$OUTPUT_DIR/results/events.csv"
        fi
        
        # Progress update every 50 events
        if (( event_id % 50 == 0 )); then
            log "Progress: ${event_id} events sent, current rate: ${current_rate} eps (Success: ${success_count}, Errors: ${error_count})"
        fi
        
        ((event_id++))
        
        # Wait for next interval
        sleep "$interval" 2>/dev/null || true
    done
    
    log_success "Bursty scenario completed: ${success_count} successful, ${error_count} failed"
}

# Generate test report
generate_report() {
    log "Generating test report..."
    
    local report_file="$OUTPUT_DIR/test_report.txt"
    
    if [[ "$DRY_RUN" == "false" ]]; then
        {
            echo "WikiSurge Load Test Report"
            echo "========================="
            echo "Test Date: $(date)"
            echo "Scenario: $SCENARIO"
            echo "Rate: $RATE eps"
            echo "Duration: $DURATION seconds"
            echo "Target: http://$HOST:$PORT$ENDPOINT"
            echo ""
            
            if [[ -f "$OUTPUT_DIR/results/events.csv" ]]; then
                local total_events
                total_events=$(tail -n +2 "$OUTPUT_DIR/results/events.csv" | wc -l)
                local successful_events
                successful_events=$(tail -n +2 "$OUTPUT_DIR/results/events.csv" | awk -F',' '$3==1' | wc -l)
                local failed_events
                failed_events=$(tail -n +2 "$OUTPUT_DIR/results/events.csv" | awk -F',' '$3==0' | wc -l)
                local success_rate
                success_rate=$(echo "scale=2; $successful_events * 100.0 / $total_events" | bc)
                
                echo "Event Statistics:"
                echo "  Total Events: $total_events"
                echo "  Successful: $successful_events"
                echo "  Failed: $failed_events"
                echo "  Success Rate: ${success_rate}%"
                echo ""
                
                # Calculate latency statistics
                if command -v awk &> /dev/null; then
                    tail -n +2 "$OUTPUT_DIR/results/events.csv" | awk -F',' '
                        $3==1 { latencies[NR] = $4; count++ }
                        END {
                            if (count > 0) {
                                # Sort latencies
                                for (i = 1; i <= count; i++) {
                                    for (j = i + 1; j <= count; j++) {
                                        if (latencies[i] > latencies[j]) {
                                            temp = latencies[i]
                                            latencies[i] = latencies[j]
                                            latencies[j] = temp
                                        }
                                    }
                                }
                                
                                # Calculate percentiles  
                                p50_idx = int(count * 0.5)
                                p95_idx = int(count * 0.95)
                                p99_idx = int(count * 0.99)
                                
                                print "Latency Statistics (ms):"
                                print "  p50:", latencies[p50_idx]
                                print "  p95:", latencies[p95_idx] 
                                print "  p99:", latencies[p99_idx]
                                print "  Max:", latencies[count]
                            }
                        }
                    '
                fi
            fi
        } > "$report_file"
        
        log_success "Report saved to $report_file"
    fi
}

# Main execution function
main() {
    echo -e "${GREEN}"
    echo "WikiSurge Load Testing Script"
    echo "============================="
    echo -e "${NC}"
    
    # Parse arguments
    parse_args "$@"
    
    # Show configuration
    log "Configuration:"
    log "  Scenario: $SCENARIO"
    log "  Rate: $RATE eps" 
    log "  Duration: $DURATION seconds"
    log "  Target: http://$HOST:$PORT$ENDPOINT"
    log "  Output: $OUTPUT_DIR"
    log "  Verbose: $VERBOSE"
    log "  Dry Run: $DRY_RUN"
    
    # Validate setup
    check_dependencies
    validate_config
    setup_output_dir
    
    # Setup cleanup trap
    trap 'log "Cleaning up..."; stop_monitoring; exit 0' INT TERM
    
    # Start monitoring
    start_monitoring
    
    # Execute scenario
    case $SCENARIO in
        "normal"|"sustained")
            run_normal_scenario
            ;;
        "spike")
            run_spike_scenario
            ;;
        "bursty")
            run_bursty_scenario
            ;;
        *)
            log_error "Unknown scenario: $SCENARIO"
            exit 1
            ;;
    esac
    
    # Stop monitoring and generate report
    stop_monitoring
    generate_report
    
    log_success "Load test completed successfully!"
    log "Results saved in: $OUTPUT_DIR"
}

# Run main function with all arguments
main "$@"