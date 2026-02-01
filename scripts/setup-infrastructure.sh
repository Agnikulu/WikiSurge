#!/bin/bash

# WikiSurge Infrastructure Setup Script
# This script sets up the complete local development environment

set -e

echo "🚀 WikiSurge Infrastructure Setup"
echo "================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if Docker is installed and running
echo -n "📦 Checking Docker installation..."
if ! command -v docker &> /dev/null; then
    echo -e "${RED}❌ Docker is not installed${NC}"
    echo "Please install Docker: https://docs.docker.com/get-docker/"
    exit 1
fi

if ! docker info &> /dev/null; then
    echo -e "${RED}❌ Docker is not running${NC}"
    echo "Please start Docker daemon"
    exit 1
fi
echo -e "${GREEN}✅ Docker is ready${NC}"

# Check if docker-compose is available
echo -n "🔧 Checking Docker Compose..."
if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
    echo -e "${RED}❌ Docker Compose is not installed${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Docker Compose is ready${NC}"

# Run resource check
echo "🔍 Running resource check..."
./scripts/check-resources.sh
if [ $? -ne 0 ]; then
    echo -e "${YELLOW}⚠️  Resource check failed, but continuing...${NC}"
fi

# Pull all required images
echo "📥 Pulling Docker images..."
docker-compose pull

# Create volumes
echo "💾 Creating Docker volumes..."
docker volume create wikisurge_kafka-data
docker volume create wikisurge_redis-data
docker volume create wikisurge_es-data
docker volume create wikisurge_prometheus-data
docker volume create wikisurge_grafana-data

# Start services
echo "🌟 Starting services..."
docker-compose up -d

# Wait for services to be healthy
echo "⏳ Waiting for services to become healthy..."
MAX_ATTEMPTS=40
SLEEP_INTERVAL=3

check_service() {
    local service=$1
    local url=$2
    local expected_response=$3
    
    for i in $(seq 1 $MAX_ATTEMPTS); do
        if curl -s "$url" | grep -q "$expected_response" 2>/dev/null; then
            echo -e "${GREEN}✅ $service is healthy${NC}"
            return 0
        fi
        sleep $SLEEP_INTERVAL
    done
    echo -e "${RED}❌ $service failed to start${NC}"
    return 1
}

# Check Redpanda (Kafka)
echo -n "🔄 Checking Redpanda..."
if docker-compose exec -T kafka rpk cluster health &>/dev/null; then
    echo -e "${GREEN}✅ Redpanda is healthy${NC}"
else
    echo -e "${RED}❌ Redpanda is not healthy${NC}"
    exit 1
fi

# Check Redis
echo -n "🔄 Checking Redis..."
if docker-compose exec -T redis redis-cli ping | grep -q "PONG"; then
    echo -e "${GREEN}✅ Redis is healthy${NC}"
else
    echo -e "${RED}❌ Redis is not healthy${NC}"
    exit 1
fi

# Check Elasticsearch
echo -n "🔄 Checking Elasticsearch..."
if curl -s http://localhost:9200/_cluster/health | grep -q "yellow\|green"; then
    echo -e "${GREEN}✅ Elasticsearch is healthy${NC}"
else
    echo -e "${RED}❌ Elasticsearch is not healthy${NC}"
    exit 1
fi

# Check Prometheus
echo -n "🔄 Checking Prometheus..."
if curl -s http://localhost:9090/-/healthy | grep -q "Prometheus Server is Healthy"; then
    echo -e "${GREEN}✅ Prometheus is healthy${NC}"
else
    echo -e "${RED}❌ Prometheus is not healthy${NC}"
    exit 1
fi

# Check Grafana
echo -n "🔄 Checking Grafana..."
if curl -s http://localhost:3000/api/health | grep -q "ok"; then
    echo -e "${GREEN}✅ Grafana is healthy${NC}"
else
    echo -e "${RED}❌ Grafana is not healthy${NC}"
    exit 1
fi

# Create test topic in Kafka
echo "📝 Creating test topic..."
docker-compose exec -T kafka rpk topic create test --brokers localhost:9092 || true

echo ""
echo -e "${GREEN}🎉 Infrastructure setup complete!${NC}"
echo ""
echo "Service URLs:"
echo "  📊 Grafana:      http://localhost:3000 (admin/wikisurge123)"
echo "  📈 Prometheus:   http://localhost:9090"
echo "  🔍 Elasticsearch: http://localhost:9200"
echo "  📡 Kafka Admin:  http://localhost:9644"
echo "  🔴 Redis:        redis://localhost:6379"
echo ""
echo "Next steps:"
echo "  1. Run 'make health' to verify all services"
echo "  2. Start developing WikiSurge components"
echo "  3. Use 'make logs' to monitor service logs"

exit 0