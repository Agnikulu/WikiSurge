# WikiSurge Operations Runbook

## Table of Contents
- [Daily Operations](#daily-operations)
- [Common Tasks](#common-tasks)
- [Troubleshooting](#troubleshooting)
- [Emergency Procedures](#emergency-procedures)
- [Maintenance Windows](#maintenance-windows)

---

## Daily Operations

### Starting Services

**Using Docker Compose:**
```bash
cd /opt/wikisurge
docker-compose up -d
```

**Using Systemd:**
```bash
sudo systemctl start wikisurge-ingestor
sudo systemctl start wikisurge-processor
sudo systemctl start wikisurge-api
```

**Using Kubernetes:**
```bash
kubectl scale deployment/wikisurge-api --replicas=3
kubectl scale deployment/wikisurge-processor --replicas=2
```

**Verify startup:**
```bash
# Check all services are healthy
./scripts/health-check.sh

# Expected output:
# ✓ Ingestor: healthy
# ✓ Processor: healthy
# ✓ API: healthy
# ✓ Kafka: healthy
# ✓ Redis: healthy
# ✓ Elasticsearch: healthy
```

---

### Stopping Services

**Graceful shutdown (recommended):**
```bash
# Docker Compose
docker-compose stop

# Systemd
sudo systemctl stop wikisurge-api
sudo systemctl stop wikisurge-processor
sudo systemctl stop wikisurge-ingestor
```

**Force stop (if hanging):**
```bash
# Docker
docker-compose kill

# Process
pkill -9 -f wikisurge
```

**Important:** Always stop API first, then processor, then ingestor to avoid data loss.

---

### Viewing Logs

**Real-time logs:**
```bash
# All services
docker-compose logs -f

# Specific service
docker-compose logs -f api
docker-compose logs -f processor

# Last 100 lines
docker-compose logs --tail=100 api

# Since timestamp
docker-compose logs --since=2026-02-09T10:00:00 processor
```

**Systemd logs:**
```bash
# Follow logs
journalctl -u wikisurge-api -f

# Last 50 lines
journalctl -u wikisurge-processor -n 50

# Today's logs
journalctl -u wikisurge-ingestor --since today
```

**Log files (if using file output):**
```bash
# Tail logs
tail -f /var/log/wikisurge/api.log

# Grep errors
grep -i error /var/log/wikisurge/*.log

# Search with context
grep -C 5 "panic" /var/log/wikisurge/processor.log
```

---

### Checking Health

**Service health endpoints:**
```bash
# Ingestor
curl http://localhost:8081/health | jq

# Processor
curl http://localhost:2113/health | jq

# API
curl http://localhost:8080/health | jq
```

**Expected response:**
```json
{
  "status": "healthy",
  "timestamp": "2026-02-09T15:30:00Z",
  "uptime": "2h30m15s",
  "components": {
    "kafka": "healthy",
    "redis": "healthy",
    "elasticsearch": "healthy"
  }
}
```

**Check resource usage:**
```bash
# Docker stats
docker stats --no-stream

# System resources
htop
vmstat 1
iostat -x 1
```

**Check network:**
```bash
# Active connections
netstat -anlp | grep :8080

# WebSocket connections
ss -tan | grep :8080 | wc -l
```

---

### Monitoring Dashboard

**Access Grafana:**
```
URL: http://localhost:3000
Default credentials: admin / admin
```

**Key dashboards:**
1. **System Overview** - High-level metrics
2. **Ingestion Dashboard** - Ingestor performance
3. **Processing Dashboard** - Processor metrics
4. **API Dashboard** - API performance

**What to watch:**
- **Edits/sec**: Should be 500-5000 normally
- **Processing lag**: Should be <5 seconds
- **Error rate**: Should be <0.1%
- **Memory usage**: Should be <80%
- **Disk usage**: Should be <85%

**Alert states:**
- 🟢 Green: All systems operational
- 🟡 Yellow: Warning threshold reached
- 🔴 Red: Critical issue, action required

---

## Common Tasks

### Adding New Language Filter

**Use case:** Only track specific Wikipedia languages.

**Steps:**

1. **Edit configuration:**
```yaml
# configs/config.yaml
wikimedia:
  enabled_wikis: ["enwiki", "frwiki", "dewiki"]  # Add/remove as needed
```

2. **Restart ingestor:**
```bash
docker-compose restart ingestor
```

3. **Verify:**
```bash
# Check logs for "Filtering enabled for wikis"
docker-compose logs ingestor | grep -i filter

# Verify edits coming through
curl http://localhost:8080/api/stats | jq '.languages'
```

**Alternative - Runtime filter (no restart):**
```bash
# Add filter via API (if implemented)
curl -X POST http://localhost:8080/admin/filters \
  -H 'Content-Type: application/json' \
  -d '{"wikis": ["enwiki", "frwiki"]}'
```

---

### Adjusting Rate Limits

**Use case:** Too many 429 errors from legitimate users, or need to tighten limits.

**Steps:**

1. **Check current limits:**
```bash
curl http://localhost:8080/api/stats | jq '.rate_limits'
```

2. **Edit configuration:**
```yaml
# configs/config.yaml
api:
  rate_limiting:
    enabled: true
    requests_per_minute: 200  # Increase from 100
    burst: 40                # Increase from 20
```

3. **Reload configuration (without restart):**
```bash
# Send SIGHUP to reload config
kill -HUP $(pgrep -f "bin/api")

# Or restart
docker-compose restart api
```

4. **Verify:**
```bash
# Test rate limit
for i in {1..150}; do
  curl -s http://localhost:8080/api/stats > /dev/null
done

# Should not get 429 errors within limit
```

**Per-IP override:**
```bash
# Whitelist IP in Redis
redis-cli SET ratelimit:whitelist:1.2.3.4 1 EX 3600
```

---

### Clearing Cache

**Use case:** Stale data showing, need to force refresh.

**Redis cache:**
```bash
# Clear specific keys
redis-cli DEL stats:cache:*
redis-cli DEL trending:en

# Clear all cache (use carefully!)
redis-cli FLUSHDB

# Clear hot pages
redis-cli --scan --pattern "hot:*" | xargs redis-cli DEL
```

**API response cache:**
```bash
# Restart API to clear in-memory cache
docker-compose restart api

# Or send cache clear signal
curl -X POST http://localhost:8080/admin/cache/clear
```

**Browser cache:**
```bash
# Deploy with cache-busting
# Update version in index.html
sed -i 's/\?v=[0-9]*/\?v='$(date +%s)'/' web/dist/index.html
```

---

### Reindexing Elasticsearch

**Use case:** Index corrupted, schema changed, or want to rebuild from Kafka replay.

**Full reindex:**

1. **Stop indexing:**
```yaml
processor:
  features:
    elasticsearch: false
```

2. **Delete old indices:**
```bash
curl -X DELETE "localhost:9200/wikisurge-edits-*"
```

3. **Create new index template:**
```bash
curl -X PUT "localhost:9200/_index_template/wikisurge" \
  -H 'Content-Type: application/json' \
  -d @deployments/elasticsearch-template.json
```

4. **Replay from Kafka:**
```bash
# Reset consumer group offset to beginning
docker exec kafka kafka-consumer-groups.sh \
  --bootstrap-server localhost:9092 \
  --group selective-indexer \
  --reset-offsets \
  --to-earliest \
  --topic wikisurge.edits \
  --execute
```

5. **Re-enable indexing:**
```yaml
processor:
  features:
    elasticsearch: true
```

6. **Monitor progress:**
```bash
# Check index document count
curl "localhost:9200/wikisurge-edits-*/_count"

# Monitor indexing rate
curl "localhost:9200/_cat/indices/wikisurge-edits-*?v"
```

**Partial reindex (date range):**
```bash
# Reindex only specific dates
POST _reindex
{
  "source": {
    "index": "wikisurge-edits-2026-02-09",
    "query": {
      "range": {
        "timestamp": {
          "gte": "2026-02-09T00:00:00",
          "lt": "2026-02-09T12:00:00"
        }
      }
    }
  },
  "dest": {
    "index": "wikisurge-edits-2026-02-09-new"
  }
}
```

---

### Backup and Restore

**Daily backup script** (add to cron):

```bash
#!/bin/bash
# /opt/wikisurge/scripts/backup.sh

BACKUP_DIR="/backup/wikisurge"
DATE=$(date +%Y%m%d-%H%M%S)

# Create backup directory
mkdir -p $BACKUP_DIR/$DATE

# Backup Redis
redis-cli BGSAVE
sleep 5
cp /var/lib/redis/dump.rdb $BACKUP_DIR/$DATE/redis.rdb

# Backup Elasticsearch snapshot
curl -X PUT "localhost:9200/_snapshot/backup/snapshot-$DATE"

# Backup configuration
tar -czf $BACKUP_DIR/$DATE/configs.tar.gz /opt/wikisurge/configs

# Cleanup old backups (keep 7 days)
find $BACKUP_DIR/* -mtime +7 -delete

echo "Backup completed: $BACKUP_DIR/$DATE"
```

**Schedule with cron:**
```bash
# Run daily at 2 AM
0 2 * * * /opt/wikisurge/scripts/backup.sh >> /var/log/wikisurge/backup.log 2>&1
```

**Restore from backup:**
```bash
#!/bin/bash
# /opt/wikisurge/scripts/restore.sh

BACKUP_DATE=$1  # e.g., 20260209-020000

if [ -z "$BACKUP_DATE" ]; then
  echo "Usage: $0 <backup_date>"
  exit 1
fi

BACKUP_DIR="/backup/wikisurge/$BACKUP_DATE"

# Stop services
docker-compose stop

# Restore Redis
cp $BACKUP_DIR/redis.rdb /var/lib/redis/dump.rdb

# Restore Elasticsearch
curl -X POST "localhost:9200/_snapshot/backup/snapshot-$BACKUP_DATE/_restore"

# Restore configs
tar -xzf $BACKUP_DIR/configs.tar.gz -C /

# Start services
docker-compose start

echo "Restore completed from $BACKUP_DIR"
```

---

### Scaling Operations

**Scale API servers (horizontal):**
```bash
# Docker Compose
docker-compose up -d --scale api=3

# Kubernetes
kubectl scale deployment/wikisurge-api --replicas=5

# Verify
kubectl get pods -l app=wikisurge-api
```

**Scale processors:**
```bash
# Scale up for high load
docker-compose up -d --scale processor=3

# Scale down during off-peak
docker-compose up -d --scale processor=1
```

**Scale Kafka consumers:**

Edit consumer configuration:
```yaml
# Increase parallelism
processor:
  consumers:
    spike_detector:
      instances: 3  # Up from 1
    edit_war_detector:
      instances: 2
```

**Add Kafka partitions:**
```bash
# Increase partitions for more parallelism
docker exec kafka kafka-topics.sh \
  --bootstrap-server localhost:9092 \
  --topic wikisurge.edits \
  --alter \
  --partitions 12
```

---

## Troubleshooting

### Service Won't Start

**Symptom:** Service exits immediately after starting.

**Diagnosis:**

1. **Check logs:**
```bash
docker-compose logs ingestor
journalctl -u wikisurge-processor -n 50
```

2. **Check configuration:**
```bash
# Validate YAML syntax
yamllint configs/config.yaml

# Check for environment variables
env | grep WIKISURGE
```

3. **Check dependencies:**
```bash
# Ensure Kafka, Redis, ES are running
docker-compose ps
```

**Common solutions:**

**Port conflict:**
```bash
# Find process using port
lsof -i :8080

# Kill process or change port in config
```

**Missing permissions:**
```bash
# Check file ownership
ls -la configs/

# Fix permissions
chown -R wikisurge:wikisurge /opt/wikisurge
```

**Database connection failed:**
```bash
# Test connectivity
telnet localhost 6379
telnet localhost 9092

# Check credentials
redis-cli -a your_password PING
```

---

### High Memory Usage

**Symptom:** Service using excessive memory, OOM killer activated.

**Diagnosis:**

```bash
# Check memory usage
docker stats
ps aux --sort=-%mem | head

# Go memory profile
curl http://localhost:6060/debug/pprof/heap > heap.prof
go tool pprof -http=:8081 heap.prof
```

**Common causes:**

1. **Too many hot pages tracked:**
```bash
# Check count
redis-cli --scan --pattern "hot:window:*" | wc -l

# Reduce limit
# configs/config.yaml
redis:
  hot_pages:
    max_tracked: 500  # Down from 1000
```

2. **Memory leak:**
```bash
# Identify leak with pprof
go tool pprof http://localhost:6060/debug/pprof/heap

# Look for growing allocations
(pprof) top
(pprof) list <function_name>
```

3. **Large WebSocket backlog:**
```bash
# Check WebSocket client count
curl http://localhost:8080/metrics | grep websocket_clients

# Add backpressure handling
# or limit client connections
```

**Solutions:**

```bash
# Restart service to reclaim memory
docker-compose restart processor

# Set memory limits
docker-compose up -d --scale processor=1 --memory=2g

# Enable GC tuning
export GOGC=50  # More aggressive GC
```

---

### Slow Queries

**Symptom:** API responses taking >1 second, timeouts.

**Diagnosis:**

```bash
# Check API latency
curl -w "@curl-format.txt" -o /dev/null -s http://localhost:8080/api/trending

# Monitor slow queries
tail -f /var/log/wikisurge/api.log | grep "slow_query"

# Check Elasticsearch
curl "localhost:9200/_cat/thread_pool?v" | grep search
```

**curl-format.txt:**
```
time_total: %{time_total}s
time_namelookup: %{time_namelookup}s
time_connect: %{time_connect}s
time_starttransfer: %{time_starttransfer}s
```

**Common solutions:**

**Redis slow:**
```bash
# Identify slow commands
redis-cli SLOWLOG GET 10

# Optimize data structures
# Use indexes, pipelines, Lua scripts
```

**Elasticsearch slow:**
```bash
# Check slow search log
curl "localhost:9200/wikisurge-edits-*/_settings" | \
  grep "slowlog"

# Optimize queries
# Add filters, reduce size, use scroll API
```

**Add caching:**
```yaml
api:
  cache_ttl: 30s  # Increase from 10s
```

---

### Missing Data

**Symptom:** Dashboard showing no data, or data gaps.

**Diagnosis:**

```bash
# Check ingestor is receiving data
curl http://localhost:8081/metrics | grep ingested_total

# Check Kafka has messages
docker exec kafka kafka-consumer-groups.sh \
  --bootstrap-server localhost:9092 \
  --group processor \
  --describe

# Check Redis has data
redis-cli --scan --pattern "hot:*" | head
redis-cli KEYS "trending:*"

# Check Elasticsearch
curl "localhost:9200/wikisurge-edits-*/_count"
```

**Common causes:**

1. **Ingestor disconnected:**
```bash
# Check ingestor logs
docker-compose logs ingestor | grep -i "disconnected\|error"

# Restart ingestor
docker-compose restart ingestor
```

2. **Kafka consumer lag:**
```bash
# Check lag
docker exec kafka kafka-consumer-groups.sh \
  --bootstrap-server localhost:9092 \
  --group processor \
  --describe

# Reset to latest if too far behind
docker exec kafka kafka-consumer-groups.sh \
  --bootstrap-server localhost:9092 \
  --group processor \
  --reset-offsets \
  --to-latest \
  --topic wikisurge.edits \
  --execute
```

3. **Data filtered out:**
```bash
# Check filter configuration
grep -r "enabled_wikis" configs/

# Verify data is being processed
redis-cli MONITOR | grep -i "trending\|hot"
```

---

### High CPU Usage

**Symptom:** CPU at 100%, service unresponsive.

**Diagnosis:**

```bash
# Identify process
top
htop

# Check goroutines
curl http://localhost:6060/debug/pprof/goroutine?debug=1

# CPU profile
curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof cpu.prof
```

**Common causes:**

1. **Infinite loop:**
```bash
# Check stack traces
curl http://localhost:6060/debug/pprof/goroutine?debug=2 | \
  grep -A 10 "goroutine"
```

2. **Heavy processing:**
```bash
# Check processing rate
curl http://localhost:2113/metrics | grep processed_edits_total

# Reduce load by scaling
docker-compose up -d --scale processor=2
```

3. **GC thrashing:**
```bash
# Check GC stats
curl http://localhost:6060/debug/pprof/heap?debug=1 | grep GC

# Increase heap size
export GOGC=200
```

---

### Connection Errors

**Symptom:** "connection refused", "timeout", "i/o timeout"

**Diagnosis:**

```bash
# Test connectivity
telnet <host> <port>
nc -zv <host> <port>

# Check service is running
docker-compose ps
systemctl status wikisurge-*

# Check network
ip route
iptables -L
docker network inspect wikisurge_default
```

**Common solutions:**

**Firewall blocking:**
```bash
# Check firewall
sudo ufw status

# Allow port
sudo ufw allow 8080/tcp
```

**Service not listen on correct interface:**
```yaml
# Listen on all interfaces
api:
  host: "0.0.0.0"  # Not "localhost"
```

**DNS issue:**
```bash
# Check resolution
nslookup kafka
dig redis

# Use IP instead
# Or add to /etc/hosts
```

---

## Emergency Procedures

### System Overload

**Symptoms:**
- All services slow
- High CPU/memory across board
- Many 503 errors

**Immediate actions:**

1. **Enable maintenance mode:**
```bash
# Return 503 from API
touch /opt/wikisurge/maintenance-mode

# Nginx will serve maintenance page
```

2. **Reduce load:**
```bash
# Stop non-critical consumers
docker-compose stop selective-indexer
docker-compose stop ws-forwarder

# Reduce Kafka poll rate
# Temporarily edit configs
```

3. **Scale up:**
```bash
# Add more API servers
docker-compose up -d --scale api=5

# Add more processors
docker-compose up -d --scale processor=3
```

4. **Investigate cause:**
```bash
# Check for spike in traffic
tail -f /var/log/nginx/access.log

# Check Wikipedia event rate
curl https://stream.wikimedia.org/v2/stream/recentchange
```

---

### Data Loss

**Symptoms:**
- Kafka messages lost
- Redis data missing
- Elasticsearch indices corrupted

**Recovery:**

**Kafka messages:**
```bash
# Check retention
docker exec kafka kafka-topics.sh \
  --describe \
  --topic wikisurge.edits

# Increase retention if needed
docker exec kafka kafka-configs.sh \
  --alter \
  --topic wikisurge.edits \
  --add-config retention.ms=604800000  # 7 days
```

**Redis data:**
```bash
# Restore from backup
./scripts/restore.sh 20260209-020000

# Or rebuild from Kafka replay
# Reset consumer offset, restart processor
```

**Elasticsearch:**
```bash
# Restore from snapshot
curl -X POST "localhost:9200/_snapshot/backup/snapshot-20260209/_restore"
```

---

### Security Incident

**Symptoms:**
- Unusual traffic patterns
- Unauthorized access attempts
- Suspicious data modifications

**Immediate actions:**

1. **Block source:**
```bash
# Block IP
sudo iptables -A INPUT -s 1.2.3.4 -j DROP
redis-cli SET ratelimit:block:1.2.3.4 1
```

2. **Enable authentication:**
```yaml
api:
  auth:
    enabled: true
    require_api_key: true
```

3. **Review logs:**
```bash
# Check access patterns
awk '{print $1}' /var/log/nginx/access.log | \
  sort | uniq -c | sort -rn | head -20

# Check for suspicious requests
grep -i "select\|union\|script" /var/log/nginx/access.log
```

4. **Rotate credentials:**
```bash
# Change Redis password
redis-cli CONFIG SET requirepass newpassword

# Update config
sed -i 's/old_password/new_password/' configs/config.yaml

# Restart services
docker-compose restart
```

---

## Maintenance Windows

### Planned Maintenance

**Schedule:** Sundays 02:00-04:00 UTC (lowest traffic)

**Pre-maintenance checklist:**

```bash
# 1. Notify users (24h advance)
curl -X POST http://localhost:8080/admin/broadcast \
  -d '{"message": "Maintenance window Sunday 02:00-04:00 UTC"}'

# 2. Take backup
./scripts/backup.sh

# 3. Verify backup
./scripts/verify-backup.sh

# 4. Document current state
./scripts/health-check.sh > pre-maintenance-state.txt
docker-compose ps >> pre-maintenance-state.txt
```

**During maintenance:**

```bash
# 1. Enable maintenance mode
touch /opt/wikisurge/maintenance-mode

# 2. Stop services gracefully
docker-compose stop

# 3. Perform updates
git pull
make build
docker-compose build

# 4. Update dependencies
docker-compose pull

# 5. Start services
docker-compose up -d

# 6. Smoke test
./scripts/health-check.sh

# 7. Disable maintenance mode
rm /opt/wikisurge/maintenance-mode
```

**Post-maintenance verification:**

```bash
# 1. Check all services healthy
./scripts/health-check.sh

# 2. Monitor for errors
docker-compose logs -f --tail=100

# 3. Check metrics
curl http://localhost:8080/api/stats

# 4. Verify user access
curl https://wikisurge.com/api/trending

# 5. Send all-clear notification
curl -X POST http://localhost:8080/admin/broadcast \
  -d '{"message": "Maintenance complete. All systems operational."}'
```

---

### Rollback Procedure

**When to rollback:**
- New version showing critical bugs
- Performance degradation
- Data corruption

**Steps:**

```bash
# 1. Identify last good version
git tag -l | tail -5

# 2. Stop current version
docker-compose down

# 3. Checkout previous version
git checkout v1.2.3

# 4. Rebuild
make build
docker-compose build

# 5. Restore configuration if needed
cp configs/config.yaml.backup configs/config.yaml

# 6. Start previous version
docker-compose up -d

# 7. Verify
./scripts/health-check.sh

# 8. Monitor closely
docker-compose logs -f
```

---

For deployment instructions, see [DEPLOYMENT.md](DEPLOYMENT.md).
For monitoring procedures, see [MONITORING.md](MONITORING.md).
For development guide, see [DEVELOPMENT.md](DEVELOPMENT.md).
