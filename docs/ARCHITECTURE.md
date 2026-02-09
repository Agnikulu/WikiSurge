# WikiSurge Architecture Documentation

## System Overview

### Purpose and Goals

WikiSurge is a real-time Wikipedia monitoring system designed to detect and analyze significant events across all Wikipedia language editions. The system ingests, processes, and analyzes Wikipedia's real-time edit stream to identify:

- **Traffic spikes**: Sudden surges in page edit activity
- **Edit wars**: Controversial content battles between editors
- **Trending pages**: Rising interest across multiple languages
- **Anomalous behavior**: Unusual editing patterns

**Key Design Goals:**
1. **Real-time processing**: Sub-second latency from Wikipedia edit to dashboard alert
2. **Scalability**: Handle 10,000+ edits/second across 300+ language editions
3. **Reliability**: 99.9% uptime with graceful degradation
4. **Resource efficiency**: Bounded memory usage via selective tracking
5. **Extensibility**: Easy to add new detectors and metrics

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Wikipedia SSE Stream                     │
│                    (EventStreams - Live Edits)                   │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
                    ┌────────────────┐
                    │   INGESTOR     │  ← Consumes SSE stream
                    │  (Go service)  │  ← Validates & filters
                    └────────┬───────┘
                             │
                             │ Produces to Kafka
                             ▼
                    ┌────────────────┐
                    │  Apache Kafka  │  ← Message broker
                    │   (1 topic)    │  ← Decouples services
                    └────────┬───────┘
                             │
          ┌──────────────────┼──────────────────┐
          │                  │                  │
          ▼                  ▼                  ▼
    ┌─────────┐      ┌─────────────┐    ┌─────────────┐
    │ Spike   │      │ Edit War    │    │  Trending   │
    │Detector │      │  Detector   │    │ Aggregator  │
    └────┬────┘      └──────┬──────┘    └──────┬──────┘
         │                  │                   │
         │                  ▼                   │
         │           ┌─────────────┐            │
         │           │ Selective   │            │
         │           │  Indexer    │            │
         │           └──────┬──────┘            │
         │                  │                   │
         └──────────┬───────┴───────────────────┘
                    ▼
            ┌───────────────┐
            │  Redis + ES   │  ← State storage
            │  (Hot state)  │  ← Search index
            └───────┬───────┘
                    │
                    ▼
            ┌───────────────┐
            │   API Server  │  ← REST + WebSocket
            │  (Go service) │  ← Rate limiting
            └───────┬───────┘
                    │
                    ▼
            ┌───────────────┐
            │   Frontend    │  ← React dashboard
            │ (React + TS)  │  ← Real-time updates
            └───────────────┘
```

### Component Responsibilities

| Component | Responsibility | Technology |
|-----------|---------------|------------|
| **Ingestor** | Consume Wikipedia SSE stream, validate, produce to Kafka | Go |
| **Kafka** | Message broker, decouples ingestion from processing | Apache Kafka |
| **Spike Detector** | Detect sudden edit rate increases on pages | Go consumer |
| **Edit War Detector** | Identify contentious editing patterns | Go consumer |
| **Trending Aggregator** | Calculate page popularity scores | Go consumer |
| **Selective Indexer** | Index significant edits to Elasticsearch | Go consumer |
| **WebSocket Forwarder** | Stream live updates to connected clients | Go consumer |
| **Redis** | Hot state storage (trending, hot pages, alerts) | Redis 7+ |
| **Elasticsearch** | Full-text search and historical queries | ES 8+ |
| **API Server** | REST API and WebSocket endpoints | Go + net/http |
| **Frontend** | Real-time monitoring dashboard | React 18 + TypeScript |

### Data Flow Diagrams

#### Edit Processing Flow
```
Wikipedia Edit
    │
    ▼
[Ingestor] Validate & enrich
    │
    ▼
