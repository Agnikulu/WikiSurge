#!/bin/bash

# =============================================================================
# WikiSurge Spike Simulation Script
# 
# This script simulates various spike scenarios by producing test events
# to Kafka for testing the spike detection system.
# =============================================================================

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
KAFKA_HOST=${KAFKA_HOST:-localhost}
KAFKA_PORT=${KAFKA_PORT:-9092}
KAFKA_TOPIC=${KAFKA_TOPIC:-wikipedia.edits}

print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to produce a Wikipedia edit event to Kafka
produce_edit_event() {
    local page_title=$1
    local user_name=$2
    local old_size=${3:-100}
    local new_size=${4:-150}
    local timestamp=${5:-$(date +%s)}
    
    # Create edit JSON
    local edit_json=$(cat <<EOF
{
  "id": $RANDOM,
  "type": "edit",
  "title": "$page_title",
  "user": "$user_name",
  "bot": false,
  "wiki": "enwiki",
  "server_url": "https://en.wikipedia.org",
  "timestamp": $timestamp,
  "length": {
    "old": $old_size,
    "new": $new_size
  },
  "revision": {
    "old": $((RANDOM + 1000000)),
    "new": $((RANDOM + 1000000))
  },
  "comment": "Test edit for spike detection"
}
EOF
    )
    
    # Use kafka-go CLI tool or kafka-console-producer if available
    if command -v kafka-console-producer.sh >/dev/null 2>&1; then
        echo "$edit_json" | kafka-console-producer.sh --bootstrap-server $KAFKA_HOST:$KAFKA_PORT --topic $KAFKA_TOPIC
    elif command -v kafkacat >/dev/null 2>&1; then
        echo "$edit_json" | kafkacat -P -b $KAFKA_HOST:$KAFKA_PORT -t $KAFKA_TOPIC
    else
        # Fallback: create a simple Go script to produce the message
        create_kafka_producer "$edit_json"
    fi
}

