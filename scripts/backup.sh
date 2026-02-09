#!/usr/bin/env bash
# =============================================================================
# WikiSurge - Backup Script
# =============================================================================
# Creates backups of Redis and Elasticsearch data.
#
# Schedule with cron:
#   0 2 * * * /path/to/scripts/backup.sh redis       # Daily 2 AM
#   0 3 * * * /path/to/scripts/backup.sh elasticsearch # Daily 3 AM
#   0 4 * * * /path/to/scripts/backup.sh all          # Daily 4 AM (both)
#
# Usage:
#   ./scripts/backup.sh all             # Backup everything
#   ./scripts/backup.sh redis           # Redis only
#   ./scripts/backup.sh elasticsearch   # Elasticsearch only
#   ./scripts/backup.sh configs         # Config files only
# =============================================================================

set -euo pipefail

# ---------- Configuration ----------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
BACKUP_DIR="${BACKUP_BASE_DIR:-${PROJECT_DIR}/backups}"
RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-7}"
DATE_TAG=$(date +%Y%m%d_%H%M%S)
COMPRESS="${BACKUP_COMPRESS:-true}"

# Service connection
REDIS_CONTAINER="${REDIS_CONTAINER:-wikisurge-redis}"
ES_URL="${ES_URL:-http://localhost:9200}"
ES_CONTAINER="${ES_CONTAINER:-wikisurge-elasticsearch}"

# Cloud upload (optional)
CLOUD_UPLOAD="${CLOUD_UPLOAD:-false}"
CLOUD_BUCKET="${CLOUD_BUCKET:-}"

# ---------- Colors ----------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# ---------- Helpers ----------
log() {
    echo -e "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

log_info()  { log "${GREEN}INFO${NC}  $1"; }
log_warn()  { log "${YELLOW}WARN${NC}  $1"; }
log_error() { log "${RED}ERROR${NC} $1"; }
log_step()  { log "${BLUE}STEP${NC}  $1"; }

die() {
    log_error "$1"
    exit 1
}

ensure_dir() {
    mkdir -p "$1"
}

# ---------- Redis Backup ----------
backup_redis() {
    local backup_path="${BACKUP_DIR}/redis/${DATE_TAG}"
    ensure_dir "$backup_path"

    log_step "Backing up Redis..."

    # Trigger RDB snapshot
    docker exec "$REDIS_CONTAINER" redis-cli BGSAVE > /dev/null 2>&1

    # Wait for background save to complete
    local retries=0
    while [ $retries -lt 30 ]; do
        local status
        status=$(docker exec "$REDIS_CONTAINER" redis-cli LASTSAVE 2>/dev/null)
        sleep 1
        local new_status
        new_status=$(docker exec "$REDIS_CONTAINER" redis-cli LASTSAVE 2>/dev/null)
        if [ "$status" != "$new_status" ] || [ $retries -gt 5 ]; then
            break
        fi
        retries=$((retries + 1))
    done

    # Copy RDB file
    docker cp "${REDIS_CONTAINER}:/data/dump.rdb" "${backup_path}/dump.rdb" 2>/dev/null || true

    # Copy AOF file if exists
    docker cp "${REDIS_CONTAINER}:/data/appendonly.aof" "${backup_path}/appendonly.aof" 2>/dev/null || true

    # Compress
    if [ "$COMPRESS" = "true" ]; then
        tar -czf "${backup_path}.tar.gz" -C "${BACKUP_DIR}/redis" "${DATE_TAG}"
        rm -rf "$backup_path"
        log_info "Redis backup: ${backup_path}.tar.gz"
    else
        log_info "Redis backup: ${backup_path}/"
    fi

    # Get backup size
    local size
    if [ "$COMPRESS" = "true" ]; then
        size=$(du -sh "${backup_path}.tar.gz" | cut -f1)
    else
        size=$(du -sh "${backup_path}" | cut -f1)
    fi
    log_info "Redis backup size: ${size}"
}

# ---------- Elasticsearch Backup ----------
backup_elasticsearch() {
    local backup_path="${BACKUP_DIR}/elasticsearch/${DATE_TAG}"
    ensure_dir "$backup_path"

    log_step "Backing up Elasticsearch..."

    # Register snapshot repository (inside container)
    curl -sf -X PUT "${ES_URL}/_snapshot/wikisurge_backup" \
        -H 'Content-Type: application/json' \
        -d "{
            \"type\": \"fs\",
            \"settings\": {
                \"location\": \"/backups/es/${DATE_TAG}\",
                \"compress\": true
            }
        }" > /dev/null 2>&1 || {
        # If filesystem repo fails, export indices manually
        log_warn "Snapshot repo not available, exporting indices manually"
        backup_es_indices "$backup_path"
        return
    }

    # Create snapshot
    local snapshot_name="snapshot_${DATE_TAG}"
    curl -sf -X PUT "${ES_URL}/_snapshot/wikisurge_backup/${snapshot_name}?wait_for_completion=true" \
        -H 'Content-Type: application/json' \
        -d '{
            "indices": "wikipedia_edits*",
            "ignore_unavailable": true,
            "include_global_state": false
        }' > /dev/null 2>&1

    if [ $? -eq 0 ]; then
        log_info "Elasticsearch snapshot: ${snapshot_name}"
    else
        log_warn "Snapshot failed, falling back to index export"
        backup_es_indices "$backup_path"
    fi
}

