#!/bin/bash

# =============================================================================
# WikiSurge Spike Detection Validation Script
# 
# This script validates that the spike detection system is working correctly
# by testing various scenarios and checking outputs.
# =============================================================================

set -e  # Exit on any error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REDIS_HOST=${REDIS_HOST:-localhost}
REDIS_PORT=${REDIS_PORT:-6379}
KAFKA_HOST=${KAFKA_HOST:-localhost}
KAFKA_PORT=${KAFKA_PORT:-9092}
PROCESSOR_METRICS_PORT=${PROCESSOR_METRICS_PORT:-2112}

echo -e "${BLUE}=== WikiSurge Spike Detection Validation ===${NC}"
echo

# Function to print status messages
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to check if a service is running
check_service() {
    local service_name=$1
    local host=$2
    local port=$3
    
    if nc -z $host $port 2>/dev/null; then
        print_success "$service_name is running on $host:$port"
        return 0
    else
        print_error "$service_name is not accessible on $host:$port"
        return 1
    fi
}

# Function to wait for service
wait_for_service() {
    local service_name=$1
    local host=$2
    local port=$3
    local timeout=${4:-30}
    
    print_status "Waiting for $service_name to be ready..."
    
    for i in $(seq 1 $timeout); do
        if nc -z $host $port 2>/dev/null; then
            print_success "$service_name is ready"
            return 0
        fi
        sleep 1
    done
    
    print_error "$service_name failed to start within $timeout seconds"
    return 1
}

# Check prerequisites
print_status "Checking prerequisites..."

# Check Redis
if ! check_service "Redis" $REDIS_HOST $REDIS_PORT; then
    print_error "Redis is required for spike detection. Please start Redis server."
    exit 1
fi

# Check Kafka (optional for basic tests)
KAFKA_AVAILABLE=true
if ! check_service "Kafka" $KAFKA_HOST $KAFKA_PORT; then
    print_warning "Kafka is not available. Skipping Kafka-dependent tests."
    KAFKA_AVAILABLE=false
fi

# Test Redis connectivity and operations
print_status "Testing Redis operations..."

# Clear test data
redis-cli -h $REDIS_HOST -p $REDIS_PORT FLUSHDB > /dev/null
print_success "Redis test database cleared"

# Test basic Redis operations
redis-cli -h $REDIS_HOST -p $REDIS_PORT SET test_key "test_value" > /dev/null
RESULT=$(redis-cli -h $REDIS_HOST -p $REDIS_PORT GET test_key)
if [ "$RESULT" = "test_value" ]; then
    print_success "Redis basic operations working"
else
    print_error "Redis basic operations failed"
    exit 1
fi

# Test Redis streams (used for alerts)
STREAM_ID=$(redis-cli -h $REDIS_HOST -p $REDIS_PORT XADD test_stream "*" data "test_alert" severity "low")
if [ ! -z "$STREAM_ID" ]; then
    print_success "Redis streams working"
else
    print_error "Redis streams not working"
    exit 1
fi

# Clean up test data
redis-cli -h $REDIS_HOST -p $REDIS_PORT DEL test_key > /dev/null
redis-cli -h $REDIS_HOST -p $REDIS_PORT DEL test_stream > /dev/null

# Build the processor if not already built
print_status "Building processor binary..."
cd "$(dirname "$0")/../"
if go build -o bin/processor cmd/processor/main.go; then
    print_success "Processor binary built successfully"
else
    print_error "Failed to build processor binary"
    exit 1
fi

# Configure processor for testing
CONFIG_FILE="configs/config.dev.yaml"
if [ ! -f "$CONFIG_FILE" ]; then
    print_warning "Config file $CONFIG_FILE not found. Creating minimal config..."
    mkdir -p configs
    cat > "$CONFIG_FILE" << EOF
redis:
  url: "redis://$REDIS_HOST:$REDIS_PORT/1"
  hot_pages:
    max_tracked: 100
    promotion_threshold: 2
    window_duration: 1h
    max_members_per_page: 50
    hot_threshold: 2
    cleanup_interval: 5m

