#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Starting development servers...${NC}"

# Function to cleanup on exit
cleanup() {
    echo -e "\n${YELLOW}Shutting down servers...${NC}"
    kill $BACKEND_PID 2>/dev/null
    kill $FRONTEND_PID 2>/dev/null
    exit 0
}

trap cleanup SIGINT SIGTERM

# Start backend
echo -e "${GREEN}[1/2] Starting Go backend...${NC}"
go run ./cmd/server &
BACKEND_PID=$!

# Wait for backend to be ready
echo -e "${YELLOW}Waiting for backend to start...${NC}"
sleep 3

# Check if backend is running
if ! kill -0 $BACKEND_PID 2>/dev/null; then
    echo -e "${RED}Backend failed to start!${NC}"
    exit 1
fi

echo -e "${GREEN}Backend running on http://localhost:3001${NC}"

# Start frontend
echo -e "${GREEN}[2/2] Starting Vite frontend...${NC}"
cd web && npm run dev &
FRONTEND_PID=$!

echo -e "${GREEN}Frontend running on http://localhost:5175${NC}"
echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}Both servers running!${NC}"
echo -e "  Backend:  http://localhost:3001"
echo -e "  Frontend: http://localhost:5175"
echo -e "${YELLOW}Press Ctrl+C to stop both servers${NC}"
echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

# Wait for either process to exit
wait
