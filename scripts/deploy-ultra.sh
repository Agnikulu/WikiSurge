#!/bin/bash
# =============================================================================
# WikiSurge Ultra-Budget Deployment Script
# =============================================================================
# Works on: Hetzner CAX11 (€3.79), DigitalOcean $12, Vultr $12, any 2GB+ VPS
#
# Usage (run on fresh Ubuntu 22.04 server):
#   curl -fsSL https://raw.githubusercontent.com/YOUR_USERNAME/WikiSurge/main/scripts/deploy-ultra.sh | bash
#
# Or clone and run:
#   git clone https://github.com/YOUR_USERNAME/WikiSurge.git
#   cd WikiSurge && bash scripts/deploy-ultra.sh
# =============================================================================

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log() { echo -e "${GREEN}[WikiSurge]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# =============================================================================
# Pre-flight Checks
# =============================================================================
log "Starting WikiSurge Ultra-Budget Deployment..."

# Check if running as root or with sudo
if [ "$EUID" -ne 0 ]; then
    error "Please run as root or with sudo"
fi

# Check minimum RAM (1.5GB minimum, 2GB recommended)
TOTAL_RAM=$(free -m | awk '/^Mem:/{print $2}')
if [ "$TOTAL_RAM" -lt 1500 ]; then
    error "Insufficient RAM: ${TOTAL_RAM}MB. Minimum 1.5GB required (2GB recommended)."
fi
log "RAM check passed: ${TOTAL_RAM}MB available"

# Check architecture
ARCH=$(uname -m)
log "Architecture: $ARCH"

# =============================================================================
# System Setup
# =============================================================================
log "Updating system packages..."
apt-get update -qq
apt-get upgrade -y -qq

log "Installing dependencies..."
apt-get install -y -qq \
    curl \
    git \
    htop \
    wget \
    ca-certificates \
    gnupg \
    lsb-release

# =============================================================================
# Docker Installation
# =============================================================================
if command -v docker &> /dev/null; then
    log "Docker already installed: $(docker --version)"
else
    log "Installing Docker..."
    curl -fsSL https://get.docker.com | sh
    
    # Start Docker
    systemctl enable docker
    systemctl start docker
    
    log "Docker installed: $(docker --version)"
fi

# Install Docker Compose plugin if not present
if ! docker compose version &> /dev/null; then
    log "Installing Docker Compose plugin..."
    apt-get install -y -qq docker-compose-plugin
fi
log "Docker Compose: $(docker compose version)"

# =============================================================================
# Swap Setup (important for 2GB servers)
# =============================================================================
if [ ! -f /swapfile ]; then
    log "Creating 1GB swap file for stability..."
    fallocate -l 1G /swapfile
    chmod 600 /swapfile
    mkswap /swapfile
    swapon /swapfile
    echo '/swapfile none swap sw 0 0' >> /etc/fstab
    log "Swap enabled"
else
    log "Swap already configured"
fi

# Optimize swappiness for low-memory server
echo 'vm.swappiness=10' > /etc/sysctl.d/99-wikisurge.conf
sysctl -p /etc/sysctl.d/99-wikisurge.conf

# =============================================================================
# Clone/Update WikiSurge
# =============================================================================
INSTALL_DIR="/opt/wikisurge"

if [ -d "$INSTALL_DIR" ]; then
    log "Updating existing WikiSurge installation..."
    cd "$INSTALL_DIR"
    git pull origin main
else
    log "Cloning WikiSurge repository..."
    git clone https://github.com/YOUR_USERNAME/WikiSurge.git "$INSTALL_DIR"
    cd "$INSTALL_DIR"
fi

# =============================================================================
# Configure Firewall
# =============================================================================
if command -v ufw &> /dev/null; then
    log "Configuring firewall..."
    ufw allow 22/tcp    # SSH
    ufw allow 80/tcp    # Frontend
    ufw allow 8080/tcp  # API
    ufw --force enable
    log "Firewall configured (ports 22, 80, 8080 open)"
fi

# =============================================================================
# Deploy WikiSurge
# =============================================================================
cd "$INSTALL_DIR"

log "Building and starting WikiSurge (ultra-budget mode)..."

# Stop any existing deployment
docker compose -f deployments/docker-compose.ultra.yml down 2>/dev/null || true

# Build images
log "Building Docker images (this may take 5-10 minutes on first run)..."
docker compose -f deployments/docker-compose.ultra.yml build --quiet

# Start services
log "Starting services..."
docker compose -f deployments/docker-compose.ultra.yml up -d

# =============================================================================
# Wait for Services
# =============================================================================
log "Waiting for services to be healthy..."

wait_for_service() {
    local service=$1
    local url=$2
    local max_attempts=30
    local attempt=1
    
    while [ $attempt -le $max_attempts ]; do
        if curl -sf "$url" > /dev/null 2>&1; then
            echo -e "  ${GREEN}✓${NC} $service is ready"
            return 0
        fi
        sleep 2
        attempt=$((attempt + 1))
    done
    
    warn "$service not ready after ${max_attempts} attempts"
    return 1
}

sleep 10  # Initial wait

wait_for_service "Redis" "http://localhost:6379" || true
wait_for_service "API" "http://localhost:8080/health" || true
wait_for_service "Frontend" "http://localhost:80" || true

# =============================================================================
# Create Management Script
# =============================================================================
cat > /usr/local/bin/wikisurge << 'EOF'
#!/bin/bash
INSTALL_DIR="/opt/wikisurge"
COMPOSE_FILE="deployments/docker-compose.ultra.yml"

case "$1" in
    start)
        cd "$INSTALL_DIR" && docker compose -f "$COMPOSE_FILE" up -d
        ;;
    stop)
        cd "$INSTALL_DIR" && docker compose -f "$COMPOSE_FILE" down
        ;;
    restart)
        cd "$INSTALL_DIR" && docker compose -f "$COMPOSE_FILE" restart
        ;;
    logs)
        cd "$INSTALL_DIR" && docker compose -f "$COMPOSE_FILE" logs -f ${2:-}
        ;;
    status)
        cd "$INSTALL_DIR" && docker compose -f "$COMPOSE_FILE" ps
        ;;
    update)
        cd "$INSTALL_DIR" && git pull && docker compose -f "$COMPOSE_FILE" build && docker compose -f "$COMPOSE_FILE" up -d
        ;;
    *)
        echo "Usage: wikisurge {start|stop|restart|logs|status|update}"
        exit 1
        ;;