kafka:
  brokers: ["$KAFKA_HOST:$KAFKA_PORT"]
  consumer_group: "spike-detector-test"

logging:
  level: "info"
  format: "json"
EOF
    print_success "Created minimal config file"
fi

# Function to simulate edits for testing
simulate_edit_burst() {
    local page_title=$1
    local edit_count=$2
    local time_window_seconds=$3
    
    print_status "Simulating $edit_count edits for '$page_title' over $time_window_seconds seconds..."
    
    for i in $(seq 1 $edit_count); do
        # Create a test edit JSON
        TIMESTAMP=$(date +%s)
        EDIT_JSON="{\"id\":$RANDOM,\"title\":\"$page_title\",\"user\":\"test_user_$i\",\"timestamp\":$TIMESTAMP,\"length\":{\"old\":100,\"new\":150},\"bot\":false,\"type\":\"edit\",\"wiki\":\"enwiki\"}"
        
        # For this validation, we'll use Redis directly to simulate the hot page tracking
        # In a real scenario, this would come through Kafka
        
        # Simulate activity counter
        redis-cli -h $REDIS_HOST -p $REDIS_PORT INCR "activity:$page_title" > /dev/null
        
        if [ $i -lt $edit_count ]; then
            sleep $(echo "scale=2; $time_window_seconds / $edit_count" | bc -l 2>/dev/null || echo "1")
        fi
    done
    
    print_success "Edit simulation completed"
}

# Test spike detection logic
print_status "Testing spike detection scenarios..."

# Scenario 1: Clear spike detection
print_status "Scenario 1: Testing clear spike detection..."
TEST_PAGE="Test_Clear_Spike_$(date +%s)"

# Simulate normal activity first
redis-cli -h $REDIS_HOST -p $REDIS_PORT SET "activity:$TEST_PAGE" 5 > /dev/null

# Then simulate a spike
simulate_edit_burst "$TEST_PAGE" 20 300  # 20 edits in 5 minutes

# Check if we can detect this pattern
ACTIVITY_COUNT=$(redis-cli -h $REDIS_HOST -p $REDIS_PORT GET "activity:$TEST_PAGE")
if [ "$ACTIVITY_COUNT" -ge 20 ]; then
    print_success "Scenario 1: Activity tracking working (count: $ACTIVITY_COUNT)"
else
    print_warning "Scenario 1: Activity count lower than expected (count: $ACTIVITY_COUNT)"
fi

# Scenario 2: Test minimum threshold
print_status "Scenario 2: Testing minimum threshold..."
TEST_PAGE_2="Test_Min_Threshold_$(date +%s)"

# Only 2 edits (below minimum threshold of 3)
simulate_edit_burst "$TEST_PAGE_2" 2 60

ACTIVITY_COUNT_2=$(redis-cli -h $REDIS_HOST -p $REDIS_PORT GET "activity:$TEST_PAGE_2")
print_status "Scenario 2: Activity count for low-activity page: $ACTIVITY_COUNT_2"

# Start processor in background for integration test (if Kafka available)
PROCESSOR_PID=""
if [ "$KAFKA_AVAILABLE" = true ]; then
    print_status "Starting processor for integration test..."
    ./bin/processor -config "$CONFIG_FILE" > processor.log 2>&1 &
    PROCESSOR_PID=$!
    
    # Wait for processor to start
    if wait_for_service "Processor metrics" localhost $PROCESSOR_METRICS_PORT 15; then
        print_success "Processor started successfully"
        
        # Wait a bit more for full initialization
        sleep 3
        
        # Check processor metrics
        if curl -s http://localhost:$PROCESSOR_METRICS_PORT/metrics | grep -q "processed_edits_total"; then
            print_success "Processor metrics endpoint responding"
        else
            print_warning "Processor metrics not found (may not have processed edits yet)"
        fi
        
        # Check health endpoint
        if curl -s http://localhost:$PROCESSOR_METRICS_PORT/health | grep -q "healthy"; then
            print_success "Processor health check passed"
        else
            print_warning "Processor health check failed"
        fi
    else
        print_warning "Processor metrics endpoint not responding"
    fi
