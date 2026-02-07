#!/bin/bash

# setup-kafka-topic.sh - Create Kafka topics for WikiSurge

set -euo pipefail

# Configuration
TOPIC_NAME="wikipedia.edits"
PARTITIONS=3
REPLICATION_FACTOR=1
RETENTION_HOURS=168  # 7 days
SEGMENT_SIZE="1073741824"  # 1GB
MIN_IN_SYNC_REPLICAS=1

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if rpk is available
check_rpk() {
    if ! command -v rpk &> /dev/null; then
        log_error "rpk command not found. Please ensure Redpanda/Kafka is running and rpk is available."
        log_info "You can run this from within the Kafka container:"
        log_info "docker-compose exec kafka /bin/bash"
        log_info "Then run this script from within the container"
        return 1
    fi
    return 0
}

# Check Kafka connectivity
check_kafka_connectivity() {
    log_info "Checking Kafka cluster connectivity..."
    
    if ! rpk cluster health &> /dev/null; then
        log_error "Cannot connect to Kafka cluster. Please ensure Kafka is running."
        log_info "Start the infrastructure with: make start"
        return 1
    fi
    
    log_info "Kafka cluster is healthy"
    return 0
}

# Check if topic exists
topic_exists() {
    rpk topic list | grep -q "^${TOPIC_NAME}$"
}

# Create the topic
create_topic() {
    log_info "Creating topic: ${TOPIC_NAME}"
    
    rpk topic create "${TOPIC_NAME}" \
        --partitions "${PARTITIONS}" \
        --replication-factor "${REPLICATION_FACTOR}" \
        --config "retention.ms=$((RETENTION_HOURS * 3600 * 1000))" \
        --config "segment.bytes=${SEGMENT_SIZE}" \
        --config "compression.type=producer" \
        --config "min.insync.replicas=${MIN_IN_SYNC_REPLICAS}" \
        --config "cleanup.policy=delete"
    
    if [ $? -eq 0 ]; then
        log_info "Topic '${TOPIC_NAME}' created successfully"
    else
        log_error "Failed to create topic '${TOPIC_NAME}'"
        return 1
    fi
}

# Display topic configuration
show_topic_config() {
    log_info "Topic configuration for '${TOPIC_NAME}':"
    echo "----------------------------------------"
    rpk topic describe "${TOPIC_NAME}"
    echo ""
    log_info "Topic configs:"
    rpk topic describe "${TOPIC_NAME}" --configs
}

# Update topic configuration if it exists
update_topic_config() {
    log_info "Updating configuration for existing topic: ${TOPIC_NAME}"
    
    rpk topic alter-config "${TOPIC_NAME}" \
        --set "retention.ms=$((RETENTION_HOURS * 3600 * 1000))" \
        --set "segment.bytes=${SEGMENT_SIZE}" \
        --set "compression.type=producer" \
        --set "min.insync.replicas=${MIN_IN_SYNC_REPLICAS}" \
        --set "cleanup.policy=delete"
    
    if [ $? -eq 0 ]; then
        log_info "Topic '${TOPIC_NAME}' configuration updated successfully"
    else
        log_error "Failed to update topic '${TOPIC_NAME}' configuration"
        return 1
    fi
}

# Main function
main() {
    log_info "Setting up Kafka topic for WikiSurge"
    log_info "Topic: ${TOPIC_NAME}"
    log_info "Partitions: ${PARTITIONS}"
    log_info "Replication Factor: ${REPLICATION_FACTOR}"
    log_info "Retention: ${RETENTION_HOURS} hours"
    echo ""
    
    # Pre-checks
    if ! check_rpk; then
        exit 1
    fi
    
    if ! check_kafka_connectivity; then
        exit 1
    fi
    
    # Create or update topic
    if topic_exists; then
        log_warn "Topic '${TOPIC_NAME}' already exists"
        
        # Ask user if they want to update configuration
        if [[ "${1:-}" == "--force" ]]; then
            update_topic_config
        else
            echo -n "Do you want to update the topic configuration? (y/N): "
            read -r response
            if [[ "$response" =~ ^[Yy]$ ]]; then
                update_topic_config
            else
                log_info "Skipping configuration update"
            fi
        fi
    else
        create_topic
    fi
    
    # Show final configuration
    echo ""
    show_topic_config
    
    log_info "Kafka topic setup complete!"
    log_info ""
    log_info "You can now:"
    log_info "1. Start the ingestor: go run cmd/ingestor/main.go"
    log_info "2. Monitor messages: rpk topic consume ${TOPIC_NAME} --offset start"
    log_info "3. Check topic status: rpk topic describe ${TOPIC_NAME}"
}

# Handle command line arguments
case "${1:-}" in
    --help|-h)
        echo "Usage: $0 [--force] [--help]"
        echo ""
        echo "Options:"
        echo "  --force    Force update topic configuration if topic exists"
        echo "  --help     Show this help message"
        echo ""
        echo "Environment variables:"
        echo "  KAFKA_BROKERS    Kafka brokers (default: localhost:9092)"
        exit 0
        ;;
    *)
        main "$@"
        ;;
esac