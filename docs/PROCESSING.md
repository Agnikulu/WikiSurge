# WikiSurge Processing Pipeline

## Overview

The WikiSurge processor consumes Wikipedia edit events from Kafka and processes them through four parallel consumers:

1. **Spike Detector** — detects sudden bursts of editing activity on pages
2. **Trending Aggregator** — maintains real-time trending page rankings
3. **Edit War Detector** — identifies editorial conflicts between users
4. **Selective Indexer** — indexes significant edits to Elasticsearch

All consumers share Redis and Elasticsearch infrastructure, and are coordinated by a central orchestrator with health monitoring and graceful shutdown.

---

## Architecture

```
                     ┌─────────────────────┐
                     │   Kafka Topic        │
                     │   wikipedia.edits    │
                     └────┬──┬──┬──┬───────┘
                          │  │  │  │
           ┌──────────────┘  │  │  └──────────────┐
           │                 │  │                  │
     ┌─────▼─────┐   ┌──────▼──▼──┐   ┌──────────▼──────────┐
     │  Spike     │   │  Trending  │   │  Edit War           │
     │  Detector  │   │  Aggregator│   │  Detector           │
     │  (group:   │   │  (group:   │   │  (group:            │
     │  spike-    │   │  trending- │   │  edit-war-          │
     │  detector) │   │  aggregator│   │  detector)          │
     └─────┬──────┘   └─────┬──────┘   └──────────┬──────────┘
           │                │                      │
           ▼                ▼                      ▼
     ┌──────────┐    ┌──────────┐          ┌──────────┐
     │  Redis   │    │  Redis   │          │  Redis   │
     │  Hot     │    │  Sorted  │          │  Editor  │
     │  Pages   │    │  Set     │          │  Tracking│
     │  + Alerts│    │  (decay) │          │  + Alerts│
     └──────────┘    └──────────┘          └──────────┘

           ┌──────────────────────┐
           │  Selective Indexer   │
           │  (group: es-indexer) │
           └──────────┬───────────┘
                      │
                      ▼
           ┌──────────────────────┐
           │   Elasticsearch      │
           │   (bulk indexing)    │
           └──────────────────────┘
```

## Consumer Details

### Spike Detector

- **Consumer Group**: `spike-detector`
- **Purpose**: Detect sudden bursts of editing activity
- **Storage**: Redis hot page tracker + `alerts:spikes` stream
- **How it works**:
  1. Each edit increments an activity counter in Redis
  2. Pages exceeding the promotion threshold become "hot"
  3. Hot pages get detailed time-window tracking (5min, 1hr)
  4. Spike ratio = (edits/min in last 5min) / (edits/min in last hour)
  5. If ratio exceeds threshold (default 5.0), a spike alert is published
- **Metrics**: `spikes_detected_total`, `processed_edits_total`, `spike_detection_processing_seconds`

### Trending Aggregator

- **Consumer Group**: `trending-aggregator`
- **Purpose**: Maintain real-time trending page rankings using exponential decay
- **Storage**: Redis sorted set with lazy decay scoring
- **How it works**:
  1. Each edit adds a score to the page in Redis sorted set
  2. Scores decay with a configurable half-life (default 30 minutes)
  3. Periodic pruning removes pages that have decayed below threshold
  4. Top-N pages can be queried at any time
- **Metrics**: `trending_edits_processed_total`, `trending_process_errors_total`

### Edit War Detector

- **Consumer Group**: `edit-war-detector`
- **Purpose**: Identify ongoing editorial conflicts
- **Storage**: Redis hash (per-page editor tracking) + `alerts:editwars` stream
- **How it works**:
  1. Only processes edits for "hot" pages (registered via hot page tracker)
  2. Tracks per-editor edit counts using Redis HINCRBY
  3. Detects reverts by analyzing byte change patterns
  4. Triggers alert when: unique editors >= 2, total edits >= 5, reverts >= 2
- **Metrics**: `edit_war_detections_total`, `edit_war_processed_edits_total`

### Selective Indexer

- **Consumer Group**: `elasticsearch-indexer`
- **Purpose**: Index only significant edits to Elasticsearch (not all traffic)
- **Storage**: Elasticsearch with bulk indexing
- **How it works**:
  1. For each edit, queries the `IndexingStrategy` to decide whether to index
  2. Strategy checks: trending rank, spike status, edit war status, watchlist
  3. Documents that pass are buffered in a channel
  4. Background goroutine flushes batches to ES bulk API
- **Indexing Criteria**:
  - Page is in trending top-N
  - Page is currently spiking (ratio >= threshold)
  - Page has an active edit war
  - Page is on the watchlist
- **Metrics**: `indexer_edits_received_total`, `indexer_edits_indexed_total`, `indexer_edits_skipped_total`

