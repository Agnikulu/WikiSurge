#!/usr/bin/env bash
# =============================================================================
# WikiSurge - SSL/TLS Setup Script
# =============================================================================
# Sets up HTTPS for the WikiSurge API and frontend using Let's Encrypt.
#
# Usage:
#   ./scripts/setup-ssl.sh <domain>              # Production (Let's Encrypt)
#   ./scripts/setup-ssl.sh --self-signed          # Self-signed for dev/testing
#
# Prerequisites:
#   - Domain pointing to this server (for Let's Encrypt)
#   - Port 80 accessible from internet (for ACME challenge)
#   - Docker installed
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
SSL_DIR="${PROJECT_DIR}/deployments/ssl"
NGINX_SSL_CONF="${PROJECT_DIR}/deployments/nginx-ssl.conf"

# ---------- Colors ----------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()  { echo -e "  ${GREEN}✓${NC} $1"; }
log_warn()  { echo -e "  ${YELLOW}⚠${NC} $1"; }
log_error() { echo -e "  ${RED}✗${NC} $1"; }
log_step()  { echo -e "\n${BLUE}==>${NC} $1"; }

# ---------- Self-Signed Certificate ----------
generate_self_signed() {
    log_step "Generating self-signed certificate"

    mkdir -p "${SSL_DIR}"

    openssl req -x509 -nodes -days 365 \
        -newkey rsa:2048 \
        -keyout "${SSL_DIR}/privkey.pem" \
        -out "${SSL_DIR}/fullchain.pem" \
        -subj "/C=US/ST=State/L=City/O=WikiSurge/CN=localhost" \
        2>/dev/null

    # Generate dhparams
    openssl dhparam -out "${SSL_DIR}/dhparam.pem" 2048 2>/dev/null

    log_info "Self-signed certificate generated at ${SSL_DIR}/"
    log_warn "This is for development only. Use Let's Encrypt for production."
}

# ---------- Let's Encrypt Certificate ----------
generate_letsencrypt() {
    local domain="$1"

    log_step "Obtaining Let's Encrypt certificate for ${domain}"

    mkdir -p "${SSL_DIR}/certbot/conf"
    mkdir -p "${SSL_DIR}/certbot/www"

    # Use certbot in Docker
    docker run --rm \
        -v "${SSL_DIR}/certbot/conf:/etc/letsencrypt" \
        -v "${SSL_DIR}/certbot/www:/var/www/certbot" \
        -p 80:80 \
        certbot/certbot certonly \
        --standalone \
        --non-interactive \
        --agree-tos \
        --email "admin@${domain}" \
        -d "${domain}" \
        -d "www.${domain}" 2>&1 || {
            log_error "Failed to obtain certificate. Ensure:"
            log_error "  1. Domain ${domain} points to this server"
            log_error "  2. Port 80 is open and not in use"
            exit 1
        }

    # Symlink for nginx
    ln -sf "${SSL_DIR}/certbot/conf/live/${domain}/fullchain.pem" "${SSL_DIR}/fullchain.pem"
    ln -sf "${SSL_DIR}/certbot/conf/live/${domain}/privkey.pem" "${SSL_DIR}/privkey.pem"

    # Generate dhparams
    if [ ! -f "${SSL_DIR}/dhparam.pem" ]; then
        openssl dhparam -out "${SSL_DIR}/dhparam.pem" 2048 2>/dev/null
    fi

    log_info "Certificate obtained for ${domain}"
}

# ---------- Generate Nginx SSL Config ----------
generate_nginx_ssl_config() {
    local domain="${1:-localhost}"

    log_step "Generating SSL Nginx configuration"

    cat > "$NGINX_SSL_CONF" << 'NGINX_CONF'
# =============================================================================
# WikiSurge - Nginx SSL Configuration
# =============================================================================

# Redirect HTTP to HTTPS
server {
    listen 80;
    server_name _;

    # ACME challenge for Let's Encrypt renewal
    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }

    location / {
        return 301 https://$host$request_uri;
    }
}

