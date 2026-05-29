# Check-in QR Command Center Design

## Purpose
Build a local-first command center for class attendance QR codes. The app serves a single-page operator dashboard plus a read-only display view. Operators can create multiple independent rooms, each with its own `classId`, QR refresh loop, and websocket state. The Node.js backend is the source of truth, keeps the Warwick session cookie in memory only, fetches QR codes from `GetQRCode`, and fans out the current room state to every connected client.

## Goals
- Create and manage multiple independent control rooms.
- Require validation before any room can start.
- Keep QR codes fresh using a proactive overlap-and-buffer refresh strategy.
- Detect expiry even when Warwick returns `200 OK` with login HTML instead of `401`.
- Push late-join state immediately to new websocket clients.
- Keep the `ASP.NET_SessionId` cookie out of client state and out of disk.
- Support shared room actions so one client starting or stopping a room updates everyone.
- Provide a clear `AUTH_EXPIRED` state without destroying the room's identity or display connections.

## Non-goals
- No login flow against Warwick itself.
- No disk persistence for QR images, room state, or cookies.
- No external database.
- No attempt to recover automatically from an invalid cookie without operator intervention.
- No public LAN exposure by default.

## User Experience
The product has two local surfaces:

1. Operator dashboard
   - Single-page UI for room creation, cookie settings, and room control.
   - Operator can paste or update the global `ASP.NET_SessionId` in a settings drawer.
   - Operator can create rooms by entering `classId` and an optional room name.
   - Each room card has `Start`, `Stop`, and `Remove` controls.
   - Validation must pass before `Start` becomes enabled.

2. Read-only display view
   - Shows the current QR and countdown for one selected room.
   - Receives live updates over websocket.
   - Does not expose controls or the cookie.

## Functional Behavior
### Room lifecycle
- `IDLE`: room exists but has not started.
- `RUNNING`: refresh loop is active.
- `FETCHING`: a refresh request is in flight while the room remains running.
- `WARNING`: the last fetch failed but the current QR is still being shown until it expires or a retry succeeds.
- `AUTH_EXPIRED`: Warwick rejected the session cookie or returned login HTML/redirect content.
- `STOPPED`: operator manually halted the room.

### Room rules
- Rooms are independent. One room's start/stop does not affect another room.
- A room has a stable `roomId` and `classId`.
- A room may also have an optional friendly `name`.
- A room cannot start until `classId` and the global cookie are validated.
- `Stop` freezes the room without deleting state.
- `Remove` deletes the room from the current process memory.

### QR refresh loop
- The backend calls `POST /admin/ClassAttendance/GetQRCode` with `id=<classId>`.
- The backend sends:
  - `Cookie: ASP.NET_SessionId=<value>`
  - `Content-Type: application/x-www-form-urlencoded; charset=UTF-8`
  - `X-Requested-With: XMLHttpRequest`
- The backend treats `qrTime` as the authoritative TTL returned by Warwick.
- The backend schedules the next fetch at roughly 75% of the TTL, with a retry buffer before expiry.
- The backend never overwrites the current QR until the new response has been fetched and validated.
- If the refresh succeeds, the room atomically swaps to the new QR and broadcasts the update.
- If refresh fails temporarily, the room enters `WARNING`, keeps showing the last QR, and retries.

### Auth failure detection
- The backend must treat these as auth expiry signals:
  - `302` redirect to sign-in
  - `200 OK` with login HTML
  - missing or malformed JSON payload where a QR payload is expected
  - page body containing Warwick sign-in markers
- On auth expiry:
  - the room enters `AUTH_EXPIRED`
  - the refresh loop pauses
  - the room identity, clients, and layout remain intact
  - the dashboard prompts the operator to paste a new cookie
- Updating the cookie resumes rooms without recreating them.

### Realtime synchronization
- The server is authoritative for all room state.
- Any room action from one client updates the same room for every connected client.
- A newly connected websocket client receives a full state snapshot immediately.

## Architecture
### Backend
- Local Node.js service on `127.0.0.1`.
- Owns:
  - HTTP routes for dashboard and display views
  - websocket gateway
  - in-memory room registry
  - refresh schedulers
  - Warwick HTTP proxy logic
  - validation and auth-expiry detection