[Kafka] wikisurge.edits topic
    │
    ├─▶ [Spike Detector] ──▶ Redis alerts:spikes
    ├─▶ [Edit War] ──────▶ Redis alerts:editwars
    ├─▶ [Trending] ───────▶ Redis trending:{lang}
    ├─▶ [Indexer] ────────▶ Elasticsearch
    └─▶ [WS Forwarder] ───▶ WebSocket clients
```

#### Alert Flow
```
Processor detects event
    │
    ▼
Write to Redis stream (alerts:{type})
    │
    ├─▶ [API] polls stream ──▶ REST clients
    └─▶ [AlertHub] pub/sub ──▶ WebSocket clients
```

#### Query Flow
```
Dashboard request
    │
    ▼
[API Server] with rate limiting
    │
    ├─▶ [Redis] for hot data (trending, stats)
    └─▶ [Elasticsearch] for search/history
         │
         ▼
    JSON response ──▶ Frontend
```

---

## Component Details

### Ingestor Service

**Purpose:** Connect to Wikipedia's EventStreams SSE endpoint, validate incoming edits, and produce to Kafka.

**Technology Stack:**
- Go 1.23+
- SSE client (`github.com/r3labs/sse/v2`)
- Kafka producer (`github.com/segmentio/kafka-go`)

**Configuration Options:**
```yaml
ingestor:
  wikimedia_stream_url: https://stream.wikimedia.org/v2/stream/recentchange
  enabled_wikis: ["*.wikipedia"]  # Filter by wiki
  batch_size: 100                 # Messages per batch
  batch_timeout: 1s               # Max wait time
  metrics_port: 2112              # Prometheus metrics
```

**Key Features:**
- **Automatic reconnection**: Exponential backoff on connection loss
- **Validation**: Schema validation before producing to Kafka
- **Filtering**: Wiki-level filtering (e.g., only Wikipedia, exclude Wikidata)
- **Metrics**: Ingestion rate, connection status, validation errors

**When to Scale:**
- If ingestion lag > 5 seconds
- If validation errors spike
- If reconnection frequency increases

---

### Kafka Broker

**Purpose:** Decouple ingestion from processing, enable multiple consumers, provide replay capability.

**Topic Structure:**
```
wikisurge.edits
├─ Partitions: 6 (configurable)
├─ Replication: 3 (production)
├─ Retention: 24 hours
└─ Compression: snappy
```

**Why Kafka?**
- **Decoupling**: Ingestor doesn't depend on processor availability
- **Replay**: Can reprocess historical data
- **Multiple consumers**: Each processor reads independently
- **Ordering**: Guarantees delivery order per partition
- **Durability**: Survives service restarts

**Configuration:**
```yaml
kafka:
  brokers: ["localhost:9092"]
  topic: "wikisurge.edits"
  partitions: 6
  replication_factor: 3
  retention_hours: 24
```

---

### Processor Service (Multi-Consumer)

**Purpose:** Consume edits from Kafka and run multiple detection/aggregation pipelines in parallel.

**Consumers:**
1. **Spike Detector** (`spike-detector`)
   - Consumer group: Processes all edits
   - Tracks edit rates per page in Redis
   - Publishes to `alerts:spikes` stream
   
2. **Edit War Detector** (`edit-war-detector`)
   - Consumer group: Processes hot pages only
   - Tracks editor patterns and reverts
   - Publishes to `alerts:editwars` stream
   
3. **Trending Aggregator** (`trending-aggregator`)
   - Consumer group: Updates trending scores
   - Per-language tracking in Redis
   - Prunes stale entries periodically
   
4. **Selective Indexer** (`selective-indexer`)
   - Consumer group: Indexes significant edits
   - Filters: hot pages, large edits, alerts
   - Writes to Elasticsearch
   
5. **WebSocket Forwarder** (`ws-forwarder`)
   - Consumer group: Real-time streaming
   - No persistence
   - Broadcasts to WebSocket hub

**Architecture Pattern:**
```go
type EditConsumer interface {
    ProcessEdit(ctx context.Context, edit *WikipediaEdit) error
}
```

Each consumer implements this interface independently. The orchestrator manages:
- Consumer lifecycle
- Health monitoring
- Graceful shutdown
- Metrics collection

**Configuration:**
```yaml
processor:
  features:
    spike_detection: true
    edit_wars: true
    trending: true
    elasticsearch: true
    websocket: true
  health_check_interval: 30s
