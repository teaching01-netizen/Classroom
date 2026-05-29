# Refined UX Plan: Teacher Classroom Check-in Dashboard

## 1. Current State Analysis

**Tech Stack:** Go backend + React/Vite frontend + Zustand store + WebSocket  
**Existing Components:** `RoomCard.jsx`, `QRDisplay.jsx`, `App.jsx` (room CRUD only)  
**External System (Warwick):** 56 courses, 18 class sessions per course, student check-in with QR codes  
**Core Problem:** Current app only manages QR rooms. No teacher-facing dashboard for viewing courses, attendance sessions, or check-in stats.

---

## 2. Refined User Flow (3 Pages, Max 2 Clicks to Target)

```
Page 1: Course Dashboard (landing)
  └─ Click course card →
Page 2: Session List (per course)
  └─ Click session row →
Page 3: Check-in Detail (per session)
```

**Entry:** Teacher logs in → sees only their filtered courses (Page 1)  
**Goal:** Teacher views attendance stats for today's session in ≤2 clicks.

---

## 3. Page 1: Course Dashboard

### Layout (Full Width, 12-col grid)

```
┌─────────────────────────────────────────────────────────┐
│ HEADER: "My Courses"                    [teacher avatar] │
├─────────────────────────────────────────────────────────┤
│ STATS BAR (h=80px, flex, gap=24px)                      │
│ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐    │
│ │ 8 Active │ │ 24 Total │ │ 156      │ │ 89%      │    │
│ │ Courses  │ │ Sessions │ │ Students │ │ Avg Att. │    │
│ └──────────┘ └──────────┘ └──────────┘ └──────────┘    │
├─────────────────────────────────────────────────────────┤
│ FILTERS (h=48px, flex)                                   │
│ [Search: ____________] [Status: ▼ All] [Sort: ▼ Name]   │
├─────────────────────────────────────────────────────────┤
│ COURSE GRID (auto-fill, minmax(340px, 1fr), gap=20px)   │
│ ┌─────────────────┐ ┌─────────────────┐ ┌─────────────┐ │
│ │ CourseCard       │ │ CourseCard       │ │ CourseCard   │ │
│ │ (340px min)      │ │                  │ │              │ │
│ └─────────────────┘ └─────────────────┘ └─────────────┘ │
└─────────────────────────────────────────────────────────┘
```

### CourseCard Component (340×220px)

```
┌──────────────────────────────────┐
│ [status-dot] SAT Math Beginner   │ ← name: 16px semibold
│             C2/2026              │ ← subtitle: 13px #94a3b8
│                                  │
│ ┌──────────────────────────────┐ │
│ │ ████████████░░░░░ 89%        │ │ ← progress bar: h=6px, #4ade80 filled
│ └──────────────────────────────┘ │
│                                  │
│ 📅 May 27 - Jul 3, 2026         │ ← date range: 12px #94a3b8
│ 👥 24 students                   │ ← enrollment: 12px #94a3b8
│ 📋 12/18 sessions               │ ← sessions completed: 12px #94a3b8
│                                  │
│ 85% attendance                   │ ← aggregate: 14px semibold, color by value
└──────────────────────────────────┘
```

**Status dot colors:**
- `Active` (in date range): `#4ade80` (green)
- `Upcoming` (future start): `#60a5fa` (blue)
- `Finished` (past end): `#94a3b8` (gray)

**Hover state:** `translateY(-2px)`, `box-shadow: 0 8px 24px rgba(0,0,0,0.2)`, transition 200ms ease

**Click:** Navigates to `/courses/:courseId/sessions`

### Stats Bar Card Component (160×80px)

```
┌────────────────┐
│ 8              │ ← value: 24px bold #fff
│ Active Courses │ ← label: 12px #94a3b8
└────────────────┘
```

Background: `#16213e`, border: `1px solid #2d3a5a`, border-radius: `12px`

---

## 4. Page 2: Session List (per Course)

### Layout

