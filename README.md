# WikiSurge

A real-time Wikipedia change monitoring and intelligence system built with Go, Kafka, Redis, and Elasticsearch.

## Overview

WikiSurge is a high-performance system that monitors Wikipedia changes in real-time, processes them through a streaming pipeline, and provides intelligent insights and alerts. The system ingests data from Wikipedia's Server-Sent Events (SSE) stream, processes it using Kafka for message queuing, stores hot data in Redis, indexes searchable content in Elasticsearch, and provides a modern web interface for monitoring and analytics.

## Architecture

- **Ingestor**: Consumes Wikipedia SSE stream and publishes to Kafka
- **Processor**: Processes Kafka messages and stores data in Redis/Elasticsearch
- **API**: REST API for querying and managing the system
- **Web Interface**: React-based dashboard for monitoring and analytics
- **Monitoring**: Prometheus metrics with Grafana dashboards

## Quick Start

### Prerequisites

- Docker and Docker Compose
- Go 1.21+ (for development)
- Minimum 4GB RAM and 10GB disk space

### Setup

1. **Clone and setup infrastructure:**
   ```bash
   git clone https://github.com/Agnikulu/WikiSurge.git
   cd WikiSurge
   make setup
   ```

2. **Check system health:**
   ```bash
   make health
   ```

3. **View service URLs:**
   ```bash
   make urls
   ```

### Available Commands

- `make setup` - Set up infrastructure
- `make start` - Start all services
- `make stop` - Stop all services  
- `make clean` - Clean up volumes and containers
- `make logs` - View service logs
- `make health` - Check service health
- `make build` - Build Go applications
- `make test` - Run tests

## Services

- **Grafana**: http://localhost:3000 (admin/admin)
- **Prometheus**: http://localhost:9090
- **Elasticsearch**: http://localhost:9200
- **Kafka**: localhost:9092
- **Redis**: localhost:6379

## Ingestion Layer

WikiSurge's ingestion layer is the entry point for real-time Wikipedia data. It connects to Wikipedia's Server-Sent Events (SSE) stream and processes edit events for downstream consumption.

### Quick Start - Ingestion Only

To run just the ingestion pipeline:

```bash
# Start Kafka and dependencies
docker-compose up -d kafka zookeeper prometheus

# Configure and start ingestion
make build
./bin/ingestor -config configs/config.dev.yaml

# Monitor ingestion metrics
curl localhost:2112/metrics | grep edits_ingested
```

### Key Features

- **Real-time Processing**: Processes Wikipedia edits as they happen
- **Intelligent Filtering**: Configurable bot, language, and edit type filters
- **Rate Limiting**: Prevents system overload with configurable rate limits
- **Auto-reconnection**: Robust connection handling with exponential backoff
- **Batched Production**: Efficient Kafka message production with batching
- **Comprehensive Monitoring**: Detailed Prometheus metrics and Grafana dashboards

### Configuration

Configure ingestion behavior in your config file:

```yaml
ingestor:
  exclude_bots: true                    # Filter bot edits
  allowed_languages: ["en", "es", "fr"] # Language whitelist  
  rate_limit: 50                        # Max events per second
  burst_limit: 100                      # Burst capacity
  reconnect_delay: 1s                   # Reconnection delay
  metrics_port: 2112                    # Metrics endpoint port
```

### Monitoring Dashboard

Import the Grafana dashboard for ingestion monitoring:

```bash
# Import ingestion dashboard
curl -X POST \
  http://admin:admin@localhost:3000/api/dashboards/db \
  -H 'Content-Type: application/json' \
  -d @monitoring/ingestion-dashboard.json
```

The dashboard provides real-time visibility into:
- Ingestion rate (events per second)
- Filter effectiveness (bot, language, type filters)
- Kafka production latency and throughput
- Error rates and buffer usage
- Connection health and reconnection statistics

### Performance Testing

Run load tests to validate system performance:

```bash
# Normal load test (10 eps for 5 minutes)
./test/load/simulate_sse.sh --rate=10 --duration=300

# Spike test (ramp 5→50 eps over 2 minutes)
./test/load/simulate_sse.sh --scenario=spike --duration=120

# Sustained high load (30 eps for 10 minutes) 
./test/load/simulate_sse.sh --rate=30 --duration=600

# Bursty load (alternate 5/50 eps every 30s)
./test/load/simulate_sse.sh --scenario=bursty --duration=300
```

### Testing

Comprehensive test suite for ingestion components:

```bash
# Unit tests
go test ./internal/ingestor/...
go test ./internal/kafka/...

# Integration tests
go test ./test/integration/...

# Benchmarks (performance validation)
go test -bench=. -benchmem ./test/benchmark/...
```

### Troubleshooting

Common issues and solutions:

**High Latency (p99 > 100ms)**
```bash
# Check Kafka broker health
kafka-topics --bootstrap-server localhost:9092 --list

# Monitor batch sizes and production rate
curl -s localhost:2112/metrics | grep kafka_produce_latency
```

**Production Errors**
```bash
# Check Kafka connectivity
kafka-console-producer --bootstrap-server localhost:9092 --topic wikipedia.edits

# Verify topic exists
kafka-topics --bootstrap-server localhost:9092 --describe --topic wikipedia.edits
```

**Connection Issues**
```bash
# Test Wikipedia SSE directly
curl -H "Accept: text/event-stream" \
     https://stream.wikimedia.org/v2/stream/recentchange

# Check reconnection metrics
curl -s localhost:2112/metrics | grep sse_reconnections_total
```

For detailed troubleshooting and configuration options, see [docs/INGESTION.md](docs/INGESTION.md).

## Development

The project follows a modular structure:

```
WikiSurge/
├── cmd/           # Application entry points
├── internal/      # Private application code
├── configs/       # Configuration files
├── monitoring/    # Prometheus & Grafana configs
├── scripts/       # Setup and utility scripts
└── web/          # Frontend application
```

## Configuration

- `config.dev.yaml` - Development configuration
- `config.minimal.yaml` - Minimal resource configuration
- `config.prod.yaml` - Production configuration

## Monitoring

The system includes comprehensive monitoring with:
- Ingestion rate metrics
- Kafka lag monitoring
- Redis memory usage
- Elasticsearch index statistics
- Custom application metrics

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

MIT License - see LICENSE file for details.