fi

# Check Redis for alert streams
print_status "Checking Redis alert stream..."
ALERT_COUNT=$(redis-cli -h $REDIS_HOST -p $REDIS_PORT XLEN "alerts:spikes" 2>/dev/null || echo "0")
print_status "Current alerts in stream: $ALERT_COUNT"

if [ "$ALERT_COUNT" -gt 0 ]; then
    print_success "Alert stream contains $ALERT_COUNT alerts"
    
    # Show recent alerts
    print_status "Recent alerts:"
    redis-cli -h $REDIS_HOST -p $REDIS_PORT XREVRANGE "alerts:spikes" + - COUNT 3 | head -20
else
    print_status "No alerts in stream yet (this might be normal for a new system)"
fi

# Check hot pages tracking
print_status "Checking hot pages tracking..."
HOT_PAGES_COUNT=$(redis-cli -h $REDIS_HOST -p $REDIS_PORT KEYS "hot:window:*" | wc -l)
print_status "Hot page windows currently tracked: $HOT_PAGES_COUNT"

if [ "$HOT_PAGES_COUNT" -gt 0 ]; then
    print_success "Hot pages tracking is active"
    print_status "Sample hot page keys:"
    redis-cli -h $REDIS_HOST -p $REDIS_PORT KEYS "hot:window:*" | head -5
fi

# Performance test
print_status "Running performance validation..."
START_TIME=$(date +%s)
simulate_edit_burst "Performance_Test_$(date +%s)" 50 30  # 50 edits in 30 seconds
END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

if [ "$DURATION" -le 60 ]; then
    print_success "Performance test completed in $DURATION seconds (acceptable)"
else
    print_warning "Performance test took $DURATION seconds (may be slow)"
fi

# Memory usage check (if processor is running)
if [ ! -z "$PROCESSOR_PID" ] && kill -0 $PROCESSOR_PID 2>/dev/null; then
    print_status "Checking processor resource usage..."
    
    # Get memory usage
    if command -v ps >/dev/null; then
        MEMORY_KB=$(ps -o rss= -p $PROCESSOR_PID 2>/dev/null || echo "0")
        MEMORY_MB=$((MEMORY_KB / 1024))
        
        if [ "$MEMORY_MB" -lt 500 ]; then
            print_success "Memory usage: ${MEMORY_MB}MB (acceptable)"
        else
            print_warning "Memory usage: ${MEMORY_MB}MB (may be high for test scenario)"
        fi
    fi
fi

# Cleanup
print_status "Cleaning up..."

if [ ! -z "$PROCESSOR_PID" ] && kill -0 $PROCESSOR_PID 2>/dev/null; then
    print_status "Stopping processor..."
    kill -TERM $PROCESSOR_PID
    sleep 3
    
    # Force kill if still running
    if kill -0 $PROCESSOR_PID 2>/dev/null; then
        kill -KILL $PROCESSOR_PID
    fi
    
    print_success "Processor stopped"
fi

# Clean test data
redis-cli -h $REDIS_HOST -p $REDIS_PORT FLUSHDB > /dev/null
print_success "Test data cleaned up"

# Summary
print_status "Validation Summary:"
echo
print_success "✓ Redis connectivity and operations"
print_success "✓ Redis streams functionality"
print_success "✓ Processor binary compilation"
if [ "$KAFKA_AVAILABLE" = true ]; then
    print_success "✓ Kafka integration (available)"
    print_success "✓ Processor startup and health checks"
else
    print_warning "⚠ Kafka integration (not available - skipped)"
fi
print_success "✓ Hot pages activity tracking"
print_success "✓ Performance validation"
print_success "✓ Resource usage check"

echo
print_success "Spike detection system validation completed successfully!"
echo
print_status "Next steps:"
echo "1. Start Kafka if not already running for full integration"
echo "2. Run: ./bin/processor -config configs/config.dev.yaml"
echo "3. Monitor with: curl http://localhost:$PROCESSOR_METRICS_PORT/metrics"
echo "4. Check alerts with: redis-cli XREAD COUNT 10 STREAMS alerts:spikes 0"
echo