```
┌─────────────────────────────────────────────────────────┐
│ ← Back to Courses    SAT Math Beginner C2/2026         │
├─────────────────────────────────────────────────────────┤
│ STATS BAR (same 4-card layout as Page 1)                │
│ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐    │
│ │ 18 Total │ │ 12 Done  │ │ 156      │ │ 89%      │    │
│ │ Sessions │ │ Active   │ │ Students │ │ Attendance│    │
│ └──────────┘ └──────────┘ └──────────┘ └──────────┘    │
├─────────────────────────────────────────────────────────┤
│ SESSION TABLE (full width)                               │
│ ┌────┬──────────────────┬──────────────┬────────┬──────┐│
│ │ #  │ Session Name     │ Date         │ Status │ Att. ││
│ ├────┼──────────────────┼──────────────┼────────┼──────┤│
│ │ 1  │ Class Attendance │ May 27, 2026 │ Done   │ 22/24││
│ │ 2  │ Class Attendance │ Jun 3, 2026  │ Done   │ 20/24││
│ │ 3  │ Class Attendance │ Jun 10, 2026 │ Active │ 18/24││
│ │ 4  │ Class Attendance │ Jun 17, 2026 │ —      │ —    ││
│ └────┴──────────────────┴──────────────┴────────┴──────┘│
└─────────────────────────────────────────────────────────┘
```

### Session Table Row Component (h=56px)

```
┌──────────────────────────────────────────────────────────────┐
│ [color-bar] │ 3 │ Class Attendance 3 │ Jun 10, 2026 │ 🟢 Active │ 18/24 │ → │
└──────────────────────────────────────────────────────────────┘
```

**Color bar (left edge, 4px wide, full row height):**
- `Active/Running`: `#4ade80`
- `Done/Finished`: `#6366f1` (indigo)
- `Not Started`: `#2d3a5a` (subtle gray)
- `Auth Error`: `#f97316` (orange)

**Status badges:**
| Status | Background | Text Color | Icon |
|--------|-----------|------------|------|
| Active | `#4ade8020` | `#4ade80` | ● |
| Done | `#6366f120` | `#6366f1` | ✓ |
| Not Started | `#2d3a5a20` | `#94a3b8` | ○ |
| Auth Error | `#f9731620` | `#f97316` | ⚠ |

**Attendance cell:** `{checkedIn}/{total}` in `14px monospace`

**Hover:** Row background → `#1a1a2e`, right arrow icon appears (slide in 150ms)

**Click:** Navigates to `/courses/:courseId/sessions/:sessionId`

---

## 5. Page 3: Check-in Detail (per Session)

### Layout

```
┌─────────────────────────────────────────────────────────────┐
│ ← Back to Sessions    Class Attendance 3                    │
│                    SAT Math Beginner C2/2026                │
├─────────────────────────────────────────────────────────────┤
│ AGGREGATE STATS (h=80px)                                     │
│ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────────┐│
│ │ 18/24    │ │ 75%      │ │ QR Active│ │ ⏱ 45s remaining  ││
│ │ Checked  │ │ Rate     │ │          │ │ (if QR running)  ││
│ └──────────┘ └──────────┘ └──────────┘ └──────────────────┘│
├─────────────────────────────────────────────────────────────┤
│ ACTIONS (h=48px)                                             │
│ [Show QR Code]  [Export CSV]  [Refresh]                      │
├─────────────────────────────────────────────────────────────┤
│ SEARCH: [________________] FILTER: [All ▼] [Checked ▼]      │
├─────────────────────────────────────────────────────────────┤
│ STUDENT TABLE                                                 │
│ ┌──┬─────────────────────────┬──────────┬────────┬────────┐ │
│ │  │ Name                    │ School   │ Status │ Points │ │
│ ├──┼─────────────────────────┼──────────┼────────┼────────┤ │
│ │✓ │ Achiraya Tansirichaiya  │ Concord  │ ✅ In  │ 0      │ │
│ │✓ │ Akkarawat Hiranrodpacha │ Satit   │ ✅ In  │ 0      │ │
│ │  │ Apichaya Srisombat      │ Punyapi │ ⏳ Not │ 0      │ │
│ └──┴─────────────────────────┴──────────┴────────┴────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### Student Row Component (h=52px)

```
┌──────────────────────────────────────────────────────────┐
│ [avatar 36px] │ Achiraya Tansirichaiya │ Concord │ ✅ │ 0 │
└──────────────────────────────────────────────────────────┘
```

**Status indicator (column 4):**
| State | Icon | Color |
|-------|------|-------|
| Checked In | ✅ | `#4ade80` |
| Not Checked | ⏳ | `#94a3b8` |
| Late | 🕐 | `#fbbf24` |

**Row hover:** `background: #1a1a2e20`, show action icons (edit points)

**Striped rows:** Odd rows `transparent`, even rows `#ffffff05` (very subtle)

---

## 6. Component Specifications

### Design Tokens (CSS Variables)

