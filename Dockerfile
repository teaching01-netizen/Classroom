# Stage 1: Build frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app/web
COPY web/package*.json ./
RUN npm install
COPY web/ ./
RUN npm run build

# Stage 2: Build backend
FROM rust:1.75-alpine AS backend-builder
WORKDIR /app
RUN apk add --no-cache musl-dev pkgconfig openssl-dev
COPY Cargo.toml Cargo.lock ./
COPY crates/ ./crates/
RUN cargo build --release

# Stage 3: Final image
FROM alpine:3.19
WORKDIR /app
RUN apk add --no-cache ca-certificates libssl3
COPY --from=backend-builder /app/target/release/qr-command-center-server ./
COPY --from=frontend-builder /app/web/dist ./web/dist/

EXPOSE 3000
CMD ["./qr-command-center-server"]