```

---

### Redis Storage Layer

**Purpose:** Hot state storage for real-time data (trending, hot pages, alerts, stats).

**Key Patterns:**

#### Hot Page Tracking
```
activity:{page}           → Counter (TTL: 10min)
hot:window:{page}         → Sorted Set (edit timestamps)
hot:meta:{page}           → Hash (edit_count, editors, etc.)
```

**Promotion Flow:**
1. Page gets first edit → `activity:{page}` counter created
2. Counter hits threshold (default: 2) → Promote to hot tracking
3. Hot tracking → Full metadata + windowed edits

**Circuit Breaker:** Max 1,000 hot pages tracked simultaneously.

#### Trending Scores
```
trending:{lang}           → Sorted Set (page → score)
trending:meta:{lang}:{page} → Hash (edit_count, spike_factor)
```

**Score Calculation:**
```
score = log10(edit_count + 1) * spike_factor * recency_decay
```

**Pruning:** Background job removes pages with score < 0.1 every 5 minutes.

#### Alerts Storage
```
alerts:spikes             → Redis Stream
alerts:editwars           → Redis Stream
alerts:trending           → Redis Stream
alerts:vandalism          → Redis Stream
```

**Retention:** MAXLEN ~ 1000 entries (auto-trimming).

#### Statistics
```
stats:timeline:{unix_min} → Hash (edit counts per minute)
stats:langs:{lang}        → Counter
stats:edits:total         → Counter
```

**TTL Strategy:**
- Activity counters: 10 minutes
- Hot page data: 1 hour + buffer
- Trending data: 24 hours
- Timeline: 25 hours
- Alert streams: 1000 entries (not time-based)

---

### Elasticsearch

**Purpose:** Full-text search and historical query support for edits.

**Index Structure:**
```
wikisurge-edits-YYYY-MM-DD
├─ Mappings:
│  ├─ title (text, keyword)
│  ├─ user (keyword)
│  ├─ comment (text)
│  ├─ wiki (keyword)
│  ├─ timestamp (date)
│  ├─ length (nested: old, new)
│  └─ meta (keyword)
└─ Settings:
   ├─ Shards: 3
   ├─ Replicas: 1
   └─ Refresh: 1s
```

**Selective Indexing Strategy:**
Only index edits that meet criteria:
- Page is hot (high edit rate)
- Edit is large (>5000 bytes changed)
- Associated with alert (spike, edit war)
- User is flagged/monitored

**Why Selective?**
- Reduce storage: 10x savings vs indexing everything
- Improve search: Only interesting data
- Lower costs: ES is expensive at scale

**Query Examples:**
```json
{
  "query": {
    "bool": {
      "must": [
        {"match": {"title": "Ukraine"}},
        {"range": {"timestamp": {"gte": "now-1d"}}}
      ]
    }
  },
  "sort": [{"timestamp": "desc"}],
  "size": 50
}
```

---

### API Server

**Purpose:** REST API and WebSocket endpoints for dashboard and integrations.

**Technology Stack:**
- Go net/http (standard library)
- WebSocket (gorilla/websocket)
- Rate limiting (Redis-based sliding window)

**Middleware Stack (inner → outer):**
```
Request
  → Logger
    → RequestID
      → Recovery (panic handler)
        → Gzip
          → ETag
            → CORS
              → Security Headers
                → Request Validation
                  → Rate Limiter
                    → Metrics
                      → [Handler]
