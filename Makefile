.PHONY: setup kafka-setup start stop stop-all clean clean-data reset logs health test dev dev-backend dev-web help wait-for-es wait-for-kafka wait-for-redis

# Configuration - Using budget mode by default
COMPOSE_FILE := deployments/docker-compose.budget.yml
CONFIG_FILE := configs/config.budget.yaml

# Show available commands
help:
	@echo "WikiSurge - Available Commands"
	@echo "==============================="
	@echo ""
	@echo "Quick Start:"
	@echo "  make dev         - Build Go services, reuse Docker containers, clear data"
	@echo "  make dev-web     - Start web app (run in separate terminal)"
	@echo "  make reset       - Stop everything and full clean (rebuild containers)"
	@echo ""
	@echo "Service Control:"
	@echo "  make start       - Start Docker services"
	@echo "  make stop        - Stop Docker services"
	@echo "  make stop-all    - Stop Docker + Go processes"
	@echo "  make dev-backend - Restart backend services only"
	@echo ""
	@echo "Build & Test:"
	@echo "  make build       - Build Go applications"
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

# Wait for Elasticsearch to be ready (longer timeout for budget ES startup)
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

# Stop all services including Go processes
stop-all:
	@echo "Stopping all services..."
	-@pkill -f "./bin/api" 2>/dev/null || true
	-@pkill -f "./bin/ingestor" 2>/dev/null || true
	-@pkill -f "./bin/processor" 2>/dev/null || true
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

# Development mode - start everything with proper health checks (BUDGET MODE)
# Uses existing containers if running, only rebuilds Go services
dev: 
	@echo "Starting development environment (BUDGET MODE)..."
	@echo "Using: $(COMPOSE_FILE)"
	@echo "Config: $(CONFIG_FILE)"
	@echo ""
	@# Stop any running Go processes
	-@pkill -f "./bin/api" 2>/dev/null || true
	-@pkill -f "./bin/ingestor" 2>/dev/null || true
	-@pkill -f "./bin/processor" 2>/dev/null || true
	@# Build Go binaries (fast if no changes)
	@echo "🔨 Building Go services..."
	@$(MAKE) build
	@# Check if containers are already running, if not start them
	@if docker-compose -f $(COMPOSE_FILE) ps 2>/dev/null | grep -q "Up"; then \
		echo "📦 Docker containers already running, reusing..."; \
	else \
		echo "🚀 Starting Docker services..."; \
		docker-compose -f $(COMPOSE_FILE) up -d; \
	fi
	@echo ""
	@# Clean old data (logs, Redis, ES, Kafka)
	@$(MAKE) clean-data
	@echo ""
	@$(MAKE) wait-for-redis
	@echo ""
	@$(MAKE) wait-for-kafka
	@echo ""
	@$(MAKE) wait-for-es
	@echo ""
	@echo "🔧 Starting backend services..."
	@sleep 1
	@echo "  - Starting processor..."
	@CONFIG_PATH=$(CONFIG_FILE) nohup ./bin/processor > processor.log 2>&1 &
	@sleep 1
	@echo "  - Starting ingestor..."
	@CONFIG_PATH=$(CONFIG_FILE) nohup ./bin/ingestor > ingestor.log 2>&1 &
	@sleep 1
	@echo "  - Starting API..."
	@CONFIG_PATH=$(CONFIG_FILE) nohup ./bin/api > api.log 2>&1 &
	@sleep 2
	@echo ""
	@echo "✅ Backend services started!"
	@echo ""
	@echo "📊 Service Status (BUDGET MODE - ~$9/month):"
	@echo "  API:        http://localhost:8080"
	@echo "  Grafana:    http://localhost:3000 (admin/wikisurge123)"
	@echo "  Prometheus: http://localhost:9090"
	@echo "  ES:         http://localhost:9200 (6h retention)"
	@echo ""
	@echo "💡 Next steps:""
	@echo "  • Start web app:  make dev-web"
	@echo "  • View logs:      tail -f api.log processor.log ingestor.log"
	@echo "  • Check health:   make health"
	@echo ""

# Start only backend services (assumes Docker is running)
dev-backend:
	@echo "Restarting backend services (BUDGET MODE)..."
	@echo ""
	@echo "🛑 Stopping existing processes..."
	-@pkill -f "./bin/api" 2>/dev/null || true
	-@pkill -f "./bin/ingestor" 2>/dev/null || true
	-@pkill -f "./bin/processor" 2>/dev/null || true
	@sleep 1
	@echo ""
	@echo "🔍 Checking dependencies..."
	@$(MAKE) wait-for-redis
	@$(MAKE) wait-for-kafka
	@$(MAKE) wait-for-es
	@echo ""
	@echo "🔧 Starting backend services..."
	@CONFIG_PATH=$(CONFIG_FILE) nohup ./bin/processor > processor.log 2>&1 &
	@sleep 1
	@CONFIG_PATH=$(CONFIG_FILE) nohup ./bin/ingestor > ingestor.log 2>&1 &
	@sleep 1
	@CONFIG_PATH=$(CONFIG_FILE) nohup ./bin/api > api.log 2>&1 &
	@sleep 2
	@echo ""
	@echo "✅ Backend services restarted!"
	@echo "📊 API available at: http://localhost:8080"

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
	@echo "Starting all services (BUDGET MODE)..."
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
	@echo "Checking service health (BUDGET MODE)..."
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
	@echo "=== Prometheus Health ==="
	@curl -s http://localhost:9090/-/healthy || echo "❌ Prometheus not responding"
	@echo ""
	@echo "=== Grafana Health ==="
	@curl -s http://localhost:3000/api/health || echo "❌ Grafana not responding"

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
	@echo "Grafana: http://localhost:3000 (admin/admin)"
	@echo "Prometheus: http://localhost:9090"
	@echo "Elasticsearch: http://localhost:9200"
	@echo "Kafka: localhost:9092"
	@echo "Redis: localhost:6379"
	@echo "API (when running): http://localhost:8080"