# Check-in QR Command Center

## Quick Start (Docker)

The easiest way to deploy is with Docker:

```bash
./deploy.sh
```

This will:
1. Build the Docker images
2. Start the application
3. Automatically use DATABASE_URL from your .env file
4. Make it available at http://localhost:3000

## Deployment Options

### Option 1: Docker Compose (Recommended)

```bash
docker-compose -f docker-compose.prod.yml up -d --build
```

### Option 2: Native Build

1. Build everything:
```bash
./build.sh
```

2. Make sure your `.env` file has the correct `DATABASE_URL`:
```env
DATABASE_URL=postgres://user:password@host:port/dbname
```

3. Run the application (it will automatically load from `.env`):
```bash
./target/release/qr-command-center-server
```

## Development

### Backend
```bash
cargo run -p qr-command-center-server
```

### Frontend
```bash
cd web
npm run dev
```

## Scripts

- `build.sh`: Builds frontend and backend
- `deploy.sh`: Deploys using Docker Compose (or native if Docker not available)
