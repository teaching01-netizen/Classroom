#!/bin/bash
set -e

echo "Building frontend..."
cd web && npm ci && npm run build && cd ..

echo "Building Go backend..."
mkdir -p target/release
go build -o target/release/qr-command-center-server ./cmd/server

echo "Build complete!"
