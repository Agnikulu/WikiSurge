#!/bin/bash

# WikiSurge Resource Check Script
# Checks system resources before starting infrastructure

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "🔍 WikiSurge Resource Check"
echo "=========================="

# Function to convert memory to MB
memory_to_mb() {
    local memory_str=$1
    local value=$(echo $memory_str | grep -o '[0-9]*')
    local unit=$(echo $memory_str | grep -o '[A-Za-z]*')
    
    case $unit in
        "kB"|"KB"|"K")
            echo $((value / 1024))
            ;;
        "MB"|"M")
            echo $value
            ;;
        "GB"|"G")
            echo $((value * 1024))
            ;;
        *)
            echo $((value / 1024 / 1024))  # Assume bytes
            ;;
    esac
}

# Check available memory
echo -n "💾 Checking available memory..."
if command -v free &> /dev/null; then
    TOTAL_MEMORY=$(free -m | awk 'NR==2{print $2}')
    AVAILABLE_MEMORY=$(free -m | awk 'NR==2{print $7}')
    
    MIN_REQUIRED_MEMORY=4096  # 4GB in MB
    RECOMMENDED_MEMORY=8192   # 8GB in MB
    
    echo " ${TOTAL_MEMORY}MB total, ${AVAILABLE_MEMORY}MB available"
    
    if [ $AVAILABLE_MEMORY -lt $MIN_REQUIRED_MEMORY ]; then
        echo -e "${RED}❌ Insufficient memory: ${AVAILABLE_MEMORY}MB available, ${MIN_REQUIRED_MEMORY}MB required${NC}"
        exit 1
    elif [ $AVAILABLE_MEMORY -lt $RECOMMENDED_MEMORY ]; then
        echo -e "${YELLOW}⚠️  Limited memory: ${AVAILABLE_MEMORY}MB available, ${RECOMMENDED_MEMORY}MB recommended${NC}"
    else
        echo -e "${GREEN}✅ Memory: ${AVAILABLE_MEMORY}MB available${NC}"
    fi
else
    echo -e "${YELLOW}⚠️  Cannot check memory (free command not found)${NC}"
fi

# Check available disk space
echo -n "💿 Checking available disk space..."
AVAILABLE_DISK=$(df -BM . | awk 'NR==2{print $4}' | sed 's/M//')
MIN_REQUIRED_DISK=10240  # 10GB in MB
RECOMMENDED_DISK=20480   # 20GB in MB

echo " ${AVAILABLE_DISK}MB available"

if [ $AVAILABLE_DISK -lt $MIN_REQUIRED_DISK ]; then
    echo -e "${RED}❌ Insufficient disk space: ${AVAILABLE_DISK}MB available, ${MIN_REQUIRED_DISK}MB required${NC}"
    exit 1
elif [ $AVAILABLE_DISK -lt $RECOMMENDED_DISK ]; then
    echo -e "${YELLOW}⚠️  Limited disk space: ${AVAILABLE_DISK}MB available, ${RECOMMENDED_DISK}MB recommended${NC}"
else
    echo -e "${GREEN}✅ Disk space: ${AVAILABLE_DISK}MB available${NC}"
fi

# Check Docker resource limits
echo -n "🐳 Checking Docker resource limits..."
if command -v docker &> /dev/null && docker info &> /dev/null; then
    DOCKER_MEMORY=$(docker system df 2>/dev/null | grep -i "total" | awk '{print $4}' | head -1 || echo "unknown")
    echo " Docker available"
    echo -e "${GREEN}✅ Docker is configured${NC}"
else
    echo -e "${YELLOW}⚠️  Docker not available for resource check${NC}"
fi

# Check required ports
echo "🔌 Checking required ports..."
REQUIRED_PORTS=(9092 6379 9200 8080 3000 9090 9644)
BLOCKED_PORTS=()

for port in "${REQUIRED_PORTS[@]}"; do
    if netstat -ln 2>/dev/null | grep ":$port " &>/dev/null || lsof -i :$port &>/dev/null; then
        BLOCKED_PORTS+=($port)
    fi
done

if [ ${#BLOCKED_PORTS[@]} -gt 0 ]; then
    echo -e "${YELLOW}⚠️  Ports already in use: ${BLOCKED_PORTS[*]}${NC}"
    echo "   These services may conflict with WikiSurge infrastructure"
else
    echo -e "${GREEN}✅ All required ports are available${NC}"
fi

# Check CPU cores
echo -n "🏭 Checking CPU cores..."
CPU_CORES=$(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo "unknown")
if [ "$CPU_CORES" != "unknown" ]; then
    if [ $CPU_CORES -lt 2 ]; then
        echo -e "${YELLOW}⚠️  Limited CPU: ${CPU_CORES} core(s) available, 2+ recommended${NC}"
    else
        echo -e "${GREEN}✅ CPU: ${CPU_CORES} core(s) available${NC}"
    fi
else
    echo -e "${YELLOW}⚠️  Cannot determine CPU cores${NC}"
fi

# Summary
echo ""
echo "📋 Resource Summary:"
echo "   Memory: ${AVAILABLE_MEMORY:-unknown}MB available"
echo "   Disk:   ${AVAILABLE_DISK:-unknown}MB available"
echo "   CPU:    ${CPU_CORES:-unknown} core(s)"
echo "   Ports:  ${#BLOCKED_PORTS[@]} blocked, $((${#REQUIRED_PORTS[@]} - ${#BLOCKED_PORTS[@]})) available"

if [ ${#BLOCKED_PORTS[@]} -gt 0 ]; then
    echo ""
    echo -e "${YELLOW}⚠️  Warning: Some ports are in use. Stop conflicting services or modify docker-compose.yml${NC}"
fi

echo ""
if [ $AVAILABLE_MEMORY -ge $MIN_REQUIRED_MEMORY ] && [ $AVAILABLE_DISK -ge $MIN_REQUIRED_DISK ]; then
    echo -e "${GREEN}🎉 System resources are sufficient for WikiSurge!${NC}"
    exit 0
else
    echo -e "${RED}❌ System resources are insufficient. Please upgrade hardware or free up resources.${NC}"
    exit 1
fi