### Frontend
- Single-page operator dashboard.
- Read-only display view for projector or wallboard use.
- The frontend never receives the cookie back from the server.
- The frontend renders absolute `expiresAt` timestamps from the server rather than maintaining its own countdown source of truth.

### Room registry
Each room stores:
- `roomId`
- `name`
- `classId`
- `status`
- `qrUrl`
- `expiresAt`
- `lastUpdatedAt`
- `warningMessage`
- `errorMessage`
- `timerHandle`
- `lastFetchAt`

The process also stores:
- `sessionCookie`
- `connectedClients`

## Public Interface
### HTTP
- `GET /` returns the operator dashboard.
- `GET /display/:roomId` returns the read-only display view.
- `POST /rooms` creates a room after validation.
- `POST /rooms/:roomId/start` starts the room.
- `POST /rooms/:roomId/stop` stops the room.
- `DELETE /rooms/:roomId` removes the room.
- `POST /session-cookie` updates the global session cookie.
- `GET /rooms/:roomId/current` returns the latest room snapshot.

### Websocket commands
Client to server:
```json
{ "action": "CREATE_ROOM", "classId": "18248", "name": "SAT Math C1" }
{ "action": "START_ROOM", "roomId": "uuid-123" }
{ "action": "STOP_ROOM", "roomId": "uuid-123" }
{ "action": "REMOVE_ROOM", "roomId": "uuid-123" }
{ "action": "UPDATE_COOKIE", "sessionId": "y5kyy1en..." }
```

### Websocket events
Server to client:
```json
{
  "event": "FULL_STATE_SYNC",
  "rooms": [
    {
      "roomId": "uuid-123",
      "classId": "18248",
      "name": "SAT Math C1",
      "status": "RUNNING",
      "qrUrl": "data:image/png;base64,...",
      "expiresAt": 1716451260000,
      "lastUpdatedAt": 1716451200000
    }
  ]
}
```

```json
{
  "event": "QR_REFRESHED",
  "roomId": "uuid-123",
  "qrUrl": "data:image/png;base64,...",
  "expiresAt": 1716451320000,
  "qrTime": 60
}
```

```json
{
  "event": "AUTH_EXPIRED",
  "roomId": "uuid-123",
  "message": "ASP.NET Session expired. Please update the global cookie."
}
```

## Failure Handling
- Invalid or missing cookie:
  - room enters `AUTH_EXPIRED`
  - room keeps its identity and connections
  - operator can paste a replacement cookie
- Network failure:
  - room enters `WARNING`
  - current QR remains visible until it expires
  - backend retries with a short delay
- Stale QR risk:
  - the backend should prefer showing the last known QR over showing a blank panel
  - if the QR has clearly expired and refresh keeps failing, the UI must surface that it is no longer valid
- Late joiners:
  - immediately receive `FULL_STATE_SYNC`
  - never wait for the next refresh cycle to see current state

## Refresh Policy
- Use native chained `setTimeout` scheduling rather than overlapping intervals.
- Fetch at about 75% of the server-provided lifetime.
- Retry during the remaining window if a transient failure occurs.
- Do not swap the active QR until the new payload is validated.
- Send `expiresAt` as an absolute Unix timestamp in milliseconds.
- Let the frontend compute remaining time from `Date.now()`.

## Security Notes
- The cookie stays in backend memory only.
- The cookie is not logged and not persisted.
- The backend binds to `127.0.0.1` only.
- Websocket clients receive QR metadata and image payloads, not credentials.
- The backend is a proxy; the frontend never calls Warwick directly.

## Testing
- Unit tests for:
  - class ID validation
  - cookie validation
  - Warwick HTML/redirect detection
  - refresh threshold math
  - absolute expiry calculation
- Integration tests with a mocked Warwick endpoint for:
  - successful QR refresh and atomic swap
  - delayed refresh with warning state
  - 302 redirect detection
  - HTML login page detection
  - websocket full-state sync for late joiners
  - shared room start/stop propagation

## Proposed Implementation Boundary
Keep the first implementation focused:
- one Node.js process
- one HTTP server
- one websocket server
- in-memory room registry only
- no file storage
- no external database
- no automatic cookie recovery
