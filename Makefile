.PHONY: setup kafka-setup start stop stop-all clean clean-data reset logs health test dev dev-backend dev-web prod help wait-for-es wait-for-kafka wait-for-redis

# Configuration - dev mode by default (includes Prometheus + Grafana)
COMPOSE_FILE := deployments/docker-compose.dev.yml
CONFIG_FILE := configs/config.prod.yaml

# Show available commands
help:
	@echo "WikiSurge - Available Commands"
	@echo "==============================="
	@echo ""
	@echo "Quick Start:"
	@echo "  make dev         - Start all services (includes Prometheus + Grafana)"
	@echo "  make prod        - Start lean stack (no monitoring)"
	@echo "  make dev-web     - Start local web app with hot reload (optional)"
	@echo "  make reset       - Stop everything and full clean"
	@echo ""
	@echo "Service Control:"
	@echo "  make start       - Start Docker services"
	@echo "  make stop        - Stop Docker services"
	@echo "  make stop-all    - Stop all Docker services"
	@echo "  make dev-backend - Restart backend containers only"
	@echo ""
	@echo "Build & Test:"
	@echo "  make build       - Build Go applications locally"
	@echo "  make test        - Run all tests"
	@echo "  make deps        - Install Go and web dependencies"
	@echo ""
	@echo "Monitoring:"
	@echo "  make health      - Check service health"
	@echo "  make logs        - View Docker logs"
	@echo "  make urls        - Show service URLs"
	@echo ""
	@echo "Cleanup:"
	@echo "  make clean-data  - Clear logs, Redis, ES, Kafka (keep containers)"
	@echo "  make clean       - Remove containers, volumes, logs, binaries, and web artifacts"
	@echo ""

# Wait for Elasticsearch to be ready
wait-for-es:
	@echo "Waiting for Elasticsearch to be ready..."
	@timeout=120; \
	elapsed=0; \
	while [ $$elapsed -lt $$timeout ]; do \
		if curl -sf http://localhost:9200/_cluster/health?wait_for_status=yellow&timeout=1s > /dev/null 2>&1; then \
			echo "✅ Elasticsearch is ready!"; \
			exit 0; \
		fi; \
		echo "⏳ Waiting for Elasticsearch... ($$elapsed/$$timeout seconds)"; \
		sleep 3; \
		elapsed=$$((elapsed + 3)); \
	done; \
	echo "❌ Elasticsearch failed to start within $$timeout seconds"; \
	exit 1

# Wait for Kafka to be ready
wait-for-kafka:
	@echo "Waiting for Kafka to be ready..."
	@timeout=60; \
	elapsed=0; \
	while [ $$elapsed -lt $$timeout ]; do \
		if docker-compose -f $(COMPOSE_FILE) exec -T kafka rpk cluster health > /dev/null 2>&1; then \
			echo "✅ Kafka is ready!"; \
			exit 0; \
		fi; \
		echo "⏳ Waiting for Kafka... ($$elapsed/$$timeout seconds)"; \
		sleep 2; \
		elapsed=$$((elapsed + 2)); \
	done; \
	echo "❌ Kafka failed to start within $$timeout seconds"; \
	exit 1

# Wait for Redis to be ready
wait-for-redis:
	@echo "Waiting for Redis to be ready..."
	@timeout=30; \
	elapsed=0; \
	while [ $$elapsed -lt $$timeout ]; do \
		if docker-compose -f $(COMPOSE_FILE) exec -T redis redis-cli ping > /dev/null 2>&1; then \
			echo "✅ Redis is ready!"; \
			exit 0; \
		fi; \
		echo "⏳ Waiting for Redis... ($$elapsed/$$timeout seconds)"; \
		sleep 2; \
		elapsed=$$((elapsed + 2)); \
	done; \
	echo "❌ Redis failed to start within $$timeout seconds"; \
	exit 1

# Stop all services
stop-all:
	@echo "Stopping all services..."
	@docker-compose -f $(COMPOSE_FILE) down 2>/dev/null || true
	@docker-compose down 2>/dev/null || true
	@echo "✅ All services stopped!"

# Full reset - stop everything and clean
reset: stop-all clean
	@echo "✅ Full reset complete!"

# Clean only data (logs, Redis, Elasticsearch) without rebuilding containers
clean-data:
	@echo "Cleaning data without rebuilding containers..."
	@echo "Removing log files..."
	@rm -f *.log
	@echo "Clearing Redis data..."
	@docker-compose -f $(COMPOSE_FILE) exec -T redis redis-cli FLUSHALL 2>/dev/null || true
	@echo "Clearing Elasticsearch indices..."
	@curl -sf -X DELETE "http://localhost:9200/wikipedia-*" 2>/dev/null || true
	@echo "Clearing Kafka topic data..."
	@docker-compose -f $(COMPOSE_FILE) exec -T kafka rpk topic delete wikipedia.edits 2>/dev/null || true
	@docker-compose -f $(COMPOSE_FILE) exec -T kafka rpk topic create wikipedia.edits --partitions 3 --replicas 1 2>/dev/null || true
	@echo "✅ Data cleared!"

