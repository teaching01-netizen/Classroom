# Stage 1: Build frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app/web
COPY web/package*.json ./
RUN npm install
COPY web/ ./
RUN npm run build

# Stage 2: Build backend
FROM golang:1.22-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend-builder /app/web/dist ./web/dist/
RUN CGO_ENABLED=0 go build -o qr-command-center-server ./cmd/server

# Stage 3: Final image
FROM alpine:3.19
RUN adduser -D -u 1001 appuser
WORKDIR /app
RUN apk add --no-cache ca-certificates
COPY --from=go-builder /app/qr-command-center-server ./
COPY --from=frontend-builder /app/web/dist ./web/dist/
USER appuser
EXPOSE 3000
CMD ["./qr-command-center-server"]
