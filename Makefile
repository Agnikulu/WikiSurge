.PHONY: setup start stop clean logs health test

# Setup infrastructure
setup:
	@echo "Setting up WikiSurge infrastructure..."
	@./scripts/setup-infrastructure.sh

# Start all Docker services
start:
	@echo "Starting all services..."
	@docker-compose up -d

# Stop all services
stop:
	@echo "Stopping all services..."
	@docker-compose down

# Stop and remove volumes
clean:
	@echo "Cleaning up services and volumes..."
	@docker-compose down -v
	@docker system prune -f

# Tail logs from all services
logs:
	@docker-compose logs -f

# Check health of all services
health:
	@echo "Checking service health..."
	@echo "=== Docker Services Status ==="
	@docker-compose ps
	@echo ""
	@echo "=== Redis Health ==="
	@docker-compose exec redis redis-cli ping || echo "Redis not responding"
	@echo ""
	@echo "=== Kafka Health ==="
	@docker-compose exec kafka rpk cluster health || echo "Kafka not responding"
	@echo ""
	@echo "=== Elasticsearch Health ==="
	@curl -s http://localhost:9200/_cluster/health?pretty || echo "Elasticsearch not responding"
	@echo ""
	@echo "=== Prometheus Health ==="
	@curl -s http://localhost:9090/-/healthy || echo "Prometheus not responding"
	@echo ""
	@echo "=== Grafana Health ==="
	@curl -s http://localhost:3000/api/health || echo "Grafana not responding"

# Run all tests (placeholder)
test:
	@echo "Running tests..."
	@go test ./... -v

# Build all Go applications
build:
	@echo "Building applications..."
	@go build -o bin/ingestor ./cmd/ingestor
	@go build -o bin/processor ./cmd/processor
	@go build -o bin/api ./cmd/api

# Install Go dependencies
deps:
	@echo "Installing dependencies..."
	@go mod tidy
	@go mod download

# Show service URLs
urls:
	@echo "=== Service URLs ==="
	@echo "Grafana: http://localhost:3000 (admin/admin)"
	@echo "Prometheus: http://localhost:9090"
	@echo "Elasticsearch: http://localhost:9200"
	@echo "Kafka: localhost:9092"
	@echo "Redis: localhost:6379"
	@echo "API (when running): http://localhost:8080"