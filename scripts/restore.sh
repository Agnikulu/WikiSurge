#!/usr/bin/env bash
# =============================================================================
# WikiSurge - Restore Script
# =============================================================================
# Restores Redis and Elasticsearch data from backups.
#
# Usage:
#   ./scripts/restore.sh <backup-date>              # Restore everything
#   ./scripts/restore.sh <backup-date> redis         # Redis only
#   ./scripts/restore.sh <backup-date> elasticsearch # Elasticsearch only
#   ./scripts/restore.sh --list                      # List available backups
#
# Example:
#   ./scripts/restore.sh 20260208_020000
#   ./scripts/restore.sh 20260208_020000 redis
# =============================================================================

set -euo pipefail

# ---------- Configuration ----------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
BACKUP_DIR="${BACKUP_BASE_DIR:-${PROJECT_DIR}/backups}"
HEALTH_CHECK="${SCRIPT_DIR}/health-check.sh"

# Service connection
REDIS_CONTAINER="${REDIS_CONTAINER:-wikisurge-redis}"
ES_URL="${ES_URL:-http://localhost:9200}"

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

# ---------- List Backups ----------
list_backups() {
    log_step "Available backups:"

    echo ""
    echo "Redis backups:"
    if [ -d "${BACKUP_DIR}/redis" ]; then
        ls -la "${BACKUP_DIR}/redis/" 2>/dev/null | grep -E "tar\.gz$|^d" | tail -20
    else
        echo "  (none)"
    fi

    echo ""
    echo "Elasticsearch backups:"
    if [ -d "${BACKUP_DIR}/elasticsearch" ]; then
        ls -la "${BACKUP_DIR}/elasticsearch/" 2>/dev/null | grep -E "tar\.gz$|^d" | tail -20
    else
        echo "  (none)"
    fi

    echo ""
    echo "Config backups:"
    if [ -d "${BACKUP_DIR}/configs" ]; then
        ls -la "${BACKUP_DIR}/configs/" 2>/dev/null | grep -E "tar\.gz$|^d" | tail -20
    else
        echo "  (none)"
    fi
}

# ---------- Restore Redis ----------
restore_redis() {
    local date_tag="$1"
    local backup_file="${BACKUP_DIR}/redis/${date_tag}.tar.gz"
    local backup_dir="${BACKUP_DIR}/redis/${date_tag}"

    log_step "Restoring Redis from ${date_tag}..."

    # Find backup
    if [ -f "$backup_file" ]; then
        # Extract compressed backup
        local tmp_dir
        tmp_dir=$(mktemp -d)
        tar -xzf "$backup_file" -C "$tmp_dir"
        backup_dir="${tmp_dir}/${date_tag}"
    elif [ ! -d "$backup_dir" ]; then
        die "Redis backup not found: ${date_tag}"
    fi

    # Verify backup contents
    if [ ! -f "${backup_dir}/dump.rdb" ]; then
        die "No dump.rdb found in backup"
    fi

    log_warn "This will OVERWRITE current Redis data. Continue? (y/N)"
    read -r confirm
    if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
        log_info "Restore cancelled"
        return
    fi

    # Stop Redis writes
    docker exec "$REDIS_CONTAINER" redis-cli CONFIG SET stop-writes-on-bgsave-error yes > /dev/null 2>&1

    # Copy RDB file into container
    docker cp "${backup_dir}/dump.rdb" "${REDIS_CONTAINER}:/data/dump.rdb"
    log_info "RDB file copied"

    # Copy AOF if exists
    if [ -f "${backup_dir}/appendonly.aof" ]; then
        docker cp "${backup_dir}/appendonly.aof" "${REDIS_CONTAINER}:/data/appendonly.aof"
        log_info "AOF file copied"
    fi

    # Restart Redis to load backup
    docker restart "$REDIS_CONTAINER"
    log_info "Redis restarted"

    # Wait for Redis to be ready
    local retries=0
    while [ $retries -lt 30 ]; do
        if docker exec "$REDIS_CONTAINER" redis-cli ping > /dev/null 2>&1; then
            break
        fi
        retries=$((retries + 1))
        sleep 1
    done

    if [ $retries -eq 30 ]; then
        die "Redis failed to start after restore"
    fi

    # Verify
    local dbsize
    dbsize=$(docker exec "$REDIS_CONTAINER" redis-cli DBSIZE 2>/dev/null || echo "unknown")
    log_info "Redis restored. DB size: ${dbsize}"

    # Cleanup temp
    [ -n "${tmp_dir:-}" ] && rm -rf "$tmp_dir"
}

