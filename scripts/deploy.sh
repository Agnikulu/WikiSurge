#!/usr/bin/env bash
# =============================================================================
# WikiSurge - Production Deployment Script
# =============================================================================
# Deploys WikiSurge to production with health checks, rollback, and smoke tests.
#
# Usage:
#   ./scripts/deploy.sh                  # Full deployment
#   ./scripts/deploy.sh --build-only     # Build images only
#   ./scripts/deploy.sh --no-build       # Deploy without rebuilding
#   ./scripts/deploy.sh --rollback       # Rollback to previous version
#   ./scripts/deploy.sh --tail           # Tail logs after deploy
#
# Prerequisites:
#   - Docker and docker-compose installed
#   - .env.prod file configured
#   - Sufficient system resources (4GB+ RAM recommended)
# =============================================================================

set -euo pipefail

# ---------- Configuration ----------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_FILE="${PROJECT_DIR}/deployments/docker-compose.prod.yml"
ENV_FILE="${PROJECT_DIR}/.env.prod"
HEALTH_CHECK="${SCRIPT_DIR}/health-check.sh"
RESOURCE_CHECK="${SCRIPT_DIR}/check-resources.sh"

# Deployment settings
HEALTH_CHECK_RETRIES=30
HEALTH_CHECK_INTERVAL=5
ROLLBACK_TAG="previous"

# ---------- Colors ----------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

# ---------- Flags ----------
BUILD_ONLY=false
NO_BUILD=false
DO_ROLLBACK=false
TAIL_LOGS=false

for arg in "$@"; do
    case $arg in
        --build-only)  BUILD_ONLY=true ;;
        --no-build)    NO_BUILD=true ;;
        --rollback)    DO_ROLLBACK=true ;;
        --tail)        TAIL_LOGS=true ;;
        --help|-h)
            echo "Usage: $0 [--build-only] [--no-build] [--rollback] [--tail]"
            echo ""
            echo "Options:"
            echo "  --build-only   Build images without deploying"
            echo "  --no-build     Deploy using existing images"
            echo "  --rollback     Rollback to previous version"
            echo "  --tail         Tail logs after deployment"
            exit 0
            ;;
    esac
done

# ---------- Helper Functions ----------
log_step() {
    echo -e "\n${BLUE}${BOLD}==>${NC} ${BOLD}$1${NC}"
}

log_info() {
    echo -e "  ${GREEN}✓${NC} $1"
}

log_warn() {
    echo -e "  ${YELLOW}⚠${NC} $1"
}

log_error() {
    echo -e "  ${RED}✗${NC} $1"
}

die() {
    log_error "$1"
    exit 1
}

docker_compose() {
    docker-compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" "$@"
}

# ---------- Prerequisite Checks ----------
check_prerequisites() {
    log_step "Checking prerequisites"

    # Docker
    if ! command -v docker &> /dev/null; then
        die "Docker is not installed"
    fi
    log_info "Docker: $(docker --version | head -1)"

    # Docker Compose
    if ! command -v docker-compose &> /dev/null; then
        die "docker-compose is not installed"
    fi
    log_info "Docker Compose: $(docker-compose --version | head -1)"

    # Environment file
    if [ ! -f "$ENV_FILE" ]; then
        die ".env.prod not found. Copy .env.prod.template to .env.prod and configure."
    fi
    log_info "Environment file: $ENV_FILE"

    # Compose file
    if [ ! -f "$COMPOSE_FILE" ]; then
        die "Compose file not found: $COMPOSE_FILE"
    fi
    log_info "Compose file: $COMPOSE_FILE"
}