```css
:root {
  /* Backgrounds */
  --bg-primary: #0f172a;
  --bg-card: #16213e;
  --bg-card-hover: #1a1a2e;
  --bg-input: #1a1a2e;
  
  /* Borders */
  --border-default: #2d3a5a;
  --border-hover: #3d4a6a;
  
  /* Text */
  --text-primary: #ffffff;
  --text-secondary: #94a3b8;
  --text-muted: #64748b;
  
  /* Status */
  --color-success: #4ade80;
  --color-info: #60a5fa;
  --color-warning: #fbbf24;
  --color-danger: #ef4444;
  --color-accent: #6366f1;
  --color-inactive: #94a3b8;
  
  /* Spacing */
  --space-xs: 4px;
  --space-sm: 8px;
  --space-md: 16px;
  --space-lg: 24px;
  --space-xl: 32px;
  
  /* Radius */
  --radius-sm: 6px;
  --radius-md: 8px;
  --radius-lg: 12px;
  --radius-full: 9999px;
  
  /* Shadows */
  --shadow-card: 0 1px 3px rgba(0,0,0,0.3);
  --shadow-card-hover: 0 8px 24px rgba(0,0,0,0.4);
}
```

### Component Inventory

| Component | File | Props | Purpose |
|-----------|------|-------|---------|
| `StatsBar` | `components/StatsBar.jsx` | `stats: StatItem[]` | 4-card aggregate row |
| `CourseCard` | `components/CourseCard.jsx` | `course, onClick` | Course grid item |
| `SessionTable` | `components/SessionTable.jsx` | `sessions, onRowClick` | Session list |
| `SessionRow` | `components/SessionRow.jsx` | `session, onClick` | Single session row |
| `StudentTable` | `components/StudentTable.jsx` | `students, onStatusChange` | Check-in list |
| `StudentRow` | `components/StudentRow.jsx` | `student` | Single student row |
| `StatusBadge` | `components/StatusBadge.jsx` | `status, variant` | Colored status pill |
| `ProgressBar` | `components/ProgressBar.jsx` | `value, color?, size?` | Progress indicator |
| `SearchInput` | `components/SearchInput.jsx` | `value, onChange, placeholder` | Filter input |
| `FilterDropdown` | `components/FilterDropdown.jsx` | `options, value, onChange` | Filter select |
| `QRModal` | `components/QRModal.jsx` | `qrUrl, expiresIn, onClose` | QR display overlay |
| `BackBreadcrumb` | `components/BackBreadcrumb.jsx` | `items: BreadcrumbItem[]` | Navigation breadcrumb |

---

## 7. Data Requirements

### API Endpoints Needed

```
GET  /api/teacher/courses
     → CourseSummary[] (filtered by teacher_id from session)

GET  /api/teacher/courses/:courseId
     → CourseDetail (with session list + stats)

GET  /api/teacher/courses/:courseId/sessions/:sessionId
     → SessionDetail (with student list + check-in status)

POST /api/teacher/courses/:courseId/sessions/:sessionId/checkin
     → Toggle student check-in status

GET  /api/teacher/courses/:courseId/sessions/:sessionId/qr
     → QR code data (url + expiry)
```

### Data Models

```typescript
// CourseSummary (Page 1 cards)
interface CourseSummary {
  course_id: string;
  name: string;                    // "SAT Math Beginner C2/2026"
  start_date: string;              // "2026-05-27"
  end_date: string;                // "2026-07-03"
  enrolled_count: number;          // 24
  total_sessions: number;          // 18
  completed_sessions: number;      // 12
  avg_attendance_rate: number;     // 0.89
  status: 'active' | 'upcoming' | 'finished';
}

// CourseDetail (Page 2 table)
interface CourseDetail extends CourseSummary {
  sessions: SessionSummary[];
}

// SessionSummary (Page 2 rows)
interface SessionSummary {
  session_id: string;
  session_number: number;          // 1-18
  name: string;                    // "Class Attendance 3"
  date: string;                    // "2026-06-10"
  checked_in_count: number;        // 18
  total_students: number;          // 24
  status: 'active' | 'done' | 'not_started' | 'auth_error';
}

// SessionDetail (Page 3)
interface SessionDetail extends SessionSummary {
  students: StudentCheckin[];
  qr_active: boolean;
  qr_expires_at: string | null;
}

// StudentCheckin (Page 3 rows)
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
```

### WebSocket Events (extend existing)

```typescript
// Existing events (keep)
type WSEvent = 
  | { type: 'ROOM_UPDATED'; payload: Room }
  | { type: 'QR_UPDATED'; payload: { session_id: string; qr_url: string; expires_at: string } }
  
// New events to add
  | { type: 'CHECKIN_UPDATED'; payload: { session_id: string; student_id: string; checked_in: boolean } }
  | { type: 'SESSION_STATS_UPDATED'; payload: { session_id: string; checked_in_count: number } }
```

