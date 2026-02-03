#!/bin/bash

# Infrastructure Test Script
# Tests connectivity and basic functionality of all infrastructure components

set -e

echo "=== WikiSurge Infrastructure Test ==="
echo "Starting infrastructure validation..."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test results
KAFKA_STATUS="❌ FAIL"
REDIS_STATUS="❌ FAIL"
ELASTICSEARCH_STATUS="❌ FAIL"
OVERALL_STATUS="❌ FAIL"

# Function to print test result
print_result() {
    local test_name="$1"
    local status="$2"
    if [[ "$status" == "PASS" ]]; then
        echo -e "${GREEN}✓ $test_name: PASS${NC}"
    else
        echo -e "${RED}✗ $test_name: FAIL${NC}"
    fi
}

# Wait for service to be ready
wait_for_service() {
    local service="$1"
    local check_command="$2"
    local max_attempts=30
    local attempt=1
    
    echo "Waiting for $service to be ready..."
    while ! eval "$check_command" >/dev/null 2>&1; do
        if [ $attempt -ge $max_attempts ]; then
            echo "❌ $service failed to start after $max_attempts attempts"
            return 1
        fi
        echo "⏳ Attempt $attempt/$max_attempts for $service..."
        sleep 2
        attempt=$((attempt + 1))
    done
    echo "✅ $service is ready"
    return 0
}

# Clean up function
cleanup() {
    echo "🧹 Cleaning up test data..."
    
    # Clean up Kafka test topic
    if command -v docker >/dev/null 2>&1; then
        docker-compose exec -T kafka rpk topic delete test-topic >/dev/null 2>&1 || true
    fi
    
    # Clean up Redis test data
    if command -v redis-cli >/dev/null 2>&1; then
        redis-cli -h localhost -p 6379 del "test-key" >/dev/null 2>&1 || true
    fi
    
    # Clean up Elasticsearch test index
    if command -v curl >/dev/null 2>&1; then
        curl -s -X DELETE "http://localhost:9200/test-index" >/dev/null 2>&1 || true
    fi
}

# Trap for cleanup on exit
trap cleanup EXIT

echo ""
echo "🔍 Testing Kafka..."
echo "----------------------------------------"

# Test Kafka
if wait_for_service "Kafka" "docker-compose exec -T kafka rpk cluster health"; then
    # Create test topic
    if docker-compose exec -T kafka rpk topic create test-topic --partitions 1 --replicas 1 >/dev/null 2>&1; then
        echo "✅ Test topic created successfully"
        
        # Produce test message
        if echo "test-message-$(date +%s)" | docker-compose exec -T kafka rpk topic produce test-topic >/dev/null 2>&1; then
            echo "✅ Test message produced successfully"
            
            # Consume test message
            if timeout 10 docker-compose exec -T kafka rpk topic consume test-topic --num 1 >/dev/null 2>&1; then
                echo "✅ Test message consumed successfully"
                KAFKA_STATUS="✅ PASS"
            else
                echo "❌ Failed to consume test message"
            fi
        else
            echo "❌ Failed to produce test message"
        fi
    else
        echo "❌ Failed to create test topic"
    fi
else
    echo "❌ Kafka is not responding"
fi

print_result "Kafka Test" "$(echo $KAFKA_STATUS | grep -o 'PASS\\|FAIL')"

echo ""
echo "🔍 Testing Redis..."
echo "----------------------------------------"

# Test Redis
if wait_for_service "Redis" "redis-cli -h localhost -p 6379 ping"; then
    # Test SET operation
    if redis-cli -h localhost -p 6379 set "test-key" "test-value-$(date +%s)" >/dev/null 2>&1; then
        echo "✅ Redis SET operation successful"
        
        # Test GET operation
        if redis-cli -h localhost -p 6379 get "test-key" >/dev/null 2>&1; then
            echo "✅ Redis GET operation successful"
            REDIS_STATUS="✅ PASS"
        else
            echo "❌ Redis GET operation failed"
        fi
    else
        echo "❌ Redis SET operation failed"
    fi
else
    echo "❌ Redis is not responding"
fi

print_result "Redis Test" "$(echo $REDIS_STATUS | grep -o 'PASS\\|FAIL')"

echo ""
echo "🔍 Testing Elasticsearch..."
echo "----------------------------------------"

# Test Elasticsearch
if wait_for_service "Elasticsearch" "curl -s http://localhost:9200/_cluster/health"; then
    # Test index creation
    if curl -s -X PUT "http://localhost:9200/test-index" -H 'Content-Type: application/json' -d'{"settings":{"number_of_shards":1,"number_of_replicas":0}}' | grep -q "acknowledged.*true" ; then
        echo "✅ Test index created successfully"
        
        # Test document indexing
        if curl -s -X POST "http://localhost:9200/test-index/_doc" -H 'Content-Type: application/json' -d'{"test":"data","timestamp":"'"$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)"'"}' | grep -q "created.*true\\|result.*created"; then
            echo "✅ Test document indexed successfully"
            
            # Wait for indexing and test search
            sleep 2
            if curl -s -X GET "http://localhost:9200/test-index/_search" | grep -q "hits"; then
                echo "✅ Test search successful"
                ELASTICSEARCH_STATUS="✅ PASS"
            else
                echo "❌ Test search failed"
            fi
        else
            echo "❌ Test document indexing failed"
        fi
    else
        echo "❌ Test index creation failed"
    fi
else
    echo "❌ Elasticsearch is not responding"
fi

print_result "Elasticsearch Test" "$(echo $ELASTICSEARCH_STATUS | grep -o 'PASS\\|FAIL')"

# Overall result
echo ""
echo "========================================="
echo "📊 Test Summary:"
echo "========================================="

if [[ "$KAFKA_STATUS" == *"PASS"* && "$REDIS_STATUS" == *"PASS"* && "$ELASTICSEARCH_STATUS" == *"PASS"* ]]; then
    OVERALL_STATUS="✅ PASS"
    echo -e "${GREEN}🎉 All infrastructure tests passed!${NC}"
    exit 0
else
    echo -e "${RED}❌ Some infrastructure tests failed!${NC}"
    echo ""
    echo "Failed components:"
    [[ "$KAFKA_STATUS" != *"PASS"* ]] && echo -e "${RED}  • Kafka${NC}"
    [[ "$REDIS_STATUS" != *"PASS"* ]] && echo -e "${RED}  • Redis${NC}"
    [[ "$ELASTICSEARCH_STATUS" != *"PASS"* ]] && echo -e "${RED}  • Elasticsearch${NC}"
    echo ""
    echo "💡 Suggestions:"
    echo "  • Check if all services are running: make start"
    echo "  • Check service logs: make logs"
    echo "  • Verify service health: make health"
    exit 1
fi