esac
EOF
chmod +x /usr/local/bin/wikisurge

# =============================================================================
# Create Auto-Restart Service
# =============================================================================
cat > /etc/systemd/system/wikisurge.service << EOF
[Unit]
Description=WikiSurge Real-Time Wikipedia Dashboard
After=docker.service
Requires=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=$INSTALL_DIR
ExecStart=/usr/bin/docker compose -f deployments/docker-compose.ultra.yml up -d
ExecStop=/usr/bin/docker compose -f deployments/docker-compose.ultra.yml down

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable wikisurge.service

# =============================================================================
# Get Server IP
# =============================================================================
SERVER_IP=$(curl -sf https://ipinfo.io/ip || hostname -I | awk '{print $1}')

# =============================================================================
# Done!
# =============================================================================
echo ""
echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}  WikiSurge Deployment Complete!${NC}"
echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "  ${BLUE}Dashboard:${NC}  http://${SERVER_IP}"
echo -e "  ${BLUE}API:${NC}        http://${SERVER_IP}:8080"
echo -e "  ${BLUE}Health:${NC}     http://${SERVER_IP}:8080/health"
echo ""
echo -e "  ${YELLOW}Management Commands:${NC}"
echo "    wikisurge status   - Check service status"
echo "    wikisurge logs     - View logs"
echo "    wikisurge restart  - Restart all services"
echo "    wikisurge update   - Pull latest and redeploy"
echo ""
echo -e "  ${YELLOW}Memory Usage:${NC}"
docker stats --no-stream --format "table {{.Name}}\t{{.MemUsage}}" 2>/dev/null | head -10
echo ""
echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
