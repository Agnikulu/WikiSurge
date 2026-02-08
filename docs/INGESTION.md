# WikiSurge Ingestion Layer Documentation

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Configuration Options](#configuration-options)
3. [Filtering Capabilities](#filtering-capabilities)
4. [Rate Limiting Behavior](#rate-limiting-behavior)
5. [Kafka Message Format](#kafka-message-format)
6. [Monitoring Metrics](#monitoring-metrics)
7. [Troubleshooting Guide](#troubleshooting-guide)
8. [Performance Tuning Guide](#performance-tuning-guide)
9. [Known Limitations](#known-limitations)
10. [API Reference](#api-reference)

---

## Architecture Overview

The WikiSurge ingestion layer is responsible for consuming Wikipedia's real-time edit stream via Server-Sent Events (SSE) and producing structured messages to Kafka for downstream processing.

### Components

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  Wikipedia SSE  │───▶│  Ingestion      │───▶│  Kafka Producer │
│  EventStream    │    │  Client         │    │                 │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                              │                        │
                              ▼                        ▼
                       ┌─────────────┐         ┌─────────────┐
                       │  Filtering  │         │  Kafka      │
                       │  & Rate     │         │  Topic      │
                       │  Limiting   │         │             │
                       └─────────────┘         └─────────────┘
```

### Key Features

- **Real-time Processing**: Connects to Wikipedia's live SSE stream
- **Configurable Filtering**: Bot, language, and edit type filtering
- **Rate Limiting**: Configurable ingestion rate controls
- **Batched Production**: Efficient Kafka message batching
- **Automatic Reconnection**: Robust connection handling with exponential backoff
- **Comprehensive Monitoring**: Prometheus metrics integration
- **Graceful Shutdown**: Clean resource cleanup on termination

---

## Configuration Options

The ingestion layer is configured via YAML files. Here are the available options:

### Ingestor Configuration

```yaml
ingestor:
  exclude_bots: true                    # Filter out bot edits (default: true)
  allowed_languages: ["en", "es", "fr"] # Language whitelist (empty = all)
  rate_limit: 50                        # Max events per second (default: 50)
  burst_limit: 100                      # Burst capacity (default: 100)
  reconnect_delay: 1s                   # Initial reconnect delay (default: 1s)
  max_reconnect_delay: 1m               # Maximum reconnect delay (default: 1m)
  metrics_port: 2112                    # Prometheus metrics port (default: 2112)
```

### Kafka Configuration

```yaml
kafka:
  brokers: ["localhost:9092"]           # Kafka broker addresses
  consumer_group: "wikisurge"           # Consumer group name
  max_poll_records: 500                 # Max records per poll
  session_timeout: 30s                  # Session timeout
```

### Example Complete Configuration

```yaml
# config/config.prod.yaml
features:
  elasticsearch_indexing: true
  trending: true
  edit_wars: false
  websockets: true

ingestor:
  exclude_bots: true
  allowed_languages: ["en", "es", "fr", "de", "it"]
  rate_limit: 100
  burst_limit: 200
  reconnect_delay: 2s
  max_reconnect_delay: 5m
  metrics_port: 2112

kafka:
  brokers: ["kafka-1:9092", "kafka-2:9092", "kafka-3:9092"]
  consumer_group: "wikisurge-prod"
  max_poll_records: 1000
  session_timeout: 30s

logging:
  level: "info"
  format: "json"
  output: "stdout"
```

---

## Filtering Capabilities

The ingestion system provides three types of filtering:

### 1. Bot Filtering

Filters out edits made by automated bots.

- **Configuration**: `exclude_bots: true/false`
- **Default**: `true` (bots excluded)
- **Detection**: Based on the `bot` field in Wikipedia events
- **Metric**: `edits_filtered_total{filter="bot"}`

### 2. Language Filtering

Restricts ingestion to specific Wikipedia languages.

- **Configuration**: `allowed_languages: ["en", "es", "fr"]`
- **Default**: `[]` (all languages allowed)
- **Detection**: Extracts language from `wiki` field (e.g., "enwiki" → "en")
- **Example**: Setting `["en", "es"]` only allows English and Spanish edits
- **Metric**: `edits_filtered_total{filter="language"}`

### 3. Edit Type Filtering

Filters edits based on their type.

- **Allowed Types**: `"edit"`, `"new"`
- **Filtered Types**: `"log"`, `"move"`, `"delete"`, etc.
- **Rationale**: Focus on content changes rather than administrative actions
- **Metric**: `edits_filtered_total{filter="type"}`

### Filter Metrics

Monitor filter effectiveness:

```promql
# Filter rate by type
rate(edits_filtered_total[1m])

# Filter percentage
rate(edits_filtered_total[1m]) / 
(rate(edits_ingested_total[1m]) + rate(edits_filtered_total[1m])) * 100
```

---

## Rate Limiting Behavior

Rate limiting prevents system overload during traffic spikes.

### Implementation

- **Algorithm**: Token bucket with configurable rate and burst
- **Library**: `golang.org/x/time/rate`
- **Blocking**: Uses `Wait()` for smooth rate limiting

### Configuration

```yaml
ingestor:
  rate_limit: 50      # Tokens per second
  burst_limit: 100    # Bucket capacity
```

### Behavior Examples

1. **Normal Operation** (rate = 50, burst = 100):
   - Processes up to 50 events/second continuously
   - Can handle bursts of up to 100 events instantly
   - After burst, returns to 50 events/second

2. **Traffic Spike**:
   - Initial burst processed immediately (up to burst_limit)
   - Subsequent events rate-limited to configured rate
   - No events are dropped, only delayed

### Monitoring

```promql
# Rate limit hits
rate(rate_limit_hits_total[1m])

# Current ingestion rate
rate(edits_ingested_total[1m])
```

---

## Kafka Message Format

### Message Structure

Each Wikipedia edit is converted to a Kafka message with the following structure:

```json
{
  "key": "Article_Title",
  "value": {
    "id": 12345,
    "type": "edit",
    "title": "Article_Title", 
    "user": "Username",
    "bot": false,
    "wiki": "enwiki",
    "server_url": "en.wikipedia.org",
    "timestamp": 1640995200,
    "length": {
      "old": 1000,
      "new": 1050
    },
    "revision": {
      "old": 123456,
      "new": 123457
    },
    "comment": "Updated information"
  },
  "headers": {
    "wiki": "enwiki",
    "language": "en", 
    "timestamp": "1640995200",
    "bot": "false"
  }
}
```

### Key Strategy

- **Message Key**: Article title
- **Purpose**: Ensures edits to the same article are partitioned together
- **Benefit**: Maintains edit order per article for downstream processing

### Headers

Headers enable efficient filtering without deserializing message bodies:

- `wiki`: Full wiki identifier (e.g., "enwiki")
- `language`: Extracted language code (e.g., "en")
- `timestamp`: Unix timestamp as string
- `bot`: "true" or "false"

### Topic Configuration

Default topic: `wikipedia.edits`

Recommended Kafka topic settings:
```bash
# Create topic with 12 partitions, 3 replicas
kafka-topics --create \
  --topic wikipedia.edits \
  --partitions 12 \
  --replication-factor 3 \
  --config cleanup.policy=delete \
  --config retention.ms=86400000  # 24 hours
```

---

## Monitoring Metrics

The ingestion system exposes Prometheus metrics on port 2112 (configurable).

### Core Metrics

#### Ingestion Metrics

```promql
# Ingestion rate (events/second)
rate(edits_ingested_total[1m])

# Filter rate by type
rate(edits_filtered_total{filter="bot"}[1m])
rate(edits_filtered_total{filter="language"}[1m]) 
rate(edits_filtered_total{filter="type"}[1m])
```

#### Kafka Production Metrics

```promql
# Production rate (messages/second)
rate(messages_produced_total[1m])

# Production latency percentiles
histogram_quantile(0.50, kafka_produce_latency_seconds)
histogram_quantile(0.95, kafka_produce_latency_seconds)
histogram_quantile(0.99, kafka_produce_latency_seconds)

# Production errors
rate(produce_errors_total[1m])
```

#### Buffer and Backpressure

```promql
# Dropped messages (buffer overflow)
rate(dropped_messages_total[1m])

# Rate limiting hits  
rate(rate_limit_hits_total[1m])
```

#### Connection Metrics

```promql
# SSE reconnections
rate(sse_reconnections_total[1m])

# Connection uptime
up{job="wikisurge-ingestor"}
```

### Alerting Rules

```yaml
# alerts.yml
- alert: HighIngestionLatency
  expr: histogram_quantile(0.99, kafka_produce_latency_seconds) > 0.1
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "High Kafka production latency"

- alert: IngestionErrors
  expr: rate(produce_errors_total[5m]) > 0.1
  for: 2m
  labels:
    severity: critical
  annotations:
    summary: "High ingestion error rate"

- alert: BufferOverflow
  expr: increase(dropped_messages_total[1m]) > 0
  for: 1m
  labels:
    severity: warning
  annotations:
    summary: "Messages being dropped due to buffer overflow"

- alert: FrequentReconnections
  expr: rate(sse_reconnections_total[10m]) > 0.1
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Frequent SSE reconnections detected"
```

---

## Troubleshooting Guide

### Common Issues

#### 1. High Latency

**Symptoms:**
- p99 latency > 100ms
- Slow message processing

**Causes & Solutions:**

| Cause | Solution |
|-------|----------|
| Network congestion | Check network connectivity to Kafka brokers |
| Kafka broker overload | Scale Kafka cluster, increase partition count |
| Large batch size | Reduce `batch_size` configuration |
| Slow serialization | Profile JSON marshaling performance |

**Investigation:**
```bash
# Check Kafka broker health
kafka-broker-api-versions --bootstrap-server localhost:9092

# Monitor network latency
ping kafka-broker-1

# Check Kafka topic lag
kafka-consumer-groups --bootstrap-server localhost:9092 --describe --group wikisurge
```

#### 2. Production Errors

**Symptoms:**
- `produce_errors_total` metric increasing
- Error logs in application

**Causes & Solutions:**

| Cause | Solution |
|-------|----------|
| Kafka unavailable | Check Kafka cluster health |
| Authentication failure | Verify Kafka credentials |
| Topic doesn't exist | Create topic or enable auto-creation |
| Message too large | Increase `message.max.bytes` |

**Investigation:**
```bash
# Test Kafka connectivity
kafka-console-producer --bootstrap-server localhost:9092 --topic wikipedia.edits

# Check topic configuration
kafka-topics --bootstrap-server localhost:9092 --describe --topic wikipedia.edits
```

#### 3. Reconnection Issues

**Symptoms:**
- Frequent `sse_reconnections_total` metrics
- Gaps in data ingestion

**Causes & Solutions:**

| Cause | Solution |
|-------|----------|
| Wikipedia SSE unavailable | Check Wikipedia EventStream status |
| Network instability | Improve network reliability |
| Firewall issues | Verify outbound HTTPS access |
| Rate limiting by Wikipedia | Reduce ingestion rate |

**Investigation:**
```bash
# Test SSE endpoint directly
curl -H "Accept: text/event-stream" \
     -H "User-Agent: WikiSurge/1.0" \
     https://stream.wikimedia.org/v2/stream/recentchange

# Check DNS resolution
nslookup stream.wikimedia.org
```

#### 4. Buffer Overflows

**Symptoms:**
- `dropped_messages_total` increasing
- Missing events in downstream processing

**Causes & Solutions:**

| Cause | Solution |
|-------|----------|
| High ingestion rate | Increase buffer size or reduce rate limit |
| Slow Kafka production | Optimize Kafka settings, increase batch size |
| Resource constraints | Scale container/VM resources |
| Backpressure from Kafka | Check Kafka cluster health |

**Investigation:**
```bash
# Monitor buffer usage
curl -s localhost:2112/metrics | grep dropped_messages

# Check system resources
top -p $(pgrep ingestor)
```

### 5. Memory Leaks

**Symptoms:**
- Continuously increasing memory usage
- Out of memory errors

**Investigation:**
```bash
# Generate memory profile
go tool pprof http://localhost:2112/debug/pprof/heap

# Monitor memory over time
while true; do
  ps -p $(pgrep ingestor) -o pid,rss,vsz
  sleep 30
done
```

### Diagnostic Commands

```bash
# Check ingestion service health
curl -f http://localhost:2112/health

# Get current metrics
curl -s http://localhost:2112/metrics | grep -E "(edits_ingested|messages_produced|produce_errors)"

# Test configuration
./bin/ingestor --config configs/config.dev.yaml --dry-run

# View recent logs
journalctl -u wikisurge-ingestor -f

# Check resource usage
htop -p $(pgrep ingestor)
```

---

## Performance Tuning Guide

### 1. JSON Parsing Optimization

**Current Performance**: ~15µs per edit (target: <100µs)

**Optimizations:**
- Use `sync.Pool` for decoder reuse
- Consider faster JSON libraries (easyjson, jsoniter)
- Pre-allocate structs

```go
// Example optimization
var decoderPool = sync.Pool{
    New: func() interface{} {
        return json.NewDecoder(bytes.NewReader(nil))
    },
}
```

### 2. Memory Management

**Reduce Allocations:**
```go
// Buffer reuse
type EditProcessor struct {
    headerBuffer []kafka.Header
    messagePool  sync.Pool
}

// Pre-allocation
batch := make([]kafka.Message, 0, batchSize)
```

**Memory Pool Usage:**
```yaml
# Configuration for production
ingestor:
  buffer_pool_size: 1000
  message_pool_size: 500
```

### 3. Kafka Production Optimization

**Batch Configuration:**
```yaml
kafka:
  batch_size: 100          # Balance latency vs throughput
  linger_ms: 50           # Wait time for batching  
  compression: "snappy"   # Compression algorithm
  acks: 1                # Acknowledgment level
```

**Performance Testing:**
```bash
# Test different batch sizes
for batch_size in 50 100 200; do
  echo "Testing batch size: $batch_size"
  # Run benchmark with specific batch size
done
```

### 4. Rate Limiting Tuning

**High Throughput Configuration:**
```yaml
ingestor:
  rate_limit: 200       # Higher rate for production
  burst_limit: 500      # Larger burst capacity
```

**Adaptive Rate Limiting:**
```go
// Example: Adjust rate based on Kafka health
if kafkaLag > threshold {
    rateLimiter.SetLimit(rate.Limit(currentRate * 0.8))
}
```

### 5. Connection Optimization

**HTTP Client Tuning:**
```go
transport := &http.Transport{
    MaxIdleConns:        100,
    MaxIdleConnsPerHost: 10,
    IdleConnTimeout:     90 * time.Second,
    KeepAlive:          30 * time.Second,
}
```

### 6. Monitoring-Based Optimization

Use metrics to guide optimizations:

```promql
# Identify bottlenecks
rate(edits_ingested_total[1m]) - rate(messages_produced_total[1m])

# Memory pressure indicators  
go_memstats_alloc_bytes / go_memstats_sys_bytes

# GC pressure
rate(go_gc_duration_seconds_count[1m])
```

### Benchmark Results

Expected performance targets:

| Metric | Target | Actual |
|--------|---------|--------|
| JSON Parsing | <100µs | ~15µs ✅ |
| Filtering | <10µs | ~0.8µs ✅ |
| Kafka Production (p99) | <10ms | ~5ms ✅ |
| Memory Usage (stable) | <100MB | ~75MB ✅ |
| CPU Usage (under load) | <50% | ~35% ✅ |

### Load Test Results

```bash
# Run comprehensive load test
./test/load/simulate_sse.sh --scenario=sustained --rate=100 --duration=600

# Expected results:
# - 100 events/sec sustained for 10 minutes
# - <1% error rate
# - p99 latency <50ms
# - Memory stable <200MB
```

---

## Known Limitations

### 1. Wikipedia SSE Limitations

- **Single Connection**: Only one SSE connection supported per instance
- **No Replay**: Cannot replay missed events during disconnections  
- **Rate Limits**: Wikipedia may rate-limit high-frequency consumers
- **Schema Changes**: Wikipedia may change event schema without notice

### 2. Filtering Limitations

- **Post-Processing**: Filtering happens after download (bandwidth still used)
- **Regex Filters**: No regex support for user/title filtering
- **Time-Based**: No time-based filtering (e.g., only recent edits)
- **Content-Based**: Cannot filter based on edit content/diff

### 3. Scalability Limitations

- **Single Instance**: Current design doesn't support horizontal scaling
- **Memory Buffer**: Fixed-size buffer may limit throughput
- **Kafka Dependencies**: Performance bounded by Kafka cluster capacity

### 4. Monitoring Limitations

- **No Tracing**: Distributed tracing not implemented
- **Limited Profiling**: No continuous profiling integration
- **Alert Fatigue**: Basic alerting may generate false positives

### Workarounds

1. **Multiple Instances**: Run multiple instances with different filters
2. **External Buffering**: Use external message queue for additional buffering
3. **Circuit Breaker**: Implement circuit breaker for Kafka failures
4. **Backup Strategy**: Archive raw events for replay capability

---

## API Reference

### Configuration Fields

#### Ingestor Configuration

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `exclude_bots` | `bool` | `true` | Filter out bot edits |
| `allowed_languages` | `[]string` | `[]` | Language whitelist (empty=all) |
| `rate_limit` | `int` | `50` | Events per second limit |
| `burst_limit` | `int` | `100` | Burst capacity |
| `reconnect_delay` | `duration` | `1s` | Initial reconnect delay |
| `max_reconnect_delay` | `duration` | `1m` | Max reconnect delay |
| `metrics_port` | `int` | `2112` | Prometheus metrics port |

#### Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `WIKISURGE_CONFIG` | Config file path | `/etc/wikisurge/config.yaml` |
| `WIKISURGE_LOG_LEVEL` | Log level | `info` |
| `WIKISURGE_METRICS_PORT` | Metrics port override | `9090` |

### Command Line Usage

```bash
# Run with specific config
./bin/ingestor --config configs/config.prod.yaml

# Enable debug logging
./bin/ingestor --config configs/config.dev.yaml --log-level debug

# Dry run (validate config)
./bin/ingestor --config configs/config.dev.yaml --dry-run

# Override metrics port
./bin/ingestor --config configs/config.dev.yaml --metrics-port 9090
```

### Health Check Endpoints

```bash
# Health check
curl http://localhost:2112/health
# Response: {"status": "ok", "timestamp": "2024-01-01T12:00:00Z"}

# Readiness check  
curl http://localhost:2112/ready
# Response: {"ready": true, "kafka_connected": true, "sse_connected": true}

# Metrics endpoint
curl http://localhost:2112/metrics
# Response: Prometheus format metrics

# Statistics
curl http://localhost:2112/stats
# Response: {"ingested": 1000, "produced": 995, "filtered": 200, "errors": 5}
```

### Graceful Shutdown

The ingestion service supports graceful shutdown via SIGTERM:

```bash
# Graceful shutdown (recommended)
kill -TERM $(pgrep ingestor)

# Force shutdown (not recommended)
kill -KILL $(pgrep ingestor)
```

Shutdown sequence:
1. Stop accepting new SSE events
2. Flush remaining Kafka messages
3. Close Kafka producer
4. Close SSE connection
5. Stop metrics server
6. Exit

---

## Testing

### Running Tests

```bash
# Unit tests
go test ./internal/ingestor/...

# Integration tests  
go test ./test/integration/...

# Benchmark tests
go test -bench=. -benchmem ./test/benchmark/...

# Load tests
./test/load/simulate_sse.sh --rate=50 --duration=300
```

### Test Coverage

```bash
# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

Expected coverage: >80% for all components

---

For additional support or questions, please refer to the main [README.md](../README.md) or create an issue in the project repository.