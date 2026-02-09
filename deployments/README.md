# WikiSurge - Production Deployment Guide

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      Internet / Users                       │
└─────────────┬───────────────────────────────┬───────────────┘
              │ HTTP/HTTPS                    │ WSS
              ▼                               ▼
┌─────────────────────────────────────────────────────────────┐
│                   Nginx (Frontend)                          │
│              Static Files + Reverse Proxy                   │
│                    Port 80 / 443                            │
└─────────────┬───────────────────────────────┬───────────────┘
              │ /api/*                        │ /ws
              ▼                               ▼
┌─────────────────────────────────────────────────────────────┐
│                    API Server (x2)                          │
│              REST API + WebSocket Hub                       │
│                      Port 8080                              │
└──────┬──────────────────────────────────────┬───────────────┘
       │                                      │
       ▼                                      ▼
┌──────────────┐                    ┌─────────────────┐
│    Redis     │                    │ Elasticsearch   │
│   Port 6379  │                    │   Port 9200     │
└──────────────┘                    └─────────────────┘
       ▲                                      ▲
       │                                      │
┌──────────────────────────────────────────────────────────────┐
│                   Processor (x3)                             │
│     Spike Detection · Edit Wars · Trending · Indexing        │
└──────────────────────────┬───────────────────────────────────┘
                           │ Consume
                           ▼
┌──────────────────────────────────────────────────────────────┐
│                    Kafka (Redpanda)                           │
│                      Port 9092                               │
└──────────────────────────┬───────────────────────────────────┘
                           │ Produce
                           ▼
┌──────────────────────────────────────────────────────────────┐
│                       Ingestor                               │
│              Wikipedia SSE → Kafka Producer                  │
└──────────────────────────────────────────────────────────────┘
                           │
                           ▼
              Wikipedia EventStreams API

┌────────────────────────────────────────┐
│           Monitoring Stack             │
│  Prometheus (9090) → Grafana (3000)    │
│  Loki (3100) ← Promtail               │
└────────────────────────────────────────┘
```

## Prerequisites

| Requirement       | Minimum   | Recommended |
|--------------------|-----------|-------------|
| Docker             | 20.10+    | 24.x+       |
| Docker Compose     | 2.x       | 2.20+       |
| RAM                | 4 GB      | 8 GB        |
| Disk               | 10 GB     | 50 GB       |
| CPU Cores          | 2         | 4           |
| OS                 | Linux     | Ubuntu 22+  |

## Quick Start

```bash
# 1. Clone the repository
git clone https://github.com/Agnikulu/WikiSurge.git
cd WikiSurge

# 2. Configure environment
cp .env.prod.template .env.prod
# Edit .env.prod with your settings

# 3. Deploy
./scripts/deploy.sh

# 4. Verify
./scripts/health-check.sh
```

## Deployment Steps

### Step 1: Environment Configuration

Copy and customize the environment file:

```bash
cp .env.prod.template .env.prod
```

Key variables to review:

| Variable               | Default       | Description                         |
|------------------------|---------------|-------------------------------------|
| `REDIS_MAX_MEMORY`     | `512mb`       | Redis memory limit                  |
| `API_PORT`             | `8080`        | API server port                     |
| `FRONTEND_PORT`        | `80`          | Frontend HTTP port                  |
| `LOG_LEVEL`            | `warn`        | Log verbosity (debug/info/warn)     |
| `GRAFANA_ADMIN_PASSWORD` | `admin123`  | **Change in production!**           |
| `IMAGE_TAG`            | `latest`      | Docker image tag                    |

### Step 2: Deploy

```bash
# Full deployment (build + start + health check)
./scripts/deploy.sh

# Build images without deploying
./scripts/deploy.sh --build-only

# Deploy with pre-built images
./scripts/deploy.sh --no-build

# Deploy and tail logs
./scripts/deploy.sh --tail
```

### Step 3: Verify

```bash
# Health check
./scripts/health-check.sh

# JSON output (for CI/CD)
./scripts/health-check.sh --json

# Docker status
docker-compose -f deployments/docker-compose.prod.yml ps
```

## Service URLs

| Service        | URL                          | Purpose              |
|----------------|------------------------------|----------------------|
| Frontend       | http://localhost:80           | Web Dashboard        |
| API            | http://localhost:8080         | REST API             |
| API Health     | http://localhost:8080/health  | Health endpoint      |
| WebSocket      | ws://localhost:8080/ws/alerts | Real-time alerts     |
| Metrics        | http://localhost:2112/metrics | Prometheus metrics   |
| Prometheus     | http://localhost:9090         | Metrics UI           |
| Grafana        | http://localhost:3000         | Dashboards           |
| Kafka Admin    | http://localhost:9644         | Redpanda admin       |

## Resource Allocation

| Service        | Memory Limit | CPU Limit | Replicas |
|----------------|-------------|-----------|----------|
| Kafka          | 1 GB        | 1.0       | 1        |
| Redis          | 512 MB      | 0.5       | 1        |
| Elasticsearch  | 2 GB        | 1.0       | 1        |
| Ingestor       | 256 MB      | 0.5       | 1        |
| Processor      | 512 MB      | 0.5       | 3        |
| API            | 256 MB      | 0.5       | 2        |
| Frontend       | 128 MB      | 0.25      | 1        |
| Prometheus     | 512 MB      | 0.5       | 1        |
| Grafana        | 256 MB      | 0.25      | 1        |
| **Total**      | **~5.5 GB** | **~4.5**  |          |

## Backup & Restore

### Automated Backups

Schedule via cron:
```bash
# Redis: daily at 2 AM
0 2 * * * /path/to/scripts/backup.sh redis

# Elasticsearch: daily at 3 AM
0 3 * * * /path/to/scripts/backup.sh elasticsearch

# Everything: daily at 4 AM
0 4 * * * /path/to/scripts/backup.sh all
```

### Manual Backup
```bash
./scripts/backup.sh all           # Backup everything
./scripts/backup.sh redis         # Redis only
./scripts/backup.sh elasticsearch # Elasticsearch only
./scripts/backup.sh configs       # Config files only
```

### Restore
```bash
# List available backups
./scripts/restore.sh --list

# Restore from a specific backup
./scripts/restore.sh 20260208_020000

# Restore Redis only
./scripts/restore.sh 20260208_020000 redis
```

### Backup Configuration

| Setting              | Default  | Env Variable            |
|----------------------|----------|-------------------------|
| Retention            | 7 days   | `BACKUP_RETENTION_DAYS` |
| Compression          | enabled  | `BACKUP_COMPRESS`       |
| Cloud upload         | disabled | `CLOUD_UPLOAD`          |
| Backup directory     | `./backups/` | `BACKUP_BASE_DIR`  |

## Monitoring

### Grafana Dashboards

Access Grafana at http://localhost:3000 (default: admin/admin123).

Available dashboards:
- **System Overview** - All services health, error rates, resource usage
- **API Dashboard** - Request rates, latency percentiles, WebSocket connections
- **Ingestion Dashboard** - Edits/second, Kafka lag, filter rates
- **Processing Dashboard** - Consumer lag, processing latency, spike detection

### Alert Rules

Prometheus alerts are configured in `monitoring/alert-rules.yml`:

| Alert                    | Condition                  | Severity |
|--------------------------|---------------------------|----------|
| ServiceDown              | `up == 0` for 1m          | critical |
| HighKafkaConsumerLag     | lag > 1000 for 5m         | warning  |
| HighRedisMemory          | memory > 80% for 5m       | warning  |
| HighESDiskUsage          | disk > 80% for 10m        | warning  |
| HighAPIErrorRate         | errors > 1% for 5m        | warning  |
| IngestionStopped         | 0 edits for 5m            | critical |
| HighAPILatency           | p95 > 1s for 5m           | warning  |

### Log Management

Centralized logging is available via Loki + Promtail (optional):

```bash
# Enable by adding loki/promtail to docker-compose.prod.yml
# See monitoring/loki-config.yml for configuration
```

Log levels:
- **Production**: `warn` and above
- **Included**: timestamp, level, component, message, context
- **Excluded**: sensitive data (passwords, tokens) - auto-redacted by Promtail

## SSL/TLS Setup

### Self-Signed (Development)
```bash
./scripts/setup-ssl.sh --self-signed
```

### Let's Encrypt (Production)
```bash
./scripts/setup-ssl.sh yourdomain.com
```

This will:
1. Obtain a certificate via ACME challenge
2. Generate an SSL-enabled Nginx config
3. Set up auto-renewal via cron (every 60 days)
4. Configure HSTS, modern TLS, and security headers

## Scaling Guide

### Horizontal Scaling

**Processors** (stateless, scale freely):
```yaml
# In docker-compose.prod.yml
processor:
  deploy:
    replicas: 5  # Increase from 3
```

**API servers** (stateless, scale freely):
```yaml
api:
  deploy:
    replicas: 4  # Increase from 2
```

### Vertical Scaling

Adjust resource limits in `docker-compose.prod.yml`:
```yaml
deploy:
  resources:
    limits:
      memory: 1G    # Increase memory
      cpus: '2.0'   # Increase CPU
```

### When to Scale

| Symptom                      | Action                           |
|------------------------------|----------------------------------|
| High Kafka consumer lag      | Add processor replicas           |
| High API latency (p95 > 1s)  | Add API replicas                 |
| Redis memory > 80%           | Increase Redis memory limit      |
| ES disk > 80%                | Reduce retention / add storage   |
| Ingestion edits dropping     | Check ingestor logs / resources  |

## Rollback

If a deployment fails, the deploy script automatically rolls back. Manual rollback:

```bash
./scripts/deploy.sh --rollback
```

This restores images tagged as `previous` during the last deployment.

## Troubleshooting

### Service Won't Start

```bash
# Check logs
docker-compose -f deployments/docker-compose.prod.yml logs <service>

# Check resource availability
./scripts/check-resources.sh

# Restart specific service
docker-compose -f deployments/docker-compose.prod.yml restart <service>
```

### High Memory Usage

```bash
# Check container stats
docker stats

# Check Redis memory
docker exec wikisurge-redis redis-cli INFO memory

# Reduce Redis max-memory in .env.prod
```

### Kafka Consumer Lag

```bash
# Check consumer groups
docker exec wikisurge-kafka rpk group list
docker exec wikisurge-kafka rpk group describe wikisurge-prod

# Increase processor replicas
# Or check processor logs for errors
```

### Elasticsearch Issues

```bash
# Cluster health
curl localhost:9200/_cluster/health?pretty

# Index stats
curl localhost:9200/_cat/indices?v

# Disk usage
curl localhost:9200/_cat/allocation?v
```

### Connection Refused

1. Verify service is running: `docker-compose ps`
2. Check port bindings: `docker port <container>`
3. Check network: `docker network inspect wikisurge-prod`
4. Check firewall rules

## Security Considerations

1. **Change default passwords** in `.env.prod` (Grafana admin, etc.)
2. **Don't commit** `.env.prod` to git - use `.env.prod.template`
3. **Enable SSL/TLS** for production deployments
4. **Non-root containers** - all Go services run as non-root user
5. **Network isolation** - services use a dedicated Docker network
6. **Rate limiting** enabled on API endpoints
7. **Security headers** configured in Nginx (CSP, HSTS, X-Frame-Options)
8. **Log redaction** - sensitive data patterns automatically redacted

## File Structure

```
deployments/
├── docker-compose.prod.yml     # Production compose file
├── Dockerfile.api              # API multi-stage build
├── Dockerfile.ingestor         # Ingestor multi-stage build
├── Dockerfile.processor        # Processor multi-stage build
├── Dockerfile.frontend         # Frontend (React + Nginx)
├── nginx.conf                  # Nginx config (HTTP)
├── nginx-ssl.conf              # Nginx config (HTTPS)
└── ssl/                        # SSL certificates
scripts/
├── deploy.sh                   # Deployment script
├── health-check.sh             # Health check script
├── backup.sh                   # Backup script
├── restore.sh                  # Restore script
├── setup-ssl.sh                # SSL/TLS setup
└── check-resources.sh          # Resource check
monitoring/
├── prometheus-prod.yml         # Production Prometheus config
├── alert-rules.yml             # Prometheus alert rules
├── system-overview-dashboard.json  # Grafana system dashboard
├── loki-config.yml             # Loki log aggregation
├── promtail-config.yml         # Promtail log collection
└── grafana-provisioning/       # Grafana datasources & dashboards
.env.prod.template              # Environment variable template
.env.prod                       # Production environment (gitignored)
```
