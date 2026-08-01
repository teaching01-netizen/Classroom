# Implementation plan: reduce Railway egress and preserve real-time QR updates

## Goal

Reduce public egress caused by repeatedly transmitting QR payloads while keeping:

* Fast QR startup
* Real-time room status
* Session check-in synchronization
* Backward-compatible API behavior during rollout

The logs show individual room responses around 62–63 KB, while room-list responses can grow into multiple megabytes.  

The root cause is that `RoomLite` still contains `qr_url`, and the WebSocket path sends complete room objects.

---

## Phase 1 — Emergency backend egress fix

### 1. Remove QR content from room-list responses

**File:** `internal/domain/room.go`

Replace the current `RoomLite` with metadata only:

```go
type RoomLite struct {
    RoomID    string     `json:"room_id"`
    ClassID   string     `json:"class_id"`
    Name      *string    `json:"name"`
    Status    RoomStatus `json:"status"`
    ExpiresAt *time.Time `json:"expires_at,omitempty"`
}
```

Remove:

```go
QRURL *string `json:"qr_url"`
```

`QRURL` is currently part of `RoomLite`, so every list operation copies the full QR representation for every room.

**File:** `internal/api/routes.go`

Update the `lite` mapping:

```go
liteRooms = append(liteRooms, domain.RoomLite{
    RoomID:    room.RoomID,
    ClassID:   room.ClassID,
    Name:      room.Name,
    Status:    room.Status,
    ExpiresAt: room.ExpiresAt,
})
```

The full QR remains available only through:

```text
GET /api/rooms/{roomID}
```

The existing handler currently inserts `QRURL` into every lite response.

### 2. Add one shared conversion function

Avoid independently constructing summaries in API and WebSocket code:

```go
func NewRoomLite(room Room) RoomLite {
    return RoomLite{
        RoomID:    room.RoomID,
        ClassID:   room.ClassID,
        Name:      room.Name,
        Status:    room.Status,
        ExpiresAt: room.ExpiresAt,
    }
}
```

Add:

```go
func RoomsToLite(rooms []Room) []RoomLite
```

Use this function everywhere a collection of rooms is returned.

### 3. Preserve compatibility

Do not change:

```text
GET /api/rooms/{roomID}
POST /api/rooms/from-session/start
```

The detail endpoint must continue returning `qr_url`, because `useSessionQr` obtains the displayed QR from the individual room query.

---

## Phase 2 — Stop QR payloads from entering WebSocket fan-out

### 1. Make `FullStateSync` metadata-only

**File:** `internal/api/websocket.go`

Current behavior:

```go
rooms = rm.GetAllRooms()
```

Change it to:

```go
rooms = domain.RoomsToLite(rm.GetAllRooms())
```

New WebSocket clients should not receive every persisted QR during connection setup. The current server sends the complete room collection immediately after connection.

### 2. Introduce a small WebSocket room event

Add a dedicated payload:

```go
type RoomChanged struct {
    RoomID    string     `json:"room_id"`
    ClassID   string     `json:"class_id"`
    Status    RoomStatus `json:"status"`
    ExpiresAt *time.Time `json:"expires_at,omitempty"`
    HasQR     bool       `json:"has_qr"`
    Revision  int64      `json:"revision"`
}
```

`Revision` can initially be based on `LastUpdatedAt.UnixNano()` if adding a database revision is too large for this release.

Emit:

```go
AppEvent{
    Type: "ROOM_CHANGED",
    Data: domain.RoomChanged{
        RoomID:    room.RoomID,
        ClassID:   room.ClassID,
        Status:    room.Status,
        ExpiresAt: room.ExpiresAt,
        HasQR:     room.QRURL != nil,
        Revision:  revision,
    },
}
```

Do not include `QRURL`.

### 3. Retain old event names temporarily

For one compatibility release:

* Keep `RoomCreated`, `RoomUpdated`, and `RoomDeleted`.
* Change their payloads to `RoomLite`, not full `Room`.
* Add `ROOM_CHANGED` for the new frontend.

After all clients are updated, remove the old event variants.

### 4. Ensure QR updates still wake detail polling

When a QR becomes available, send:

```json
{
  "ROOM_CHANGED": {
    "room_id": "18879",
    "status": "Running",
    "has_qr": true,
    "revision": 123
  }
}
```

The browser can then refetch only:

