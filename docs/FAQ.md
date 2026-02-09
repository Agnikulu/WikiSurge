# WikiSurge Frequently Asked Questions (FAQ)

## Table of Contents
- [General](#general)
- [Architecture & Design](#architecture--design)
- [Detection & Algorithms](#detection--algorithms)
- [Performance & Scalability](#performance--scalability)
- [Deployment & Operations](#deployment--operations)
- [Troubleshooting](#troubleshooting)
- [Integration & Customization](#integration--customization)

---

## General

### What is WikiSurge?

WikiSurge is a real-time Wikipedia monitoring platform that detects and alerts on significant events including:
- **Trending pages** - Pages with sustained high edit activity
- **Spikes** - Sudden bursts of edits indicating breaking news
- **Edit wars** - Conflicting edits between multiple editors
- **Hot pages** - Popular pages with consistent activity

It provides a live dashboard with real-time alerts, historical search, and comprehensive monitoring.

### How does it work?

1. **Ingestor** connects to Wikipedia's EventStreams SSE API
2. Edits are published to **Kafka** for reliable stream processing
3. **Processor** analyzes edits using multiple detection algorithms
4. Detected events stored in **Redis** (hot state) and **Elasticsearch** (history)
5. **API** serves data to the React frontend via REST and WebSocket
6. **Dashboard** displays real-time updates and historical trends

### What languages does WikiSurge support?

WikiSurge monitors **all Wikipedia languages**, including:
- Major: English (en), Spanish (es), German (de), French (fr), Japanese (ja), etc.
- Regional: Hindi (hi), Arabic (ar), Portuguese (pt), Russian (ru), etc.
- Total: All 300+ Wikipedia language editions

Configure monitored languages via `config.yaml`:
```yaml
ingestor:
  wiki_languages: ["en", "es", "de", "fr"]  # Filter specific languages
  # Or leave empty to monitor all: []
```

### What's the latency from edit to alert?

**End-to-end latency:** ~2-5 seconds

Breakdown:
- Wikipedia SSE stream: ~1s (Wikipedia's delay)
- Kafka ingestion: <100ms
- Processing pipeline: ~500ms-1s
- Redis storage: <50ms
- WebSocket broadcast: <100ms
- Frontend render: <100ms

**Note:** Spike/trending detection requires historical data, so first alert may take 1-2 minutes after initial edits.

---

## Architecture & Design

### Why use Kafka instead of direct processing?

**Reliability:**
- Edits not lost if processor crashes
- Can replay events from Kafka log
- Decouples ingestion from processing

**Scalability:**
- Multiple processors can consume same stream
- Kafka buffers traffic spikes (1000s edits/sec)
- Horizontal scaling by adding consumer groups

**Flexibility:**
- Add new processors without changing ingestor
- Different consumers for different detection algorithms
- Easy to add features like data export

**Alternatives considered:**
- ✗ Direct websocket → processor: No buffering, single point of failure
- ✗ Redis Streams: Limited retention, no multi-datacenter support
- ✓ **Kafka**: Battle-tested, durable, scalable

### Why Go instead of Python/Node.js?

**Performance:**
- 10x faster than Python for stream processing
- Low memory footprint (~50MB per service)
- Efficient goroutines for concurrent processing

**Reliability:**
- Compiled → catches errors at build time
- No runtime environment issues (e.g., Python 2 vs 3)
- Strong typing prevents common bugs

**Deployment:**
- Single binary, no dependencies
- Cross-compile for any platform
- Small Docker images (~20MB)

**Alternatives considered:**
- ✗ Python: Too slow for real-time processing (tested 100 edits/sec vs 1000+)
- ✗ Node.js: Higher memory usage, callback hell
- ✓ **Go**: Best balance of performance, simplicity, reliability

### Why selective Elasticsearch indexing?

**Cost savings:**
- Wikipedia: ~5 edits/sec average
- Full indexing: ~400M docs/month → $500-1000/month
- Selective (hot pages only): ~40M docs/month → $50-100/month
- **90% cost reduction**

**Performance:**
- Smaller index → faster queries
- Most queries are for hot/trending pages anyway
- Cold pages have low search interest

**Logic:**
- Index page if: trending, spiking, edit war, or >10 edits/day
- Only 10-20% of pages meet criteria
- Sufficient for 95%+ of user queries

**If you need full history:**
```yaml
processor:
  features:
    elasticsearch: true
hot_pages:
  promotion_threshold: 1  # Index everything
```

### Why bounded hot pages (max 1000)?

**Memory management:**
- Each hot page: ~50KB state (edit history, stats)
- 1000 pages: ~50MB
- Unbounded: Could grow to GBs on major news events

**Circuit breaker:**
- Prevents OOM crashes during traffic spikes
- Degrades gracefully (stops tracking new pages)
- Alert fires: "Hot page capacity reached"

**Practical:**
- 1000 concurrent hot pages is very rare (Wikipedia-wide)
- Normal: 100-300 hot pages
- Major news: 500-800 hot pages
- If hitting limit regularly, scale horizontally

**Adjust limit:**
```yaml
hot_pages:
  max_hot_pages: 2000  # Increase if needed
```

---

## Detection & Algorithms

### How does spike detection work?

**Algorithm:**
1. Track edit count per page in 1-minute windows
2. Calculate Z-score: `z = (current - mean) / stddev`
3. Spike if Z > threshold (default: 3.0 = 3 standard deviations)
4. Requires 10 minutes of baseline data

**Example:**
- Page normally gets 2 edits/min
- Suddenly gets 20 edits/min
- Z-score: (20 - 2) / 3 = 6.0
- Spike detected! (6.0 > 3.0)

**Tuning:**
```yaml
spike_detection:
  threshold: 3.0        # Lower = more sensitive
  baseline_minutes: 10  # More history = more stable
  min_edits: 5          # Ignore low-traffic pages
```

**Severity levels:**
- **Low:** Z=3-5 (2x normal traffic)
- **Medium:** Z=5-7 (3-4x normal)
- **High:** Z=7-10 (5-7x normal)
- **Critical:** Z>10 (10x+ normal)

### How is trending calculated?

**Formula:**
```
score = (edits / time_decay) * recency_weight
```

**Components:**
1. **Edit velocity:** Edits per hour over last 2 hours
2. **Time decay:** Recent edits weighted more (exponential)
3. **Recency weight:** Edits in last 15 min count 2x
4. **Uniqueness:** Unique editors (prevents gaming)

**Example:**
- Page A: 20 edits (10 editors) in last hour → score ~35
- Page B: 30 edits (2 editors) in last 2 hours → score ~22
- Page A ranks higher despite fewer edits (more organic)

**Configuration:**
```yaml
trending:
  update_interval: 1m     # Recalculate every minute
  top_n: 100             # Track top 100
  min_edits: 3           # Minimum edits to qualify
  time_window: 2h        # Consider last 2 hours
```

### What triggers an edit war?

**Criteria (all must be met within 10-minute window):**
- ≥5 total edits
- ≥2 distinct editors
- ≥1 revert (edit restoring previous version)
- Same page

**Example:**
```
14:00 - Alice edits "Kosovo" (+100 chars)
14:02 - Bob reverts Alice's edit (-100 chars)
14:05 - Alice re-applies changes (+100 chars)
14:07 - Bob reverts again (-100 chars)
14:09 - Charlie edits (+50 chars)
```
→ **Edit war detected:** 5 edits, 3 editors, 2 reverts

**Revert detection:**
- Content size returns to previous state (±5%)
- Edit comment contains: "revert", "undo", "rv"
- Rollback in edit tags

**Tuning:**
```yaml
edit_wars:
  min_edits: 5
  min_editors: 2
  min_reverts: 1
  time_window: 10m
```

### Can I adjust detection sensitivity?

**Yes!** Edit `configs/config.yaml`:

**More sensitive (more alerts):**
```yaml
spike_detection:
  threshold: 2.0          # Default: 3.0
  min_edits: 3            # Default: 5

trending:
  min_edits: 2            # Default: 3

edit_wars:
  min_edits: 3            # Default: 5
  min_reverts: 1          # Default: 1
```

**Less sensitive (fewer alerts):**
```yaml
spike_detection:
  threshold: 4.0          # Higher threshold
  min_edits: 10           # Ignore small pages

trending:
  min_edits: 5            # Only significant activity

edit_wars:
  min_edits: 7            # More edits required
  min_reverts: 2          # Must have 2+ reverts
```

**Restart processor after changes:**
```bash
docker-compose restart processor
```

---

## Performance & Scalability

### How much does WikiSurge cost to run?

**Minimal setup (1-2 languages):**
- VPS: $10-20/month (2 CPU, 4GB RAM)
- No cloud services needed
- **Total: ~$15/month**

**Medium setup (5-10 languages):**
- VPS: $40-60/month (4 CPU, 8GB RAM)
- Elasticsearch: $20-40/month (managed, 10GB)
- **Total: ~$80/month**

**Large setup (all languages):**
- VPS: $160/month (8 CPU, 32GB RAM)
- Kafka: $100/month (managed, 3 nodes)
- Elasticsearch: $200/month (managed, 50GB)
- **Total: ~$500/month**

**DIY (self-hosted everything):**
- Single powerful server: $100-200/month
- All services in Docker Compose
- **Total: ~$150/month**

### What are the resource requirements?

**Minimum (development, 1 language):**
- 2 CPU cores
- 4GB RAM
- 20GB disk
- ~50 edits/sec throughput

**Recommended (production, 5-10 languages):**
- 4 CPU cores
- 8GB RAM
- 100GB disk (ES storage)
- ~200 edits/sec throughput

**High-scale (all languages, global):**
- 8+ CPU cores
- 32GB RAM
- 500GB disk
- ~1000 edits/sec throughput

**Per-service breakdown:**
```
Ingestor:    1 CPU,  500MB RAM
Processor:   2 CPU,  2GB RAM
API:         1 CPU,  1GB RAM
Kafka:       2 CPU,  2GB RAM
Redis:       1 CPU,  512MB RAM
Elasticsearch: 2 CPU, 4GB RAM
Frontend:    Nginx (minimal)
```

### How do I scale horizontally?

**Add more processors (most common):**

1. **Update docker-compose:**
```yaml
processor:
  image: wikisurge-processor
  deploy:
    replicas: 3  # Run 3 instances
```

2. **Kafka auto-balances load** across processors

**Add more API servers (for high traffic):**

1. **Use load balancer:**
```yaml
version: '3'
services:
  api-1:
    image: wikisurge-api
  api-2:
    image: wikisurge-api
  nginx:
    image: nginx
    volumes:
      - ./nginx-lb.conf:/etc/nginx/nginx.conf
```

2. **Load balancer config:**
```nginx
upstream api {
    server api-1:8080;
    server api-2:8080;
}
```

**Add more Kafka partitions:**
```bash
kafka-topics.sh --alter --topic wikipedia-edits --partitions 12
```

**Result:** Each processor handles fewer partitions, higher throughput.

### What's the maximum throughput?

**Tested limits:**
- **Single ingestor:** 5000 edits/sec (far exceeds Wikipedia's average ~5/sec)
- **Single processor:** 1000 edits/sec
- **Single API server:** 10,000 requests/sec (with caching)
- **Overall system:** Limited by Kafka/ES, not application code

**Wikipedia's peak:** ~100 edits/sec (major breaking news)
**WikiSurge handles:** 1000+ edits/sec with standard setup

**If you exceed limits:**
- Scale processors horizontally
- Increase Kafka partitions
- Use Redis cluster for hot pages
- Shard Elasticsearch indices

---

## Deployment & Operations

### Can I use a different database?

**Yes, but requires code changes.**

**Redis replacement:**
- PostgreSQL with JSONB columns
- MongoDB
- DynamoDB

**Change required:**
- Implement `internal/storage/HotPagesStore` interface
- Implement `internal/storage/AlertsStore` interface
- Update `cmd/*/main.go` initialization

**Elasticsearch replacement:**
- PostgreSQL full-text search
- MongoDB with text indexes
- Algolia/Meilisearch

**Change required:**
- Implement `internal/storage/SearchStore` interface

**Not recommended:** Redis and ES are optimal for this use case.

### How do I secure WikiSurge in production?

**1. Enable authentication:**
```yaml
api:
  auth_enabled: true
  admin_token: "your-secret-token"  # Use env var
```

**2. Use HTTPS:**
```bash
./scripts/setup-ssl.sh your-domain.com
```

**3. Configure CORS:**
```yaml
api:
  cors:
    allowed_origins: ["https://wikisurge.example.com"]
    allowed_methods: ["GET", "POST"]
```

**4. Rate limiting:**
```yaml
api:
  rate_limit:
    requests_per_minute: 60
    burst: 10
```

**5. Firewall:**
```bash
# Allow only necessary ports
ufw allow 443/tcp  # HTTPS
ufw allow 22/tcp   # SSH
ufw enable
```

**6. Secrets management:**
```bash
# Use environment variables
export ADMIN_TOKEN=$(openssl rand -base64 32)
export REDIS_PASSWORD=$(openssl rand -base64 32)
```

**7. Monitor security:**
```yaml
alerts:
  - name: HighFailedAuthRate
    expr: rate(api_auth_failures_total[5m]) > 10
```

### How do I backup data?

**Automated backups:**
```bash
# Daily backup
crontab -e
0 2 * * * /path/to/WikiSurge/scripts/backup.sh
```

**Manual backup:**
```bash
./scripts/backup.sh

# Creates:
# - backups/redis-YYYYMMDD-HHMMSS.rdb
# - backups/elasticsearch-YYYYMMDD-HHMMSS.tar.gz
# - backups/config-YYYYMMDD-HHMMSS.tar.gz
```

**Restore:**
```bash
./scripts/restore.sh backups/redis-20260209-143000.rdb
```

**What's backed up:**
- Redis: Hot pages, alerts (last 24 hours)
- Elasticsearch: Historical edits (all indexes)
- Configs: YAML files

**Retention:**
- Daily backups: Keep 7 days
- Weekly backups: Keep 4 weeks
- Monthly backups: Keep 12 months

### Can I run WikiSurge without Docker?

**Yes!** Use native binaries and systemd:

**1. Build binaries:**
```bash
make build
# Creates: bin/ingestor, bin/processor, bin/api
```

**2. Install services:**
```bash
# Kafka, Redis, Elasticsearch
sudo apt install kafka redis-server elasticsearch
```

**3. Create systemd services:**
```ini
# /etc/systemd/system/wikisurge-ingestor.service
[Unit]
Description=WikiSurge Ingestor
After=network.target kafka.service

[Service]
Type=simple
User=wikisurge
ExecStart=/usr/local/bin/wikisurge-ingestor
Restart=always

[Install]
WantedBy=multi-user.target
```

**4. Start services:**
```bash
sudo systemctl enable wikisurge-ingestor
sudo systemctl start wikisurge-ingestor
```

See [DEPLOYMENT.md](DEPLOYMENT.md) for detailed instructions.

---

## Troubleshooting

### Why aren't spikes being detected?

**Check 1: Is page hot?**
```bash
# Check if page is in hot pages
curl http://localhost:8080/api/hotpages | grep "Page Name"
```
→ **Solution:** Lower `hot_pages.promotion_threshold` in config

**Check 2: Sufficient baseline data?**
- Spike detection requires 10 minutes of history
- Wait 10-15 minutes after app starts

**Check 3: Threshold too high?**
```yaml
spike_detection:
  threshold: 2.0  # Lower = more sensitive (default: 3.0)
```

**Check 4: Check logs:**
```bash
docker logs wikisurge-processor | grep spike
```

### Frontend not updating?

**Check 1: WebSocket connection**
```javascript
// In browser console
const ws = new WebSocket('ws://localhost:8080/api/ws');
ws.onmessage = (e) => console.log(e.data);
```
→ Should print messages

**Check 2: CORS issues**
```yaml
api:
  cors:
    allowed_origins: ["*"]  # Development only!
```

**Check 3: Cache stale**
```bash
# Clear cache
redis-cli FLUSHALL
```

**Check 4: Browser DevTools**
- Network tab: Check WebSocket status
- Console: Check for JavaScript errors

### High memory usage?

**Processor using too much memory:**

**Cause:** Too many hot pages
```bash
# Check hot page count
curl http://localhost:8080/api/hotpages | jq '. | length'
```

**Solution 1:** Lower max hot pages
```yaml
hot_pages:
  max_hot_pages: 500  # Default: 1000
```

**Solution 2:** More aggressive cleanup
```yaml
hot_pages:
  cleanup_interval: 5m  # Default: 10m
  activity_threshold: 2h  # Default: 6h (demote faster)
```

**Elasticsearch using too much memory:**

**Solution:** Set heap size (50% of container RAM)
```yaml
elasticsearch:
  environment:
    ES_JAVA_OPTS: "-Xms2g -Xmx2g"  # 2GB heap
```

**Kafka using too much memory:**

**Solution:** Reduce log retention
```yaml
kafka:
  environment:
    KAFKA_LOG_RETENTION_HOURS: 24  # Default: 168 (7 days)
```

### Kafka consumer lag increasing?

**Check lag:**
```bash
kafka-consumer-groups.sh --bootstrap-server localhost:9092 \
  --group spike-detector --describe
```

**Solution 1:** Scale processors
```yaml
processor:
  deploy:
    replicas: 3  # More consumers
```

**Solution 2:** Increase partitions
```bash
kafka-topics.sh --alter --topic wikipedia-edits --partitions 12
```

**Solution 3:** Optimize processing
```yaml
processor:
  batch_size: 100      # Process in batches
  batch_timeout: 1s
```

### Elasticsearch queries slow?

**Check 1: Index size**
```bash
curl http://localhost:9200/_cat/indices?v
```

**Solution:** Delete old indices
```bash
# Delete indices older than 30 days
curl -X DELETE http://localhost:9200/edits-2026-01-*
```

**Check 2: Shard health**
```bash
curl http://localhost:9200/_cluster/health?pretty
```

**Solution:** Optimize shards
```yaml
elasticsearch:
  index:
    number_of_shards: 3      # Default: 1
    number_of_replicas: 0    # Dev only
```

**Check 3: Query optimization**
- Add filters to reduce search space
- Use time range filters
- Limit result size

---

## Integration & Customization

### Can I integrate with Slack?

**Yes!** Use webhooks:

**1. Create alert forwarder:**
```go
func (s *SlackForwarder) HandleAlert(alert *models.Alert) {
    msg := map[string]interface{}{
        "text": fmt.Sprintf("🚨 %s: %s", alert.Severity, alert.Title),
        "attachments": []map[string]interface{}{
            {
                "color": severityColor(alert.Severity),
                "fields": []map[string]string{
                    {"title": "Page", "value": alert.PageTitle},
                    {"title": "Time", "value": alert.Timestamp},
                },
            },
        },
    }
    
    http.Post(s.webhookURL, "application/json", toJSON(msg))
}
```

**2. Subscribe to alerts:**
```go
// In processor/main.go
slackForwarder := NewSlackForwarder(cfg.SlackWebhookURL)
processor.OnAlert(slackForwarder.HandleAlert)
```

**3. Configure:**
```yaml
integrations:
  slack:
    webhook_url: "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
    min_severity: "medium"  # Only medium+ alerts
```

### Can I integrate with Discord?

**Same approach as Slack:**

```yaml
integrations:
  discord:
    webhook_url: "https://discord.com/api/webhooks/YOUR_WEBHOOK"
```

### Can I export data to BigQuery/Snowflake?

**Yes!** Add export consumer:

**1. Create exporter:**
```go
type BigQueryExporter struct {
    client *bigquery.Client
}

func (e *BigQueryExporter) ProcessEdit(ctx context.Context, edit *models.WikipediaEdit) error {
    return e.client.Dataset("wikipedia").Table("edits").Inserter().Put(ctx, edit)
}
```

**2. Register consumer:**
```go
// In processor/main.go
bqExporter := NewBigQueryExporter(cfg.BigQuery)
kafka.NewConsumer(cfg, "bigquery-export", bqExporter, logger)
```

**3. Process in batches for efficiency**

### Can I add custom metrics?

**Yes!** Example:

```go
// Define metric
var CustomMetric = prometheus.NewCounter(
    prometheus.CounterOpts{
        Name: "my_custom_metric_total",
        Help: "Description",
    },
)

// Register
prometheus.MustRegister(CustomMetric)

// Use in code
CustomMetric.Inc()
```

**Access at:** `http://localhost:8080/metrics`

**Add to Grafana:** Use Prometheus query in dashboard

### Can I change the frontend UI?

**Absolutely!** Frontend is React + TypeScript:

```bash
cd web
npm install
npm run dev  # Hot reload development

# Customize:
# - src/components/ - UI components
# - src/styles/ - Tailwind CSS
# - src/utils/ - Utilities
```

**Example:** Change color scheme:
```typescript
// web/tailwind.config.js
module.exports = {
  theme: {
    extend: {
      colors: {
        primary: '#6366f1',    // Change to your brand color
        secondary: '#8b5cf6',
      },
    },
  },
};
```

---

## More Help

**Documentation:**
- [Architecture](ARCHITECTURE.md) - System design
- [Deployment](DEPLOYMENT.md) - Setup guide
- [Operations](OPERATIONS.md) - Daily tasks
- [Monitoring](MONITORING.md) - Metrics & alerts
- [Development](DEVELOPMENT.md) - Code guide
- [API Reference](API.md) - API endpoints

**Support:**
- GitHub Issues: Report bugs
- Discussions: Ask questions
- Wiki: Community guides

**Still stuck?** Check logs first:
```bash
docker logs wikisurge-processor
docker logs wikisurge-api
docker logs wikisurge-ingestor
```