# Development mode - start all services including monitoring
# All services + Prometheus + Grafana run in containers
dev:
	@echo "Starting development environment (DEV MODE)..."
	@echo "Using: $(COMPOSE_FILE)"
	@echo "Config: $(CONFIG_FILE)"
	@echo ""
	@# Check if containers are already running, if not start them
	@if docker-compose -f $(COMPOSE_FILE) ps 2>/dev/null | grep -q "Up\|running"; then \
		echo "📦 Docker containers already running, reusing..."; \
	else \
		echo "🚀 Starting Docker services..."; \
		docker-compose -f $(COMPOSE_FILE) up -d; \
	fi
	@echo ""
	@# Wait for infrastructure to be healthy
	@$(MAKE) wait-for-redis
	@echo ""
	@$(MAKE) wait-for-kafka
	@echo ""
	@$(MAKE) wait-for-es
	@echo ""
	@# Clean old data (logs, Redis, ES, Kafka)
	@$(MAKE) clean-data
	@echo ""
	@echo "✅ All services started!"
	@echo ""
	@echo "📊 Service Status (DEV MODE):"
	@echo "  Frontend:   http://localhost:3000"
	@echo "  API:        http://localhost:8081"
	@echo "  Grafana:    http://localhost:3001 (admin/wikisurge)"
	@echo "  Prometheus: http://localhost:9090"
	@echo "  ES:         http://localhost:9200"
	@echo "  Redis:      localhost:6379"
	@echo "  Kafka:      localhost:19092"
	@echo ""
	@echo "💡 Next steps:"
	@echo "  • View logs:      make logs"
	@echo "  • Check health:   make health"
	@echo "  • Local web dev:  make dev-web (optional, frontend already in Docker)"
	@echo ""

# Production mode - lean stack without monitoring
prod:
	@echo "Starting production environment (PROD MODE)..."
	@echo "Using: deployments/docker-compose.prod.yml"
	@echo ""
	@docker-compose -f deployments/docker-compose.prod.yml up -d
	@echo ""
	@echo "✅ Production services started!"
	@echo "  Frontend: http://localhost:3000"
	@echo "  API:      http://localhost:8081"

# Restart backend containers (api, processor, ingestor)
dev-backend:
	@echo "Restarting backend containers..."
	@echo ""
	@echo "🛑 Stopping backend containers..."
	@docker-compose -f $(COMPOSE_FILE) stop api processor ingestor 2>/dev/null || true
	@docker-compose -f $(COMPOSE_FILE) rm -f api processor ingestor 2>/dev/null || true
	@echo ""
	@echo "🚀 Starting backend containers..."
	@docker-compose -f $(COMPOSE_FILE) up -d api processor ingestor
	@echo ""
	@echo "🔍 Waiting for dependencies..."
	@$(MAKE) wait-for-redis
	@$(MAKE) wait-for-kafka
	@$(MAKE) wait-for-es
	@echo ""
	@echo "✅ Backend containers restarted!"
	@echo "📊 API available at: http://localhost:8081"

# Start web app
dev-web:
	@echo "Starting web application..."
	@if [ ! -d "web/node_modules" ]; then \
		echo "Installing web dependencies..."; \
		cd web && npm install; \
	fi
	@cd web && npm run dev

# Setup infrastructure
setup:
	@echo "Setting up WikiSurge infrastructure..."
	@./scripts/setup-infrastructure.sh

# Setup Kafka topic
kafka-setup:
	@echo "Setting up Kafka topic..."
	@./scripts/setup-kafka-topic.sh

# Start all Docker services
start:
	@echo "Starting all services..."
	@docker-compose -f $(COMPOSE_FILE) up -d
	@echo ""
	@echo "⏳ Services starting... Use 'make health' to check status"

# Stop all services
stop:
	@echo "Stopping all services..."
	@docker-compose -f $(COMPOSE_FILE) down

# Stop and remove volumes
clean:
	@echo "Cleaning up services and volumes..."
	@docker-compose -f $(COMPOSE_FILE) down -v 2>/dev/null || true
	@docker-compose down -v 2>/dev/null || true
	@docker system prune -f
	@echo "Removing log files..."
	@rm -f *.log
	@echo "Removing binaries..."
	@rm -rf bin/
	@echo "Removing web artifacts..."
	@rm -rf web/node_modules web/dist
	@echo "✅ Full cleanup complete!"