```

**Rate Limiting:**
- Algorithm: Redis sliding window counter
- Default: 100 requests/minute per IP
- Burst: 20 requests
- Response: `429 Too Many Requests` with `Retry-After` header

**Caching:**
- Stats endpoint: 5-second cache
- Trending endpoint: 10-second cache
- ETag support for conditional requests

**WebSocket Behavior:**
- Heartbeat: 30-second ping/pong
- Reconnection: Client handles with exponential backoff
- Filtering: Query params for language, wiki, user
- Backpressure: Drop messages if client slow (prevents memory leak)

---

### Frontend Dashboard

**Purpose:** Real-time monitoring interface for Wikipedia activity.

**Technology Stack:**
- React 18 with TypeScript
- Zustand (state management)
- Recharts (data visualization)
- react-window (virtualization)
- Vite (build tool)

**Architecture:**
```
App
├─ Global State (Zustand)
│  ├─ Stats (edits/sec, hot pages, etc.)
│  ├─ Alerts (spikes, edit wars)
│  └─ Settings (theme, filters)
├─ Components
│  ├─ StatsOverview (cards)
│  ├─ EditsTimeline (chart)
│  ├─ AlertsPanel (list)
│  ├─ TrendingList (table)
│  ├─ EditWarsList (cards)
│  ├─ LiveFeed (virtualized)
│  └─ SearchInterface (Elasticsearch)
└─ Hooks
   ├─ useWebSocket (SSE fallback)
   ├─ usePolling (REST fallback)
   └─ useAPI (data fetching)
```

**Real-Time Update Strategy:**
1. **Primary**: WebSocket for live events
2. **Fallback**: Polling every 10-15 seconds if WebSocket fails
3. **Graceful Degradation**: Show stale data with warning

**Performance Optimizations:**
- Virtual scrolling for long lists (react-window)
- Memoization (React.memo, useMemo)
- Lazy loading for tabs
- Debounced search input
- Time-based chart updates (not on every data point)

---

## Data Models

### Wikipedia Edit (Core Model)

```go
type WikipediaEdit struct {
    ID          int64     `json:"id"`
    Type        string    `json:"type"`         // "edit", "new", "log"
    Title       string    `json:"title"`        // Page title
    User        string    `json:"user"`         // Editor username
    Bot         bool      `json:"bot"`          // Is bot edit
    Comment     string    `json:"comment"`      // Edit summary
    Timestamp   int64     `json:"timestamp"`    // Unix timestamp
    Wiki        string    `json:"wiki"`         // e.g., "enwiki"
    Length      Length    `json:"length"`
    Minor       bool      `json:"minor"`
    Patrolled   bool      `json:"patrolled"`
    Meta        Meta      `json:"meta"`
}

type Length struct {
    Old int `json:"old"`  // Previous page size
    New int `json:"new"`  // New page size
}

type Meta struct {
    Domain string `json:"domain"`    // e.g., "en.wikipedia.org"
    URI    string `json:"uri"`       // Full page URL
    Stream string `json:"stream"`    // EventStreams stream name
}
```

### Alert Models

#### Spike Alert
```go
type SpikeAlert struct {
    PageTitle   string    `json:"page_title"`
    EditRate    float64   `json:"edit_rate"`       // Edits per minute
    BaselineRate float64  `json:"baseline_rate"`   // Historical average
    Severity    string    `json:"severity"`        // low/medium/high/critical
    Timestamp   time.Time `json:"timestamp"`
    Wiki        string    `json:"wiki"`
}
```

#### Edit War Alert
```go
type EditWarAlert struct {
    PageTitle   string    `json:"page_title"`
    EditorCount int       `json:"editor_count"`
    EditCount   int       `json:"edit_count"`
    RevertCount int       `json:"revert_count"`
    Severity    string    `json:"severity"`
    StartTime   time.Time `json:"start_time"`
    Editors     []string  `json:"editors"`
}
```

### Kafka Message Format

**Topic:** `wikisurge.edits`

**Message Structure:**
```json
{
  "key": "enwiki:Ukraine",
  "value": {
    "id": 1234567890,
    "type": "edit",
    "title": "Ukraine",
    "user": "Example",
    "bot": false,
    "comment": "Updated statistics",
    "timestamp": 1707523200,
    "wiki": "enwiki",
    "length": {"old": 45000, "new": 45123},
    "minor": false,
    "patrolled": true,
    "meta": {
      "domain": "en.wikipedia.org",
      "uri": "https://en.wikipedia.org/wiki/Ukraine",
      "stream": "recentchange"
    }
  }
}
```

**Partitioning Strategy:** Hash by `wiki:title` ensures same page goes to same partition (ordering guarantee).

---

## Algorithms

### Spike Detection

**Purpose:** Detect pages experiencing sudden increase in edit activity.

**Algorithm:**
```
1. Maintain sliding window of edit timestamps per page
   - Window size: 1 hour
   - Storage: Redis Sorted Set (score = timestamp)