```text
GET /api/rooms/18879
```

This transfers one QR payload to the one client that needs it, rather than every QR to every connected client.

---

## Phase 3 — Frontend targeted cache updates

### 1. Separate summary and detail schemas

**File:** `web/src/features/rooms/api/room.schemas.ts`

Add:

```ts
export const roomSummarySchema = z.object({
  room_id: z.string().min(1),
  class_id: z.string().nullish(),
  name: z.string().nullish(),
  status: z.string(),
  expires_at: z.string().nullish(),
})

export const roomSummariesSchema = z.array(roomSummarySchema)

export const roomDetailSchema = roomSummarySchema.extend({
  qr_url: z.string().nullish(),
  warning_message: z.string().nullish(),
  error_message: z.string().nullish(),
})
```

Currently one schema is used for both room lists and QR-bearing detail responses.

### 2. Update query types

**File:** `web/src/features/rooms/api/room.queries.ts`

Use:

```ts
export function useRoomsQuery() {
  return useQuery({
    queryKey: roomKeys.all,
    queryFn: ({ signal }) =>
      apiClient.get(endpoints.rooms, {
        schema: roomSummariesSchema,
        signal,
      }),
  })
}
```

The existing endpoint already requests `?lite=true`; the backend needs to make that name truthful.

Keep `useRoomQuery` on `roomDetailSchema`.

### 3. Replace global invalidation

**File:** `web/src/shared/realtime/websocket-client.ts`

Remove this behavior for room updates:

```ts
queryClient.invalidateQueries({ queryKey: ['rooms'] })
```

The current client invalidates the complete room list for every room event.

Instead:

```ts
function applyRoomChanged(event: RoomChanged): void {
  queryClient.setQueryData<RoomSummary[]>(roomKeys.all, (rooms) => {
    if (rooms === undefined) {
      return rooms
    }

    return rooms.map((room) =>
      room.room_id === event.room_id
        ? {
            ...room,
            status: event.status,
            expires_at: event.expires_at,
          }
        : room,
    )
  })

  if (event.has_qr) {
    void queryClient.invalidateQueries({
      queryKey: roomKeys.detail(event.room_id),
      exact: true,
    })
  }
}
```

For creation and deletion:

* Creation: append one summary or invalidate the small summary list once.
* Deletion: remove the matching room locally.
* Status change: patch locally.
* QR availability: refetch only the specific detail query.

### 4. Stop polling after QR arrives

Retain the current behavior in `useRoomQuery`:

```ts
if (query.state.data?.qr_url) {
  return false
}
```

The detail query already stops polling when it receives a QR.

---

## Phase 4 — Remove stale QR payloads from storage and memory

Removing QR values from the list prevents new egress, but old QR strings remain in PostgreSQL and process memory.

### 1. Add repository cleanup

Add a repository method:

```go
type RoomRepository interface {
    // Existing methods...
    ClearExpiredRoomQRs(ctx context.Context, now time.Time) (int64, error)
}
```

Clear QR-related fields when:

* `expires_at < now`
* Room is `Stopped`
* Room is `Idle`
* Room has not been updated within the room-retention period

Conceptual update:

```sql
UPDATE rooms
SET
    qr_url = NULL,
    expires_at = NULL
WHERE
    qr_url IS NOT NULL
    AND (
        expires_at IS NULL
        OR expires_at < NOW()
        OR status IN ('Stopped', 'Idle')
    );
```

Verify actual table and column names before committing the migration.

### 2. Clear QR values when stopping a room

In `StopRoom`:

```go
state.room.QRURL = nil
state.room.ExpiresAt = nil
```

Persist the cleared values.

### 3. Clear expired QR values during startup

After `LoadRoomsFromDB`:

* Remove expired `QRURL` values from in-memory rooms.
* Persist cleanup asynchronously or through the repository cleanup method.

### 4. Add room retention

Delete old stopped rooms after a configurable period:

```env
ROOM_RETENTION=168h
```

Do not retain QR-room records indefinitely unless historical room records are a product requirement.

---

## Phase 5 — Add traffic safeguards

### 1. Response-size middleware

Extend the existing response recorder to count bytes:

```go
type responseStatusRecorder struct {
    http.ResponseWriter
    status int
    bytes  int64
}

func (w *responseStatusRecorder) Write(payload []byte) (int, error) {
    n, err := w.ResponseWriter.Write(payload)
    w.bytes += int64(n)
    return n, err
}
```

