# WikiSurge Recovery Procedures

This document describes how to detect, diagnose, resolve, and prevent failures
in the WikiSurge real-time Wikipedia edit processing pipeline.

---

## Table of Contents

1. [Redis Down](#1-redis-down)
2. [Elasticsearch Down](#2-elasticsearch-down)
3. [Kafka Lag / Broker Restart](#3-kafka-lag--broker-restart)
4. [Complete System Failure](#4-complete-system-failure)
5. [Poison Messages (DLQ)](#5-poison-messages-dlq)
6. [Memory Pressure](#6-memory-pressure)

---

## 1. Redis Down

### Detection
- **Health check** returns `"redis": {"status": "unhealthy"}` at `GET /health`.
- **Prometheus alert**: `redis_memory_usage_percent` goes to `0` or scrape fails.
- **Circuit breaker** for Redis opens: `circuit_breaker_state{breaker="redis"} == 1`.
- Application logs: `"Error connecting to Redis"` / `"circuit breaker is open"`.

### Diagnosis
```bash
# Check Redis process
redis-cli -h <host> -p 6379 ping

# Check container
docker ps --filter "name=redis"
docker logs wikisurge-redis --tail 50

# Check memory
redis-cli INFO memory | grep used_memory_human
```

### Resolution
1. **Restart Redis** (if crashed):
   ```bash
   docker-compose restart redis
   # or systemctl restart redis
   ```
2. **Wait for circuit breaker recovery** (~30 s after Redis is back):
   - The system automatically transitions to half-open → closed.
3. **Verify hot-page data**:
   - Hot-page windows are TTL-based and self-healing.
   - Trending scores will rebuild from incoming edits.
4. **If data loss occurred** (no persistence):
   ```bash
   # Trending and hot-page data will repopulate within 15–30 minutes
   # from live Wikipedia edits. No manual replay needed.
   ```

### Prevention
- Enable Redis persistence (`RDB` or `AOF`) in production.
- Set up Redis Sentinel or Cluster for high availability.
- Monitor `redis_memory_usage_percent` with Grafana alerts.
- Connection pool health checks run every 30 s (`HealthCheckInterval`).

---

## 2. Elasticsearch Down

### Detection
- **Health check**: `"elasticsearch": {"status": "unhealthy"}`.
- **Feature flag** auto-disabled: `feature_flag_enabled{feature="elasticsearch_indexing"} == 0`.
- **Circuit breaker**: `circuit_breaker_state{breaker="elasticsearch"} == 1`.
- Application logs: `"Elasticsearch unavailable — indexing disabled"`.

### Diagnosis
```bash
# Check ES cluster health
curl -s http://localhost:9200/_cluster/health | jq .

# Check disk space
curl -s http://localhost:9200/_cat/allocation?v

# Check container
docker logs wikisurge-elasticsearch --tail 50
```

### Resolution
1. **Restart Elasticsearch**:
   ```bash
   docker-compose restart elasticsearch
   ```
2. **Verify cluster status**:
   ```bash
   curl -s http://localhost:9200/_cluster/health | jq .status
   # Should return "green" or "yellow"
   ```
3. **Re-enable indexing** (automatic once circuit breaker closes, or manual):
   ```bash
   # The degradation manager re-enables indexing automatically.
   # To force: restart the processor service.
   ```
4. **Reindex missed data from Kafka** (if needed):
   ```bash
   # Reset consumer group offset to replay missed messages:
   kafka-consumer-groups.sh \
     --bootstrap-server localhost:9092 \
     --group wikisurge-indexer \
     --topic wikipedia.edits \
     --reset-offsets --to-datetime "2026-02-08T00:00:00.000" \
     --execute
   ```

### Prevention
- Monitor `elasticsearch_disk_usage_percent` — auto-action reduces retention
  to 3 days when >80%.
- Set ILM policies for automatic index rollover and deletion.
- Use a dedicated data volume with adequate space.

---

## 3. Kafka Lag / Broker Restart

### Detection
- **Prometheus**: `kafka_consumer_lag > 1000`.
- **Resource monitor** fires alert: `"Kafka lag at N (limit 1000)"`.
- **Auto-action**: ES indexing paused to prioritise real-time updates.
- Application logs: `"High Kafka lag — ES indexing paused"`.

### Diagnosis
```bash
# Check consumer group lag
kafka-consumer-groups.sh \
  --bootstrap-server localhost:9092 \
  --describe --group wikisurge

# Check broker status
kafka-broker-api-versions.sh --bootstrap-server localhost:9092

# Check topic partitions
kafka-topics.sh --bootstrap-server localhost:9092 \
  --describe --topic wikipedia.edits
```

### Resolution
1. **If broker restarted**: consumers auto-reconnect (session timeout 30 s).
2. **If lag is high**:
   - System automatically pauses ES indexing.
   - Once lag drops below threshold, indexing resumes automatically.
3. **Scale consumers** (if persistent lag):
   ```bash
   # Increase partition count (one-time):
   kafka-topics.sh --bootstrap-server localhost:9092 \
     --alter --topic wikipedia.edits --partitions 6

   # Start additional processor instances.
   ```
4. **If broker is permanently down**:
   ```bash
   docker-compose restart kafka
   # Wait for topic leadership re-election.
   ```

### Prevention
- Monitor `kafka_consumer_lag_seconds` in Grafana.
- Set appropriate `max_poll_records` to avoid batch overload.
- Use Kafka replication factor ≥ 2 in production.

---

## 4. Complete System Failure

Full restart procedure when all services are down.

### Step-by-step
```bash
# 1. Start infrastructure in dependency order
docker-compose up -d zookeeper
sleep 10
docker-compose up -d kafka
sleep 15
docker-compose up -d redis
sleep 5
docker-compose up -d elasticsearch
sleep 20

# 2. Verify infrastructure
redis-cli ping                                          # PONG
curl -s http://localhost:9200/_cluster/health | jq .     # green/yellow
kafka-topics.sh --bootstrap-server localhost:9092 --list # wikipedia.edits

# 3. Create Kafka topic (if missing)
./scripts/setup-kafka-topic.sh

# 4. Setup ES indices / ILM
# (Handled automatically on processor startup)

# 5. Start application services
./bin/ingestor -config configs/config.prod.yaml &
./bin/processor -config configs/config.prod.yaml &
./bin/api -config configs/config.prod.yaml &

# 6. Verify health
curl -s http://localhost:8080/health | jq .
# All components should show status "healthy"
```

### Verification checklist
- [ ] `GET /health` returns `200` with all components healthy.
- [ ] `GET /api/v1/trending` returns data within 5 minutes.
- [ ] WebSocket at `ws://localhost:8080/ws` receives live edits.
- [ ] Grafana dashboards show metrics flowing.

---

## 5. Poison Messages (DLQ)

### Detection
- **Prometheus**: `dlq_messages_total` increasing.
- **Alert**: `poison_high_dlq_rate_alerts_total > 0`.
- Application logs: `"Poison message detected"` / `"Message sent to dead letter queue"`.

### Diagnosis
```bash
# Check DLQ topic size
kafka-run-class.sh kafka.tools.GetOffsetShell \
  --broker-list localhost:9092 \
  --topic wikipedia.edits.dlq

# Inspect messages
kafkacat -C -b localhost:9092 -t wikipedia.edits.dlq -c 5 | jq .
```

### Resolution
1. **Inspect messages** to identify the corruption pattern.
2. **If upstream schema changed**: update parser and redeploy processor.
3. **Replay valid messages** from DLQ:
   ```bash
   # Read from DLQ, filter valid, produce back to main topic
   kafkacat -C -b localhost:9092 -t wikipedia.edits.dlq -e | \
     jq -r '.original_value' | \
     jq -c 'select(.title != null)' | \
     kafkacat -P -b localhost:9092 -t wikipedia.edits
   ```
4. **Purge DLQ** after investigation (retention is 7 days by default).

### Prevention
- Validate message schema at the ingestor before producing.
- Version the Kafka message schema.
- Monitor DLQ rate with Grafana alerts.

---

## 6. Memory Pressure

### Detection
- **Redis**: `redis_memory_usage_percent > 80`.
- **System**: high RSS in `top`/`htop`.
- **Auto-actions**: hot page limit reduced, trending may be disabled.

### Resolution
1. **Redis memory**:
   ```bash
   # Check what's using memory
   redis-cli --bigkeys
   redis-cli INFO memory

   # Manually evict if needed
   redis-cli FLUSHDB    # WARNING: loses all data
   ```
2. **Application memory**:
   ```bash
   # Check Go heap
   curl -s http://localhost:2112/debug/pprof/heap > heap.prof
   go tool pprof heap.prof
   ```
3. **Auto-recovery**: once memory drops below thresholds, the degradation
   manager automatically restores normal limits.

### Prevention
- Set `maxmemory` in Redis config.
- Use `allkeys-lru` eviction policy.
- Monitor with Prometheus + Grafana dashboards.
- Configure connection pool sizes appropriately (see `pool_config.go`).

---

## Quick Reference: Circuit Breaker States

| State     | Behaviour                        | Transition                          |
|-----------|----------------------------------|-------------------------------------|
| Closed    | All requests pass through        | → Open after 5 consecutive failures |
| Open      | All requests rejected instantly  | → Half-Open after 30 s timeout      |
| Half-Open | 1 probe request allowed          | → Closed on success, Open on fail   |

Manual reset: call `CircuitBreaker.Reset()` or restart the service.

---

## Quick Reference: Feature Flags

| Flag                       | Controls                  | Auto-disabled when          |
|----------------------------|---------------------------|-----------------------------|
| `elasticsearch_indexing`   | Document indexing to ES   | ES down, high Kafka lag     |
| `trending_tracking`        | Trending page scoring     | Redis memory critical       |
| `edit_war_detection`       | Edit war analysis         | (manual only)               |
| `websocket_broadcast`      | Live WebSocket updates    | (manual only)               |

Check current state: `GET /health` → `components` section.

---

## Contacts

- **On-call**: Check PagerDuty rotation.
- **Grafana dashboards**: `http://localhost:3000`.
- **Prometheus**: `http://localhost:9090`.
