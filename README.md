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