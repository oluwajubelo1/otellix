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

# Find the compose directory. When piped from curl there's no checkout yet,
# so clone the repo (into ./otellix by default) and use that.
REPO_URL="https://github.com/oluwajubelo1/otellix.git"
CLONE_DIR="${OTELLIX_DIR:-otellix}"

if [ -d examples/docker-compose ]; then
    : # already in a checkout
elif [ -d "$CLONE_DIR/examples/docker-compose" ]; then
    echo -e "${BLUE}📂 Using existing checkout in ./$CLONE_DIR${NC}"
    cd "$CLONE_DIR"
else
    if ! command -v git &> /dev/null; then
        echo -e "${RED}❌ git is not installed. Please install it, or clone ${REPO_URL} and run scripts/demo.sh from the repo root.${NC}"
        exit 1
    fi
    echo -e "${BLUE}📥 Cloning Otellix into ./$CLONE_DIR...${NC}"
    git clone --depth 1 "$REPO_URL" "$CLONE_DIR"
    cd "$CLONE_DIR"
fi

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
echo -e "To stop the demo, run: ${BLUE}cd $(pwd) && $COMPOSE_CMD down${NC}"
