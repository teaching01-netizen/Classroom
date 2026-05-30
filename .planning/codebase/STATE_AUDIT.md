# Frontend State Management Audit

**Analysis Date:** Fri May 29 2026

**Stack:** React 18.2.0 + Zustand 4.5.0 + Vite 5.0.8
**Scope:** `web/src/store/` (4 stores), `web/src/hooks/` (7 hooks), `web/src/App.jsx`

---

## 1. Zustand Store Race Conditions: `useRoomStore.updateRoom()`

### Severity: **HIGH**

### Failure Sequence

Two WebSocket messages for different properties of the **same room** arrive in rapid succession (separate macrotasks):

```
Msg A: {RoomUpdated: {room_id: 1, status: "active"}}
Msg B: {RoomUpdated: {room_id: 1, enrolled_count: 150}}
```

Each calls `useRoomStore.getState().updateRoom(...)`. The updater function **replaces the entire room object** rather than merging:

```js
// web/src/store/useRoomStore.js:9-17
updateRoom: (updatedRoom) => {
    set((state) => ({
      rooms: state.rooms.map((room) => {
        const currentId = String(room.room_id);
        const updatedId = String(updatedRoom.room_id);
        return currentId === updatedId ? updatedRoom : room;  // <-- FULL REPLACE
      }),
    }));
  },
```

After Msg A: `rooms = [{room_id: 1, status: "active"}]`
After Msg B: `rooms = [{room_id: 1, enrolled_count: 150}]` — **`status` lost**

### Code Evidence

`web/src/store/useRoomStore.js:9-17` — `updatedRoom` replaces the matched room wholesale. If the server sends partial room objects per event type, properties from the first message are silently dropped.

### Fix

```js
// Merge instead of replace
updateRoom: (updatedRoom) => set((state) => ({
  rooms: state.rooms.map((room) => {
    const currentId = String(room.room_id);
    const updatedId = String(updatedRoom.room_id);
    return currentId === updatedId ? { ...room, ...updatedRoom } : room;
  }),
})),
```

This preserves properties from the first update when the second arrives.

### Scope

This affects every route that renders rooms (CourseDashboard, SessionList). Mitigation: if the server always sends full room objects, the bug is dormant. **Verify server behavior.** If the server sends partial updates, this is actively losing data.

---

## 2. localStorage Cross-Tab Sync

### Severity: **LOW** (existing impl works, but no other store has it)

### Analysis

`usePinnedCoursesStore.js` has correct cross-tab sync via the `storage` event listener:

```js
// web/src/store/usePinnedCoursesStore.js:75-89
if (typeof window !== 'undefined') {
  window.addEventListener('storage', (event) => {
    if (event.key === 'warwick-pinned-courses' && event.newValue) {
      try {
        const parsed = JSON.parse(event.newValue);
        if (parsed.state && Array.isArray(parsed.state.pinnedCourseIds)) {
          usePinnedCoursesStore.setState({
            pinnedCourseIds: parsed.state.pinnedCourseIds,
          });
        }
      } catch { /* ignore */ }
    }
  });
}
```

**Tab A** pins a course → Zustand `persist` middleware writes to localStorage → browser fires `storage` event in **Tab B** → Tab B calls `setState()` → Tab B re-renders via Zustand subscription.

This works as expected. Tab B will see the updated list.

### Gap: No other store has cross-tab sync

`useCourseStore`, `useRoomStore`, `useSessionStore` are all in-memory only. If Tab A and Tab B are on the same URL:
- Data fetched in Tab A does NOT propagate to Tab B
- WS events may populate both tabs independently
- But polling + focus refetch creates eventual consistency

### Fix

For stores backed by server data, cross-tab sync is less critical because WS broadcasts or short polling intervals (5s) provide eventual consistency. The current design is acceptable if the server broadcasts all state changes via WebSocket.

---

## 3. useEffect Cleanup: Stale Fetch Completion

### Severity: **MEDIUM**

### Issue A: `useCourses.js` — No abort mechanism

```js
// web/src/hooks/useCourses.js:28-30
useEffect(() => {
    fetchCourses();
}, [fetchCourses]);
```

`fetchCourses` starts an HTTP fetch but provides **no AbortController**. If the component unmounts while the fetch is in flight:

1. Component unmounts (user navigates away from `/`)
2. Fetch completes
3. `setCourses(result.data.courses)` writes to Zustand store
4. **Stale data overwrites potentially fresher data** if the user later returns