---

## 8. Interaction Design

### Transitions

| Interaction | Transition | Duration |
|-------------|-----------|----------|
| Card hover | `transform: translateY(-2px)` + `box-shadow` | 200ms ease |
| Row hover | `background-color` fade | 150ms ease |
| Page navigate | `opacity: 0 → 1` + `translateY(8px → 0)` | 250ms ease-out |
| QR Modal open | `opacity: 0 → 1` + `scale(0.95 → 1)` | 200ms ease |
| QR Modal close | `opacity: 1 → 0` + `scale(1 → 0.95)` | 150ms ease-in |
| Stats counter | Number count-up animation | 500ms ease-out |
| Status badge | `background-color` + `color` transition | 200ms ease |
| Progress bar fill | `width: 0% → value%` | 600ms ease-out (on mount) |

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `⌘ + K` | Focus search input |
| `Escape` | Close modal / go back |
| `Enter` | Open selected row |
| `↑ ↓` | Navigate table rows |

### Loading States

| View | Skeleton |
|------|----------|
| Course grid | 6 shimmer cards (340×220px) with pulsing `#16213e → #1a1a2e` |
| Session table | 8 shimmer rows (100% × 56px) with pulsing |
| Student table | 10 shimmer rows (100% × 52px) with pulsing |

### Empty States

| View | Message | Action |
|------|---------|--------|
| No courses | "No courses assigned to you yet" | Contact admin |
| No sessions | "No attendance sessions for this course" | — |
| No students | "No students enrolled" | — |
| No search results | "No results matching '{query}'" | Clear search |

---

## 9. Responsive Breakpoints

| Breakpoint | Layout Change |
|------------|--------------|
| ≥1200px | 3-column course grid, full table columns |
| 768-1199px | 2-column course grid, hide "School" column |
| <768px | 1-column course grid, card layout for table rows, hide "Points" |

---

## 10. File Structure (New Components)

```
web/src/
├── components/
│   ├── StatsBar.jsx           (NEW)
│   ├── CourseCard.jsx         (NEW - replaces RoomCard for courses)
│   ├── SessionTable.jsx       (NEW)
│   ├── SessionRow.jsx         (NEW)
│   ├── StudentTable.jsx       (NEW)
│   ├── StudentRow.jsx         (NEW)
│   ├── StatusBadge.jsx        (NEW)
│   ├── ProgressBar.jsx        (NEW)
│   ├── SearchInput.jsx        (NEW)
│   ├── FilterDropdown.jsx     (NEW)
│   ├── QRModal.jsx            (NEW - extracted from QRDisplay)
│   ├── BackBreadcrumb.jsx     (NEW)
│   ├── RoomCard.jsx           (KEEP - for QR room management)
│   └── QRDisplay.jsx          (KEEP - for display screens)
├── pages/
│   ├── CourseDashboard.jsx    (NEW - Page 1)
│   ├── SessionList.jsx        (NEW - Page 2)
│   └── CheckinDetail.jsx      (NEW - Page 3)
├── hooks/
│   ├── useCourses.js          (NEW - fetch courses)
│   ├── useSessions.js         (NEW - fetch sessions)
│   ├── useCheckins.js         (NEW - fetch/toggle check-ins)
│   ├── useWebSocket.js        (KEEP - extend for new events)
│   └── useCountdown.js        (KEEP)
├── store/
│   ├── useRoomStore.js        (KEEP)
│   ├── useCourseStore.js      (NEW)
│   └── useSessionStore.js     (NEW)
└── App.jsx                    (MODIFY - add routing)
```

---

## 11. Priority Implementation Order

### P0 (Week 1): Core Data Flow
1. `useCourseStore.js` + `useSessions.js` + `useCheckins.js`
2. `StatsBar.jsx` component
3. `CourseCard.jsx` + `CourseDashboard.jsx` (Page 1)
4. `SessionTable.jsx` + `SessionList.jsx` (Page 2)
5. `StudentTable.jsx` + `CheckinDetail.jsx` (Page 3)
6. `BackBreadcrumb.jsx`

### P1 (Week 2): Polish & Interaction
7. `StatusBadge.jsx`
8. `ProgressBar.jsx`
9. `SearchInput.jsx` + `FilterDropdown.jsx`
10. `QRModal.jsx` (extract from QRDisplay)
11. Hover states + transitions
12. Loading skeletons

### P2 (Week 3): Enhancement
13. Keyboard shortcuts
14. Responsive breakpoints
15. Empty states
16. Export CSV
17. WebSocket live updates for check-ins
