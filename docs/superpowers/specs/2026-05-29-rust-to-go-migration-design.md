# Design: Rust → Go Migration — QR Command Center Backend

## Context

The Check-in QR Command Center is a Rust/Axum backend that polls Warwick Institute's attendance API for QR codes, manages room configurations, and pushes live updates to a React frontend via WebSocket. Rust compile times are slowing down development. This plan migrates the backend to Go (1:1 feature parity) with the frontend unchanged.

## Decisions

| Decision | Choice |
|---|---|
| Motivation | Rust compile times too slow |
| Approach | In-place rewrite (delete Rust, replace with Go) |
| Frontend | No changes (React stays as-is) |
| HTTP Framework | stdlib + chi/v5 |
| WebSocket | nhooyr.io/websocket (not gorilla — archived) |
| DB Driver | pgx/v5 |
| DB Migrations | golang-migrate/v4 |
| Testing | Go stdlib + testify |
| Logging | log/slog (stdlib) |
| Config | godotenv (dev-only) |

## JSON Serialization Contract

The frontend depends on exact JSON shapes. Getting these wrong breaks the UI silently.

### RoomStatus — PascalCase strings

```json
"Idle", "Running", "Fetching", "Warning", "AuthExpired", "Stopped"
```

Go: custom `MarshalJSON`/`UnmarshalJSON` on `RoomStatus`.

### RoomManagerEvent — externally-tagged enum

```json
{"RoomUpdated": {room...}}
{"RoomCreated": {room...}}
{"RoomDeleted": "uuid-string"}
{"FullStateSync": [{room...}]}
```

Go: custom struct with `MarshalJSON` producing wrapper keys.

### Room JSON — snake_case, optional fields as null

```json
{
  "room_id": "uuid",
  "class_id": "18248",
  "name": "Math",
  "status": "Running",
  "qr_url": "data:image/png;base64,...",
  "expires_at": "2024-01-01T00:01:00Z",
  "last_updated_at": "2024-01-01T00:00:00Z",
  "warning_message": null,
  "error_message": null,
  "last_fetch_at": "..."
}
```

- All optional `time.Time` fields are `*time.Time` (pointers) → serialize as `null` when unset
- `CreatedAt` is `json:"-"` (not sent to frontend)
- `QrTime` custom unmarshal handles both u64 and string from Warwick API

### ApiResponse — lowercase keys

```json
{"success": true, "data": {...}, "error": null}
```

## Go Project Structure

```
check in auto/
├── cmd/server/main.go
├── internal/
│   ├── domain/
│   │   ├── room.go          # RoomStatus, Room, QrResponse, FetchError, TransitionError, CalculateNextFetchDelay
│   │   ├── room_test.go
│   │   └── client.go        # QrClient interface
│   ├── db/
│   │   ├── migrations/001_create_rooms_table.sql
│   │   ├── repository.go    # RoomRepository interface + PgRoomRepository
│   │   ├── repository_test.go
│   │   └── db.go            # Connection pool + migration runner
│   ├── warwick/
│   │   ├── auth.go          # WarwickAuth (session management, login flow)
│   │   ├── client.go        # WarwickQrClient (QR fetch)
│   │   ├── auth_test.go
│   │   └── client_test.go
│   ├── api/
│   │   ├── routes.go        # chi router setup
│   │   ├── handlers.go      # REST handlers
│   │   ├── websocket.go     # WS handler + per-client write goroutine
│   │   └── handlers_test.go
│   └── service/
│       └── room_manager.go  # Core service (RoomManager + goroutine workers)
├── web/                     # React (unchanged)
├── go.mod
├── go.sum
├── Dockerfile
├── docker-compose.yml
├── docker-compose.prod.yml
├── .env
├── build.sh
└── deploy.sh
```

### Dependencies

```
github.com/go-chi/chi/v5          HTTP router
nhooyr.io/websocket               WebSocket (context-native, not archived)
github.com/jackc/pgx/v5           PostgreSQL driver + pool
github.com/golang-migrate/migrate/v4  DB migrations
github.com/google/uuid            UUID generation
github.com/stretchr/testify       Test assertions
github.com/joho/godotenv          .env loading (dev only)
```

## Phase 1: Scaffold + Domain Types

**Files:** `go.mod`, `internal/domain/room.go`, `internal/domain/client.go`, `internal/domain/room_test.go`

### domain/room.go