# Tail logs from all services
logs:
	@docker-compose -f $(COMPOSE_FILE) logs -f

# Check health of all services
health:
	@echo "Checking service health..."
	@echo ""
	@echo "=== Docker Services Status ==="
	@docker-compose -f $(COMPOSE_FILE) ps
	@echo ""
	@echo "=== Redis Health ==="
	@docker-compose -f $(COMPOSE_FILE) exec -T redis redis-cli ping || echo "❌ Redis not responding"
	@echo ""
	@echo "=== Kafka Health ==="
	@docker-compose -f $(COMPOSE_FILE) exec -T kafka rpk cluster health || echo "❌ Kafka not responding"
	@echo ""
	@echo "=== Elasticsearch Health ==="
	@curl -s http://localhost:9200/_cluster/health?pretty || echo "❌ Elasticsearch not responding"
	@echo ""
	@echo "=== API Health ==="
	@curl -s http://localhost:8081/health || echo "❌ API not responding"
	@echo ""
	@echo "=== Frontend Health ==="
	@curl -s -o /dev/null -w "%{http_code}" http://localhost:3000/ | grep -q 200 && echo "✅ Frontend OK" || echo "❌ Frontend not responding"
	@echo ""
	@echo "=== Prometheus Health ==="
	@curl -s -o /dev/null -w "%{http_code}" http://localhost:9090/-/healthy | grep -q 200 && echo "✅ Prometheus OK" || echo "❌ Prometheus not responding (only in dev mode)"
	@echo ""
	@echo "=== Grafana Health ==="
	@curl -s -o /dev/null -w "%{http_code}" http://localhost:3001/api/health | grep -q 200 && echo "✅ Grafana OK" || echo "❌ Grafana not responding (only in dev mode)"

# Run all tests (placeholder)
test:
	@echo "Running tests..."
	@go test ./... -v -count=1

# Run integration tests
test-integration:
	@echo "Running integration tests..."
	@go test ./test/integration/... -v -count=1

# Run resource limit tests
test-resource:
	@echo "Running resource limit tests..."
	@go test ./test/resource/... -v -count=1

# Run benchmarks
test-bench:
	@echo "Running benchmarks..."
	@go test ./test/benchmark/... -bench=. -benchtime=5s -benchmem

# Run API unit tests
test-api:
	@echo "Running API unit tests..."
	@go test ./internal/api/... -v -count=1

# Run the API server
api:
	@echo "Starting API server..."
	@go run ./cmd/api/main.go

# Test infrastructure components
test-infra:
	@echo "Testing infrastructure components..."
	@./scripts/test-infrastructure.sh

# Test configuration and metrics
test-config:
	@echo "Testing configuration and metrics..."
	@./scripts/test-config.sh

# Build Go applications
build:
	@echo "Building applications..."
	@go build -o bin/api ./cmd/api
	@go build -o bin/ingestor ./cmd/ingestor
	@go build -o bin/processor ./cmd/processor

# Build and run demo
demo:
	@echo "Building and running configuration/metrics demo..."
	@go build -o bin/demo ./cmd/demo
	@./bin/demo

# Validate configuration and metrics
validate:
	@echo "Validating WikiSurge configuration and metrics framework..."
	@echo ""
	@echo "=== Configuration Validation ==="
	@echo "Testing dev config..."
	@go run ./cmd/demo configs/config.dev.yaml & sleep 3 && pkill -f demo
	@echo ""
	@echo "Testing minimal config..."
	@go run ./cmd/demo configs/config.minimal.yaml & sleep 3 && pkill -f demo
	@echo ""
	@echo "Testing prod config..."
	@go run ./cmd/demo configs/config.prod.yaml & sleep 3 && pkill -f demo
	@echo ""
	@echo "=== Metrics Endpoint Test ==="
	@echo "Starting metrics server..."
	@go run ./cmd/demo & sleep 3 && curl -s http://localhost:2112/metrics | head -5 && pkill -f demo
	@echo ""
	@echo "✅ Configuration and metrics validation complete!"
	@echo "Running tests..."
	@go test ./... -v

# Install Go dependencies
deps:
	@echo "Installing dependencies..."
	@echo "Installing Go dependencies..."
	@go mod tidy
	@go mod download
	@echo "Installing web dependencies..."
	@cd web && npm install

# Show service URLs
urls:
	@echo "=== Service URLs ==="
	@echo "Frontend:      http://localhost:3000"
	@echo "API:           http://localhost:8081"
	@echo "Grafana:       http://localhost:3001 (admin/wikisurge)"
	@echo "Prometheus:    http://localhost:9090"
	@echo "Elasticsearch: http://localhost:9200"
	@echo "Kafka:         localhost:19092"
	@echo "Redis:         localhost:6379"