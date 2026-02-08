#!/bin/bash

# WikiSurge Storage Layer Validation Script
# This script validates the Elasticsearch and Redis storage setup

set -e

echo "=== WikiSurge Storage Layer Validation ==="
echo "Validating Elasticsearch and Redis storage components..."
echo

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    if [ $1 -eq 0 ]; then
        printf "${GREEN}✓${NC} $2\n"
    else
        printf "${RED}✗${NC} $2\n"
    fi
}

# Function to print warnings
print_warning() {
    printf "${YELLOW}⚠${NC} $1\n"
}

# Function to print section headers
print_header() {
    echo
    echo "=== $1 ==="
}

# Check if required services are running
print_header "Service Availability Check"

# Check Elasticsearch
echo "Checking Elasticsearch connection..."
if curl -s -f http://localhost:9200/_cluster/health > /dev/null 2>&1; then
    print_status 0 "Elasticsearch is running and accessible"
    ES_AVAILABLE=true
else
    print_status 1 "Elasticsearch is not accessible at localhost:9200"
    ES_AVAILABLE=false
fi

# Check Redis
echo "Checking Redis connection..."
if redis-cli ping > /dev/null 2>&1; then
    print_status 0 "Redis is running and accessible"
    REDIS_AVAILABLE=true
else
    print_status 1 "Redis is not accessible"
    REDIS_AVAILABLE=false
fi

# Check Go modules and dependencies
print_header "Dependency Check"

echo "Checking Go module setup..."
if go mod verify > /dev/null 2>&1; then
    print_status 0 "Go modules are valid"
else
    print_status 1 "Go module verification failed"
fi

echo "Checking required dependencies..."
REQUIRED_DEPS=("github.com/elastic/go-elasticsearch/v8" "github.com/redis/go-redis/v9" "github.com/prometheus/client_golang")

for dep in "${REQUIRED_DEPS[@]}"; do
    if go list -m "$dep" > /dev/null 2>&1; then
        print_status 0 "Dependency available: $dep"
    else
        print_status 1 "Missing dependency: $dep"
        print_warning "Run: go get $dep"
    fi
done

# Test Go compilation
print_header "Code Compilation Test"

echo "Testing storage package compilation..."
if go build ./internal/storage/... > /dev/null 2>&1; then
    print_status 0 "Storage package compiles successfully"
else
    print_status 1 "Storage package compilation failed"
    echo "Compilation errors:"
    go build ./internal/storage/...
fi

echo "Testing models package compilation..."
if go build ./internal/models/... > /dev/null 2>&1; then
    print_status 0 "Models package compiles successfully"
else
    print_status 1 "Models package compilation failed"
    echo "Compilation errors:"
    go build ./internal/models/...
fi

# Run unit tests
print_header "Unit Tests"

echo "Running storage unit tests..."
if go test ./internal/storage/ -v > /tmp/storage_test.log 2>&1; then
    print_status 0 "Storage unit tests pass"
else
    print_status 1 "Storage unit tests fail"
    echo "Test output:"
    cat /tmp/storage_test.log
fi

echo "Running models unit tests..."
if go test ./internal/models/ -v > /dev/null 2>&1; then
    print_status 0 "Models unit tests pass"
else
    print_status 1 "Models unit tests fail"
fi

