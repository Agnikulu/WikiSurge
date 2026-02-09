# WikiSurge Monitoring Guide

## Table of Contents
- [Metrics Overview](#metrics-overview)
- [Dashboards](#dashboards)
- [Alerts](#alerts)
- [Log Analysis](#log-analysis)
- [Performance Tuning](#performance-tuning)

---

## Metrics Overview

### Key Performance Indicators (KPIs)

| Metric | Target | Warning | Critical | Description |
|--------|--------|---------|----------|-------------|
| Ingestion Rate | 500-5000/s | <100/s | <10/s | Edits ingested per second |
| Processing Lag | <5s | 5-30s | >60s | Time behind real-time |
| API Latency (p95) | <200ms | 200-500ms | >1s | 95th percentile response time |
| Error Rate | <0.1% | 0.1-1% | >1% | HTTP 5xx errors |
| Hot Pages Tracked | 200-800 | 800-950 | >950 | Active hot pages |
| WebSocket Clients | 0-1000 | 1000-1500 | >1500 | Connected WSclients |
| Memory Usage | <60% | 60-80% | >85% | RAM utilization |
| Disk Usage | <70% | 70-85% | >90% | Storage utilization |

### Prometheus Metrics

#### Ingestor Metrics

```promql
# Ingestion rate
rate(ingested_edits_total[1m])

# Connection status
wikisurge_connection_status

# Validation errors
rate(validation_errors_total[5m])

# Kafka producer lag
kafka_producer_record_send_total - kafka_producer_record_success_total
```

**Key metrics:**
- `ingested_edits_total` - Total edits ingested (counter)
- `wikisurge_connection_status` - SSE connection status (gauge, 1=connected)
- `validation_errors_total` - Failed validations (counter)
- `kafka_produce_duration_seconds` - Time to produce to Kafka (histogram)

#### Processor Metrics

```promql
# Processing rate per detector
rate(processed_edits_total{detector="spike"}[1m])
rate(processed_edits_total{detector="edit_war"}[1m])
rate(processed_edits_total{detector="trending"}[1m])

# Consumer lag
kafka_consumer_lag_seconds

# Hot pages tracked
hot_pages_tracked

# Alerts published
rate(alerts_published_total[5m])
```

**Key metrics:**
- `processed_edits_total{detector}` - Edits processed per detector (counter)
- `kafka_consumer_lag_seconds` - Seconds behind Kafka (gauge)
- `hot_pages_tracked` - Current hot pages count (gauge)
- `edit_wars_detected_total{severity}` - Edit wars by severity (counter)
- `alerts_published_total{type}` - Alerts published (counter)

#### API Metrics

```promql
# Request rate
rate(http_requests_total[1m])

# Latency percentiles
histogram_quantile(0.95, http_request_duration_seconds_bucket)
histogram_quantile(0.99, http_request_duration_seconds_bucket)

# Error rate
rate(http_requests_total{status=~"5.."}[1m]) / rate(http_requests_total[1m])

# WebSocket connections
websocket_clients_total
```

**Key metrics:**
- `http_requests_total{method,endpoint,status}` - HTTP requests (counter)
- `http_request_duration_seconds` - Request latency (histogram)
- `websocket_clients_total` - Active WebSocket clients (gauge)
- `cache_hits_total` - Cache hit rate (counter)
- `rate_limit_exceeded_total` - Rate-limited requests (counter)

#### Infrastructure Metrics

**Kafka:**
```promql
# Broker metrics
kafka_server_brokertopicmetrics_messagesinpersec
kafka_server_brokertopicmetrics_bytesinpersec

# Consumer group lag
kafka_consumergroup_lag
```

**Redis:**
```promql
# Memory usage
redis_memory_used_bytes / redis_memory_max_bytes

# Commands per second
rate(redis_commands_processed_total[1m])

# Hit rate
rate(redis_keyspace_hits_total[1m]) / (rate(redis_keyspace_hits_total[1m]) + rate(redis_keyspace_misses_total[1m]))
```

**Elasticsearch:**
```promql
# Indexing rate
rate(elasticsearch_indices_indexing_index_total[1m])

# Search latency
elasticsearch_indices_search_query_time_seconds / elasticsearch_indices_search_query_total

# Disk usage
elasticsearch_filesystem_data_size_bytes - elasticsearch_filesystem_data_free_bytes
```

---

## Dashboards

### System Overview Dashboard

**Purpose:** High-level system health and performance.

**Panels:**

1. **Status Summary (Stat panels)**
   - Ingestion Status: 🟢 Connected / 🔴 Disconnected
   - Processing Status: 🟢 Running / 🔴 Stopped
   - API Status: 🟢 Healthy / ⚠️ Degraded / 🔴 Down
   - Overall Health: Calculated from above

2. **Throughput (Time series)**
   ```promql
   # Edits per second
   rate(ingested_edits_total[1m])
   ```
   - Target line at 500/s
   - Warning zone above 8000/s

3. **Processing Lag (Time series)**
   ```promql
   kafka_consumer_lag_seconds{group="processor"}
   ```
   - Target: <5s
   - Alert threshold: 60s

4. **Active Alerts (Bar chart)**
   ```promql
   sum by (type) (alerts_active)
   ```
   - Grouped by: spikes, edit_wars, trending

5. **Hot Pages Tracked (Gauge)**
   ```promql
   hot_pages_tracked
   ```
   - Max: 1000 (circuit breaker)
   - Warning: 80% (800)

6. **Resource Usage (Time series)**
   ```promql
   # CPU
   rate(process_cpu_seconds_total[1m]) * 100
   
   # Memory
   process_resident_memory_bytes / 1024 / 1024 / 1024
   ```

**Access:** `http://grafana:3000/d/system-overview`

---

### Ingestion Dashboard

**Purpose:** Monitor Wikipedia stream ingestion and Kafka production.

**Panels:**

1. **Connection Status (Stat)**
   ```promql
   wikisurge_connection_status{service="ingestor"}
   ```

2. **Ingestion Rate (Time series)**
   ```promql
   rate(ingested_edits_total[1m])
   ```
   - Show: Current, Min, Max, Average

3. **Edits by Type (Pie chart)**
   ```promql
   increase(ingested_edits_total{type=~"edit|new|log"}[5m])
   ```

4. **Edits by Wiki (Table)**
   ```promql
   topk(20, increase(ingested_edits_total[5m]) by (wiki))
   ```
   - Top 20 most active wikis

5. **Validation Errors (Time series)**
   ```promql
   rate(validation_errors_total[1m])
   ```
   - Should be near zero

6. **Kafka Producer Performance (Time series)**
   ```promql
   # Batch size
   kafka_producer_batch_size_avg
   
   # Latency
   kafka_producer_record_send_latency_avg
   ```

7. **Reconnection Events (Time series)**
   ```promql
   increase(ingestor_reconnections_total[5m])
   ```
   - Spike indicates connection issues

**Access:** `http://grafana:3000/d/ingestion`

---

### Processing Dashboard

**Purpose:** Monitor edit processing and alert generation.

**Panels:**

1. **Consumer Lag by Group (Time series)**
   ```promql
   kafka_consumer_lag_seconds
   ```
   - One series per consumer group

2. **Processing Rate by Detector (Time series)**
   ```promql
   rate(processed_edits_total[1m]) by (detector)
   ```
   - Lines for: spike, edit_war, trending, indexer

3. **Spike Detections (Time series)**
   ```promql
   rate(spikes_detected_total[1m]) by (severity)
   ```
   - Stacked by severity

4. **Edit Wars Active (Stat + Time series)**
   ```promql
   edit_wars_active
   ```

5. **Trending Pages (Gauge)**
   ```promql
   sum(trending_pages_count) by (language)
   ```

6. **Elasticsearch Indexing (Time series)**
   ```promql
   rate(elasticsearch_indexed_total[1m])
   ```
   - Compared to filter rate

7. **Hot Page Circuit Breaker (Gauge)**
   ```promql
   hot_pages_tracked / 1000 * 100
   ```
   - Shows % of limit

8. **Processing Duration (Heatmap)**
   ```promql
   rate(processing_duration_seconds_bucket[5m])
   ```
   - Shows latency distribution

**Access:** `http://grafana:3000/d/processing`

---

### API Dashboard

**Purpose:** Monitor API performance and user experience.

**Panels:**

1. **Request Rate (Time series)**
   ```promql
   rate(http_requests_total[1m]) by (endpoint)
   ```

2. **Latency Percentiles (Time series)**
   ```promql
   histogram_quantile(0.50, rate(http_request_duration_seconds_bucket[5m]))
   histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))
   histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))
   ```
   - P50, P95, P99

3. **Error Rate (Time series)**
   ```promql
   rate(http_requests_total{status=~"5.."}[1m]) / rate(http_requests_total[1m]) * 100
   ```
   - Target: <0.1%

4. **Status Code Distribution (Pie chart)**
   ```promql
   increase(http_requests_total[5m]) by (status)
   ```

5. **Top Endpoints (Table)**
   ```promql
   topk(10, rate(http_requests_total[5m]) by (endpoint))
   ```

6. **WebSocket Connections (Time series)**
   ```promql
   websocket_clients_total
   ```

7. **Cache Performance (Time series)**
   ```promql
   # Hit rate
   rate(cache_hits_total[1m]) / (rate(cache_hits_total[1m]) + rate(cache_misses_total[1m]))
   ```

8. **Rate Limiting (Time series)**
   ```promql
   rate(rate_limit_exceeded_total[1m]) by (ip)
   ```

**Access:** `http://grafana:3000/d/api`

---

## Alerts

### Alert Definitions

Alerts defined in `monitoring/alert-rules.yml`:

```yaml
groups:
- name: wikisurge_critical
  interval: 30s
  rules:
  - alert: IngestorDown
    expr: up{job="ingestor"} == 0
    for: 2m
    labels:
      severity: critical
    annotations:
      summary: "Ingestor is down"
      description: "Ingestor has been down for more than 2 minutes"
      
  - alert: HighProcessingLag
    expr: kafka_consumer_lag_seconds > 60
    for: 5m
    labels:
      severity: critical
    annotations:
      summary: "Processing lag is high"
      description: "Consumer lag is {{ $value }}s (>60s threshold)"
      
  - alert: APIHighErrorRate
    expr: rate(http_requests_total{status=~"5.."}[5m]) / rate(http_requests_total[5m]) > 0.01
    for: 5m
    labels:
      severity: critical
    annotations:
      summary: "API error rate is high"
      description: "Error rate is {{ $value | humanizePercentage }}"

- name: wikisurge_warning
  interval: 1m
  rules:
  - alert: HighMemoryUsage
    expr: process_resident_memory_bytes / node_memory_MemTotal_bytes > 0.80
    for: 10m
    labels:
      severity: warning
    annotations:
      summary: "High memory usage"
      description: "Memory usage is {{ $value | humanizePercentage }}"
      
  - alert: HighDiskUsage
    expr: (node_filesystem_size_bytes - node_filesystem_free_bytes) / node_filesystem_size_bytes > 0.85
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "High disk usage"
      description: "Disk usage is {{ $value | humanizePercentage }}"
      
  - alert: HotPageCircuitBreakerNearLimit
    expr: hot_pages_tracked > 900
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "Hot page circuit breaker near limit"
      description: "Tracking {{ $value }} hot pages (limit: 1000)"
```

### Alert Severity Levels

**Critical** (immediate action required):
- Service down
- High error rate (>1%)
- Processing lag >60s
- Data loss detected

**High** (action required within 1 hour):
- Service degraded
- Error rate 0.5-1%
- Processing lag 30-60s
- Circuit breaker triggered

**Medium** (investigate during business hours):
- Resource usage high (>80%)
- Unusual traffic patterns
- Reconnection events
- Performance degradation

**Low** (informational):
- Capacity planning alerts
- Trend anomalies
- Non-critical warnings

### Response Procedures

#### IngestorDown

**Symptoms:**
- Dashboard shows ingestor offline
- No new edits flowing
- Pagerduty alarm

**Diagnosis:**
```bash
# Check service status
docker-compose ps ingestor
systemctl status wikisurge-ingestor

# Check logs
docker-compose logs --tail=100 ingestor

# Test Wikipedia stream
curl -N https://stream.wikimedia.org/v2/stream/recentchange
```

**Resolution:**
```bash
# Restart ingestor
docker-compose restart ingestor

# Or manually
./bin/ingestor --config configs/config.yaml &

# Verify reconnection
curl http://localhost:8081/health
```

**Post-resolution:**
- Monitor ingestion rate returns to normal
- Check for any data gaps
- Review logs for root cause

---

#### HighProcessingLag

**Symptoms:**
- Consumer lag >60 seconds
- Dashboard data stale
- Alerts delayed

**Diagnosis:**
```bash
# Check consumer lag details
docker exec kafka kafka-consumer-groups.sh \
  --bootstrap-server localhost:9092 \
  --group processor \
  --describe

# Check processor health
curl http://localhost:2113/health

# Check resource usage
docker stats processor
```

**Resolution:**

**Option 1: Scale up**
```bash
# Add more processor instances
docker-compose up -d --scale processor=3
```

**Option 2: Skip to current**
```bash
# Only if acceptable to skip past data
docker exec kafka kafka-consumer-groups.sh \
  --bootstrap-server localhost:9092 \
  --group processor \
  --reset-offsets \
  --to-latest \
  --topic wikisurge.edits \
  --execute
```

**Option 3: Disable features temporarily**
```yaml
processor:
  features:
    elasticsearch: false  # Disable slow indexing
```

---

#### APIHighErrorRate

**Symptoms:**
- 5xx errors in dashboard
- User complaints
- Elevated error metrics

**Diagnosis:**
```bash
# Check error logs
docker-compose logs api | grep -i error

# Check specific endpoints
curl http://localhost:8080/metrics | grep http_requests_total | grep 5

# Check dependencies
curl http://localhost:8080/health
```

**Resolution:**

**If database issue:**
```bash
# Check Redis
redis-cli PING

# Check Elasticsearch
curl http://localhost:9200/_cluster/health
```

**If timeout issue:**
```yaml
# Increase timeouts
api:
  read_timeout: 30s
  write_timeout: 30s
```

**If memory issue:**
```bash
# Restart API to reclaim memory
docker-compose restart api
```

**If external issue (Wikipedia):**
- Check Wikipedia status
- Enable cached responses
- Show maintenance notice

---

### Escalation Paths

#### Level 1: Automated Response
- Restart unhealthy service
- Scale up if under load
- Enable cached responses

#### Level 2: On-Call Engineer (PagerDuty)
- Critical alerts (service down, high error rate)
- Response time: 15 minutes
- Actions: Diagnose, apply known fixes, escalate if needed

#### Level 3: Senior Engineer
- Complex issues requiring deep investigation
- Data loss or corruption
- Security incidents

#### Level 4: Management
- Prolonged outage (>2 hours)
- Data breach
- Service-wide failure

---

## Log Analysis

### Log Locations

**Docker Compose:**
```bash
# All services
docker-compose logs

# Specific service
docker-compose logs api
docker-compose logs processor
docker-compose logs ingestor
```

**Systemd:**
```bash
# Ingestor
journalctl -u wikisurge-ingestor -f

# Processor
journalctl -u wikisurge-processor -f

# API
journalctl -u wikisurge-api -f
```

**File-based (if configured):**
```bash
/var/log/wikisurge/ingestor.log
/var/log/wikisurge/processor.log
/var/log/wikisurge/api.log
```

---

### Log Formats

**Structured JSON logs:**
```json
{
  "level": "info",
  "timestamp": "2026-02-09T15:30:00Z",
  "service": "processor",
  "component": "spike_detector",
  "message": "Spike detected",
  "page_title": "Breaking News",
  "severity": "high",
  "edit_rate": 15.3,
  "baseline_rate": 2.1
}
```

**Text logs:**
```
2026-02-09 15:30:00 INFO [processor/spike_detector] Spike detected page="Breaking News" severity=high rate=15.3
```

---

### Common Log Patterns

**Normal operation:**
```bash
# Successful processing
grep "processed successfully" /var/log/wikisurge/processor.log

# Alerts published
grep "alert published" /var/log/wikisurge/processor.log
```

**Errors:**
```bash
# All errors
grep -i "error\|failure\|failed" /var/log/wikisurge/*.log

# Specific error types
grep "connection refused" /var/log/wikisurge/ingestor.log
grep "timeout" /var/log/wikisurge/api.log
grep "out of memory" /var/log/wikisurge/processor.log
```

**Performance issues:**
```bash
# Slow operations
grep "slow" /var/log/wikisurge/api.log

# High latency
awk '/request_duration/ && $NF > 1000' /var/log/wikisurge/api.log
```

---

### Log Analysis with Loki

**Query examples:**

```logql
# All errors
{job="wikisurge"} |= "error"

# API 500 errors
{job="api"} | json | status="500"

# Processing lag warnings
{job="processor"} |= "lag" |= "warning"

# Spike detections
{job="processor", component="spike_detector"} | json | severity="high"

# Rate by endpoint
sum(rate({job="api"} | json | status="200" [5m])) by (endpoint)
```

**Accessing Loki:**
```
URL: http://localhost:3100
Grafana datasource: Loki
```

---

## Performance Tuning

### Identifying Bottlenecks

**CPU-bound:**
```promql
# High CPU usage
rate(process_cpu_seconds_total[1m]) > 0.8

# Look for hot loops in profiles
go tool pprof http://localhost:6060/debug/pprof/profile
```

**Memory-bound:**
```promql
# High memory usage
process_resident_memory_bytes / node_memory_MemTotal_bytes > 0.8

# Memory allocations
go tool pprof http://localhost:6060/debug/pprof/heap
```

**I/O-bound:**
```bash
# Disk I/O
iostat -x 1

# Network I/O
iftop
nethogs
```

**Database-bound:**
```bash
# Redis slow commands
redis-cli SLOWLOG GET 10

# Elasticsearch slow queries
curl "localhost:9200/_nodes/stats/indices/search?pretty"
```

---

### Optimization Techniques

#### Reduce Memory Usage

1. **Lower hot page limit:**
```yaml
redis:
  hot_pages:
    max_tracked: 500
```

2. **Increase pruning frequency:**
```yaml
redis:
  hot_pages:
    cleanup_interval: 2m
  trending:
    prune_interval: 2m
```

3. **Disable features:**
```yaml
processor:
  features:
    elasticsearch: false
```

#### Improve Latency

1. **Increase cache TTL:**
```yaml
api:
  cache_ttl: 30s
```

2. **Add database indexes:**
```bash
# Elasticsearch
curl -X PUT "localhost:9200/wikisurge-edits-*/_settings" \
  -d '{"index": {"refresh_interval": "30s"}}'
```

3. **Use connection pooling:**
```yaml
redis:
  pool_size: 100
```

#### Increase Throughput

1. **Batch operations:**
```yaml
kafka:
  batch_size: 1000
  batch_timeout: 100ms
```

2. **Increase parallelism:**
```bash
# More Kafka partitions
docker exec kafka kafka-topics.sh --alter \
  --topic wikisurge.edits --partitions 12

# More consumers
docker-compose up -d --scale processor=3
```

3. **Optimize data structures:**
```go
// Use sync.Map for concurrent access
// Use channels for goroutine communication
// Use object pools for frequent allocations
```

---

For operational procedures, see [OPERATIONS.md](OPERATIONS.md).
For architecture details, see [ARCHITECTURE.md](ARCHITECTURE.md).
