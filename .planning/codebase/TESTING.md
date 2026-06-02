# Testing Patterns

**Analysis Date:** 2026-06-02

## Test Framework

**Backend:**
- Runner: Go standard `testing` package
- Assertions: `github.com/stretchr/testify` v1.11.1 (`assert`, `require`)
- HTTP mocking: `net/http/httptest` (in-process test servers)

**Frontend:**
- Runner: Vitest 1.1.0
- Assertions: `@testing-library/jest-dom` v6.9.1
- DOM: jsdom 29.1.1
- Component testing: `@testing-library/react` v16.3.2

**Run Commands:**
```bash
# Backend tests
go test ./internal/...                    # Run all backend tests
go test ./internal/warwick/... -v         # Warwick package tests with verbose output
go test ./internal/cache/... -v           # Cache tests
go test ./internal/db/... -v              # DB repository tests (requires running PG)

# Frontend tests
cd web && npm test                        # Run all Vitest tests
cd web && npm run test:coverage           # Coverage report
```

## Test File Organization

**Location:**
- Backend: Co-located with source files (`*_test.go` in same package)
- Frontend: Separate `__tests__/` directory under `web/src/`

**Backend Naming:**
```
internal/
├── warwick/
│   ├── client.go                     → client_test.go
│   ├── classroom_client.go           → classroom_client_db_test.go
│   ├── report_client.go              → report_client_test.go
│   └── session_pool.go               → session_pool_test.go
├── cache/
│   └── cache.go                      → cache_test.go
├── db/
│   └── session_checkin_repository.go → session_checkin_repository_test.go
├── domain/
│   └── room.go                       → room_test.go
└── middleware/
    └── ratelimit.go                  → ratelimit_test.go
```

**Frontend Naming:**
```
web/src/__tests__/
├── websocket-handling.test.js
├── useCheckins.test.js
├── ErrorBoundary.test.jsx
├── usePolling.test.js
├── useFocusRefetch.test.js
└── state-update.test.js
```

## Test Structure

**Backend Test Pattern:**
```go
func TestClassName_MethodName_Scenario(t *testing.T) {
    t.Helper()
    // Setup: create mock servers, caches, repos
    // Execute: call the method under test
    // Assert: use require for critical checks, assert for soft checks
    // Cleanup: t.Cleanup() for server shutdown
}
```

Example from `classroom_client_db_test.go`:
```go
func TestClassroomClient_GetSessionDetail_WithDBRepo_CachesAfterFirstFetch(t *testing.T) {
    mc := cache.New()
    repo := &mockCheckinRepo{
        students:     make(map[string][]domain.StudentCheckin),
        maxToggledAt: make(map[string]*time.Time),
    }
    apiCalls := 0
    apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        apiCalls++
        w.Header().Set("Content-Type", "application/json")
        w.Write(makeSessionDetailResponse([]StudentCheckInRow{...}))
    }))
    t.Cleanup(apiServer.Close)

    loginServer := newTestLoginServer(t)
    pool, err := NewSessionPool("test@test.com", "pass", loginServer.URL, 1, 1, 1)
    require.NoError(t, err)

    client := NewClassroomClientFromPool(pool, TierTeacher, mc, repo)
    client.baseURL = apiServer.URL

    detail1, err := client.GetSessionDetail("c1", "session1")
    require.NoError(t, err)
    assert.Equal(t, 2, detail1.TotalStudents)

    // Second call should hit cache
    detail2, err := client.GetSessionDetail("c1", "session1")
    require.NoError(t, err)
    assert.Equal(t, 1, apiCalls, "should fetch from Warwick only once")
}
```

**Frontend Test Pattern:**
```javascript
import { renderHook, waitFor } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { useHookName } from '../hooks/useHookName';

describe('useHookName', () => {
  it('should fetch data on mount', async () => {
    global.fetch = vi.fn().mockResolvedValue({
      json: () => Promise.resolve({ success: true, data: {...} }),
    });
    const { result } = renderHook(() => useHookName());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.data).toBeDefined();
  });
});
```

## Mocking

**Backend Mocking Pattern:**
- HTTP servers: `httptest.NewServer` with custom handlers
- DB repositories: Manual mock structs implementing interfaces (e.g., `mockCheckinRepo`)
- Session pools: Real `SessionPool` with mock login servers
- No mocking framework — hand-written mocks following interface contracts