# Create a simple Kafka producer using Go
create_kafka_producer() {
    local message=$1
    
    cat > /tmp/producer.go <<EOF
package main

import (
    "context"
    "log"
    "time"
    
    "github.com/segmentio/kafka-go"
)

func main() {
    writer := &kafka.Writer{
        Addr:     kafka.TCP("$KAFKA_HOST:$KAFKA_PORT"),
        Topic:    "$KAFKA_TOPIC",
        Balancer: &kafka.LeastBytes{},
    }
    defer writer.Close()
    
    message := kafka.Message{
        Value: []byte(\`$message\`),
    }
    
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    if err := writer.WriteMessages(ctx, message); err != nil {
        log.Fatal(err)
    }
}
EOF
    
    cd /tmp && go mod init producer 2>/dev/null || true
    go get github.com/segmentio/kafka-go 2>/dev/null || true
    go run producer.go
    rm -f producer.go go.mod go.sum
}

# Check Kafka availability
check_kafka() {
    if ! nc -z $KAFKA_HOST $KAFKA_PORT 2>/dev/null; then
        print_error "Kafka is not accessible on $KAFKA_HOST:$KAFKA_PORT"
        print_error "Please ensure Kafka is running or set KAFKA_HOST and KAFKA_PORT environment variables"
        exit 1
    fi
}

# Scenario 1: Clear spike pattern
scenario_clear_spike() {
    local page_title="Clear_Spike_Test_$(date +%s)"
    
    print_status "Scenario 1: Simulating clear spike for page '$page_title'"
    
    # Normal activity: 1 edit every 15 minutes for the last hour
    print_status "Producing baseline edits (1 every 15 minutes)..."
    local base_time=$(($(date +%s) - 3600))  # 1 hour ago
    
    for i in {0..3}; do
        local edit_time=$((base_time + i * 900))  # Every 15 minutes
        produce_edit_event "$page_title" "baseline_user_$i" 100 110 $edit_time
        sleep 0.5
    done
    
    print_success "Baseline edits produced"
    
    # Wait a moment
    sleep 2
    
    # Spike: 20 edits in 5 minutes
    print_status "Producing spike: 20 edits in 5 minutes..."
    local spike_start=$(date +%s)
    
    for i in {1..20}; do
        local edit_time=$((spike_start + i * 15))  # Every 15 seconds
        local user_num=$((i % 5))  # 5 different users
        produce_edit_event "$page_title" "spike_user_$user_num" 100 200 $edit_time
        sleep 0.2
    done
    
    print_success "Spike simulation completed for '$page_title'"
    echo "Expected: High spike ratio (~20x), severity: high"
}

# Scenario 2: Gradual increase (no spike expected)
scenario_gradual_increase() {
    local page_title="Gradual_Increase_Test_$(date +%s)"
    
    print_status "Scenario 2: Simulating gradual increase for page '$page_title'"
    
    # Gradual increase from 1 edit per 30 minutes to 1 edit per 3 minutes over 30 minutes
    local base_time=$(($(date +%s) - 1800))  # 30 minutes ago
    local edit_count=0
    
    for minute in {0..29}; do
        # Calculate interval: starts at 30 minutes, decreases to 3 minutes
        local interval=$((30 - minute * 27 / 29))
        
        if [ $((minute % interval)) -eq 0 ]; then
            local edit_time=$((base_time + minute * 60))
            local user_num=$((edit_count % 3))
            produce_edit_event "$page_title" "gradual_user_$user_num" 100 120 $edit_time
            edit_count=$((edit_count + 1))
            sleep 0.2
        fi
    done
    
    print_success "Gradual increase simulation completed for '$page_title'"
    echo "Expected: No high-severity spike (gradual change)"
}

# Scenario 3: False positive prevention
scenario_false_positive() {
    local page_title="False_Positive_Test_$(date +%s)"
    
    print_status "Scenario 3: Testing false positive prevention for page '$page_title'"
    
    # High baseline: 12 edits in the last hour (1 every 5 minutes)
    print_status "Creating high baseline activity..."
    local base_time=$(($(date +%s) - 3600))
    
    for i in {0..11}; do
        local edit_time=$((base_time + i * 300))  # Every 5 minutes
        local user_num=$((i % 4))
        produce_edit_event "$page_title" "regular_user_$user_num" 100 110 $edit_time
        sleep 0.2
    done
    
    # Recent activity: only 3 edits in 5 minutes (rate similar to baseline)
    print_status "Producing recent activity similar to baseline..."
    local recent_start=$(date +%s)
    
    for i in {1..3}; do
        local edit_time=$((recent_start + i * 100))  # Every ~1.7 minutes
        produce_edit_event "$page_title" "recent_user" 100 105 $edit_time
        sleep 0.3
    done
    
    print_success "False positive test completed for '$page_title'"
    echo "Expected: No spike detected (ratio ~1.25x, below threshold)"
}

# Scenario 4: Below minimum threshold
scenario_minimum_threshold() {
    local page_title="Min_Threshold_Test_$(date +%s)"
    
    print_status "Scenario 4: Testing minimum threshold for page '$page_title'"
    
    # Very low baseline
    local hour_ago=$(($(date +%s) - 3600))
    produce_edit_event "$page_title" "minimal_user" 100 101 $hour_ago
    sleep 0.5
    
    # Only 2 edits in 5 minutes (below minimum of 3)
    print_status "Producing activity below minimum threshold..."
    local recent_start=$(date +%s)
    
    for i in {1..2}; do
        local edit_time=$((recent_start + i * 150))  # Every 2.5 minutes
        produce_edit_event "$page_title" "threshold_user" 100 120 $edit_time
        sleep 0.3
    done
    
    print_success "Minimum threshold test completed for '$page_title'"
    echo "Expected: No spike detected (below minimum edits)"
}

# Main menu
show_menu() {
    echo
    echo -e "${BLUE}=== WikiSurge Spike Detection Test Scenarios ===${NC}"
    echo
    echo "Choose a scenario to simulate:"
    echo "1) Clear spike detection (should trigger alert)"
    echo "2) Gradual increase (should NOT trigger)"
    echo "3) False positive prevention (should NOT trigger)"
    echo "4) Minimum threshold test (should NOT trigger)"
    echo "5) All scenarios"
    echo "6) Custom continuous load"
    echo "q) Quit"
    echo
}

# Custom continuous load for stress testing
scenario_continuous_load() {
    local duration=${1:-60}  # Default 60 seconds
    local pages=${2:-5}      # Default 5 pages
    local rate=${3:-2}       # Default 2 edits per second
    
    print_status "Running continuous load test for $duration seconds"
    print_status "Pages: $pages, Rate: $rate edits/second"
    
    local end_time=$(($(date +%s) + duration))
    local edit_counter=0
    
    while [ $(date +%s) -lt $end_time ]; do
        for page_num in $(seq 1 $pages); do
            local page_title="Load_Test_Page_$page_num"
            local user_num=$((edit_counter % 10))
            
            produce_edit_event "$page_title" "load_user_$user_num" 100 $((110 + RANDOM % 50))
            edit_counter=$((edit_counter + 1))
            
            if [ $((edit_counter % 10)) -eq 0 ]; then
                print_status "Produced $edit_counter edits..."
            fi
        done
        
        sleep $(echo "scale=2; 1 / $rate" | bc -l 2>/dev/null || echo "0.5")
    done
    
    print_success "Continuous load test completed. Total edits: $edit_counter"
}

# Main execution
main() {
    print_status "Checking Kafka connectivity..."
    check_kafka
    print_success "Connected to Kafka on $KAFKA_HOST:$KAFKA_PORT"
    
    if [ $# -eq 0 ]; then
        # Interactive mode
        while true; do
            show_menu
            read -p "Enter your choice: " choice
            
            case $choice in
                1)
                    scenario_clear_spike
                    ;;
                2)
                    scenario_gradual_increase
                    ;;
                3)
                    scenario_false_positive
                    ;;
                4)
                    scenario_minimum_threshold
                    ;;
                5)
                    print_status "Running all scenarios..."
                    scenario_clear_spike
                    sleep 3
                    scenario_gradual_increase
                    sleep 3
                    scenario_false_positive
                    sleep 3
                    scenario_minimum_threshold
                    print_success "All scenarios completed"
                    ;;
                6)
                    echo
                    read -p "Duration in seconds (default 60): " duration
                    read -p "Number of pages (default 5): " pages
                    read -p "Edits per second (default 2): " rate
                    
                    scenario_continuous_load ${duration:-60} ${pages:-5} ${rate:-2}
                    ;;
                q)
                    print_status "Goodbye!"
                    exit 0
                    ;;
                *)
                    print_error "Invalid choice. Please try again."
                    ;;
            esac
            
            echo
            read -p "Press Enter to continue..."
        done
    else
        # Command line mode
        case $1 in
            "spike")
                scenario_clear_spike
                ;;
            "gradual")
                scenario_gradual_increase
                ;;
            "false-positive")
                scenario_false_positive
                ;;
            "threshold")
                scenario_minimum_threshold
                ;;
            "all")
                scenario_clear_spike
                scenario_gradual_increase
                scenario_false_positive
                scenario_minimum_threshold
                ;;
            "load")
                scenario_continuous_load ${2:-60} ${3:-5} ${4:-2}
                ;;
            *)
                echo "Usage: $0 [spike|gradual|false-positive|threshold|all|load [duration] [pages] [rate]]"
                exit 1
                ;;
        esac
    fi
}

main "$@"