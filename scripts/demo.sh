#!/bin/bash

# scripts/demo.sh — One-liner to spin up the Otellix demo stack

set -e

# ANSI escape codes for colors
BLUE='\033[0;34m'
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BLUE}🚀 Starting Otellix Demo Stack...${NC}"

# Check for Docker
if ! command -v docker &> /dev/null; then
    echo -e "${RED}❌ Docker is not installed. Please install it to run the demo.${NC}"
    exit 1
fi

# Check for Docker Compose
if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
    echo -e "${RED}❌ Docker Compose is not installed.${NC}"
    exit 1
fi

# Determine compose command
COMPOSE_CMD="docker compose"
if command -v docker-compose &> /dev/null; then
    COMPOSE_CMD="docker-compose"
fi

# Navigate to the compose directory
# (Assumes script is run from repo root)
cd examples/docker-compose

echo -e "${BLUE}📦 Building and starting containers...${NC}"
$COMPOSE_CMD up -d --build

echo -e "${GREEN}✅ Otellix Demo is now running!${NC}"
echo -e ""
echo -e "📊 ${GREEN}Grafana Dashboard:${NC}  http://localhost:3000"
echo -e "📈 ${GREEN}Prometheus Metrics:${NC} http://localhost:9091"
echo -e ""
echo -e "Login to Grafana with: ${BLUE}admin / admin${NC}"
echo -e "The demo app is generating mock LLM traffic automatically."
echo -e ""
echo -e "To stop the demo, run: ${BLUE}$COMPOSE_CMD down${NC}"
