#!/bin/bash

echo "🔨 Building Check-in QR Command Center..."

# Build frontend
echo "📦 Building frontend..."
cd web || exit 1
npm install
npm run build
cd ..

# Build backend
echo "🦀 Building backend..."
cargo build --release

echo "✅ Build complete!"
echo "Backend binary: target/release/qr-command-center-server"
echo "Frontend static files: web/dist/"
echo ""
echo "To run:"
echo "  ./target/release/qr-command-center-server"
echo "  (it will automatically load DATABASE_URL from .env)"
