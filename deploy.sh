#!/bin/bash

echo "🚀 Deploying Check-in QR Command Center..."

# Check if Docker is available
if command -v docker &> /dev/null && command -v docker-compose &> /dev/null; then
    echo "🐳 Using Docker Compose..."
    docker-compose -f docker-compose.prod.yml up -d --build
    echo "✅ App deployed! Access at http://localhost:3000"
    echo "   (Automatically using DATABASE_URL from .env)"
else
    echo "⚠️ Docker not found, using native build..."
    ./build.sh
    echo ""
    echo "✅ Build complete!"
    echo "To run the app:"
    echo "  ./target/release/qr-command-center-server"
    echo "  (Automatically using DATABASE_URL from .env)"
fi