- `RoomStatus` — `type RoomStatus string` with constants (`Idle`, `Running`, `Fetching`, `Warning`, `AuthExpired`, `Stopped`)
- Custom `MarshalJSON`/`UnmarshalJSON` on `RoomStatus` → PascalCase strings
- `Room` struct:
  ```go
  type Room struct {
      RoomID        uuid.UUID  `json:"room_id"`
      ClassID       string     `json:"class_id"`
      Name          *string    `json:"name"`
      Status        RoomStatus `json:"status"`
      QRURL         *string    `json:"qr_url"`
      ExpiresAt     *time.Time `json:"expires_at"`
      LastUpdatedAt *time.Time `json:"last_updated_at"`
      WarningMessage *string  `json:"warning_message"`
      ErrorMessage   *string  `json:"error_message"`
      LastFetchAt    *time.Time `json:"last_fetch_at"`
      CreatedAt     time.Time  `json:"-"`
  }
  ```
- `NewRoom(classID string, name *string) Room`
- `CanTransitionTo(next RoomStatus) error` — same state machine as Rust
- `CalculateNextFetchDelay(ttl uint64) uint64` — `(ttl * 3) / 4`

### domain/client.go

- `QrClient` interface:
  ```go
  type QrClient interface {
      FetchQR(classID string) (QrResponse, error)
      FetchQRWithFreshAuth(classID string) (QrResponse, error)
  }
  ```

### QrResponse

```go
type QrResponse struct {
    QrURL  string `json:"qrUrl"`
    QrTime uint64 // custom UnmarshalJSON: handles u64 OR string
}
```

Custom `QrTime` type with `UnmarshalJSON`:
```go
type QrTime uint64

func (qt *QrTime) UnmarshalJSON(data []byte) error {
    var num uint64
    if err := json.Unmarshal(data, &num); err == nil {
        *qt = QrTime(num)
        return nil
    }
    var s string
    if err := json.Unmarshal(data, &s); err == nil {
        n, err := strconv.ParseUint(s, 10, 64)
        if err != nil { return fmt.Errorf("cannot parse qrTime %q: %w", s, err) }
        *qt = QrTime(n)
        return nil
    }
    return fmt.Errorf("qrTime must be number or string, got %s", string(data))
}
```

### FetchError

```go
var ErrAuthExpired = errors.New("warwick session expired")

type NetworkError struct{ Msg string }
func (e *NetworkError) Error() string { return "network request failed: " + e.Msg }

type InvalidPayloadError struct{ Msg string }
func (e *InvalidPayloadError) Error() string { return "invalid response payload: " + e.Msg }
```

### Tests
- `TestCalculateNextFetchDelay` — 60→45, 100→75, 120→90
- `TestQrResponseDeserializeNumber` — u64 JSON
- `TestQrResponseDeserializeString` — string JSON
- `TestValidTransitions` — all 12 valid state machine transitions
- `TestInvalidTransitions` — all 24 invalid transitions
- `TestFetchErrorToRoomStatus` — AuthExpired→AuthExpired, Network→Warning, InvalidPayload→Warning

## Phase 2: Database Layer

**Files:** `internal/db/migrations/001_create_rooms_table.sql`, `internal/db/db.go`, `internal/db/repository.go`, `internal/db/repository_test.go`

### Migration (same as Rust)

```sql
CREATE TYPE room_status AS ENUM (
    'idle', 'running', 'fetching', 'warning', 'auth_expired', 'stopped'
);

CREATE TABLE rooms (
    room_id UUID PRIMARY KEY,
    class_id TEXT NOT NULL,
    name TEXT,
    status room_status NOT NULL DEFAULT 'idle',
    qr_url TEXT,
    expires_at TIMESTAMPTZ,
    last_updated_at TIMESTAMPTZ,
    warning_message TEXT,
    error_message TEXT,
    last_fetch_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### db.go

- `//go:embed migrations/*.sql` for migration files
- `NewPool(databaseURL string) (*pgxpool.Pool, error)`
- `RunMigrations(pool) error` — `pgx/v5/stdlib.Open()` → `*sql.DB` → golang-migrate postgres driver

### repository.go

```go
type RoomRepository interface {
    CreateRoom(room domain.Room) (domain.Room, error)
    GetRoom(roomID uuid.UUID) (domain.Room, error)
    GetAllRooms() ([]domain.Room, error)
    UpdateRoom(room domain.Room) (domain.Room, error)
    DeleteRoom(roomID uuid.UUID) error
}
```

- `PgRoomRepository` struct with `*pgxpool.Pool`
- `roomStatusToString()` / `stringToRoomStatus()` — returns error for unknown strings
- Row scanning via `pgx.Row` → `Room`

### Tests
- In-memory repository for unit tests
- Integration tests with testcontainers-go (ephemeral PostgreSQL)

## Phase 3: Warwick Client

**Files:** `internal/warwick/auth.go`, `internal/warwick/client.go`, `internal/warwick/auth_test.go`, `internal/warwick/client_test.go`

### auth.go