No React warning (Zustand stores aren't React state), but the race leads to **stale data display**.

### Issue B: `useSessions.js` — Same problem

```js
// web/src/hooks/useSessions.js:37-39
useEffect(() => {
    if (!courseId) return;
    fetchSessions();
}, [courseId, fetchSessions, reset]);
```

No abort controller. Same stale-data-after-navigation risk.

### Issue C: `useCheckins.js` — Correctly handled

```js
// web/src/hooks/useCheckins.js:71-74
abortRef.current = new AbortController();
setInitialLoading();
fetchStudents(abortRef.current.signal);
return () => abortRef.current?.abort();
```

This IS properly abort-protected. When component unmounts, the fetch is cancelled and the catch handler silently ignores `AbortError`.

**Consistency gap**: Only `useCheckins` handles cleanup. `useCourses` and `useSessions` do not.

### Fix

Add AbortController to `useCourses.js` and `useSessions.js` following the `useCheckins.js` pattern:

```js
const abortRef = useRef(null);

useEffect(() => {
  abortRef.current = new AbortController();
  fetchCourses(abortRef.current.signal);
  return () => abortRef.current?.abort();
}, [fetchCourses]);
```

And modify `fetchCourses` / `fetchSessions` to accept and pass the signal to `fetch()`.

---

## 4. WebSocket Message Ordering + Zustand Batching

### Severity: **LOW**

### Analysis

WS `onmessage` callbacks execute as **separate macrotasks** in the JS event loop. There is no interleaving — each message is fully processed before the next begins.

```js
// web/src/hooks/useWebSocket.js:28-49
wsRef.current.onmessage = (event) => {
    // ... processes exactly ONE event type per message
    if (data.FullStateSync !== undefined) { ... }
    else if (data.RoomCreated !== undefined) { ... }
    else if (data.RoomUpdated !== undefined) { ... }
    // ...
};
```

Each `set()` call in Zustand synchronously notifies subscribers. But since only one store mutation happens per message, there's no batching concern within a single callback.

### Edge case: React 18 automatic batching

Zustand stores are **external stores** — they don't use React's `setState`. React 18's automatic batching applies to React state updates, not to Zustand `set()`. Zustand v4 uses `useSyncExternalStore` which triggers synchronous re-renders.

**Practical impact**: If two WS events for the same room arrive in sequence (macrotask A, macrotask B), each triggers a full React re-render. This is fine for correctness but means two renders instead of one. No data loss.

### Verdict

No ordering bug. The only concern (Issue #1) is the replace-vs-merge in `updateRoom`, not ordering.

---

## 5. Memory Leak: WS Subscriptions

### Severity: **MEDIUM**

### Issue A: WebSocket lifecycle tied to `HomePage`

```js
// web/src/App.jsx:139
function HomePage() {
  useWebSocket();
  // ...
```

The WebSocket connection lives ONLY while `HomePage` is mounted. If a user navigates to `/courses` directly (bookmark, URL bar), `useWebSocket()` is **never called** → no WS connection → no room updates, no reconnection refetches.

### Issue B: Cleanup IS correct

```js
// web/src/hooks/useWebSocket.js:65-69
return () => {
    if (wsRef.current) {
        wsRef.current.close();
    }
};
```

When `HomePage` unmounts, the WS connection closes. The `onclose` handler fires, but since `reconnectAttempts` resets on unmount, no reconnect loop happens (the `connect` closure captures the component's render, but the effect cleanup prevents it from running).

Actually, there IS a subtle leak: **if the WS closes during reconnection**, the `onclose` callback schedules a `setTimeout(connect, 3000)`. If the component unmounts during that 3-second window, the `setTimeout` still fires, calling `connect()` which creates a new WebSocket on a stale reference. The stale WS would receive messages but never be cleaned up.

```js
// web/src/hooks/useWebSocket.js:52-58
wsRef.current.onclose = (event) => {
    useRoomStore.getState().setIsWsConnected(false);
    reconnectAttempts.current += 1;
    if (reconnectAttempts.current <= MAX_RECONNECT) {
        setTimeout(connect, 3000);  // <-- survives unmount
    }
};
```

### Fix options

1. **Hoist WS to App level**: Move `useWebSocket()` to the root `<App>` component so it lives for the entire SPA lifetime, not just `HomePage`. This also fixes the "no WS on direct nav" issue.

2. **Guard reconnect**: Track a `mountedRef` and check it before calling `connect`:

```js
const mountedRef = useRef(true);
useEffect(() => {
  mountedRef.current = true;
  return () => { mountedRef.current = false; };
}, []);

// In onclose:
if (mountedRef.current && reconnectAttempts.current <= MAX_RECONNECT) {
  setTimeout(connect, 3000);
}
```

---

## 6. State Initialization Race

### Severity: **LOW**

### Analysis

Timeline on first load:

```
T0: App mounts, HomePage renders
T0: useCourses() → useEffect → fetchCourses() starts (HTTP GET /api/teacher/courses)
T0: useWebSocket() → WS connection starts
T1: WS connects → server sends FullStateSync → rooms populated
T2: fetchCourses() resolves → courses populated
```

**What the user sees between T0–T2:**

```
Loading courses...
```

(App.jsx:163-167). The loading skeleton covers this window. No flash-of-empty-content.

**Mid-render FullStateSync:** Impossible. WS `onmessage` is a separate macrotask that cannot interleave with React's synchronous rendering cycle. React 18 renders are atomic within one task.

**What if `fetchCourses()` resolves before `FullStateSync`?** (T2 < T1)

```
T2: courses populated, isLoading=false → UI renders pinned courses, rooms list
T1: FullStateSync arrives → rooms populated → UI re-renders with rooms
```

The user briefly sees an empty room list, then it fills in. Acceptable but suboptimal UX.

### Verdict

No dangerous race. The loading state prevents incorrect data display. However, the brief empty-state flash could be eliminated by coordinating the two data sources.

### Fix (optional)

Consider a combined loading state: wait for both courses and FullStateSync before removing the loading indicator. In practice, the 5-second polling covers this quickly enough.

---

## 7. Optimistic Updates vs Server State (Multi-Tab)

### Severity: **HIGH** (multi-user scenario)

### Code Evidence

`toggleCheckin` in `useCheckins.js` is **NOT** optimistic — it waits for the server response:

```js
// web/src/hooks/useCheckins.js:45-59
const toggleCheckin = async (studentId, checked) => {
    try {
      const response = await fetch(`/api/teacher/.../toggle-checkin`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ student_id: studentId, checked }),
      });
      const result = await response.json();
      if (result.success) {
        updateStudentCheckin(studentId, checked);  // <-- only after server confirms
      }
    } catch (err) {
      console.error('Failed to toggle checkin:', err);
    }
  };
```

### Multi-Tab Failure

**Scenario:**

1. Tab A opens session S, sees Student X as `checked_in: false`
2. Tab B opens same session S, sees Student X as `checked_in: false`
3. Tab A: teacher clicks check-in → POST succeeds → Tab A updates store → **Tab A shows checked_in: true**
4. Server does NOT broadcast `CHECKIN_UPDATED` via WebSocket to other tabs
5. Tab B: **still shows `checked_in: false`** → teacher sees stale state
6. If Tab B's teacher tries to check in Student X again, the server may reject (already checked in) or double-check-in
7. Worst case: Tab B shows a student as absent who has already checked in, and the teacher marks them absent based on stale data

**Polling mitigates, but slowly:**

```js
// web/src/hooks/useCheckins.js:79
usePolling(fetchStudentsNoAbort, POLL_INTERVAL_MS, isActive);  // 5000ms
```

Tab B picks up the change at most 5 seconds later. But in a fast-moving check-in flow, 5 seconds is enough for a bad decision.

### Root Cause

The WS handler in `useWebSocket.js:42-43` processes `CHECKIN_UPDATED` events:

```js
} else if (data.CHECKIN_UPDATED !== undefined) {
    sessionActions.updateStudentCheckin(data.CHECKIN_UPDATED.student_id, data.CHECKIN_UPDATED.checked_in);
```

But the server either doesn't send them for check-in updates, or the format doesn't match. **If the server does broadcast them, this should work.** The issue is either server-side (not broadcasting) or a data format mismatch.

### Fix

**If server doesn't broadcast:** Implement client-side polling with a shorter interval (2s) or fix the server to emit `CHECKIN_UPDATED` WS events on check-in toggle.

**If server does broadcast but format mismatches:** Add debug logging to `onmessage` to capture all event types:

```js
wsRef.current.onmessage = (event) => {
    const data = JSON.parse(event.data);
    console.debug('[WS]', Object.keys(data));  // debug
    // ... existing handler
};
```

**Best fix:** Make the server emit a `RoomUpdated` event for the session's room whenever a check-in changes. This leverages the existing WS infrastructure and triggers cross-tab sync naturally.

---

## Summary

| # | Issue | Severity | File(s) | Root Cause |
|---|-------|----------|---------|------------|
| 1 | `updateRoom` replace vs merge | **HIGH** | `useRoomStore.js:9-17` | Full object replace drops concurrent updates |
| 2 | Cross-tab sync gap | LOW | `usePinnedCoursesStore.js:75-89` | Other stores not synced (acceptable if WS covers it) |
| 3 | Stale fetch on unmount | **MEDIUM** | `useCourses.js:28-30`, `useSessions.js:37-39` | Missing AbortController |
| 4 | WS ordering | LOW | `useWebSocket.js:28-49` | No bug (separate macrotasks) |
| 5 | WS zombie reconnect | **MEDIUM** | `useWebSocket.js:52-58` | `setTimeout` survives unmount; WS tied to HomePage |
| 6 | Init race | LOW | App.jsx, `useCourses.js` | Loading skeleton covers the window |
| 7 | Multi-tab check-in staleness | **HIGH** | `useCheckins.js:45-59`, `useWebSocket.js:42-43` | Server doesn't broadcast CHECKIN_UPDATED or format mismatch |

**Priority order for fixes:**
1. `updateRoom` merge (HIGH, one-liner, prevents data corruption)
2. WS reconnect guard + hoist WS to App level (MEDIUM, prevents zombie connections and fixes no-WS-on-direct-nav)
3. Add AbortController to `useCourses`/`useSessions` (MEDIUM, consistency with `useCheckins`)
4. Investigate CHECKIN_UPDATED WS broadcast (HIGH if multi-user; depends on server behavior)
5. Cross-tab sync for other stores (LOW, polling covers it)
