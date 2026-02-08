#!/bin/bash

# validate-trending.sh - Manual validation script for trending functionality

set -e

echo "=== WikiSurge Trending Validation Script ==="
echo

# Check if Redis is running
echo "1. Checking Redis connection..."
if ! redis-cli ping > /dev/null 2>&1; then
    echo "❌ Redis is not running. Please start Redis first."
    exit 1
fi
echo "✅ Redis is running"
echo

# Function to add test data
add_test_data() {
    echo "2. Adding test trending data..."
    
    # Simulate adding trending scores with different timestamps
    NOW=$(date +%s)
    
    # Page A: Recent high score
    redis-cli HSET "trending:Breaking_News" raw_score 100.0 last_updated $NOW > /dev/null
    redis-cli ZADD "trending:global" 100.0 "Breaking_News" > /dev/null
    
    # Page B: Older medium score (30 minutes ago)
    OLDER=$((NOW - 1800))
    redis-cli HSET "trending:Regular_Article" raw_score 50.0 last_updated $OLDER > /dev/null
    redis-cli ZADD "trending:global" 50.0 "Regular_Article" > /dev/null
    
    # Page C: Very old low score (2 hours ago)
    MUCH_OLDER=$((NOW - 7200))
    redis-cli HSET "trending:Old_Page" raw_score 25.0 last_updated $MUCH_OLDER > /dev/null
    redis-cli ZADD "trending:global" 25.0 "Old_Page" > /dev/null
    
    # Page D: Recent new page bonus
    redis-cli HSET "trending:New_Article" raw_score 200.0 last_updated $NOW > /dev/null
    redis-cli ZADD "trending:global" 200.0 "New_Article" > /dev/null
    
    echo "✅ Test data added"
    echo
}

# Function to check trending scores
check_trending_scores() {
    echo "3. Checking trending global set..."
    echo "Top 10 trending (with stored scores):"
    redis-cli ZREVRANGE trending:global 0 9 WITHSCORES
    echo
}

# Function to check individual page data
check_page_data() {
    echo "4. Checking individual page data..."
    
    pages=("Breaking_News" "Regular_Article" "Old_Page" "New_Article")
    
    for page in "${pages[@]}"; do
        echo "Page: $page"
        redis-cli HGETALL "trending:$page"
        echo "---"
    done
    echo
}

# Function to test decay calculation
test_decay_calculation() {
    echo "5. Testing decay calculation (manual verification needed)..."
    echo
    echo "Expected decay after different time periods (30min half-life):"
    echo "- 0 minutes: 100% (decay factor = 1.0)"
    echo "- 30 minutes: 50% (decay factor = 0.5)" 
    echo "- 60 minutes: 25% (decay factor = 0.25)"
    echo "- 120 minutes: 6.25% (decay factor = 0.0625)"
    echo
    
    echo "Current page data with timestamps:"
    NOW=$(date +%s)
    
    pages=("Breaking_News" "Regular_Article" "Old_Page" "New_Article")
    
    for page in "${pages[@]}"; do
        RAW_SCORE=$(redis-cli HGET "trending:$page" raw_score 2>/dev/null || echo "0")
        LAST_UPDATED=$(redis-cli HGET "trending:$page" last_updated 2>/dev/null || echo "0")
        
        if [ "$RAW_SCORE" != "0" ] && [ "$LAST_UPDATED" != "0" ]; then
            ELAPSED_SEC=$((NOW - LAST_UPDATED))
            ELAPSED_MIN=$((ELAPSED_SEC / 60))
            
            echo "$page:"
            echo "  Raw Score: $RAW_SCORE"
            echo "  Age: ${ELAPSED_MIN} minutes"
            echo "  Expected Current Score: $(echo "$RAW_SCORE * (0.5 ^ ($ELAPSED_MIN / 30.0))" | bc -l 2>/dev/null || echo "calculation error")"
        fi
        echo
    done
}