- `WarwickAuth` struct: email, password, loginURL, client (`*http.Client` with `CheckRedirect` returning `http.ErrUseLastResponse`), session (`sync.RWMutex` + `*SessionState`)
- `SessionState`: cookieValue, obtainedAt, expiresAt (15 min TTL)
- `NewWarwickAuth(email, password, loginURL string) *WarwickAuth`
- `FromEnv() (*WarwickAuth, error)` — reads WARWICK_EMAIL, WARWICK_PASSWORD
- `GetValidSession() (string, error)` — double-checked locking pattern
- `ForceRefresh() (string, error)` — unconditional re-login
- `performLogin() (*SessionState, error)` — POST form data, extract Set-Cookie via manual string splitting (matching Rust's approach, not stdlib parsing)
- `isLoginPage(body string) bool` — same detection: `idg-box-login-primary`, `idg-btn-sumbit`, or title+forgot password+password field

### client.go

- `WarwickQrClient` struct: auth, client, qrEndpoint
- `NewWarwickQrClient(auth) *WarwickQrClient`
- `NewWarwickQrClientWithEndpoint(auth, endpoint) *WarwickQrClient`
- `FetchQR(classID)` → `GetValidSession()` → `doFetch(classID, cookie)`
- `FetchQRWithFreshAuth(classID)` → `ForceRefresh()` → `doFetch(classID, cookie)`
- `doFetch(classID, cookie) (QrResponse, error)`:
  1. POST to Warwick QR endpoint with `Cookie: ASP.NET_SessionId={cookie}`
  2. Check 302/301 → `ErrAuthExpired`
  3. Check Content-Type `text/html` → `is_login_page` → `ErrAuthExpired`, else `InvalidPayloadError`
  4. Parse JSON → validate `qrUrl` is non-empty and starts with `data:image/`

### Tests (httptest.NewServer mocking)
- `TestLoginPageDetection` — HTML with markers, dashboard HTML
- `TestExtractSessionCookie` — Set-Cookie header parsing
- `TestGetValidSessionReturnsCached` — second call doesn't hit server
- `TestFetchQRSuccess` — valid JSON response
- `TestFetchQRAuthExpired` — 302 redirect
- `TestFetchQRInvalidPayload` — HTML without login markers
- `TestFetchQREmptyQRUrl` — empty qrUrl rejected

## Phase 4: Service / RoomManager

**Files:** `internal/service/room_manager.go`

### RoomManager

```go
type RoomManager struct {
    rooms      sync.RWMutex           // map[uuid.UUID]*RoomState
    eventCh    chan RoomManagerEvent   // buffered 100
    qrClient   domain.QrClient
    repository db.RoomRepository
}

type RoomState struct {
    room   domain.Room
    ctx    context.Context
    cancel context.CancelFunc
}
```

### RoomManagerEvent

```go
type RoomManagerEvent struct {
    Type string      // "RoomUpdated", "RoomCreated", "RoomDeleted", "FullStateSync"
    Data interface{}
}
```

Custom `MarshalJSON` producing externally-tagged format:
```json
{"RoomUpdated": {...}}
```

### Methods

- `NewRoomManager(qrClient, repository) *RoomManager`
- `LoadRoomsFromDB() error` — hydrate in-memory from DB
- `Subscribe() <-chan RoomManagerEvent` — new buffered channel (256)
- `CreateRoom(classID, name)` — **persist to DB first**, then insert to in-memory, emit `RoomCreated`
- `DeleteRoom(roomID)` — cancel context, remove from DB + memory, emit `RoomDeleted`
- `GetRoom(roomID) *Room`, `GetAllRooms() []Room`
- `StartRoom(roomID)` — set status to Running **synchronously**, store cancel in RoomState, emit `RoomUpdated`, spawn goroutine
- `StopRoom(roomID)` — call cancel, transition to Stopped, persist synchronously, emit `RoomUpdated`

### runRoomWorker

```go
func (rm *RoomManager) runRoomWorker(room domain.Room, state *RoomState) {
    defer func() { /* cleanup */ }()
    for {
        select {
        case <-state.ctx.Done():
            return
        case <-time.After(1 * time.Second):
            now := time.Now()
            defaultTTL := uint64(60)
            shouldFetch := room.ExpiresAt == nil || now.After(...)
            if shouldFetch {
                // transition to Fetching, emit
                resp, err := rm.qrClient.FetchQR(room.ClassID)
                // update room on success, warn on error, break on auth expired
                // persist to DB (log errors)
            }
        }
    }
}
```

### Tests (same as Rust)
- InMemoryRoomRepository + MockQrClient
- TestCreateRoom, TestGetRoom, TestGetRoomNotFound, TestDeleteRoom
- TestStartRoom, TestStartRoomTwiceFails, TestStopRoom
- TestWorkerFetchSuccess, TestWorkerAuthExpired, TestWorkerNetworkWarning
- TestDeleteRoomStopsWorker

## Phase 5: API + WebSocket

**Files:** `internal/api/routes.go`, `internal/api/handlers.go`, `internal/api/websocket.go`, `internal/api/handlers_test.go`

### routes.go

```go
func NewRouter(rm *service.RoomManager) *chi.Mux {
    r := chi.NewRouter()
    r.Use(corsMiddleware) // allow all for dev
    r.Get("/api/", rootHandler)
    r.Route("/api/rooms", func(r chi.Router) {
        r.Get("/", getRoomsHandler(rm))
        r.Post("/", createRoomHandler(rm))
        r.Get("/{id}", getRoomHandler(rm))
        r.Delete("/{id}", deleteRoomHandler(rm))
        r.Post("/{id}/start", startRoomHandler(rm))
        r.Post("/{id}/stop", stopRoomHandler(rm))
    })
    r.Get("/ws", wsHandler(rm))
    r.Handle("/*", spaFallbackHandler()) // web/dist + SPA fallback
    return r
}
```

### handlers.go

- Each handler: decode JSON → call RoomManager → return `ApiResponse` JSON
- `ApiResponse` struct with `json:"success"`, `json:"data"`, `json:"error"`

### websocket.go

- nhooyr.io/websocket upgrade
- On connect: send `FullStateSync` with all rooms
- Per-client write goroutine with buffered channel (256)
- Subscribe to RoomManager events, non-blocking send to client channel (drop slow clients)
- Handle client Close frames

### Tests
- httptest.NewServer with chi router
- Test root endpoint returns success JSON
- Test WebSocket: connect → receive FullStateSync

## Phase 6: Main + Config

**File:** `cmd/server/main.go`

- Load .env via godotenv
- Parse env: WARWICK_EMAIL, WARWICK_PASSWORD, DATABASE_URL, SERVER_ADDR (default `0.0.0.0:3000`)
- Validate Warwick auth at startup (fatal if fails)
- Connect to PostgreSQL, run migrations
- Create WarwickQrClient → RoomManager → Router
- Load rooms from DB
- Graceful shutdown: `signal.NotifyContext` → cancel all workers → `srv.Shutdown` with 10s timeout
- Healthcheck: GET /api/ returns `{"success": true, "message": "QR Command Center API is running!"}`

## Phase 7: Docker + Deployment

### Dockerfile (multi-stage)

```dockerfile
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
```

### docker-compose.yml (dev)

```yaml
version: '3.8'
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: qruser
      POSTGRES_PASSWORD: qrpassword
      POSTGRES_DB: qrcommandcenter
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U qruser -d qrcommandcenter"]
      interval: 5s
      timeout: 5s
      retries: 5
volumes:
  postgres_data:
```

### docker-compose.prod.yml

Add `depends_on: postgres: condition: service_healthy`.

### build.sh

```bash
cd web && npm install && npm run build && cd ..
mkdir -p target/release
go build -o target/release/qr-command-center-server ./cmd/server
```

## Phase 8: Cleanup + Verification

1. Delete: `crates/`, `Cargo.toml`, `Cargo.lock`, `target/`
2. `go test -race ./...` — all pass
3. `go vet ./...` — no issues
4. `go build ./cmd/server` — compiles
5. `docker-compose up --build` — app starts, frontend loads, WS connects
6. Verify frontend: create room, start, see QR, stop, delete

## Key Bugs Fixed During Migration

1. **Hardcoded TTL**: Rust `calculate_next_fetch_delay(60)` ignores actual `qrTime`. Go uses actual `qrTime` from response, with 60s default for first fetch.
2. **Fire-and-forget DB writes**: Rust silently drops DB errors. Go logs all persistence errors.
3. **create_room ordering**: Rust inserts to memory before DB. Go persists first, then updates memory.
4. **str_to_room_status**: Rust defaults to Idle on unknown. Go returns error for unknown status strings.

## WebSocket Protocol (must match exactly)

```
Server → Client on connect:
  {"FullStateSync": [{room...}, ...]}

Server → Client on events:
  {"RoomCreated": {room...}}
  {"RoomUpdated": {room...}}
  {"RoomDeleted": "uuid-string"}
```

## API Endpoints (must match exactly)

| Method | Path | Response |
|---|---|---|
| GET | /api/ | `{"success": true, "message": "..."}` |
| GET | /api/rooms | `{"success": true, "data": [Room, ...]}` |
| POST | /api/rooms | `{"success": true, "data": Room}` |
| GET | /api/rooms/:id | `{"success": true, "data": Room}` |
| DELETE | /api/rooms/:id | `{"success": true}` |
| POST | /api/rooms/:id/start | `{"success": true}` |
| POST | /api/rooms/:id/stop | `{"success": true}` |
| GET | /ws | WebSocket upgrade |