2. On each new edit:
   a. Add to window: ZADD hot:window:{page} {timestamp} {edit_id}
   b. Remove old: ZREMRANGEBYSCORE ... -inf {cutoff}
   c. Count recent: ZCOUNT ... {now-5min} {now}

3. Calculate edit rate:
   rate = edits_last_5min / 5 (edits per minute)

4. Compare with baseline:
   baseline = edits_last_hour / 60
   spike_factor = rate / max(baseline, 1.0)

5. Classify severity:
   if spike_factor >= 10: critical
   elif spike_factor >= 5: high
   elif spike_factor >= 3: medium
   else: low

6. Publish alert if spike_factor >= 3
```

**Example:**
```
Page: "Breaking News Event"
Last hour: 120 edits → baseline = 2 edits/min
Last 5 min: 50 edits → rate = 10 edits/min
Spike factor: 10 / 2 = 5x → HIGH severity
```

**Edge Cases:**
- **New pages**: No baseline → use rate > 5 edits/min threshold
- **Bot edits**: Excluded from spike detection
- **Very active pages**: Cap baseline at 95th percentile to avoid false negatives
- **Memory bounds**: Max 1000 hot pages tracked (circuit breaker)

**Deduplication:** Once alert published, cooldown period of 5 minutes before next alert for same page.

---

### Trending Calculation

**Purpose:** Identify pages gaining sustained interest across short time window.

**Algorithm:**
```
1. For each edit, update trending score:
   score = log10(edit_count + 1) * spike_factor * recency_decay

2. Components:
   a. edit_count: Total edits in last hour
   b. spike_factor: From spike detection
   c. recency_decay: time_since_last_edit based penalty
      decay = 1.0 - (minutes_since / 60)

3. Store in Redis Sorted Set:
   ZADD trending:{lang} {score} {page}

4. Pruning:
   - Every 5 minutes: ZREMRANGEBYSCORE ... -inf 0.1
   - Remove pages with score < 0.1

5. Retrieval:
   ZREVRANGE trending:{lang} 0 20  # Top 20
```

**Example:**
```
Page: "Taylor Swift"
Edits in last hour: 45
Spike factor: 2.5
Last edit: 2 minutes ago
Decay: 1.0 - (2/60) = 0.967

Score = log10(45+1) * 2.5 * 0.967 = 1.66 * 2.5 * 0.967 = 4.01
```

**Multi-Language Handling:**
- Separate sorted sets per language: `trending:en`, `trending:fr`, etc.
- Global trending: Union of top 10 from each language
- Language detection: From `wiki` field (enwiki → en)

**Edge Cases:**
- **One-time spikes**: Decay quickly (not sustained interest)
- **Gradual growth**: Lower spike factor but high edit count
- **Vandalism revert cycles**: Filtered by edit war detection

---

### Edit War Detection

**Purpose:** Identify contentious pages where editors repeatedly undo each other's changes.

**Algorithm:**
```
1. Track editors per hot page:
   HINCRBY editwar:editors:{page} {user} 1