backup_es_indices() {
    local backup_path="$1"

    # Export index mappings and settings
    local indices
    indices=$(curl -sf "${ES_URL}/_cat/indices?format=json" 2>/dev/null | \
        grep -oP '"index"\s*:\s*"[^"]*"' | grep -oP '"[^"]*"$' | tr -d '"' | grep "wikipedia" || true)

    for index in $indices; do
        log_info "Exporting index: $index"

        # Export mapping
        curl -sf "${ES_URL}/${index}/_mapping" > "${backup_path}/${index}_mapping.json" 2>/dev/null || true

        # Export settings
        curl -sf "${ES_URL}/${index}/_settings" > "${backup_path}/${index}_settings.json" 2>/dev/null || true

        # Export doc count
        local count
        count=$(curl -sf "${ES_URL}/${index}/_count" 2>/dev/null | grep -oP '"count"\s*:\s*\d+' | grep -oP '\d+' || echo "0")
        log_info "  Documents: ${count}"
    done

    if [ "$COMPRESS" = "true" ]; then
        tar -czf "${backup_path}.tar.gz" -C "${BACKUP_DIR}/elasticsearch" "${DATE_TAG}"
        rm -rf "$backup_path"
        log_info "Elasticsearch backup: ${backup_path}.tar.gz"
    fi
}

# ---------- Config Backup ----------
backup_configs() {
    local backup_path="${BACKUP_DIR}/configs/${DATE_TAG}"
    ensure_dir "$backup_path"

    log_step "Backing up configurations..."

    # Copy config files
    cp -r "${PROJECT_DIR}/configs/"* "${backup_path}/" 2>/dev/null || true
    cp "${PROJECT_DIR}/.env.prod" "${backup_path}/.env.prod" 2>/dev/null || true
    cp "${PROJECT_DIR}/deployments/docker-compose.prod.yml" "${backup_path}/docker-compose.prod.yml" 2>/dev/null || true
    cp -r "${PROJECT_DIR}/monitoring/"* "${backup_path}/monitoring/" 2>/dev/null || true

    if [ "$COMPRESS" = "true" ]; then
        tar -czf "${backup_path}.tar.gz" -C "${BACKUP_DIR}/configs" "${DATE_TAG}"
        rm -rf "$backup_path"
        log_info "Config backup: ${backup_path}.tar.gz"
    else
        log_info "Config backup: ${backup_path}/"
    fi
}

# ---------- Cleanup Old Backups ----------
cleanup_old_backups() {
    log_step "Cleaning up backups older than ${RETENTION_DAYS} days..."

    local count=0
    while IFS= read -r -d '' file; do
        rm -rf "$file"
        count=$((count + 1))
    done < <(find "$BACKUP_DIR" -maxdepth 3 -name "*.tar.gz" -mtime +"$RETENTION_DAYS" -print0 2>/dev/null)

    while IFS= read -r -d '' dir; do
        rm -rf "$dir"
        count=$((count + 1))
    done < <(find "$BACKUP_DIR" -maxdepth 3 -type d -name "20*" -mtime +"$RETENTION_DAYS" -print0 2>/dev/null)

    log_info "Removed ${count} old backup(s)"
}

# ---------- Cloud Upload ----------
upload_to_cloud() {
    if [ "$CLOUD_UPLOAD" != "true" ] || [ -z "$CLOUD_BUCKET" ]; then
        return
    fi

    log_step "Uploading backups to cloud storage..."

    if command -v aws &> /dev/null; then
        aws s3 sync "$BACKUP_DIR" "s3://${CLOUD_BUCKET}/wikisurge-backups/${DATE_TAG}/" \
            --exclude "*" --include "*.tar.gz"
        log_info "Uploaded to s3://${CLOUD_BUCKET}/"
    elif command -v gsutil &> /dev/null; then
        gsutil -m rsync -r "$BACKUP_DIR" "gs://${CLOUD_BUCKET}/wikisurge-backups/${DATE_TAG}/"
        log_info "Uploaded to gs://${CLOUD_BUCKET}/"
    else
        log_warn "No cloud CLI found (aws/gsutil). Skipping upload."
    fi
}

# ---------- Summary ----------
show_summary() {
    log ""
    log "========== Backup Summary =========="
    log "Date:      ${DATE_TAG}"
    log "Location:  ${BACKUP_DIR}"

    local total_size
    total_size=$(du -sh "$BACKUP_DIR" 2>/dev/null | cut -f1)
    log "Total:     ${total_size}"
    log "Retention: ${RETENTION_DAYS} days"
    log "===================================="
}

# =============================================================================
# Main
# =============================================================================
main() {
    local target="${1:-all}"

    log ""
    log "=========================================="
    log "  WikiSurge Backup - ${DATE_TAG}"
    log "=========================================="
    log ""

    ensure_dir "$BACKUP_DIR"

    case "$target" in
        redis)
            backup_redis
            ;;
        elasticsearch|es)
            backup_elasticsearch
            ;;
        configs)
            backup_configs
            ;;
        all)
            backup_redis
            backup_elasticsearch
            backup_configs
            ;;
        *)
            echo "Usage: $0 {all|redis|elasticsearch|configs}"
            exit 1
            ;;
    esac

    cleanup_old_backups
    upload_to_cloud
    show_summary

    log_info "Backup completed successfully"
}

main "$@"