# HTTPS server
server {
    listen 443 ssl http2;
    server_name _;

    # ---------- SSL Configuration ----------
    ssl_certificate /etc/nginx/ssl/fullchain.pem;
    ssl_certificate_key /etc/nginx/ssl/privkey.pem;
    ssl_dhparam /etc/nginx/ssl/dhparam.pem;

    # Modern TLS configuration
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384;
    ssl_prefer_server_ciphers off;
    ssl_session_timeout 1d;
    ssl_session_cache shared:SSL:10m;
    ssl_session_tickets off;

    # OCSP stapling
    ssl_stapling on;
    ssl_stapling_verify on;

    # ---------- Security Headers ----------
    add_header Strict-Transport-Security "max-age=63072000; includeSubDomains; preload" always;
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    add_header Content-Security-Policy "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' wss: ws:;" always;

    root /usr/share/nginx/html;
    index index.html;

    # ---------- Gzip Compression ----------
    gzip on;
    gzip_vary on;
    gzip_proxied any;
    gzip_comp_level 6;
    gzip_min_length 256;
    gzip_types text/plain text/css text/javascript application/javascript application/json application/xml image/svg+xml font/woff font/woff2;

    # ---------- Static Assets ----------
    location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2|ttf|eot)$ {
        expires 1y;
        add_header Cache-Control "public, immutable";
        access_log off;
    }

    # ---------- API Proxy ----------
    location /api/ {
        proxy_pass http://api:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 30s;
        proxy_connect_timeout 5s;
    }

    # ---------- WebSocket Proxy (WSS) ----------
    location /ws {
        proxy_pass http://api:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }

    # ---------- Health Check ----------
    location /nginx-health {
        access_log off;
        return 200 "healthy\n";
        add_header Content-Type text/plain;
    }

    # ---------- SPA Fallback ----------
    location / {
        try_files $uri $uri/ /index.html;
    }

    # ---------- Deny Hidden Files ----------
    location ~ /\. {
        deny all;
        access_log off;
        log_not_found off;
    }
}
NGINX_CONF

    log_info "SSL Nginx config written to ${NGINX_SSL_CONF}"
}

# ---------- Setup Auto-Renewal ----------
setup_renewal() {
    local domain="$1"

    log_step "Setting up certificate auto-renewal"

    local cron_job="0 0 */60 * * docker run --rm -v ${SSL_DIR}/certbot/conf:/etc/letsencrypt -v ${SSL_DIR}/certbot/www:/var/www/certbot certbot/certbot renew --quiet && docker exec wikisurge-frontend nginx -s reload"

    # Add to crontab
    (crontab -l 2>/dev/null | grep -v "certbot renew"; echo "$cron_job") | crontab -

    log_info "Auto-renewal cron job configured (every 60 days)"
}

# =============================================================================
# Main
# =============================================================================
main() {
    echo ""
    echo "=========================================="
    echo "  WikiSurge SSL/TLS Setup"
    echo "=========================================="
    echo ""

    if [ "${1:-}" = "--self-signed" ]; then
        generate_self_signed
        generate_nginx_ssl_config "localhost"
        log_info "Self-signed SSL setup complete"
        echo ""
        echo "To use SSL, update Dockerfile.frontend to use nginx-ssl.conf:"
        echo "  COPY deployments/nginx-ssl.conf /etc/nginx/conf.d/wikisurge.conf"
        echo "  COPY deployments/ssl/ /etc/nginx/ssl/"
        echo "And expose port 443 in docker-compose.prod.yml"
    elif [ -n "${1:-}" ]; then
        local domain="$1"
        generate_letsencrypt "$domain"
        generate_nginx_ssl_config "$domain"
        setup_renewal "$domain"
        log_info "Let's Encrypt SSL setup complete for ${domain}"
        echo ""
        echo "To use SSL, update Dockerfile.frontend and docker-compose.prod.yml:"
        echo "  - Mount SSL certs: ./deployments/ssl:/etc/nginx/ssl:ro"
        echo "  - Use nginx-ssl.conf instead of nginx.conf"
        echo "  - Expose port 443"
    else
        echo "Usage:"
        echo "  $0 <domain>         # Let's Encrypt certificate"
        echo "  $0 --self-signed    # Self-signed certificate (dev/test)"
        exit 1
    fi
}

main "$@"