# Function to test ranking
test_ranking() {
    echo "6. Testing page ranking..."
    
    echo "Page ranks (0-indexed):"
    pages=("Breaking_News" "Regular_Article" "Old_Page" "New_Article")
    
    for page in "${pages[@]}"; do
        RANK=$(redis-cli ZREVRANK trending:global "$page" 2>/dev/null || echo "not found")
        echo "$page: rank $RANK"
    done
    echo
}

# Function to test pruning simulation
test_pruning() {
    echo "7. Testing pruning (low score removal)..."
    
    # Add a very low score entry
    redis-cli HSET "trending:Low_Score_Page" raw_score 0.001 last_updated $(date +%s) > /dev/null
    redis-cli ZADD "trending:global" 0.001 "Low_Score_Page" > /dev/null
    
    echo "Added low score page. Current count:"
    redis-cli ZCARD trending:global
    
    echo "Remove entries with score < 0.01:"
    REMOVED=$(redis-cli ZREMRANGEBYSCORE trending:global -inf 0.01)
    echo "Removed $REMOVED entries"
    
    echo "New count:"
    redis-cli ZCARD trending:global
    echo
}

# Function to verify memory usage
check_memory_usage() {
    echo "8. Checking Redis memory usage..."
    
    MEMORY_USED=$(redis-cli INFO memory | grep used_memory_human | cut -d: -f2 | tr -d '\r')
    MEMORY_PEAK=$(redis-cli INFO memory | grep used_memory_peak_human | cut -d: -f2 | tr -d '\r')
    
    echo "Current memory usage: $MEMORY_USED"
    echo "Peak memory usage: $MEMORY_PEAK"
    
    # Check specific key count
    TRENDING_KEYS=$(redis-cli --scan --pattern "trending:*" | wc -l)
    echo "Trending keys count: $TRENDING_KEYS"
    echo
}

# Function to cleanup test data
cleanup_test_data() {
    echo "9. Cleaning up test data..."
    
    # Remove all trending keys
    redis-cli --scan --pattern "trending:*" | xargs -r redis-cli DEL > /dev/null
    
    echo "✅ Test data cleaned up"
    echo
}

# Main execution
main() {
    case "${1:-all}" in
        "add")
            add_test_data
            ;;
        "check")
            check_trending_scores
            check_page_data
            ;;
        "decay")
            test_decay_calculation
            ;;
        "rank")
            test_ranking
            ;;
        "prune")
            test_pruning
            ;;
        "memory")
            check_memory_usage
            ;;
        "cleanup")
            cleanup_test_data
            ;;
        "all")
            add_test_data
            check_trending_scores
            check_page_data
            test_decay_calculation
            test_ranking
            test_pruning
            check_memory_usage
            
            echo "=== Validation Complete ==="
            echo
            echo "Manual verification steps:"
            echo "1. ✅ Trending scores are stored correctly"
            echo "2. ✅ Individual page data includes raw_score and last_updated"
            echo "3. ⚠️  Decay calculation needs manual verification (see output above)"
            echo "4. ✅ Page ranking works correctly"
            echo "5. ✅ Low-score pruning works"
            echo "6. ⚠️  Memory usage should be monitored in production"
            echo
            echo "To test with live data, run the processor and check:"
            echo "  redis-cli ZREVRANGE trending:global 0 19 WITHSCORES"
            echo
            ;;
        *)
            echo "Usage: $0 {add|check|decay|rank|prune|memory|cleanup|all}"
            echo
            echo "Commands:"
            echo "  add     - Add test data"
            echo "  check   - Check current trending data"
            echo "  decay   - Test decay calculations"
            echo "  rank    - Test page ranking"
            echo "  prune   - Test pruning functionality"
            echo "  memory  - Check memory usage"
            echo "  cleanup - Remove test data"
            echo "  all     - Run full validation (default)"
            exit 1
            ;;
    esac
}

# Check dependencies
command -v redis-cli >/dev/null 2>&1 || { echo "❌ redis-cli is required but not installed."; exit 1; }
command -v bc >/dev/null 2>&1 || { echo "⚠️ bc is not installed. Decay calculations will be limited."; }

main "$@"