---

## Resource Management

### Redis Memory

- **Max Hot Pages**: Configurable via `redis.hot_pages.max_tracked` (default 1000)
- **Promotion Circuit Breaker**: Rejects new hot page promotions when limit reached
- **Activity Counter TTL**: Keys auto-expire based on `window_duration`
- **Cleanup Goroutine**: Runs periodically to remove expired hot pages
- **Trending Pruning**: Removes decayed pages to keep sorted set bounded

### Elasticsearch Storage

- **Selective Indexing**: Only ~5-10% of edits are indexed (trending/spiking/war pages)
- **ILM Policy**: Automatic index lifecycle management with configurable retention
- **Daily Indices**: `wikipedia-edits-YYYY-MM-DD` pattern for easy cleanup
- **Retention**: Old indices deleted after `retention_days` (default 7)

### Kafka Offsets

- Each consumer group maintains independent offsets
- Auto-commit interval: 1 second (configurable)
- Consumers start from `FirstOffset` on initial run
- Offset management is per-partition for parallel consumption

---

## Configuration

```yaml
# configs/config.dev.yaml
features:
  elasticsearch_indexing: true
  trending: true
  edit_wars: true

redis:
  url: "redis://localhost:6379"
  max_memory: "256mb"
  hot_pages:
    max_tracked: 1000          # Max hot pages tracked simultaneously
    promotion_threshold: 5     # Edits needed before promotion
    window_duration: 15m       # Time window for activity tracking
    max_members_per_page: 100  # Max edit entries per hot page
    hot_threshold: 2           # Threshold to consider page "hot"
    cleanup_interval: 5m       # How often to clean up expired pages
  trending:
    enabled: true
    max_pages: 1000            # Max pages in trending set
    half_life_minutes: 30.0    # Score decay half-life
    prune_interval: 5m         # How often to prune low-score pages

elasticsearch:
  enabled: true
  url: "http://localhost:9200"
  retention_days: 7
  max_docs_per_day: 10000
  selective_criteria:
    trending_top_n: 100        # Index edits for top N trending pages
    spike_ratio_min: 2.0       # Min spike ratio to trigger indexing
    edit_war_enabled: true     # Index edits for pages with edit wars

kafka:
  brokers:
    - "localhost:19092"
  consumer_group: "wikisurge-dev"
```

---

## Monitoring Guide

### Grafana Dashboard

Import `monitoring/processing-dashboard.json` into Grafana. The dashboard includes:

| Panel                         | What to Watch                                    |
|-------------------------------|--------------------------------------------------|
| Processing Rate by Consumer   | All consumers should process at equal rates       |
| Kafka Consumer Lag            | Should stay near 0; alert if > 1000               |
| Hot Pages Tracked             | Should stay under `max_tracked` limit             |
| Trending Pages Count          | Should stay under `max_pages` limit               |
| Indexing Rate                 | Should be much lower than total processing rate   |
| Alert Rates                   | Spike and edit war detection counts               |
| Error Rates                   | Should be near 0; alert if > 0.1/sec              |
| Resource Usage                | Redis memory and ES index size                    |
| Component Health              | All components should show HEALTHY                |
| Processing Latency            | p99 should be < 5 seconds                         |

### Key Metrics

```
# Consumer throughput
rate(processed_edits_total[1m])
rate(trending_edits_processed_total[1m])
rate(edit_war_processed_edits_total[1m])
rate(indexer_edits_received_total[1m])

# Consumer lag
kafka_consumer_lag

# Detection rates
rate(spikes_detected_total[1m])
rate(edit_war_detections_total[1m])

# Error rates
rate(processing_errors_total[1m])
rate(processor_component_failures_total[1m])

# Resource usage
redis_memory_bytes
elasticsearch_index_size_bytes
hot_pages_tracked
trending_pages_total
```

### Health Endpoints

| Endpoint            | Purpose                                           |
|---------------------|---------------------------------------------------|
| `GET /health`       | Detailed health status of all components           |
| `GET /ready`        | Kubernetes readiness probe (checks Redis)          |
| `GET /metrics`      | Prometheus metrics endpoint                        |

---

## Troubleshooting

### High Consumer Lag

**Symptoms**: `kafka_consumer_lag` > 1000 for any consumer

**Causes**:
- Processing too slow (check `processing_duration_seconds`)
- Redis latency spike (check Redis `slowlog`)
- Burst of edits (transient, should recover)

**Solutions**:
1. Check Redis connection: `redis-cli ping`
2. Check processor logs for errors
3. Increase `max_poll_records` in Kafka config
4. Scale horizontally by adding consumer instances (partition-based)
5. If persistent, check for hot key contention in Redis