2. Track byte changes (for revert detection):
   RPUSH editwar:changes:{page} {byte_diff}
   LTRIM ... -100 -1  # Keep last 100

3. Check conditions (after each edit):
   a. editors >= 2
   b. total_edits >= 5
   c. reverts >= 1

4. Revert detection heuristic:
   - Scan byte change sequence
   - Look for alternating signs with similar magnitude:
     Example: [+500, -480, +510, -505] → 3 reverts
   - Similarity threshold: within 20% magnitude

5. Severity classification:
   if editors >= 4 AND reverts >= 3: critical
   elif editors >= 3 OR reverts >= 2: high
   elif editors >= 2 AND reverts >= 1: medium
   else: low

6. Publish alert (deduplicated per page)

7. TTL: 10 minutes (edit war must occur within window)
```

**Example Scenario:**
```
Page: "Controversial Topic"
Timeline:
1. User A: +500 bytes (adds content)
2. User B: -480 bytes (removes most)
3. User A: +510 bytes (re-adds)
4. User B: -505 bytes (removes again)
5. User C: +200 bytes (adds different content)

Detection:
- Editors: 3 (A, B, C)
- Reverts: 3 (alternating +/- pattern)
- Severity: HIGH (3 editors, 3 reverts)
```

**Edge Cases:**
- **Collaborative editing**: Not alternating pattern → no revert
- **Bot maintenance**: Bots excluded from editor count
- **Size-based reverts**: Only detect if magnitude similar (±20%)
- **Single large edit followed by cleanup**: Not classified as revert

**False Positive Mitigation:**
- Require minimum 2 editors (not just back-and-forth with self)
- Require alternating pattern (not just many edits)
- Time window: Must occur within 10 minutes (not historical)

---

## Design Decisions

### Why Kafka (vs alternatives)?

**Decision:** Use Apache Kafka as message broker between ingestor and processors.

**Alternatives Considered:**
1. **RabbitMQ**: Good for RPC patterns, but less suited for high-throughput streaming
2. **Redis Streams**: Simpler, but lacks partitioning and replication
3. **Direct processing**: No broker, ingestor → processors directly
4. **Cloud options**: AWS Kinesis, Google Pub/Sub

**Rationale:**
- **Throughput**: Kafka handles 10K+ msgs/sec easily
- **Replay**: Can reprocess historical data for new detectors
- **Decoupling**: Ingestor doesn't block if processor slow/down
- **Ordering**: Per-partition ordering guarantees for same page
- **Ecosystem**: Well-documented, widely used, mature
- **Cost**: Open-source, run on own infrastructure

**Trade-offs:**
- ✅ High throughput, great for event streaming
- ✅ Durable, replayable
- ✅ Mature ecosystem
- ❌ More complex than Redis streams
- ❌ Operational overhead (Zookeeper or KRaft mode)

---

### Why Go (vs alternatives)?

**Decision:** Implement backend services in Go.

**Alternatives Considered:**
1. **Python**: Easier to prototype, rich data science ecosystem
2. **Java/JVM**: More enterprise tooling, similar performance
3. **Rust**: Better performance, stricter safety
4. **Node.js**: JavaScript everywhere, large ecosystem

**Rationale:**
- **Concurrency**: Goroutines perfect for handling many WebSocket connections
- **Performance**: Near C-level performance with easier development
- **Deployment**: Single binary, no runtime dependencies
- **Memory**: Lower footprint than JVM, more predictable than Python
- **Standard library**: Excellent HTTP, JSON, testing support built-in
- **Kafka libraries**: `segmentio/kafka-go` is excellent

**Trade-offs:**
- ✅ Fast compilation, fast execution
- ✅ Great for I/O bound workloads (network, DB)
- ✅ Strong standard library
- ✅ Easy deployment (single binary)
- ❌ Less flexible than Python for data science
- ❌ Smaller ecosystem than Java or JavaScript
- ❌ Less ergonomic error handling than Rust

---

### Selective Indexing Rationale

**Decision:** Only index significant edits to Elasticsearch, not all edits.

**Full Indexing Costs (estimated):**
- Wikipedia: ~5,000 edits/sec average, 10,000 peak
- Daily volume: ~430 million edits
- Storage: ~1KB per edit → 430GB/day → 12TB/month
- ES cost: ~$500-1000/month for this storage + compute

**Selective Indexing Strategy:**
Only index if edit meets ANY criteria:
1. Page is "hot" (high edit rate)
2. Edit is large (>5000 bytes changed)
3. Associated with alert (spike, edit war)
4. User is on watch list

**Result:**
- Index ~5-10% of edits (~43 million/day)
- Storage: ~40GB/day → 1.2TB/month
- Cost reduction: 90% savings
- Search quality: Not degraded (interesting edits still indexed)

**Trade-offs:**
- ✅ 90% cost savings
- ✅ Faster queries (smaller index)
- ✅ Focus on interesting edits
- ❌ Can't search all historical edits
- ❌ Need to adjust filters if requirements change

---

### Bounded State Management

**Decision:** Circuit breakers and TTLs to prevent memory explosion.

**Problem:** Wikipedia generates unbounded data. Without limits:
- Tracking all 60M+ pages → OOM
- Indefinite retention → disk fills up
- Zombie pages (inactive but tracked) → wasted resources

**Solutions Implemented:**

#### Hot Page Tracker
```
Stage 1: Activity Counter
- Lightweight counter per page
- TTL: 10 minutes
- Only promoted to full tracking if hits threshold