# Elasticsearch-specific validation
if [ "$ES_AVAILABLE" = true ]; then
    print_header "Elasticsearch Validation"
    
    echo "Checking cluster health..."
    CLUSTER_STATUS=$(curl -s http://localhost:9200/_cluster/health | jq -r '.status' 2>/dev/null || echo "unknown")
    if [ "$CLUSTER_STATUS" = "green" ] || [ "$CLUSTER_STATUS" = "yellow" ]; then
        print_status 0 "Elasticsearch cluster status: $CLUSTER_STATUS"
    else
        print_status 1 "Elasticsearch cluster status: $CLUSTER_STATUS"
    fi
    
    echo "Testing ILM policy creation..."
    TEST_POLICY='{"policy":{"phases":{"hot":{"actions":{"rollover":{"max_size":"1gb","max_age":"1d"}}},"delete":{"min_age":"7d","actions":{"delete":{}}}}}}'
    if curl -s -X PUT "http://localhost:9200/_ilm/policy/test-policy" -H "Content-Type: application/json" -d "$TEST_POLICY" > /dev/null; then
        print_status 0 "ILM policy creation works"
        curl -s -X DELETE "http://localhost:9200/_ilm/policy/test-policy" > /dev/null
    else
        print_status 1 "ILM policy creation failed"
    fi
    
    echo "Testing index template creation..."
    TEST_TEMPLATE='{"index_patterns":["test-*"],"template":{"settings":{"number_of_shards":1},"mappings":{"properties":{"title":{"type":"text"}}}}}'
    if curl -s -X PUT "http://localhost:9200/_index_template/test-template" -H "Content-Type: application/json" -d "$TEST_TEMPLATE" > /dev/null; then
        print_status 0 "Index template creation works"
        curl -s -X DELETE "http://localhost:9200/_index_template/test-template" > /dev/null
    else
        print_status 1 "Index template creation failed"
    fi
    
    echo "Testing document indexing..."
    TEST_DOC='{"title":"Test Document","timestamp":"2024-01-01T00:00:00.000Z","test":true}'
    if curl -s -X POST "http://localhost:9200/test-index/_doc" -H "Content-Type: application/json" -d "$TEST_DOC" > /dev/null; then
        print_status 0 "Document indexing works"
        curl -s -X DELETE "http://localhost:9200/test-index" > /dev/null 2>&1
    else
        print_status 1 "Document indexing failed"
    fi
    
else
    print_warning "Skipping Elasticsearch validation (service not available)"
fi

# Redis-specific validation
if [ "$REDIS_AVAILABLE" = true ]; then
    print_header "Redis Validation"
    
    echo "Testing basic Redis operations..."
    if redis-cli set test:key "test value" > /dev/null && redis-cli get test:key > /dev/null; then
        print_status 0 "Redis basic operations work"
        redis-cli del test:key > /dev/null
    else
        print_status 1 "Redis basic operations failed"
    fi
    
    echo "Testing Redis sorted sets (for trending)..."
    if redis-cli zadd test:zset 1.0 "item1" > /dev/null && redis-cli zrange test:zset 0 -1 > /dev/null; then
        print_status 0 "Redis sorted sets work"
        redis-cli del test:zset > /dev/null
    else
        print_status 1 "Redis sorted sets failed"
    fi
    
    echo "Testing Redis streams (for alerts)..."
    if redis-cli xadd test:stream "*" field1 value1 > /dev/null && redis-cli xlen test:stream > /dev/null; then
        print_status 0 "Redis streams work"
        redis-cli del test:stream > /dev/null
    else
        print_status 1 "Redis streams failed"
    fi
    
    echo "Testing Redis hash operations (for metadata)..."
    if redis-cli hset test:hash field1 value1 > /dev/null && redis-cli hget test:hash field1 > /dev/null; then
        print_status 0 "Redis hash operations work"
        redis-cli del test:hash > /dev/null
    else
        print_status 1 "Redis hash operations failed"
    fi
    
else
    print_warning "Skipping Redis validation (service not available)"
fi

# Integration tests (if both services available)
if [ "$ES_AVAILABLE" = true ] && [ "$REDIS_AVAILABLE" = true ]; then
    print_header "Integration Tests"
    
    echo "Running integration tests..."
    if go test ./test/integration/ -v -timeout=30s > /tmp/integration_test.log 2>&1; then
        print_status 0 "Integration tests pass"
    else
        print_status 1 "Integration tests fail"
        echo "Test output:"
        cat /tmp/integration_test.log
    fi
else
    print_warning "Skipping integration tests (services not available)"
fi

# Configuration validation
print_header "Configuration Validation"

echo "Checking configuration files..."
CONFIG_FILES=("configs/config.dev.yaml" "configs/config.minimal.yaml" "configs/config.prod.yaml")

for config_file in "${CONFIG_FILES[@]}"; do
    if [ -f "$config_file" ]; then
        if python3 -c "import yaml; yaml.safe_load(open('$config_file'))" 2>/dev/null; then
            print_status 0 "Configuration file valid: $config_file"
        else
            print_status 1 "Configuration file invalid: $config_file"
        fi
    else
        print_status 1 "Configuration file missing: $config_file"
    fi
done

# Performance benchmark
print_header "Performance Benchmarks"

echo "Running performance benchmarks..."
if go test ./internal/storage/ -bench=. -benchtime=1s > /tmp/benchmark.log 2>&1; then
    print_status 0 "Benchmarks completed"
    echo "Benchmark results:"
    grep "Benchmark" /tmp/benchmark.log
else
    print_status 1 "Benchmarks failed"
fi

# Summary
print_header "Validation Summary"

echo
echo "Storage Layer Implementation Status:"
echo

# Count implemented components
COMPONENTS=(
    "internal/storage/elasticsearch.go"
    "internal/storage/redis_hot_pages.go" 
    "internal/storage/redis_trending.go"
    "internal/storage/redis_alerts.go"
    "internal/storage/storage_strategy.go"
    "internal/models/document.go"
)

IMPLEMENTED=0
for component in "${COMPONENTS[@]}"; do
    if [ -f "$component" ]; then
        ((IMPLEMENTED++))
        print_status 0 "Implemented: $component"
    else
        print_status 1 "Missing: $component"
    fi
done

echo
echo "Implementation Progress: $IMPLEMENTED/${#COMPONENTS[@]} components"

if [ $IMPLEMENTED -eq ${#COMPONENTS[@]} ]; then
    printf "${GREEN}✅ Storage layer implementation is complete!${NC}\n"
else
    printf "${RED}❌ Storage layer implementation is incomplete.${NC}\n"
fi

# Cleanup
rm -f /tmp/storage_test.log /tmp/integration_test.log /tmp/benchmark.log

echo
echo "Validation completed. Check the output above for any issues that need to be addressed."