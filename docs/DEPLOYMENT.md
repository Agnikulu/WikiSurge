# WikiSurge Deployment Guide

## Table of Contents
- [Prerequisites](#prerequisites)
- [Local Development](#local-development)
- [Production Deployment](#production-deployment)
- [Configuration](#configuration)
- [Initial Setup](#initial-setup)
- [Verification](#verification)
- [Troubleshooting](#troubleshooting)

---

## Prerequisites

### System Requirements

**Minimum (Development):**
- CPU: 2 cores
- RAM: 8GB
- Disk: 20GB SSD
- OS: Linux, macOS, or Windows with WSL2

**Recommended (Production):**
- CPU: 4-8 cores
- RAM: 16-32GB
- Disk: 100GB+ SSD
- OS: Ubuntu 22.04 LTS or similar

### Software Dependencies

**Required:**
- **Docker** 24.0+ and Docker Compose 2.20+
- **Go** 1.23+ (for local development)
- **Node.js** 20+ and npm (for frontend)
- **Git** for version control

**Optional:**
- **Make** for build automation
- **jq** for JSON processing in scripts
- **curl** for API testing

### Installation

#### Ubuntu/Debian
```bash
# Update package list
sudo apt update

# Install Docker
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER
newgrp docker

# Install Docker Compose
sudo apt install docker-compose-plugin

# Install Go
wget https://go.dev/dl/go1.23.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.23.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Install Node.js
curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash -
sudo apt install -y nodejs

# Install Make
sudo apt install build-essential
```

#### macOS
```bash
# Install Homebrew if not present
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# Install dependencies
brew install docker docker-compose go node make
```

#### Windows (WSL2)
```powershell
# Install WSL2
wsl --install

# Inside WSL2, follow Ubuntu instructions above
```

---

## Local Development

### Quick Start

1. **Clone repository:**
```bash
git clone https://github.com/yourusername/WikiSurge.git
cd WikiSurge
```

2. **Start infrastructure (Kafka, Redis, Elasticsearch):**
```bash
docker-compose up -d kafka redis elasticsearch
```

3. **Wait for services to be ready:**
```bash
# Check services are healthy
docker-compose ps

# Wait for Kafka to be ready (takes ~30 seconds)
./scripts/test-infrastructure.sh
```

4. **Build and run backend:**
```bash
# Build all services
make build

# Run ingestor
./bin/ingestor --config configs/config.dev.yaml &

# Run processor
./bin/processor --config configs/config.dev.yaml &

# Run API server
./bin/api --config configs/config.dev.yaml &
```

5. **Run frontend:**
```bash
cd web
npm install
npm run dev
```

6. **Access dashboard:**
```
Open http://localhost:5173 in your browser
```

### Using Makefile

The project includes a Makefile for common tasks:

```bash
# Build all services
make build

# Run tests
make test

# Run linter
make lint

# Clean build artifacts
make clean

# Start all services
make run

# Stop all services
make stop

# View logs
make logs

# Run integration tests
make test-integration
```

### Configuration

Development uses `configs/config.dev.yaml`:

```yaml
wikimedia:
  stream_url: https://stream.wikimedia.org/v2/stream/recentchange

kafka:
  brokers: ["localhost:9092"]
  topic: "wikisurge.edits"

redis:
  addr: "localhost:6379"
  db: 0

elasticsearch:
  enabled: true
  addresses: ["http://localhost:9200"]

api:
  port: 8080
  rate_limit: 100  # Higher limit for development

ingestor:
  metrics_port: 2112

processor:
  features:
    spike_detection: true
    edit_wars: true
    trending: true
    elasticsearch: true
    websocket: true
```

### Development Workflow

#### Hot Reload (Backend)

Using `air` for hot reload:

```bash
# Install air
go install github.com/cosmtrek/air@latest

# Run with hot reload
cd cmd/api
air
```

#### Frontend Development

```bash
cd web
npm run dev  # Starts Vite dev server with HMR
```

#### Running Specific Components

```bash
# Only ingestor
./bin/ingestor --config configs/config.dev.yaml

# Only processor
./bin/processor --config configs/config.dev.yaml

# Only API
./bin/api --config configs/config.dev.yaml
```

#### Debugging

```bash
# Enable debug logging
export LOG_LEVEL=debug
./bin/processor --config configs/config.dev.yaml

# Run with Delve debugger
dlv debug ./cmd/api -- --config configs/config.dev.yaml
```

---

## Production Deployment

### Option 1: Docker Compose (Single Server)

**Best for:** Small to medium deployments, up to 5,000 edits/sec.

#### Step 1: Prepare Server

```bash
# Update system
sudo apt update && sudo apt upgrade -y

# Install Docker
curl -fsSL https://get.docker.com | sh

# Install Docker Compose
sudo apt install docker-compose-plugin

# Create user for WikiSurge
sudo useradd -m -s /bin/bash wikisurge
sudo usermod -aG docker wikisurge
```

#### Step 2: Deploy

```bash
# Switch to wikisurge user
sudo -u wikisurge bash

# Clone repository
cd /home/wikisurge
git clone https://github.com/yourusername/WikiSurge.git
cd WikiSurge

# Copy production config
cp configs/config.prod.yaml configs/config.yaml

# Edit configuration
nano configs/config.yaml
# Update:
# - API host/port
# - Redis password
# - Elasticsearch credentials
# - Kafka settings

# Build frontend
cd web
npm install
npm run build
cd ..

# Build backend
make build

# Start services
docker-compose -f deployments/docker-compose.prod.yml up -d
```

#### Step 3: Setup Nginx Reverse Proxy

```bash
sudo apt install nginx

# Copy configuration
sudo cp deployments/nginx.conf /etc/nginx/sites-available/wikisurge
sudo ln -s /etc/nginx/sites-available/wikisurge /etc/nginx/sites-enabled/

# Edit for your domain
sudo nano /etc/nginx/sites-available/wikisurge

# Test configuration
sudo nginx -t

# Reload Nginx
sudo systemctl reload nginx
```

#### Step 4: Setup SSL with Let's Encrypt

```bash
# Install Certbot
sudo apt install certbot python3-certbot-nginx

# Obtain certificate
sudo certbot --nginx -d wikisurge.yourdomain.com

# Auto-renewal is configured automatically
```

#### Step 5: Setup Systemd Services

Create `/etc/systemd/system/wikisurge.service`:

```ini
[Unit]
Description=WikiSurge Services
After=docker.service
Requires=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/home/wikisurge/WikiSurge
ExecStart=/usr/bin/docker-compose -f deployments/docker-compose.prod.yml up -d
ExecStop=/usr/bin/docker-compose -f deployments/docker-compose.prod.yml down
User=wikisurge
Group=wikisurge

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable wikisurge
sudo systemctl start wikisurge
```

---

### Option 2: VPS Deployment (Manual)

**Best for:** Custom configurations, specific requirements.

#### Step 1: Provision VPS

Recommended providers:
- DigitalOcean (Droplets)
- Linode
- Vultr
- Hetzner Cloud

**Specs:** 4-8 cores, 16GB RAM, 100GB SSD

#### Step 2: Secure Server

```bash
# Update system
sudo apt update && sudo apt upgrade -y

# Setup firewall
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow 22/tcp    # SSH
sudo ufw allow 80/tcp    # HTTP
sudo ufw allow 443/tcp   # HTTPS
sudo ufw enable

# Setup fail2ban
sudo apt install fail2ban
sudo systemctl enable fail2ban
sudo systemctl start fail2ban

# Disable root login
sudo nano /etc/ssh/sshd_config
# Set: PermitRootLogin no
sudo systemctl restart sshd
```

#### Step 3: Install Services

```bash
# Install dependencies
./scripts/setup-infrastructure.sh

# This script installs:
# - Docker and Docker Compose
# - Kafka
# - Redis
# - Elasticsearch
# - Nginx
```

#### Step 4: Configure Services

Edit `/etc/redis/redis.conf`:
```conf
bind 127.0.0.1
port 6379
requirepass your_secure_password_here
maxmemory 4gb
maxmemory-policy allkeys-lru
```

Edit Kafka `server.properties`:
```properties
listeners=PLAINTEXT://localhost:9092
log.retention.hours=24
log.segment.bytes=1073741824
num.partitions=6
```

Edit Elasticsearch `/etc/elasticsearch/elasticsearch.yml`:
```yaml
cluster.name: wikisurge
node.name: node-1
network.host: 127.0.0.1
http.port: 9200
xpack.security.enabled: true
```

#### Step 5: Deploy Application

```bash
# Build services
make build

# Copy binaries
sudo mkdir -p /opt/wikisurge/bin
sudo cp bin/* /opt/wikisurge/bin/
sudo cp -r configs /opt/wikisurge/
sudo cp -r web/dist /opt/wikisurge/frontend

# Setup systemd services
sudo cp deployments/systemd/*.service /etc/systemd/system/
sudo systemctl daemon-reload

# Start services
sudo systemctl start wikisurge-ingestor
sudo systemctl start wikisurge-processor
sudo systemctl start wikisurge-api

# Enable auto-start
sudo systemctl enable wikisurge-ingestor
sudo systemctl enable wikisurge-processor
sudo systemctl enable wikisurge-api
```

---

### Option 3: Cloud Deployment (AWS/GCP/Azure)

#### AWS Deployment

**Architecture:**
```
Route53 (DNS)
  → CloudFront (CDN) → S3 (Frontend)
  → ALB (Load Balancer)
    → ECS (API Containers)
    → ECS (Processor Containers)
  → MSK (Kafka)
  → ElastiCache (Redis)
  → OpenSearch (Elasticsearch)
```

**Step 1: Infrastructure as Code**

Use Terraform or CloudFormation:

```hcl
# terraform/main.tf
resource "aws_ecs_cluster" "wikisurge" {
  name = "wikisurge-cluster"
}

resource "aws_msk_cluster" "kafka" {
  cluster_name           = "wikisurge-kafka"
  kafka_version          = "3.5.1"
  number_of_broker_nodes = 3
  
  broker_node_group_info {
    instance_type = "kafka.m5.large"
    # ... configuration
  }
}

resource "aws_elasticache_cluster" "redis" {
  cluster_id           = "wikisurge-redis"
  engine               = "redis"
  node_type            = "cache.m5.large"
  num_cache_nodes      = 1
  parameter_group_name = "default.redis7"
}

# ... additional resources
```

**Step 2: Build and Push Container Images**

```bash
# Login to ECR
aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin <account-id>.dkr.ecr.us-east-1.amazonaws.com

# Build images
docker build -t wikisurge-api -f deployments/Dockerfile.api .
docker build -t wikisurge-ingestor -f deployments/Dockerfile.ingestor .
docker build -t wikisurge-processor -f deployments/Dockerfile.processor .

# Tag and push
docker tag wikisurge-api:latest <account-id>.dkr.ecr.us-east-1.amazonaws.com/wikisurge-api:latest
docker push <account-id>.dkr.ecr.us-east-1.amazonaws.com/wikisurge-api:latest
```

**Step 3: Deploy via ECS**

```bash
# Deploy using AWS CLI or Terraform
aws ecs update-service --cluster wikisurge-cluster --service wikisurge-api --force-new-deployment
```

#### GCP Deployment

Similar architecture using:
- Cloud Run (containers)
- Pub/Sub (instead of Kafka)
- Memorystore (Redis)
- Cloud Search (Elasticsearch alternative)

#### Azure Deployment

Similar architecture using:
- Azure Container Instances
- Azure Event Hubs (Kafka-compatible)
- Azure Cache for Redis
- Azure Cognitive Search

---

### Option 4: Kubernetes Deployment

**Best for:** Large scale, high availability, multi-region.

#### Prerequisites

- Kubernetes cluster (GKE, EKS, AKS, or self-hosted)
- kubectl installed and configured
- Helm 3.0+

#### Step 1: Install Dependencies

```bash
# Add Helm repos
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update

# Install Kafka
helm install kafka bitnami/kafka \
  --set persistence.size=100Gi \
  --set replicaCount=3

# Install Redis
helm install redis bitnami/redis \
  --set auth.password=your-password \
  --set master.persistence.size=10Gi

# Install Elasticsearch
helm install elasticsearch elastic/elasticsearch \
  --set replicas=3 \
  --set volumeClaimTemplate.resources.requests.storage=100Gi
```

#### Step 2: Deploy WikiSurge

Create Kubernetes manifests:

**ConfigMap** (`k8s/configmap.yaml`):
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: wikisurge-config
data:
  config.yaml: |
    kafka:
      brokers: ["kafka:9092"]
    redis:
      addr: "redis-master:6379"
    elasticsearch:
      addresses: ["http://elasticsearch:9200"]
    # ... rest of config
```

**Deployment** (`k8s/deployment.yaml`):
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: wikisurge-api
spec:
  replicas: 3
  selector:
    matchLabels:
      app: wikisurge-api
  template:
    metadata:
      labels:
        app: wikisurge-api
    spec:
      containers:
      - name: api
        image: wikisurge/api:latest
        ports:
        - containerPort: 8080
        env:
        - name: CONFIG_PATH
          value: /config/config.yaml
        volumeMounts:
        - name: config
          mountPath: /config
        resources:
          requests:
            memory: "512Mi"
            cpu: "500m"
          limits:
            memory: "1Gi"
            cpu: "1000m"
      volumes:
      - name: config
        configMap:
          name: wikisurge-config
```

**Service** (`k8s/service.yaml`):
```yaml
apiVersion: v1
kind: Service
metadata:
  name: wikisurge-api
spec:
  type: LoadBalancer
  ports:
  - port: 80
    targetPort: 8080
  selector:
    app: wikisurge-api
```

**Apply:**
```bash
kubectl apply -f k8s/
```

#### Step 3: Setup Ingress

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: wikisurge-ingress
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  tls:
  - hosts:
    - wikisurge.yourdomain.com
    secretName: wikisurge-tls
  rules:
  - host: wikisurge.yourdomain.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: wikisurge-api
            port:
              number: 80
```

---

## Configuration

### Environment Variables

**Precedence:** CLI flags > Environment variables > Config file > Defaults

**Common Variables:**

```bash
# Logging
export LOG_LEVEL=info          # debug, info, warn, error
export LOG_FORMAT=json         # json or text

# Kafka
export KAFKA_BROKERS=localhost:9092,localhost:9093
export KAFKA_TOPIC=wikisurge.edits

# Redis
export REDIS_ADDR=localhost:6379
export REDIS_PASSWORD=yourpassword
export REDIS_DB=0

# Elasticsearch
export ES_ADDRESSES=http://localhost:9200
export ES_USERNAME=elastic
export ES_PASSWORD=changeme

# API
export API_PORT=8080
export API_HOST=0.0.0.0
export CORS_ORIGINS=https://wikisurge.com

# Ingestor
export WIKIMEDIA_STREAM_URL=https://stream.wikimedia.org/v2/stream/recentchange
```

### Configuration Files

**Structure:**
```
configs/
├── config.dev.yaml      # Development (local)
├── config.minimal.yaml  # Minimal features (testing)
└── config.prod.yaml     # Production (optimized)
```

**Production Configuration:**

```yaml
# configs/config.prod.yaml
wikimedia:
  stream_url: https://stream.wikimedia.org/v2/stream/recentchange
  enabled_wikis: ["*.wikipedia"]

kafka:
  brokers: ["kafka-1:9092", "kafka-2:9092", "kafka-3:9092"]
  topic: "wikisurge.edits"
  partitions: 12
  replication_factor: 3
  compression: snappy
  batch_size: 1000
  batch_timeout: 100ms

redis:
  addr: "redis-cluster:6379"
  password: "${REDIS_PASSWORD}"  # From environment
  db: 0
  pool_size: 50
  hot_pages:
    hot_threshold: 3
    max_tracked: 2000
    cleanup_interval: 5m
  trending:
    prune_interval: 5m
    score_threshold: 0.1

elasticsearch:
  enabled: true
  addresses: ["https://es-1:9200", "https://es-2:9200", "https://es-3:9200"]
  username: "elastic"
  password: "${ES_PASSWORD}"
  index_prefix: "wikisurge"
  shards: 6
  replicas: 2
  refresh_interval: "5s"

api:
  port: 8080
  host: "0.0.0.0"
  cors_origins: ["https://wikisurge.com"]
  rate_limiting:
    enabled: true
    requests_per_minute: 100
    burst: 20
  cache_ttl: 10s

ingestor:
  batch_size: 500
  batch_timeout: 500ms
  metrics_port: 2112
  health_port: 8081

processor:
  features:
    spike_detection: true
    edit_wars: true
    trending: true
    elasticsearch: true
    websocket: true
  health_check_interval: 30s
  metrics_port: 2113

logging:
  level: info
  format: json
  output: stdout
```

### Feature Flags

Enable/disable features without redeploying:

```yaml
processor:
  features:
    spike_detection: true     # Enable spike detection
    edit_wars: true           # Enable edit war detection
    trending: true            # Enable trending calculation
    elasticsearch: false      # Disable ES indexing (save costs)
    websocket: true           # Enable WebSocket streaming
```

### Tuning Parameters

**For High Throughput:**
```yaml
kafka:
  batch_size: 2000          # Larger batches
  batch_timeout: 50ms       # Shorter timeout
  compression: snappy       # Fast compression

redis:
  pool_size: 100            # More connections

ingestor:
  batch_size: 1000
```

**For Low Latency:**
```yaml
kafka:
  batch_size: 100           # Smaller batches
  batch_timeout: 10ms       # Immediate flush

processor:
  poll_interval: 100ms      # More frequent polling
```

**For Cost Optimization:**
```yaml
processor:
  features:
    elasticsearch: false    # Disable indexing

redis:
  hot_pages:
    max_tracked: 500        # Fewer hot pages

elasticsearch:
  retention_days: 7         # Shorter retention
```

### Secrets Management

**Development:** `.env` file (not committed)

```bash
# .env
REDIS_PASSWORD=dev_password
ES_PASSWORD=dev_password
```

**Production:** Use secrets manager

**Docker Compose:**
```yaml
services:
  api:
    env_file:
      - /etc/wikisurge/secrets.env
```

**Kubernetes:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: wikisurge-secrets
type: Opaque
data:
  redis-password: <base64-encoded>
  es-password: <base64-encoded>
```

**AWS:** Use Secrets Manager or Parameter Store

```bash
# Store secret
aws secretsmanager create-secret \
  --name wikisurge/redis-password \
  --secret-string "your-password"

# Retrieve in application
aws secretsmanager get-secret-value \
  --secret-id wikisurge/redis-password \
  --query SecretString \
  --output text
```

---

## Initial Setup

### Database Initialization

**Redis:**
```bash
# No initialization needed
# Verify connection
redis-cli -h localhost -p 6379 PING
```

**Elasticsearch:**
```bash
# Create index template
curl -X PUT "localhost:9200/_index_template/wikisurge" \
  -H 'Content-Type: application/json' \
  -d @deployments/elasticsearch-template.json

# Verify
curl -X GET "localhost:9200/_index_template/wikisurge"
```

**Template** (`deployments/elasticsearch-template.json`):
```json
{
  "index_patterns": ["wikisurge-edits-*"],
  "template": {
    "settings": {
      "number_of_shards": 3,
      "number_of_replicas": 1,
      "refresh_interval": "5s"
    },
    "mappings": {
      "properties": {
        "title": {"type": "text", "fields": {"keyword": {"type": "keyword"}}},
        "user": {"type": "keyword"},
        "comment": {"type": "text"},
        "wiki": {"type": "keyword"},
        "timestamp": {"type": "date"},
        "length": {
          "properties": {
            "old": {"type": "integer"},
            "new": {"type": "integer"}
          }
        }
      }
    }
  }
}
```

### Kafka Topic Creation

```bash
# Create topic
docker exec -it kafka kafka-topics.sh \
  --create \
  --topic wikisurge.edits \
  --bootstrap-server localhost:9092 \
  --partitions 6 \
  --replication-factor 1 \
  --config compression.type=snappy \
  --config retention.ms=86400000

# Verify
docker exec -it kafka kafka-topics.sh \
  --describe \
  --topic wikisurge.edits \
  --bootstrap-server localhost:9092
```

Or use the setup script:
```bash
./scripts/setup-kafka-topic.sh
```

### Test Data Loading

```bash
# Run in demo mode (generates synthetic data)
./bin/ingestor --config configs/config.dev.yaml --demo

# Or replay from file
cat testdata/sample-edits.json | \
  ./scripts/replay-edits.sh
```

---

## Verification

### Health Checks

**Check all services:**
```bash
./scripts/health-check.sh
```

**Manual checks:**
```bash
# Ingestor
curl http://localhost:8081/health

# Processor
curl http://localhost:2113/health

# API
curl http://localhost:8080/health

# Kafka
docker exec kafka kafka-broker-api-versions.sh --bootstrap-server localhost:9092

# Redis
redis-cli PING

# Elasticsearch
curl http://localhost:9200/_cluster/health
```

### Smoke Tests

```bash
# Run automated smoke tests
./scripts/test-infrastructure.sh

# Test API endpoints
curl http://localhost:8080/api/stats
curl http://localhost:8080/api/trending
curl http://localhost:8080/api/alerts

# Test WebSocket
websocat ws://localhost:8080/ws/feed
```

### Monitoring Setup

**Access Grafana:**
```
http://localhost:3000
Username: admin
Password: admin (change on first login)
```

**Access Prometheus:**
```
http://localhost:9090
```

**Import Dashboards:**
```bash
# Dashboards are in monitoring/*.json
# Import via Grafana UI or provisioning
```

### Log Verification

```bash
# View all logs
docker-compose logs -f

# View specific service
docker-compose logs -f api

# Grep for errors
docker-compose logs | grep -i error

# Follow ingestor
tail -f /var/log/wikisurge/ingestor.log
```

---

## Troubleshooting

### Service Won't Start

**Symptoms:** Service exits immediately or fails to start.

**Diagnosis:**
```bash
# Check logs
docker-compose logs <service>

# Check configuration
./bin/<service> --config configs/config.yaml --validate

# Check dependencies
./scripts/test-infrastructure.sh
```

**Common Causes:**
1. **Port already in use:**
   ```bash
   # Find process using port
   lsof -i :8080
   # Kill process or change port
   ```

2. **Missing dependencies:**
   ```bash
   # Ensure Kafka, Redis, ES are running
   docker-compose ps
   ```

3. **Configuration error:**
   ```bash
   # Validate YAML syntax
   yamllint configs/config.yaml
   ```

### High Memory Usage

**Symptoms:** Service using >4GB RAM, OOM errors.

**Diagnosis:**
```bash
# Check memory usage
docker stats

# Check Go memory profiling
go tool pprof http://localhost:6060/debug/pprof/heap
```

**Solutions:**
1. **Reduce hot page limit:**
   ```yaml
   redis:
     hot_pages:
       max_tracked: 500  # Down from 1000
   ```

2. **Increase pruning frequency:**
   ```yaml
   redis:
     hot_pages:
       cleanup_interval: 2m  # Down from 5m
   ```

3. **Disable features:**
   ```yaml
   processor:
     features:
       elasticsearch: false
   ```

### Connection Errors

**Symptoms:** "connection refused", "timeout", "no route to host"

**Diagnosis:**
```bash
# Test connectivity
ping <host>
telnet <host> <port>
curl http://<host>:<port>/health

# Check firewall
sudo ufw status

# Check Docker network
docker network inspect wikisurge_default
```

**Solutions:**
1. **Check service is running:**
   ```bash
   docker-compose ps
   systemctl status wikisurge-api
   ```

2. **Check network configuration:**
   ```yaml
   # Ensure services are on same network
   networks:
     - wikisurge
   ```

3. **Check DNS resolution:**
   ```bash
   nslookup kafka
   ```

---

## Backup and Restore

### Backup

**Redis:**
```bash
# Manual save
redis-cli BGSAVE

# Copy dump file
cp /var/lib/redis/dump.rdb /backup/redis-$(date +%Y%m%d).rdb
```

**Elasticsearch:**
```bash
# Create snapshot repository
curl -X PUT "localhost:9200/_snapshot/backup" -H 'Content-Type: application/json' -d'
{
  "type": "fs",
  "settings": {
    "location": "/backup/elasticsearch"
  }
}'

# Take snapshot
curl -X PUT "localhost:9200/_snapshot/backup/snapshot-$(date +%Y%m%d)"
```

**Configuration:**
```bash
# Backup configs
tar -czf /backup/configs-$(date +%Y%m%d).tar.gz /etc/wikisurge/
```

### Restore

**Redis:**
```bash
# Stop Redis
docker-compose stop redis

# Replace dump file
cp /backup/redis-20260209.rdb /var/lib/redis/dump.rdb

# Start Redis
docker-compose start redis
```

**Elasticsearch:**
```bash
# Restore snapshot
curl -X POST "localhost:9200/_snapshot/backup/snapshot-20260209/_restore"
```

---

## Maintenance

### Updates

```bash
# Pull latest code
git pull origin main

# Rebuild
make build

# Restart services
docker-compose restart
```

### Scaling

**Horizontal:**
```bash
# Scale API servers
docker-compose up -d --scale api=3

# Scale processors
docker-compose up -d --scale processor=2
```

**Vertical:**
```yaml
# Increase resources
services:
  processor:
    deploy:
      resources:
        limits:
          cpus: '4'
          memory: 8G
```

---

For operational procedures, see [OPERATIONS.md](OPERATIONS.md).
For monitoring, see [MONITORING.md](MONITORING.md).