# ---------- Restore Elasticsearch ----------
restore_elasticsearch() {
    local date_tag="$1"
    local backup_file="${BACKUP_DIR}/elasticsearch/${date_tag}.tar.gz"
    local backup_dir="${BACKUP_DIR}/elasticsearch/${date_tag}"

    log_step "Restoring Elasticsearch from ${date_tag}..."

    # Find backup
    if [ -f "$backup_file" ]; then
        local tmp_dir
        tmp_dir=$(mktemp -d)
        tar -xzf "$backup_file" -C "$tmp_dir"
        backup_dir="${tmp_dir}/${date_tag}"
    elif [ ! -d "$backup_dir" ]; then
        die "Elasticsearch backup not found: ${date_tag}"
    fi

    log_warn "This will restore Elasticsearch indices. Continue? (y/N)"
    read -r confirm
    if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
        log_info "Restore cancelled"
        return
    fi

    # Check if snapshot repository exists with this backup
    local snapshot_name="snapshot_${date_tag}"
    local snapshot_check
    snapshot_check=$(curl -sf "${ES_URL}/_snapshot/wikisurge_backup/${snapshot_name}" 2>/dev/null || echo "")

    if echo "$snapshot_check" | grep -q "AVAILABLE\|SUCCESS"; then
        # Restore from snapshot
        log_info "Restoring from ES snapshot: ${snapshot_name}"

        # Close indices that will be restored
        curl -sf -X POST "${ES_URL}/wikipedia_edits*/_close" > /dev/null 2>&1 || true

        # Restore
        curl -sf -X POST "${ES_URL}/_snapshot/wikisurge_backup/${snapshot_name}/_restore" \
            -H 'Content-Type: application/json' \
            -d '{
                "indices": "wikipedia_edits*",
                "ignore_unavailable": true,
                "include_global_state": false
            }' > /dev/null 2>&1

        log_info "Snapshot restore initiated"
    else
        # Restore from exported mappings
        log_info "Restoring from exported index mappings"

        for mapping_file in "${backup_dir}"/*_mapping.json; do
            if [ ! -f "$mapping_file" ]; then continue; fi

            local index
            index=$(basename "$mapping_file" _mapping.json)
            local settings_file="${backup_dir}/${index}_settings.json"

            log_info "Restoring index: ${index}"

            # Delete existing index
            curl -sf -X DELETE "${ES_URL}/${index}" > /dev/null 2>&1 || true

            # Recreate with settings
            if [ -f "$settings_file" ]; then
                curl -sf -X PUT "${ES_URL}/${index}" \
                    -H 'Content-Type: application/json' \
                    -d @"$settings_file" > /dev/null 2>&1 || true
            fi

            # Apply mapping
            curl -sf -X PUT "${ES_URL}/${index}/_mapping" \
                -H 'Content-Type: application/json' \
                -d @"$mapping_file" > /dev/null 2>&1 || true
        done

        log_warn "Index structure restored. Data will need to be re-ingested."
    fi

    # Wait for cluster health
    curl -sf "${ES_URL}/_cluster/health?wait_for_status=yellow&timeout=30s" > /dev/null 2>&1 || true
    log_info "Elasticsearch restore complete"

    # Cleanup temp
    [ -n "${tmp_dir:-}" ] && rm -rf "$tmp_dir"
}

# ---------- Validate Restore ----------
validate_restore() {
    log_step "Validating restored data..."

    # Redis
    if docker exec "$REDIS_CONTAINER" redis-cli ping > /dev/null 2>&1; then
        local dbsize
        dbsize=$(docker exec "$REDIS_CONTAINER" redis-cli DBSIZE 2>/dev/null)
        log_info "Redis: healthy - ${dbsize}"
    else
        log_error "Redis: unhealthy"
    fi

    # Elasticsearch
    if curl -sf "${ES_URL}/_cluster/health" > /dev/null 2>&1; then
        local indices
        indices=$(curl -sf "${ES_URL}/_cat/indices?format=json" 2>/dev/null | grep -c '"index"' || echo "0")
        log_info "Elasticsearch: healthy - ${indices} indices"
    else
        log_error "Elasticsearch: unhealthy"
    fi

    # Full health check
    if [ -x "$HEALTH_CHECK" ]; then
        bash "$HEALTH_CHECK" --quiet 2>/dev/null && log_info "All services healthy" || log_warn "Some services unhealthy"
    fi
}

# =============================================================================
# Main
# =============================================================================
main() {
    # Handle --list
    if [ "${1:-}" = "--list" ]; then
        list_backups
        exit 0
    fi

    # Require date tag
    if [ $# -lt 1 ]; then
        echo "Usage: $0 <backup-date> [redis|elasticsearch|all]"
        echo "       $0 --list"
        echo ""
        echo "Example: $0 20260208_020000"
        exit 1
    fi

    local date_tag="$1"
    local target="${2:-all}"

    log ""
    log "=========================================="
    log "  WikiSurge Restore - ${date_tag}"
    log "=========================================="
    log ""

    case "$target" in
        redis)
            restore_redis "$date_tag"
            ;;
        elasticsearch|es)
            restore_elasticsearch "$date_tag"
            ;;
        all)
            restore_redis "$date_tag"
            restore_elasticsearch "$date_tag"
            ;;
        *)
            echo "Usage: $0 <backup-date> {all|redis|elasticsearch}"
            exit 1
            ;;
    esac

    validate_restore

    log ""
    log_info "Restore completed"
}

main "$@"