# ---------- Resource Check ----------
check_resources() {
    log_step "Checking system resources"

    local mem_total_kb
    mem_total_kb=$(grep MemTotal /proc/meminfo | awk '{print $2}')
    local mem_total_mb=$((mem_total_kb / 1024))

    if [ "$mem_total_mb" -lt 4096 ]; then
        log_warn "Available RAM: ${mem_total_mb}MB (recommended: 4096MB+)"
    else
        log_info "Available RAM: ${mem_total_mb}MB"
    fi

    local disk_avail
    disk_avail=$(df -m "$PROJECT_DIR" | tail -1 | awk '{print $4}')
    if [ "$disk_avail" -lt 10240 ]; then
        log_warn "Available disk: ${disk_avail}MB (recommended: 10240MB+)"
    else
        log_info "Available disk: ${disk_avail}MB"
    fi

    if [ -x "$RESOURCE_CHECK" ]; then
        "$RESOURCE_CHECK" || log_warn "Resource check script reported warnings"
    fi
}

# ---------- Tag Previous Version ----------
tag_previous() {
    log_step "Tagging current images as 'previous' for rollback"

    for service in ingestor processor api frontend; do
        local image="wikisurge/${service}:latest"
        if docker image inspect "$image" &> /dev/null; then
            docker tag "$image" "wikisurge/${service}:${ROLLBACK_TAG}" 2>/dev/null || true
            log_info "Tagged $image → wikisurge/${service}:${ROLLBACK_TAG}"
        fi
    done
}

# ---------- Build Images ----------
build_images() {
    log_step "Building custom images"

    docker_compose build --parallel 2>&1 | tail -5
    log_info "All images built successfully"
}

# ---------- Pull Images ----------
pull_images() {
    log_step "Pulling latest base images"

    docker_compose pull kafka redis elasticsearch prometheus grafana 2>&1 | tail -5
    log_info "Base images pulled"
}

# ---------- Create Infrastructure ----------
create_infrastructure() {
    log_step "Creating networks and volumes"

    docker_compose up --no-start 2>&1 | tail -5
    log_info "Infrastructure created"
}

# ---------- Start Services ----------
start_services() {
    log_step "Starting services"

    # Start infrastructure first
    docker_compose up -d kafka redis elasticsearch
    log_info "Infrastructure services starting..."

    # Wait for infrastructure health
    local retries=0
    while [ $retries -lt $HEALTH_CHECK_RETRIES ]; do
        if docker_compose exec -T kafka rpk cluster health &>/dev/null && \
           docker_compose exec -T redis redis-cli ping &>/dev/null; then
            log_info "Infrastructure services healthy"
            break
        fi
        retries=$((retries + 1))
        sleep $HEALTH_CHECK_INTERVAL
    done

    if [ $retries -eq $HEALTH_CHECK_RETRIES ]; then
        die "Infrastructure services failed to become healthy"
    fi

    # Start application services
    docker_compose up -d
    log_info "All services starting..."
}

# ---------- Wait for Health ----------
wait_for_health() {
    log_step "Waiting for all services to become healthy"

    local retries=0
    while [ $retries -lt $HEALTH_CHECK_RETRIES ]; do
        sleep $HEALTH_CHECK_INTERVAL
        retries=$((retries + 1))

        echo -ne "  Attempt ${retries}/${HEALTH_CHECK_RETRIES}...\r"

        if bash "$HEALTH_CHECK" --quiet 2>/dev/null; then
            echo ""
            log_info "All services healthy!"
            return 0
        fi
    done

    echo ""
    log_error "Services did not become healthy within timeout"
    log_warn "Running detailed health check..."
    bash "$HEALTH_CHECK" 2>/dev/null || true
    return 1
}