Stage 2: Hot Page Tracking
- Full metadata + windowed edits
- Circuit breaker: Max 1000 hot pages
- TTL: 1 hour + buffer
- Background cleanup of stale pages
```

#### Trending Scorer
```
- Pruning: Remove scores < 0.1 every 5 minutes
- Decay: Scores decrease over time if no new edits
- Per-language cap: Top 1000 per language
```

#### Alert Streams
```
- MAXLEN ~ 1000 (auto-trim old alerts)
- In-memory cache with LRU eviction
- No indefinite storage
```

**Result:**
- Memory usage: Bounded to ~2GB for processor
- Redis: Bounded to ~4GB
- Predictable performance under load

**Trade-offs:**
- ✅ Prevents OOM
- ✅ Predictable resource usage
- ✅ Graceful degradation under extreme load
- ❌ May miss low-activity pages during spike
- ❌ Need to tune circuit breaker thresholds

---

## Scalability

### Current Limits

**Single-Server Deployment:**
- Ingestion: 10,000 edits/sec
- Processing: 8,000 edits/sec (bottleneck: ES indexing)
- WebSocket clients: 1,000 concurrent
- API requests: 10,000 req/min
- Redis memory: 4GB
- Elasticsearch storage: 1TB (30 days retention)

**Bottlenecks:**
1. **Elasticsearch indexing**: 500 edits/sec per node
2. **Kafka throughput**: Limited by disk I/O
3. **Redis memory**: Hot page tracking growth
4. **WebSocket broadcast**: O(N) to all clients

### Horizontal Scaling Strategies

#### Kafka Partitioning
```yaml
# Increase partitions for parallelism
partitions: 12  # Up from 6

# More consumers per group
spike-detector: 3 instances
edit-war-detector: 2 instances
trending: 2 instances
```

**Result:** Linear scaling up to partition count.

#### API Server Scaling
```
Load Balancer (nginx/HAProxy)
    │
    ├─▶ API Server 1
    ├─▶ API Server 2
    └─▶ API Server 3

Shared state: Redis
Session affinity: Not required (stateless)
```

**Considerations:**
- WebSocket clients: Sticky sessions or Redis pub/sub for fanout
- Rate limiting: Redis-based (shared counter)

#### Redis Scaling
```
Option 1: Redis Cluster
- Shard hot pages across nodes
- Key-based sharding: hash(page_title)

