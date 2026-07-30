# Frontend Architecture Reference

> **Project:** Check-in QR Command Center — B2B SaaS for classroom QR check-in management  
> **Framework:** React 18 + Vite 8 + Zustand 4 + React Router 7  
> **Last updated:** 2026-07-30

---

## Table of Contents

1. [Tech Stack & Build System](#1-tech-stack--build-system)
2. [Frontend File Structure](#2-frontend-file-structure)
3. [Routing & Page Architecture](#3-routing--page-architecture)
4. [State Management (Zustand Stores)](#4-state-management-zustand-stores)
5. [Data Fetching & Hooks Layer](#5-data-fetching--hooks-layer)
6. [Real-Time Updates (WebSocket & Snapshots)](#6-real-time-updates-websocket--snapshots)
7. [Component Library](#7-component-library)
8. [Design System & Tokens](#8-design-system--tokens)
9. [UX Design Direction](#9-ux-design-direction)
10. [Error Handling, Loading & Empty States](#10-error-handling-loading--empty-states)
11. [Accessibility & Interaction Patterns](#11-accessibility--interaction-patterns)
12. [Known Gaps & Limitations](#12-known-gaps--limitations)

---

## 1. Tech Stack & Build System

### Dependency Footprint (Intentional Minimalism)
The frontend has only **4 runtime dependencies** — an unusually small footprint chosen deliberately to avoid framework churn:

| Dependency | Version | Purpose |
|---|---|---|
| `react` | ^18.2.0 | UI library |
| `react-dom` | ^18.2.0 | DOM renderer |
| `react-router-dom` | ^7.18.1 | Client-side routing (latest v7) |
| `zustand` | ^4.5.0 | State management (lightweight, no boilerplate) |

**No** UI component library (no MUI, Chakra, shadcn/ui, Radix, etc.).  
**No** CSS framework (no Tailwind, PostCSS, CSS-in-JS library).  
**No** HTTP client (uses native `fetch` only).  
**No** data fetching framework (no React Query, SWR, Apollo).

### Build Toolchain

| Tool | Version | Purpose |
|---|---|---|
| `vite` | ^8.1.5 | Bundler & dev server |
| `@vitejs/plugin-react` | ^6.0.4 | Fast Refresh, JSX transform |
| `vitest` | ^4.1.10 | Unit + integration tests |
| `@testing-library/react` | ^16.3.2 | Component tests |
| `jsdom` | ^29.1.1 | DOM environment for tests |
| `eslint` | ^10.8.0 | Linting |

### Dev Server Configuration (`vite.config.js`)
- Dev server runs on **port 5175**
- Proxies `/api/*` → `http://127.0.0.1:3001` (Go backend)
- Proxies `/ws` → `ws://127.0.0.1:3001` (WebSocket)
- TypeScript-only type-checking via `tsconfig.json` (no TS compilation — Vite handles JSX natively)

### Scripts
```bash
npm run dev      # Dev server with HMR
npm run build    # Production build → web/dist/
npm run preview  # Preview production build
npm run test     # Vitest
npm run lint     # ESLint (strict: --max-warnings 0)
```

---

## 2. Frontend File Structure

```
web/
├── index.html                          # HTML entry point
├── package.json                        # Dependencies & scripts
├── vite.config.js                      # Vite config (proxy, plugins)
├── tsconfig.json                       # TypeScript config (JSX type-checking)
├── eslint.config.cjs                   # ESLint flat config
├── .env                                # Env variables (shared with Go backend)
│
├── src/
│   ├── main.jsx                        # App bootstrap (ReactDOM.createRoot)
│   ├── App.jsx                         # Root component: router + layout shell
│   ├── index.css                       # CSS reset (box-sizing, body base)
│   │
│   ├── styles/
│   │   └── tokens.css                  # Design tokens (CSS custom properties)
│   │
│   ├── api/
│   │   └── fetchFresh.js              # fetch wrapper (cache: no-store)
│   │
│   ├── pages/                          # Page-level components (routed)
│   │   ├── CourseDashboard.jsx         # Landing: course grid + filters
│   │   ├── SessionList.jsx             # Per-course session table
│   │   ├── CheckinDetail.jsx           # Per-session student check-in UI
│   │   ├── CourseAttendance.jsx        # Attendance report (matrix view)
│   │   └── AbsenceDashboard.jsx        # Cross-course absence alerts
│   │
│   ├── components/                     # Reusable presentational components
│   │   ├── dashboard/                  # Absence dashboard sub-components
│   │   │   ├── AbsenceList.jsx
│   │   │   ├── AbsenceMatrix.jsx
│   │   │   ├── AtRiskCallout.jsx
│   │   │   ├── DashboardCourseCard.jsx
│   │   │   ├── DashboardKPIRow.jsx
│   │   │   └── FilterBar.jsx
│   │   ├── AttendanceRow.jsx
│   │   ├── AttendanceTable.jsx
│   │   ├── BackBreadcrumb.jsx
│   │   ├── CourseCard.jsx
│   │   ├── ErrorBoundary.jsx
│   │   ├── Pagination.jsx
│   │   ├── QRDisplay.jsx               # Full-screen QR display
│   │   ├── QRModal.jsx                 # QR overlay modal
│   │   ├── RoomCard.jsx                # QR room management card
│   │   ├── SessionRow.jsx
│   │   ├── SessionTable.jsx
│   │   ├── StatsBar.jsx
│   │   ├── StatusBadge.jsx
│   │   ├── StudentRow.jsx
│   │   └── StudentTable.jsx
│   │
│   ├── hooks/                          # Custom React hooks
│   │   ├── useAbsenceDashboard.js
│   │   ├── useBatchAttendance.js
│   │   ├── useCheckins.js
│   │   ├── useCountdown.js
│   │   ├── useCourseAttendance.js
│   │   ├── useCourses.js
│   │   ├── useDashboardViews.js
│   │   ├── useFocusRefetch.js
│   │   ├── usePolling.js
│   │   ├── useSessions.js
│   │   ├── useSnapshotEvents.js
│   │   └── useWebSocket.js
│   │
│   └── store/                          # Zustand state stores
│       ├── useCourseStore.js
│       ├── useDashboardFiltersStore.js
│       ├── usePinnedCoursesStore.js
│       ├── useRoomStore.js
│       └── useSessionStore.js
│
└── __tests__/                          # Test files (20+ tests)
└── tests/                              # Additional test directory
```

### File Naming Conventions
- **Hooks:** `use*` prefix, camelCase, `.js` extension (e.g., `useCourses.js`)
- **Components:** PascalCase, `.jsx` extension (e.g., `CourseCard.jsx`)
- **Stores:** `use*Store` prefix (e.g., `useCourseStore.js`)

---

## 3. Routing & Page Architecture

### Route Map
All routes live in `App.jsx` inside `<BrowserRouter>`:

```
GET  /                                           → HomePage (pinned course grid)
GET  /courses                                    → CourseDashboard (all courses)
GET  /courses/:courseId/sessions                 → SessionList (per-course)
GET  /courses/:courseId/sessions/:sessionId       → CheckinDetail (per-session)
GET  /courses/:courseId/attendance               → CourseAttendance (report)
GET  /absence-dashboard                          → AbsenceDashboard
```

### Navigation User Flow
```
HomePage (pinned courses)
  │
  ├─ Click "All Courses" → CourseDashboard
  │                           │
  │                           └─ Click course card → SessionList
  │                                                     │
  │                                                     ├─ Click session row → CheckinDetail
  │                                                     └─ Click "View Attendance Report" → CourseAttendance
  │
  └─ Click "Absence Dashboard" → AbsenceDashboard
```

### Page Descriptions

| Page | Route | Purpose | Key Data |
|---|---|---|---|
| **HomePage** | `/` | Pinned courses overview with live attendance | `useCourses` → pinned subset, `useBatchAttendance` |
| **CourseDashboard** | `/courses` | Browse all courses, search, filter by status | `useCourses` (full list) |
| **SessionList** | `/courses/:courseId/sessions` | Sessions for one course | `useSessions(courseId)` |
| **CheckinDetail** | `/courses/:courseId/sessions/:sessionId` | Check-in management: toggle, QR, search/filter students | `useCheckins(courseId, sessionId)` |
| **CourseAttendance** | `/courses/:courseId/attendance` | Attendance matrix (students × sessions) with at-risk detection | `useCourseAttendance(courseId)` |
| **AbsenceDashboard** | `/absence-dashboard` | Cross-course absence alerts with saved views | `useAbsenceDashboard`, `useDashboardViews` |

### App Shell Layout
The root `App.jsx` provides a basic shell:
```
┌────────────────────────────────────────────────────────────────────┐
│ TOP BAR: Brand | Dashboard · Absence alerts · All courses | Status │
├──────────────────────────────────┤
│ <Routes> (ErrorBoundary-wrapped)                                  │
└──────────────────────────────────┘
```

The production shell uses a fixed top navigation bar rather than a side navigation rail. It holds the branded home link, all three primary destinations, density control, and live connection status. On mobile it uses an explicit brand-and-utility row followed by a three-column navigation row, without clipped labels or horizontal scrolling.

---

## 4. State Management (Zustand Stores)

### Store Inventory

#### `useCourseStore.js`
```typescript
interface CourseStore {
  courses: CourseSummary[];
  isInitialLoading: boolean;    // true on first fetch
  isRefreshing: boolean;        // true on subsequent fetches
  error: string | null;

  setCourses(courses): void;    // Clears loading + error
  setInitialLoading(): void;
  setRefreshing(): void;
  setError(error): void;
  clearError(): void;
}
```

#### `useSessionStore.js`
Shared between `SessionList` and `CheckinDetail` pages (same store, different fields populated per page):
```typescript
interface SessionStore {
  sessions: SessionSummary[];     // Used by SessionList
  courseName: string;             // Set by SessionList, read by CheckinDetail
  currentSession: SessionDetail;  // Used by CheckinDetail
  students: StudentCheckin[];     // Used by CheckinDetail
  isInitialLoading: boolean;
  isRefreshing: boolean;
  error: string | null;

  setSessions(sessions): void;
  setCourseName(name): void;
  setCurrentSession(session): void;
  setStudents(students): void;
  updateStudentCheckin(studentId, checkedIn): void;  // Optimistic update
  updateSessionStats(stats): void;                     // WebSocket-driven
  reset(): void;                                       // On course/session change
}
```

#### `useRoomStore.js`
Manages QR rooms (legacy feature, still active):
```typescript
interface RoomStore {
  rooms: Room[];
  isWsConnected: boolean;
  lastReconnectAt: number | null;

  setRooms(rooms): void;
  addRoom(room): void;
  updateRoom(room): void;
  removeRoom(roomId): void;
  setIsWsConnected(connected): void;
  signalReconnect(): void;
}
```

#### `usePinnedCoursesStore.js`
Favourite/pinned courses with optimistic UI:
```typescript
interface PinnedCoursesStore {
  pinnedCourseIds: string[];
  isLoading: boolean;

  loadFavourites(): Promise<void>;
  pinCourse(courseId): Promise<void>;
  unpinCourse(courseId): Promise<void>;
  toggleCourse(courseId): Promise<void>;
}
```

#### `useDashboardFiltersStore.js`
Filter state for the Absence Dashboard, with saved-view support:
```typescript
interface DashboardFiltersStore {
  filters: {
    courseIds: string[];
    dateRange: { from: string; to: string } | null;
    threshold: number;
    sortBy: 'risk' | 'rate-asc' | 'rate-desc' | 'name';
    wCodes: string[];
  };

  setFilters(newFilters): void;
  loadView(view): void;
  resetFilters(): void;
  getFilterString(): string;  // JSON.stringify for comparison
}
// Selectors:
// selectHasActiveFilters(state) → boolean
// selectFilterSummary(state) → string
```

### State Flow Pattern

```
Page Component
  │
  ├── Calls custom hook (e.g., useCourses)
  │     │
  │     ├── Reads store state (courses, isLoading, error)
  │     ├── Calls store actions on fetch success/error
  │     └── Wires background refetch triggers
  │
  └── Passes store data down to presentational components via props
```

**Key architectural choice:** Hooks read from and write to stores. Presentational components receive data via props. This creates a clean separation: pages orchestrate, components render.

---

## 5. Data Fetching & Hooks Layer

### Core Fetch Wrapper
```javascript
// api/fetchFresh.js
export function fetchFresh(input, init = {}) {
  return fetch(input, { ...init, cache: 'no-store' });
}
```
Simple wrapper ensuring every API request bypasses the browser cache. No retry, no timeout, no serialization — these are handled per-hook.

### Hook Architecture
Every data-fetching hook follows a consistent pattern:

```
use* (e.g., useCourses)
  │
  ├── Reads/writes to a Zustand store (shared state)
  ├── Fetches data via fetchFresh
  ├── Calls store actions on success/error
  ├── Calls useFocusRefetch(silentFetch)  → refetch on tab focus
  ├── Calls useWsReconnect(silentFetch)    → refetch on WebSocket reconnect
  ├── Calls useSnapshotEvents(predicate, callback) → refetch on relevant snapshot commit
  │
  └── Returns: { data, isLoading, isRefreshing, error }
```

### Hook Inventory

| Hook | Endpoint | Trigger | Polling? | Notes |
|---|---|---|---|---|
| `useCourses` | `GET /api/teacher/courses` | Mount, focus, WS reconnect, catalog snapshot | No | |
| `useSessions(courseId)` | `GET /api/teacher/courses/:courseId` | Mount, courseId change, focus, WS reconnect, course snapshot | No | Resets store on courseId change |
| `useCheckins(courseId, sessionId)` | `GET /api/teacher/courses/:courseId/sessions/:sessionId` | Mount, key change, focus, WS reconnect, session snapshot | **Yes, 10s** | AbortController cancel; optimistic toggle |
| `useCourseAttendance(courseId)` | `GET /api/teacher/courses/:courseId/attendance-report?threshold=N` | Mount, session snapshot | No | AbortController cancel |
| `useBatchAttendance(courseIds)` | `POST /api/teacher/courses/attendance-batch` | Mount, courseIds change | No | AbortController cancel; key-driven re-fetch |
| `useAbsenceDashboard` | `GET /api/teacher/absence-dashboard?filters=JSON` | Manual (`loadDashboard`) | No | 90s timeout; AbortController cancel |
| `useDashboardViews` | CRUD `/api/teacher/dashboard-views` | Mount | No | Full CRUD with optimistic list update |
| `useCountdown(expiresAt)` | — | Interval | 1s tick | Returns seconds remaining |
| `usePolling(callback, intervalMs, enabled)` | — | Interval | Configurable | Ref-based to avoid stale closures |
| `useFocusRefetch(callback)` | — | `visibilitychange` | Tab focus | Ref-based |
| `useSnapshotEvents(predicate, callback)` | — | Custom event `snapshot-committed` | — | Ref-based predicate + callback |

### Optimistic Updates Pattern
`useCheckins` implements optimistic check-in toggling:

```javascript
const toggleCheckin = async (studentId, checked) => {
  // 1. Optimistic: update UI immediately
  updateStudentCheckin(studentId, checked);

  try {
    // 2. Send to server
    const response = await fetchFresh(`.../toggle-checkin`, { method: 'POST', body });
    const result = await response.json();

    if (!result.success) {
      // 3. Roll back on failure
      updateStudentCheckin(studentId, !checked);
    } else if (result.data?.snapshot_refresh_pending) {
      // 4. Trigger reconciliation if snapshot is stale
      fetchStudentsNoAbort();
    }
  } catch (err) {
    // 5. Roll back on network error
    updateStudentCheckin(studentId, !checked);
  }
};
```

### API Endpoint Summary

```
GET    /api/teacher/courses                              → CourseSummary[]
GET    /api/teacher/courses/:courseId                     → CourseDetail (sessions + name)
GET    /api/teacher/courses/:courseId/sessions/:sessionId → SessionDetail (students + check-in data)
POST   /api/teacher/courses/:courseId/sessions/:sessionId/toggle-checkin
POST   /api/teacher/courses/attendance-batch              → Batch attendance data
GET    /api/teacher/courses/:courseId/attendance-report?threshold=N
GET    /api/teacher/absence-dashboard?filters=JSON
GET    /api/teacher/dashboard-views                       → Saved views list
POST   /api/teacher/dashboard-views                       → Create view
PUT    /api/teacher/dashboard-views/:id                   → Update view
DELETE /api/teacher/dashboard-views/:id                   → Delete view
POST   /api/teacher/dashboard-views/:id/use               → Touch view (update last-used)
GET    /api/teacher/favourites                            → Favourite course IDs
POST   /api/teacher/favourites                            → Pin course
DELETE /api/teacher/favourites/:courseId                   → Unpin course
GET    /api/rooms                                         → QR rooms list
POST   /api/rooms/:roomId/start                           → Start room worker
POST   /api/rooms/:roomId/stop                            → Stop room worker
DELETE /api/rooms/:roomId                                 → Delete room
POST   /api/rooms/from-session                            → Create room from session
```

---

## 6. Real-Time Updates (WebSocket & Snapshots)

### WebSocket Architecture

**Connection lifecycle** (`useWebSocket.js`):
1. Opens `ws://<host>/ws` on mount
2. On `open`: sets `isWsConnected = true`; dispatches `ws-reconnect` custom event if reconnecting
3. On `message`: parses JSON, dispatches to appropriate store
4. On `close`: attempts reconnect up to **10 times** with **3s delay**
5. On unmount: tears down connection + timers

**WebSocket event types handled:**
```javascript
data.FullStateSync     → roomActions.setRooms(rooms)      // Initial sync
data.RoomCreated       → roomActions.addRoom(room)
data.RoomUpdated       → roomActions.updateRoom(room)
data.RoomDeleted       → roomActions.removeRoom(roomId)
data.CHECKIN_UPDATED   → sessionActions.updateStudentCheckin(studentId, checkedIn)
data.SESSION_STATS_UPDATED → sessionActions.updateSessionStats(stats)
data.SnapshotStateSync → applySnapshotStateSync(metadata)  // Update version tracking
data.SnapshotCommitted → publishSnapshotCommitted(metadata) // Dispatch custom event
```

### Snapshot Event System (`useSnapshotEvents.js`)

The Warwick backend uses a **snapshot-based data model** — data is periodically scraped from external systems and committed as versioned snapshots. The frontend uses a lightweight event system to react to these:

**Version tracking:**
- A `Map<kind\0parent_key\0resource_key, version>` tracks the latest known version
- `applySnapshotStateSync` processes initial snapshot version state
- `publishSnapshotCommitted` dispatches a `snapshot-committed` custom event if version > current

**Hooks react to snapshots via `useSnapshotEvents`:**

| Hook | Predicate | Refetches |
|---|---|---|
| `useCourses` | `isCatalogSnapshot` → `kind === 'course_catalog' && key === 'catalog'` | Course list |
| `useSessions` | `isCourseSnapshot(metadata, courseId)` | Session list |
| `useCheckins` | `isSessionSnapshot(metadata, courseId, sessionId)` | Student check-in data |
| `useCourseAttendance` | `isCourseSessionSnapshot(metadata, courseId)` | Attendance report |

**Reconnect refetch:** All hooks also re-fetch via `useWsReconnect` when the WebSocket reconnects, ensuring data converges after a disconnection.

### Refetch Strategy (per hook)

| Hook | Polling | Focus Refetch | WS Reconnect | Snapshot Events | Manual |
|---|---|---|---|---|---|
| `useCourses` | — | ✅ | ✅ | ✅ Catalog | — |
| `useSessions` | — | ✅ | ✅ | ✅ Course | — |
| `useCheckins` | ✅ 10s | ✅ | ✅ | ✅ Session | `refetch()` |
| `useCourseAttendance` | — | — | — | ✅ Session | `refetch()` |
| `useBatchAttendance` | — | — | — | — | Depends on courseIds |
| `useAbsenceDashboard` | — | — | — | — | `loadDashboard()` |

---

## 7. Component Library

### Component Hierarchy

```
ErrorBoundary
└── App shell (header + nav + Routes)
    ├── HomePage
    │   └── DashboardCourseCard (one per pinned course)
    │       └── AttendanceTable (expandable)
    │           └── AttendanceRow
    ├── CourseDashboard
    │   ├── StatsBar
    │   └── CourseCard (×N, in grid)
    ├── SessionList
    │   ├── BackBreadcrumb
    │   ├── StatsBar
    │   └── SessionTable
    │       └── SessionRow (×N)
    │           └── StatusBadge
    ├── CheckinDetail
    │   ├── BackBreadcrumb
    │   ├── StatsBar
    │   ├── StudentTable
    │   │   ├── StudentRow (×N) + inline avatar
    │   │   └── Pagination
    │   └── QRModal (overlay, conditional)
    ├── CourseAttendance
    │   ├── BackBreadcrumb
    │   ├── StatPill (inline helper, ×N)
    │   └── AttendanceTable
    │       └── AttendanceRow (×N)
    └── AbsenceDashboard
        ├── DashboardKPIRow
        ├── FilterBar (complex: course picker, WCode input, sort, date, threshold)
        ├── AtRiskCallout
        └── AbsenceList
            └── StudentSummary (expandable)
                └── StudentDetail (table per course)
```

### All Components (25 total)

#### Shared / Generic
| Component | Props | States Handled | Description |
|---|---|---|---|
| `ErrorBoundary` | `children` | Error, retry | Class-based React error boundary; catches render errors, shows "Something went wrong" + retry button |
| `StatsBar` | `stats: [{value, label}]` | — | Row of 4 stat cards (24px bold value, 12px label) |
| `Pagination` | `currentPage, totalItems, perPage, onPageChange` | Edge (0 items), disabled buttons | "Showing X–Y of Z" + page number buttons with ellipsis |
| `StatusBadge` | `status` | Unknown status (falls to `not_started`) | Rounded pill with icon + label; 4 variants: active/done/not_started/auth_error |
| `BackBreadcrumb` | `to, label` | — | Styled Link ← label with hover color transition |

#### Course
| Component | Props | States Handled | Description |
|---|---|---|---|
| `CourseCard` | `course` | — | Clickable card with status dot, name, progress bar, dates, enrolled count, sessions, attendance %, pin toggle, "View Attendance Report" link |
| `DashboardCourseCard` | `course, attendanceData, attendanceLoading, attendanceError` | Loading, error, empty (no completed sessions), expanded | Expanded version on homepage with inline AttendanceTable; unpin button |

#### Session
| Component | Props | States Handled | Description |
|---|---|---|---|
| `SessionTable` | `sessions, courseId` | Empty sessions | Table wrapper with thead + tbody |
| `SessionRow` | `session, courseId` | — | Clickable row with color bar (left edge), number, name, date, StatusBadge, attendance count, → arrow |

#### Student / Check-in
| Component | Props | States Handled | Description |
|---|---|---|---|
| `StudentTable` | `students, onToggleCheckin, page, perPage, totalItems, onPageChange` | — | Table + Pagination combo |
| `StudentRow` | `student, onToggleCheckin, index` | Missing avatar (falls to UI Avatars API) | Avatar, name + school + student ID, status icon (✅/⏳), points; striped rows; click toggles check-in |
| `AttendanceRow` | `student, sessions` | Various cell states (not started, checked in ✓, absent ✗, error !) | Per-session matrix cell with color-coded symbols; sticky student name column; at-risk row highlighting |
| `AttendanceTable` | `report` | Empty sessions, empty students | Full matrix table with sticky student column + session column headers |

#### QR / Room
| Component | Props | States Handled | Description |
|---|---|---|---|
| `QRDisplay` | `room` | Null room, null qr_url | Full-screen QR display with countdown timer; used on display monitors |
| `QRModal` | `qrUrl, expiresIn, onClose, courseId, roomName, className, checkedCount, totalCount, onRefresh` | Expired (auto-refresh), countdown ≤10s warns | Fixed overlay with QR image, progress bar, auto-refresh on expiry; fadeIn/scaleIn animation |
| `RoomCard` | `room` | Warning, error, various statuses | QR room management card with Start/Stop/Delete actions; status badge; countdown |

#### Dashboard
| Component | Props | States Handled | Description |
|---|---|---|---|
| `FilterBar` | `courses, views, activeViewId, onLoadView, onSaveView, onDeleteView, onLoadDashboard, dashboardLoading` | Loading courses, no courses, save dialog, delete confirm | Complex filter panel: course picker with search select-all/none, WCode textarea, sort dropdown, date range, absence threshold, saved views dropdown + save/delete |
| `DashboardKPIRow` | `data` | Null data | 4 KPI cards (at-risk count, avg attendance %, total students, courses) |
| `AtRiskCallout` | `students` | Empty → success message | At-risk students summary with name, avatar, attendance %, absences count |
| `AbsenceList` | `students, sessions` | No students, no absences | Expandable list of student absence summaries across courses |
| `AbsenceMatrix` | `students, sessions` | No students | Legacy matrix table (similar to AttendanceTable, in the absence dashboard) |

---

## 8. Design System & Tokens

### Design Token File
All tokens live in `web/src/styles/tokens.css` (imported in `App.jsx` line 15). An identical copy exists at the project root as `attio_inspired_tokens.css`.

### Color Palette

**Primary / Brand:**
```css
--color-primary-600: #276BF0;       /* Default primary */
--color-primary-650: #256CF1;       /* Hover variant */
--color-primary-700: #236AF5;       /* Active/pressed */
--color-primary-hover: #286BEF;
--color-primary-soft: #EAF0FE;      /* Table row selected, soft badges */
--color-primary-soft-2: #E6EBFE;    /* Darker soft variant */
--color-primary-banner: #E5EEFF;    /* Banner backgrounds */
--color-primary-text: #1E3C7D;      /* Text on primary surfaces */
```

**Surfaces (mostly white/grey):**
```css
--color-bg: #FFFFFF;                /* Card/table backgrounds */
--color-bg-app: #FBFBFB;            /* Page/app background */
--color-bg-table: #FAF9FE;          /* Table row hover (subtle lavender) */
--color-bg-subtle: #F5F5F5;         /* Table header */
--color-bg-hover: #F1F2F4;          /* Button hover, nav hover */
--color-bg-selected: #EEEFF1;       /* Navigation active item */
```

**Borders (thin, subtle):**
```css
--color-border-subtle: #EEEFF1;     /* Default gridlines, nav borders */
--color-border: #DCDBDD;            /* Card borders, button borders */
--color-border-strong: #CFCFD9;     /* Stronger emphasis */
```

**Text (dark greys, not pure black):**
```css
--color-text-primary: #111113;      /* Primary content */
--color-text-secondary: #4F5056;    /* Secondary, metadata */
--color-text-muted: #696A6C;        /* Helper text, drawer copy */
--color-text-placeholder: #AAAAAA;  /* Input placeholders */
--color-text-disabled: #B8BCC4;     /* Disabled text */
--color-text-inverse: #FFFFFF;      /* Text on primary buttons */
```

**Status colors:**
```css
--color-success: #257348;           /* Green — active, checked in, good */
--color-success-bg: #DCF3E5;
--color-info: #315EBA;              /* Blue — informational */
--color-info-bg: #E1E9FE;
--color-warning: #7A631C;           /* Yellow/amber — at-risk, warnings */
--color-warning-bg: #FAF0C4;
--color-danger: #9A3D4A;            /* Red — errors, danger actions */
--color-danger-bg: #F9E0E3;
--color-purple-bg: #F0ECFE;         /* Pill variants */
--color-orange-bg: #F9E9E3;
--color-neutral-pill-bg: #F0F5CF;
```

### Typography
```css
--font-sans: Inter, ui-sans-serif, system-ui, -apple-system,
             BlinkMacSystemFont, "Segoe UI", sans-serif;
```
- **Default size:** 14px
- **Base body:** `font-size: 14px; line-height: 20px; -webkit-font-smoothing: antialiased;`
- **Table data:** 12-13px
- **Headings:** 1.5rem–1.75rem, font-weight 600–700
- **Monospace** used for: attendance counts (`font-family: monospace`), error messages, student IDs

### Spacing (4px grid)
```
--space-1: 4px   --space-5: 20px  --space-10: 40px
--space-2: 8px   --space-6: 24px  --space-12: 48px
--space-3: 12px  --space-8: 32px  --space-14: 56px
--space-4: 16px
```

### Border Radius
```
--radius-xs: 4px   --radius-lg: 10px  --radius-2xl: 16px
--radius-sm: 6px   --radius-xl: 12px
--radius-md: 8px   --radius-full: 999px   (pills, progress bars)
```

### Shadows
```css
--shadow-xs: 0 1px 2px rgba(16, 24, 40, 0.06);
--shadow-sm: 0 2px 8px rgba(16, 24, 40, 0.08);
--shadow-md: 0 8px 24px rgba(16, 24, 40, 0.10);
--shadow-lg: 0 16px 48px rgba(16, 24, 40, 0.14);
--focus-ring: 0 0 0 3px rgba(39, 107, 240, 0.16);
```

### How Components Reference Tokens
Every component uses `var(--token-name, fallback-value)` via inline `style={{}}`:
```jsx
style={{
  background: 'var(--color-bg, #FFFFFF)',
  border: '1px solid var(--color-border, #DCDBDD)',
  borderRadius: 'var(--radius-xl, 12px)',
}}
```

**Why inline styles?** The project has no CSS module system, no CSS-in-JS library, and no utility framework. Inline styles referencing CSS custom properties is the chosen approach — it works without any build-time CSS tooling. The trade-off is no hover/active CSS selectors without JavaScript event handlers, which is why every component implements hover via `onMouseEnter`/`onMouseLeave` manually.

### Spec vs Reality: Design System Implementation Status

The `attio_inspired_design_system.md` spec (626 lines) describes an Attio-inspired design with:
- Top navigation shell is implemented; no sidebar layout is used
- Component classes (`.btn-primary`, `.btn-secondary`, `.input`, `.data-table`, `.nav-item`, `.pill`) — **NOT used** (inline styles instead)
- Modal (470px centered), Drawer (380px right panel), Floating action bar — **NOT implemented as spec**
- Stepper/Import flow, Onboarding screens, Workflow builder — **NOT implemented**
- Skeleton loading shimmer cards — **NOT implemented**
- Transition specifications (card hover, page navigate, QR modal) — **Partially implemented**

The `tokens.css` file contains only the CSS custom properties and base body styles. The component classes from `attio_inspired_tokens.css` are **not imported** anywhere in the app.

---

## 9. UX Design Direction

### Design Origin
The design system is **visually extracted from Attio product screenshots**, documented in `attio_inspired_design_system.md`. The interface direction is described as:

> "A dense, calm B2B SaaS workspace: spreadsheet-like data tables, left navigation, top action bars, import flows, onboarding screens, drawers, and workflow builder cards."

### Core Visual Traits
- Mostly white canvas with very soft grey/lavender table backgrounds
- Compact spacing and high information density
- Thin 1px borders instead of heavy shadows
- Blue as the primary action, focus, and selected-state color
- Rounded but restrained corners: 6–12px
- Small typography, strong hierarchy through weight rather than size
- Line-based icons, 16px, monochrome or blue when active
- Borders over shadows for most elements

### UX Plan (`docs/UX_PLAN.md`)
The UX plan outlines a **3-page architecture with ≤2 clicks to target**:

1. **Course Dashboard** (landing) → Click course card →
2. **Session List** (per course) → Click session row →
3. **Check-in Detail** (per session)

**All 3 pages are implemented** with this click hierarchy.

The UX Plan also specifies (some implemented, some not):
- **Transitions:** Card hover (translateY(-2px) + shadow) ✅, Row hover (background) ✅, QR Modal (fadeIn + scaleIn) ✅, Page navigate (opacity + translateY) ❌
- **Loading skeletons:** ❌ (simple text/shared spinner instead)
- **Empty states:** ✅ (all pages have empty states)
- **Keyboard shortcuts:** ⌘K focus search ❌, Escape close modal ✅, Enter open row ❌, ↑↓ navigate ❌
- **Responsive breakpoints:** ❌ (no media queries anywhere)
- **Export CSV:** ✅ (implemented in CheckinDetail)

---

## 10. Error Handling, Loading & Empty States

### State Coverage Table

| Page/Component | Loading | Error | Empty (no data) | Empty (no results) | Edge Cases |
|---|---|---|---|---|---|
| **CourseDashboard** | Centered text "Loading courses..." | Centered red text "Error: {msg}" | "No courses assigned to you yet" | "No courses match your search" | isRefreshing indicator (fixed badge) |
| **SessionList** | Centered text "Loading sessions..." | Centered red text "Error: {msg}" | "No attendance sessions for this course" | — | isRefreshing indicator |
| **CheckinDetail** | Centered text "Loading students..." | Red error text + Retry button | "No students enrolled" | "No students match your search" | autoStartError with Retry; JSON parse errors handled per API call; AbortError silently ignored |
| **CourseAttendance** | Spinner + "Computing attendance report... (up to 60s)" | Error card + Retry button | Null data → null render | "No session data available" / "No student attendance data found" | `truncated` warning banner; per-session error list |
| **AbsenceDashboard** | Spinner + "Loading dashboard data from Warwick..." | Error card (red) | "Configure your dashboard above" (pre-fetch) / "No students with absences" (post-fetch) | "No absences in the current filter" | 90s timeout → specific error message; wCode count indicator |
| **HomePage** | "Loading courses..." | Error card (red, with color-mix danger bg) | "No pinned courses yet" + call-to-action | — | — |
| **DashboardCourseCard** | "Loading attendance data..." | "Attendance data unavailable" | "No completed sessions" (grey card) | — | Expandable → AttendanceTable inline |
| **ErrorBoundary** | — | Full page "Something went wrong" + retry | — | — | Catches any React render error |

### Error Handling Pattern
```
API call → try/catch
  ├── HTTP error (res.ok === false): text body + status
  ├── JSON parse error (SyntaxError): specific "Invalid response" message
  ├── AbortError: silently ignored (component unmounted or superseded)
  ├── Network error: error.message
  └── Success (!result.success): result.error message
```

### Loading Pattern
Two-phase loading tracked per store:
1. **`isInitialLoading`** → `true` on first fetch; shows full loading state
2. **`isRefreshing`** → `true` on subsequent/silent fetches; shows subtle "Syncing..." badge (fixed-position, top-right)

---

## 11. Accessibility & Interaction Patterns

### What's Implemented
- **Focus-visible indicator:** `tokens.css` line 90–93 sets `focus-visible` outline + blue focus ring on `button`, `a`, `input`, `select`
- **Keyboard interaction:** CourseCard (Enter/Space to navigate), QRModal (Escape to close), Pagination (keyboard-navigable buttons)
- **ARIA attributes:** `aria-label` on icon buttons, `aria-pressed` on pin toggle, `aria-expanded` on expandable sections, `aria-selected` on table rows (in spec, not confirmed in code), `role="progressbar"` on QR progress bar
- **Semantic HTML:** `<nav>`, `<main>`, `<table>`, `<th scope="col">`, `<button>` for actions
- **Min interactive targets:** 32px minimum, 36–40px preferred

### What's Missing (per spec)
- Keyboard shortcuts (⌘K, ↑↓ table navigation)
- Proper screen reader announcements for dynamic updates
- Color-is-not-the-only-indicator for status (emoji icons help, but no aria-live regions)
- `aria-selected` on table rows

### Hover State Implementation Pattern
Because inline styles can't use CSS `:hover`, every interactive element implements hover via:
```jsx
onMouseEnter={(e) => { e.currentTarget.style.background = 'var(--color-bg-hover)' }}
onMouseLeave={(e) => { e.currentTarget.style.background = 'transparent' }}
```
This adds boilerplate but works without any CSS preprocessing.

---

## 12. Known Gaps & Limitations

### Design System
1. **No reusable UI component library.** Buttons, inputs, selects, badges are re-implemented inline in every component with slightly different styles (e.g., FilterBar defines 8+ separate inline style objects for buttons alone).
2. **No CSS class system.** The `.btn-primary`, `.btn-secondary`, `.input`, `.data-table` classes in `attio_inspired_tokens.css` exist but are not imported. All styling is inline.
3. **Hover states require JavaScript.** No CSS classes = no `:hover` selectors = manual onMouseEnter/onMouseLeave on every element.
4. **No dark mode.** Tokens are hardcoded light-mode values.
5. **No responsive design.** No media queries anywhere. Layout works on desktop only.

### State Management
6. **`useSessionStore` is shared** between SessionList and CheckinDetail but used for different fields (sessions vs students). A navigation from SessionList to CheckinDetail passes through `reset()` which clears everything.
7. **No request deduplication.** Multiple components mounting simultaneously (e.g., DashboardCourseCard → AttendanceTable for each card) could trigger parallel requests for the same data.

### Error Handling
8. **No global error boundary for navigation errors.** If a route component throws, the ErrorBoundary catches it but there's no fallback route.
9. **No offline detection.** If the network drops, the WebSocket reconnects but there's no "you are offline" UI.
10. **alert()/confirm() used in RoomCard.** Legacy pattern that should be replaced with modal UI.

### Performance
11. **No virtualization.** Student tables with hundreds of rows render all DOM elements.
12. **No code splitting.** All routes bundled in one chunk.
13. **Inline style objects recreated on every render** in many components (objects defined inside render functions).

### Testing
14. Test files exist in `__tests__/` but coverage is unknown — no CI integration visible.

### Missing UX Features (from UX Plan)
15. Loading skeletons (shimmer placeholder cards)
16. Page transition animations
17. Responsive breakpoints (<768px, 768-1199px, ≥1200px)
18. Keyboard shortcuts (⌘K search, ↑↓ table navigation, Enter to open)
19. Table column visibility toggles
20. Bulk actions (select multiple students, batch operations)

---

## Appendix: Data Models

```typescript
interface CourseSummary {
  course_id: string;
  name: string;
  start_date: string;
  end_date: string;
  enrolled_count: number;
  total_sessions: number;
  completed_sessions: number;
  avg_attendance_rate: number;        // 0-1 float
  status: 'active' | 'upcoming' | 'finished';
  term_dates?: string;
}

interface CourseDetail extends CourseSummary {
  sessions: SessionSummary[];
}

interface SessionSummary {
  session_id: string;
  session_number: number;
  name: string;
  date: string;
  checked_in_count: number;
  total_students: number;
  status: 'active' | 'done' | 'not_started' | 'auth_error';
}

interface SessionDetail extends SessionSummary {
  students: StudentCheckin[];
  qr_active: boolean;
  qr_expires_at: string | null;
  qr_url?: string;
}

interface StudentCheckin {
  student_id: string;
  name: string;
  nickname: string;
  school: string;
  avatar_url: string;
  checked_in: boolean;
  checked_in_at: string | null;
  participation_points: number;
}

interface AttendanceReport {
  courseName: string;
  students: AttendanceStudent[];
  sessions: AttendanceSession[];
  truncated: boolean;
  errors: { sessionId: string; reason: string }[];
  durationMs?: number;
}

interface AttendanceStudent {
  studentId: string;
  name: string;
  nickname: string;
  school: string;
  avatarUrl: string;
  attendanceRate: number;
  atRisk: boolean;
  attendedSessions: number;
  totalSessions: number;
  perSession: PerSessionCell[];
}

interface PerSessionCell {
  sessionId: string;
  status: 'ok' | 'absent' | 'error';
  checkedIn: boolean;
  sessionDate?: string;
  sessionNumber?: number;
  sessionStatus?: 'active' | 'done' | 'not_started';
}

interface AbsenceDashboardData {
  atRiskCount: number;
  avgAttendanceRate: number;
  totalStudents: number;
  totalCourses: number;
  students: StudentDetail[];
  sessions: AttendanceSession[];
}

interface Room {
  room_id: string;
  class_id: string;
  name: string;
  qr_url: string;
  expires_at: string;
  status: 'Running' | 'Fetching' | 'Warning' | 'AuthExpired' | 'Stopped';
  warning_message?: string;
  error_message?: string;
}

interface DashboardView {
  id: number;
  name: string;
  filters: DashboardFilters;
  last_used_at?: string;
}
```

---

## Appendix: Styling Patterns Reference

### Pattern 1: Card/Container
```jsx
style={{
  background: 'var(--color-bg, #FFFFFF)',
  border: '1px solid var(--color-border, #DCDBDD)',
  borderRadius: 'var(--radius-xl, 12px)',
  padding: 'var(--space-6, 24px)',
}}
```

### Pattern 2: Button (Primary)
```jsx
style={{
  padding: '10px 24px',
  borderRadius: 'var(--radius-md, 8px)',
  border: 'none',
  background: 'var(--color-primary-600, #276BF0)',
  color: '#fff',
  fontWeight: '500',
  cursor: 'pointer',
}}
```

### Pattern 3: Button (Secondary)
```jsx
style={{
  padding: '10px 20px',
  borderRadius: 'var(--radius-md, 8px)',
  border: '1px solid var(--color-border, #DCDBDD)',
  background: 'transparent',
  color: 'var(--color-text-secondary, #4F5056)',
  fontWeight: '500',
  cursor: 'pointer',
}}
```

### Pattern 4: Error Banner
```jsx
style={{
  padding: 'var(--space-6, 24px)',
  background: 'color-mix(in srgb, var(--color-danger, #9A3D4A) 12%, transparent)',
  color: 'var(--color-danger, #9A3D4A)',
  borderRadius: 'var(--radius-md, 8px)',
  marginBottom: 'var(--space-6, 24px)',
}}
```

### Pattern 5: Spinner
```jsx
<div style={{
  width: '32px', height: '32px',
  border: '3px solid var(--color-border, #DCDBDD)',
  borderTopColor: 'var(--color-primary-600, #276BF0)',
  borderRadius: '50%',
  animation: 'spin 0.8s linear infinite',
}} />
<style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
```

### Pattern 6: Hover on Clickable Row
```jsx
onMouseEnter={(e) => e.currentTarget.style.background = 'var(--color-bg-hover, #F1F2F4)'}
onMouseLeave={(e) => e.currentTarget.style.background = 'transparent'}
```

### Pattern 7: Fixed "Syncing..." Badge
```jsx
<div style={{
  position: 'fixed', top: '12px', right: '12px',
  background: 'var(--color-bg, #FFFFFF)',
  border: '1px solid var(--color-border, #DCDBDD)',
  borderRadius: 'var(--radius-md, 8px)',
  padding: '6px 12px', fontSize: '12px',
  color: 'var(--color-text-secondary, #4F5056)',
  zIndex: 1000, opacity: 0.8,
}}> Syncing... </div>
```