Example mock from `classroom_client_db_test.go`:
```go
type mockCheckinRepo struct {
    mu           sync.Mutex
    students     map[string][]domain.StudentCheckin
    toggledAt    map[string]map[string]time.Time
    maxToggledAt map[string]*time.Time
}

func (m *mockCheckinRepo) GetStudentsBySession(ctx context.Context, sessionID string) ([]domain.StudentCheckin, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    s := m.students[sessionID]
    if s == nil {
        return []domain.StudentCheckin{}, nil
    }
    result := make([]domain.StudentCheckin, len(s))
    copy(result, s)
    return result, nil
}
```

**Frontend Mocking Pattern:**
- `global.fetch = vi.fn()` for API mocking
- No MSW or other mocking service detected

## Fixtures and Factories

**Test Data Helpers:**
```go
// From classroom_client_db_test.go
func makeSessionDetailResponse(students []StudentCheckInRow) []byte {
    resp := StudentCheckInSearchResponse{
        Draw:            1,
        RecordsTotal:    len(students),
        RecordsFiltered: len(students),
        Data:            students,
    }
    b, _ := json.Marshal(resp)
    return b
}

func newTestLoginServer(t *testing.T) *httptest.Server {
    t.Helper()
    s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Add("Set-Cookie", "ASP.NET_SessionId=testcookie; path=/; HttpOnly")
        w.WriteHeader(http.StatusFound)
    }))
    t.Cleanup(s.Close)
    return s
}
```

**Location:**
- Inline in test files (no separate fixture directories)
- Helper functions prefixed with `make` or `new` and use `t.Helper()`

## Coverage

**Requirements:** None enforced (no CI pipeline detected)

**View Coverage:**
```bash
# Backend
go test ./internal/... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Frontend
cd web && npm run test:coverage
```

## Test Types

**Unit Tests:**
- Scope: Individual functions and methods
- Examples: Cache get/set, rate limiter allow/deny, domain status transitions, DataTables encoding
- Files: `internal/cache/cache_test.go`, `internal/middleware/ratelimit_test.go`, `internal/domain/room_test.go`

**Integration Tests:**
- Scope: Multi-component interactions with mock external dependencies
- Examples: ClassroomClient with mock Warwick servers + mock DB repos, session pool acquire/release cycles
- Files: `internal/warwick/classroom_client_db_test.go`, `internal/warwick/session_pool_test.go`, `internal/warwick/report_client_test.go`
- Pattern: Real pgxpool with test PostgreSQL (or mock repo) + httptest servers

**E2E Tests:**
- Not used — no browser-based testing (Playwright, Cypress, etc.)

## Common Patterns

**Async Testing (Go):**
```go
// Room worker tests use context cancellation to stop long-running goroutines
ctx, cancel := context.WithCancel(context.Background())
go rm.runRoomWorker(state)
// ... test assertions ...
cancel() // Stop the worker goroutine
```

**Error Testing (Go):**
```go
// Verify error type mapping
err := client.ToggleCheckin("c1", "s1", "S1", true)
require.Error(t, err)
var fe *domain.FetchError
assert.True(t, errors.As(err, &fe))
assert.Equal(t, domain.ErrKindAuthExpired, fe.Kind)
```

**Frontend Async Testing:**
```javascript
// Polling hook tests use fake timers
vi.useFakeTimers();
const { result } = renderHook(() => usePolling(fetchFn, 1000, true));
await vi.advanceTimersByTimeAsync(1000);
await waitFor(() => expect(fetchFn).toHaveBeenCalledTimes(2));
vi.useRealTimers();
```

## Notable Test Files

- `internal/warwick/classroom_client_db_test.go` (559 lines) — Most thorough test file; covers DB-backed cache, stale cache detection, course name population, toggle-to-DB persistence
- `internal/warwick/report_client_test.go` — Attendance report computation with bounded concurrency
- `internal/warwick/session_pool_test.go` — Pool acquire/release, timeout, tier isolation
- `web/src/__tests__/websocket-handling.test.js` — WebSocket reconnect and message handling

---

*Testing analysis: 2026-06-02*