### Memory Issues

**Symptoms**: `redis_memory_bytes` growing, OOM kills

**Causes**:
- Too many hot pages tracked
- Trending set not pruning
- Activity counters not expiring

**Solutions**:
1. Reduce `hot_pages.max_tracked`
2. Decrease `trending.prune_interval`
3. Check `hot_pages.cleanup_interval` is running
4. Verify Redis `maxmemory-policy` is set to `allkeys-lru`
5. Run `redis-cli info memory` to diagnose

### Indexing Failures

**Symptoms**: `indexer_index_errors_total` increasing, documents not appearing in ES

**Causes**:
- Elasticsearch unreachable
- Index buffer full (check `indexer_buffer_full_drops_total`)
- Bulk API failures (check ES logs)
- ILM policy issues

**Solutions**:
1. Check ES health: `curl localhost:9200/_cluster/health`
2. Check indexer buffer: look for "buffer full" warnings
3. Review ES bulk response errors in processor logs
4. Verify ILM policy: `curl localhost:9200/_ilm/policy/wikipedia-edits-policy`
5. Check disk space on ES nodes

### Missing Detections

**Symptoms**: Known spikes/edit wars not detected

**Causes**:
- Page not promoted to hot tracking (below `promotion_threshold`)
- Spike ratio threshold too high
- Edit war minimum thresholds too strict
- Time window too narrow

**Solutions**:
1. Lower `hot_pages.promotion_threshold` (default 5)
2. Check if page appears in activity counters: `redis-cli get activity:<page>`
3. Verify spike ratio threshold in detector config
4. For edit wars: check `minEdits`, `minEditors`, `minReverts`
5. Widen `hot_pages.window_duration` if edits are spread over longer periods

### Graceful Shutdown Issues

**Symptoms**: Data loss on restart, or process hangs during shutdown

**Solutions**:
1. Ensure SIGTERM is sent (not SIGKILL)
2. Check shutdown timeout (default 30s) is sufficient
3. Look for "shutdown complete" in logs
4. If hanging: check for stuck goroutines in consumer loops
5. Verify ES bulk buffer flushes on stop

---

## Performance Tuning

### Throughput Optimization

| Parameter                    | Effect                         | Recommendation        |
|------------------------------|--------------------------------|-----------------------|
| `kafka.max_poll_records`     | Messages per fetch             | 500-1000              |
| `kafka.session_timeout`      | Consumer group timeout         | 30s                   |
| `CommitInterval`             | Offset commit frequency        | 1s (balance latency)  |
| `MaxWait`                    | Max wait for messages          | 500ms                 |
| ES `bulkSize`                | Documents per bulk request     | 500                   |
| ES `flushInterval`           | Max wait before flush          | 5s                    |

### Latency Optimization

- Lower `CommitInterval` for faster offset commits
- Reduce `MaxWait` for lower message fetch latency
- Use smaller `bulkSize` for faster ES indexing (at cost of throughput)
- Set `hot_pages.cleanup_interval` higher to reduce Redis load

### Resource Optimization

- Set `max_tracked` appropriately for available Redis memory
- Use `trending.max_pages` to cap trending set size
- Enable selective indexing to reduce ES storage (default: only 5-10% indexed)
- Set `retention_days` based on available disk

---

## Running

### Start Full System

```bash
# Start infrastructure
docker-compose up -d

# Start ingestor (feeds Kafka)
go run cmd/ingestor/main.go &

# Start processor (all consumers)
go run cmd/processor/main.go &

# Verify
curl localhost:2112/metrics
curl localhost:2112/health
```

### Run Tests

```bash
# Unit tests
go test ./internal/... -v

# Integration tests
go test ./test/integration/... -v

# Resource limit tests
go test ./test/resource/... -v

# Benchmarks
go test ./test/benchmark/... -bench=. -benchtime=5s

# All tests
make test
```

### Validate

```bash
# Let system run for 30 minutes, then:
curl localhost:2112/metrics | grep -E '(processed_edits|spikes_detected|edit_war|kafka_consumer_lag|hot_pages)'

# Check Grafana
open http://localhost:3000
# Dashboard: "WikiSurge Processing Pipeline"
```

---

## Success Criteria

- [ ] All consumers run in parallel
- [ ] No race conditions or deadlocks
- [ ] Graceful shutdown works
- [ ] Integration tests pass
- [ ] Resource limits respected
- [ ] Failure recovery works
- [ ] Performance benchmarks pass
- [ ] Kafka lag stays near 0
- [ ] Redis memory bounded
- [ ] ES storage efficient
- [ ] Monitoring dashboard complete
- [ ] Documentation accurate
- [ ] System stable for 1+ hour continuous operation