# ---------- Smoke Tests ----------
run_smoke_tests() {
    log_step "Running smoke tests"

    # API health endpoint
    if curl -sf http://localhost:8080/health > /dev/null 2>&1; then
        log_info "API health check passed"
    else
        log_warn "API health check failed"
    fi

    # API stats endpoint
    if curl -sf http://localhost:8080/api/stats > /dev/null 2>&1; then
        log_info "API stats endpoint accessible"
    else
        log_warn "API stats endpoint not accessible (may need data)"
    fi

    # Metrics endpoint
    if curl -sf http://localhost:2112/metrics > /dev/null 2>&1; then
        log_info "Metrics endpoint accessible"
    else
        log_warn "Metrics endpoint not accessible"
    fi

    # Prometheus
    if curl -sf http://localhost:9090/-/ready > /dev/null 2>&1; then
        log_info "Prometheus ready"
    else
        log_warn "Prometheus not ready"
    fi

    # Grafana
    if curl -sf http://localhost:3000/api/health > /dev/null 2>&1; then
        log_info "Grafana ready"
    else
        log_warn "Grafana not ready"
    fi
}

# ---------- Display Status ----------
display_status() {
    log_step "Deployment Status"

    echo ""
    docker_compose ps
    echo ""
    echo -e "${BOLD}Service URLs:${NC}"
    echo -e "  Frontend:    http://localhost:${FRONTEND_PORT:-80}"
    echo -e "  API:         http://localhost:${API_PORT:-8080}"
    echo -e "  API Health:  http://localhost:${API_PORT:-8080}/health"
    echo -e "  Metrics:     http://localhost:${METRICS_PORT:-2112}/metrics"
    echo -e "  Prometheus:  http://localhost:${PROMETHEUS_PORT:-9090}"
    echo -e "  Grafana:     http://localhost:${GRAFANA_PORT:-3000}"
    echo -e "  Kafka Admin: http://localhost:9644"
    echo ""
}

# ---------- Rollback ----------
rollback() {
    log_step "Rolling back to previous version"

    for service in ingestor processor api frontend; do
        local prev_image="wikisurge/${service}:${ROLLBACK_TAG}"
        if docker image inspect "$prev_image" &> /dev/null; then
            docker tag "$prev_image" "wikisurge/${service}:latest"
            log_info "Restored $service to previous version"
        else
            log_warn "No previous image found for $service"
        fi
    done

    docker_compose up -d
    log_info "Rollback deployment started"

    wait_for_health || die "Rollback failed - services unhealthy"
    log_info "Rollback completed successfully"
}

# =============================================================================
# Main Execution
# =============================================================================
main() {
    echo -e "${BOLD}"
    echo "╔══════════════════════════════════════════╗"
    echo "║       WikiSurge Production Deploy        ║"
    echo "║       $(date '+%Y-%m-%d %H:%M:%S')              ║"
    echo "╚══════════════════════════════════════════╝"
    echo -e "${NC}"

    # Load env file for variable access
    set -a
    source "$ENV_FILE" 2>/dev/null || true
    set +a

    # Handle rollback
    if $DO_ROLLBACK; then
        check_prerequisites
        rollback
        display_status
        exit 0
    fi

    # Step 1: Prerequisites
    check_prerequisites

    # Step 2: Resource check
    check_resources

    # Step 3: Tag previous images for rollback
    tag_previous

    # Step 4: Pull base images
    pull_images

    # Step 5: Build custom images
    if ! $NO_BUILD; then
        build_images
    else
        log_info "Skipping build (--no-build)"
    fi

    if $BUILD_ONLY; then
        log_info "Build complete (--build-only)"
        exit 0
    fi

    # Step 6: Create infrastructure
    create_infrastructure

    # Step 7: Start services
    start_services

    # Step 8: Wait for health
    if ! wait_for_health; then
        log_error "Deployment failed - initiating rollback"
        rollback
        die "Deployment failed. Rolled back to previous version."
    fi

    # Step 9: Smoke tests
    run_smoke_tests

    # Step 10: Display status
    display_status

    echo -e "${GREEN}${BOLD}Deployment completed successfully!${NC}"

    # Optional: tail logs
    if $TAIL_LOGS; then
        log_step "Tailing logs (Ctrl+C to stop)"
        docker_compose logs -f --tail=50
    fi
}

main "$@"