Record metrics using bounded route labels:

```text
http_response_bytes_total{route_class="rooms_list"}
http_response_bytes_total{route_class="room_detail"}
websocket_bytes_sent_total{event_class="room"}
```

Do not label metrics with room ID, course ID, URL, student ID, or session ID.

### 2. Add explicit response-size tests

Suggested limits:

| Response                             |       Test threshold |
| ------------------------------------ | -------------------: |
| One `RoomLite`                       |            `< 512 B` |
| 100-room lite list                   |            `< 64 KB` |
| `ROOM_CHANGED` WebSocket frame       |             `< 1 KB` |
| WebSocket `FullStateSync`, 100 rooms |            `< 64 KB` |
| One room detail with QR              | Allowed to be larger |

The test should use a QR string of at least 64 KB to prove it never leaks into summary paths.

### 3. Optional compression

Add HTTP compression for JSON, JavaScript, CSS, and metrics responses.

Do not treat compression as the main fix. Base64 QR content has already been encoded and may compress poorly.

---

## Phase 6 — Tests

### Backend unit tests

**`internal/domain/room_test.go`**

* `RoomLite` JSON does not contain `qr_url`.
* `RoomsToLite` does not copy QR values.
* Large QR input produces a small summary.

**`internal/api/rooms_test.go`**

* `GET /api/rooms?lite=true` omits `qr_url`.
* A list containing 100 rooms with 64 KB QR values remains below the threshold.
* `GET /api/rooms/{id}` still returns `qr_url`.

**`internal/api/websocket_test.go`**

* `FullStateSync` omits QR data.
* `ROOM_CHANGED` contains no QR string.
* A QR refresh sends a frame under 1 KB.
* Detail retrieval still works after the event.

**`internal/service/room_manager_test.go`**

* Stopping a room clears `QRURL` and `ExpiresAt`.
* Expired QR values are removed during recovery.
* Emitted room events use summaries.

### Frontend tests

**`room.schemas.test.ts`**

* Summary parses without `qr_url`.
* Detail parses with `qr_url`.
* Summary rejects or strips unintended large fields according to the selected Zod mode.

**`websocket-client.test.ts`**

* `ROOM_CHANGED` patches only the matching room.
* QR availability invalidates only `['rooms', roomId]`.
* It does not invalidate the complete `['rooms']` tree.
* Room deletion removes one cached item.
* Duplicate revisions are ignored.

**`room.queries.test.tsx`**

* List query uses the summary schema.
* Detail polling stops when QR arrives.
* A room event does not cause a complete list refetch.

---

## Phase 7 — Deployment order

### Release A: backend emergency fix

Deploy:

1. Remove `QRURL` from `RoomLite`.
2. Make WebSocket initial state metadata-only.
3. Make old room events metadata-only.
4. Clear QR values when rooms stop.
5. Add response-byte metrics.

This is the highest-value release. Even old frontend clients may continue invalidating the room list, but the list will be small.

### Release B: frontend targeting

Deploy:

1. Summary/detail schema split.
2. `ROOM_CHANGED` support.
3. Targeted detail invalidation.
4. Local room-list cache patching.
5. Remove whole-list invalidation.

### Release C: storage cleanup

Deploy:

1. Repository cleanup.
2. Existing stale-data cleanup.
3. Room retention.
4. Cleanup metrics.

### Release D: remove compatibility events

Once all active clients support `ROOM_CHANGED`:

* Remove full `RoomUpdated` compatibility behavior.
* Remove obsolete WebSocket branches.
* Keep only metadata events.

---

## Phase 8 — Validation gates

Before release, capture baseline values:

```text
GET /api/rooms?lite=true response bytes
GET /api/rooms/{id} response bytes
WebSocket FullStateSync bytes
Room events per minute
Room-list requests per minute
Railway public egress per hour
```

Release succeeds when:

* `/api/rooms?lite=true` stays below **64 KB** under production-shaped room counts.
* No WebSocket room event contains `qr_url`.
* One QR refresh produces no complete room-list download.
* Browser network tools show only one detail request for the active room.
* Railway egress falls by at least **90%** relative to the spike window.
* QR startup and refresh continue working.
* Check-in synchronization remains unaffected.

---