Option 2: Separate Redis Instances
- Trending: redis-trending:6379
- Hot pages: redis-hotpages:6379
- Alerts: redis-alerts:6379
```

#### Elasticsearch Scaling
```
Current: 1 node
Scale to: 3-node cluster
- Shards: 6 (up from 3)
- Replicas: 2 (up from 1)
- Daily indices: wikisurge-edits-2026-02-09

Optimization:
- Index lifecycle management (ILM)
- Close old indices
- Delete after 30 days
```

### Vertical Scaling

**When to scale up (not out):**

| Resource | Current | Scale To | When |
|----------|---------|----------|------|
| Kafka broker | 4 cores, 8GB RAM | 8 cores, 16GB | Lag > 1 minute |
| Processor | 2 cores, 4GB | 4 cores, 8GB | CPU > 80% sustained |
| Redis | 4GB | 8GB | Memory > 3.5GB |
| ES node | 4GB heap | 8GB heap | JVM GC > 30% time |

### Regional Deployment

**Multi-Region Architecture:**
```
Region 1 (US-East)
├─ Ingestor → Kafka-US → Processor → Redis-US → API-US
└─ Frontend CDN → API-US

Region 2 (EU-West)
├─ Ingestor → Kafka-EU → Processor → Redis-EU → API-EU
└─ Frontend CDN → API-EU

Cross-Region:
- Kafka MirrorMaker: Replicate edits from US → EU
- Redis: No replication (regional data only)
- Elasticsearch: Cross-cluster replication (optional)
```

**Latency Benefits:**
- API response: <50ms (regional)
- WebSocket: <100ms RTT
- Search queries: <200ms

### Cost Optimization

**Current Monthly Cost (single server):**
```
VPS (8 cores, 16GB): $80
Kafka (managed): $100
Redis (4GB): $20
Elasticsearch (1TB): $200
Total: ~$400/month
```

**Optimizations:**
1. **Self-host Kafka**: Save $100/month
2. **Compress ES data**: Reduce storage 50%
3. **S3 cold storage**: Archive old indices
4. **Reserved instances**: 30% discount on VPS

**Scaled Deployment (10K users):**
```
Load Balancer: $20
API Servers (3x): $240
Kafka Cluster (3 nodes): $300
Redis Cluster (3 nodes): $150
ES Cluster (3 nodes): $600
Total: ~$1,310/month
```

---

## Monitoring and Observability

**Key Metrics:**
- Ingestion rate (edits/sec)
- Processing lag (seconds behind real-time)
- Alert latency (time from edit to alert)
- Hot pages tracked (count)
- WebSocket clients (count)
- API response times (p50, p95, p99)
- Error rates (per endpoint)

**Dashboards:**
1. System Overview: Ingestion, processing, alerts
2. Ingestion: SSE connection, Kafka producer metrics
3. Processing: Consumer lag, detector performance
4. API: Request rate, latency, errors

**Alerts:**
- Processing lag > 60 seconds → Critical
- Ingestor disconnected > 5 minutes → High
- API error rate > 1% → Medium
- Disk usage > 85% → Medium

---

## Security Considerations

**Current Implementation:**
- No authentication (designed for public read-only access)
- Rate limiting by IP
- CORS headers
- Security headers (CSP, X-Frame-Options, etc.)
- Input validation on all endpoints

**Future Enhancements:**
1. API keys for programmatic access
2. OAuth for user accounts
3. Role-based access control (RBAC)
4. Audit logging
5. TLS/SSL in production
6. DDoS protection (Cloudflare, AWS Shield)

---

## Conclusion

WikiSurge is designed for real-time Wikipedia monitoring at scale with:
- **Bounded resource usage** via circuit breakers
- **Horizontal scalability** via Kafka partitioning
- **Cost efficiency** via selective indexing
- **Reliability** via health monitoring and graceful degradation

The architecture balances performance, cost, and operational simplicity while remaining extensible for future features.
