This is a comprehensive, engineering-grade implementation plan for the **Check-in QR Command Center**, adapted for a **Rust (Backend)**, **React (Frontend)**, and **PostgreSQL (Database)** stack, driven by a **Rigorous Test-Driven Development (TDD)** methodology.

### 🏗️ Architecture & Tech Stack

| Layer | Technology | Purpose |
| :--- | :--- | :--- |
| **Backend Framework** | `axum` + `tokio` | High-performance, async HTTP & WebSocket routing. |
| **HTTP Client** | `reqwest` | Proxying requests to the Warwick API. |
| **Database ORM** | `sqlx` | Compile-time checked, async PostgreSQL queries. |
| **Frontend** | React + `vite` | SPA dashboard and display views. |
| **State Management** | `zustand` (React) | Lightweight global state for WS events and rooms. |
| **Testing (Rust)** | `cargo test`, `wiremock`, `testcontainers-rs` | Unit, Mocked HTTP, and Ephemeral DB integration tests. |
| **Testing (React)** | `vitest`, `@testing-library/react`, `playwright` | Component testing and E2E browser automation. |

---

### 🛡️ Resolving the "Database vs. Security" Constraint
The previous design strictly forbade saving the `ASP.NET_SessionId` to disk. To honor both the **PostgreSQL requirement** and the **Security Non-Goal**:
1. **PostgreSQL** will persist **Room Configurations** (Room ID, Class ID, Name, Status, Last Known QR, Expiry Timestamps). This ensures that if the server restarts, the operator's dashboard layout and room history are preserved.
2. **The Session Cookie** will **never** touch the database or disk. It lives strictly in an in-memory `Arc<RwLock<String>>` secured by the Rust backend.
3. **On Restart:** Rooms load from Postgres in an `AUTH_EXPIRED` or `STOPPED` state. The operator must re-paste the cookie into the Settings Drawer to resume fetching.

---

### 🧪 The Rigorous TDD Strategy
Every feature follows the **Red-Green-Refactor** cycle. In Rust, "Red" often manifests as a compiler error (e.g., missing trait implementation) before it becomes a failing test.

1. **Unit Tests (The Core Logic):** Test TTL math, HTML login detection, and state transitions without network or DB.
2. **Integration Tests (The Boundaries):** Use `testcontainers-rs` to spin up an ephemeral PostgreSQL container for every test run. Use `wiremock` to simulate Warwick's exact HTTP responses (200 OK HTML, 302 Redirects, JSON payloads).
3. **E2E Tests (The User Flow):** Playwright scripts that start the Rust binary, open the React app, create a room, and verify the WebSocket updates the DOM.

---

### 📅 Phased Implementation Plan

#### Phase 1: Scaffold, Domain & Database (Days 1-2)
**Goal:** Define the domain models, set up the DB schema, and prove persistence.
* **Action:** Initialize Rust workspace (`cargo new`) and React app (`npm create vite`). Set up Docker Compose for local Postgres.
* **TDD Cycle (Database):**
  * *Red:* Write an integration test using `testcontainers-rs` that attempts to insert a `Room` and read it back. It fails because the table doesn't exist.
  * *Green:* Write the `sqlx` migration (`CREATE TABLE rooms ...`). Run the migration in the test setup. The test passes.
  * *Refactor:* Create a `RoomRepository` trait and implement it for `SqlitePool`/`PgPool`.
* **Deliverable:** `sqlx` models, migrations, and a passing repository test suite.

#### Phase 2: The Warwick Proxy & Auth Detection (Days 3-4)
**Goal:** Build the HTTP client that talks to Warwick and accurately detects silent auth failures.
* **Action:** Implement the `WarwickClient` trait.
* **TDD Cycle (Auth Detection):**
  * *Red:* Write unit tests asserting that a `200 OK` with `<title>WarWick</title>` returns `AuthError::SessionExpired`. Write a test asserting a `302 Found` returns `AuthError::Redirect`.
  * *Green:* Implement `reqwest` logic with `max_redirects(0)` and body inspection.
  * *Refactor:* Extract the HTML parsing logic into a pure function `fn is_login_page(body: &str) -> bool` for easier unit testing.
* **TDD Cycle (TTL Math):**
  * *Red:* Test that a 60s TTL schedules the next fetch at 45s (75%).
  * *Green:* Implement `calculate_next_fetch_delay(ttl: u64)`.
* **Deliverable:** A fully tested, mocked Warwick client that guarantees auth failures are caught.

#### Phase 3: Concurrency & The Refresh Loop (Days 5-6)
**Goal:** Build the background workers that manage the QR lifecycle without blocking the main thread.
* **Action:** Implement the `RoomManager` using `tokio::spawn` and `tokio::sync::broadcast` for internal event fan-out.
* **TDD Cycle (Overlap & Buffer):**
  * *Red:* Write an integration test that mocks a slow Warwick response (takes 5s). Assert that the *old* QR remains active and visible until the new one is fully validated.
  * *Green:* Implement the atomic swap logic using `Arc<RwLock<RoomState>>`.
  * *Refactor:* Ensure `tokio::select!` is used to handle graceful shutdowns (e.g., when a room is stopped, the background task must abort immediately).
* **Deliverable:** The core engine that reliably fetches, buffers, and swaps QR codes.

#### Phase 4: API & WebSocket Gateway (Days 7-8)
**Goal:** Expose the state to the frontend via REST and WebSockets.
* **Action:** Build `axum` routes and the WebSocket upgrade handler.
* **TDD Cycle (Late Joiner Sync):**
  * *Red:* Write a test that connects a WS client *after* a room has been running for 10 seconds. Assert that the client immediately receives a `FULL_STATE_SYNC` event containing the current QR and absolute `expiresAt` timestamp.
  * *Green:* Implement the WS connection handler to query the `RoomManager` state and send the snapshot before entering the broadcast loop.
* **TDD Cycle (Shared Actions):**
  * *Red:* Connect two WS clients. Send `STOP_ROOM` from Client A. Assert Client B receives the `ROOM_STOPPED` event within 100ms.
  * *Green:* Wire the WS message router to the `RoomManager`.
* **Deliverable:** A fully functional, realtime backend API.

#### Phase 5: React Frontend & Integration (Days 9-11)
**Goal:** Build the Operator Dashboard and the Read-Only Display View.
* **Action:** Create React components (`RoomCard`, `SettingsDrawer`, `QRDisplay`). Connect to the Rust WS gateway.
* **TDD Cycle (Component Logic):**
  * *Red:* Use `vitest` to test the countdown hook. Pass an `expiresAt` timestamp of `Date.now() + 5000`. Assert the UI renders "5s" and ticks down.
  * *Green:* Implement the `useCountdown` hook using `requestAnimationFrame` or `setInterval` tied to the absolute server timestamp.
* **TDD Cycle (Security):**
  * *Red:* Write a test that intercepts all network requests from the React app. Assert that the string `ASP.NET_SessionId` *never* appears in any outgoing or incoming payload.
  * *Green:* Ensure the frontend only sends `UPDATE_COOKIE` via a secure, local-only POST request, and the backend never echoes it back.
* **Deliverable:** A polished, reactive UI that mirrors the backend state perfectly.

#### Phase 6: E2E & Hardening (Days 12-14)
**Goal:** Prove the entire system works from browser to database to Warwick and back.
* **Action:** Write Playwright E2E tests.
* **TDD Cycle (The Golden Path):**
  * *Script:*
    1. Start Rust backend & Postgres.
    2. Open React app.
    3. Paste mock cookie.
    4. Create Room "18248".
    5. Click Start.
    6. Assert QR image appears.
    7. Wait for mock TTL to expire.
    8. Assert QR image *changes* (new base64 string).
    9. Click Stop.
    10. Restart backend process.
    11. Reload React app.
    12. Assert Room "18248" is still there, but status is `AUTH_EXPIRED` (because cookie was in memory only).
* **Hardening:**
  * Bind Rust server strictly to `127.0.0.1`.
  * Add `tower-http` CORS and Trace layers.
  * Implement rate-limiting on the `/session-cookie` endpoint to prevent brute-force.

---

### 📂 Project Structure (Rust Workspace)

```text
qr-command-center/
├── crates/
│   ├── core/           # Pure domain logic, TTL math, Auth detection (No IO)
│   ├── db/             # sqlx migrations, Repository traits, Postgres impl
│   ├── warwick/        # reqwest client, wiremock tests
│   └── server/         # axum routes, WS gateway, tokio background workers
├── web/                # React (Vite) frontend
│   ├── src/
│   │   ├── components/ # RoomCard, DisplayView
│   │   ├── hooks/      # useWebSocket, useCountdown
│   │   └── store/      # zustand state
│   └── tests/          # Playwright E2E
├── docker-compose.yml  # Postgres for local dev
└── Cargo.toml          # Workspace root
```

### 🔑 Key Rust Crates to Use
* **`axum`**: For ergonomic, type-safe HTTP and WebSocket routing.
* **`sqlx`**: With the `runtime-tokio-rustls` and `postgres` features. Use `sqlx::migrate!` macro to embed migrations into the binary.
* **`reqwest`**: With `cookies` feature disabled (we manage headers manually for strict control).
* **`wiremock`**: Essential for TDD. Allows you to spin up a mock server in your Rust tests that perfectly mimics Warwick's quirky ASP.NET behaviors.
* **`testcontainers-rs`**: Spins up a real, isolated Postgres Docker container for your `cargo test` suite, ensuring tests are parallelizable and leave no garbage.
* **`chrono`** or **`time`**: For handling the absolute `expiresAt` timestamps safely.

### ⚠️ Critical TDD Edge Cases to Cover
1. **The "Ghost" Room:** What happens if the backend crashes while a fetch is in flight? (Test: Ensure DB transaction rolls back or state defaults to `WARNING` on reboot).
2. **Clock Skew:** What if the operator's local machine clock is 5 minutes behind the server? (Test: Ensure the frontend relies *only* on the `expiresAt` timestamp sent by the server, not `Date.now()` at the time of receipt).
3. **The Infinite Redirect:** What if Warwick returns a 302 loop? (Test: Assert `reqwest` is configured with `redirect(Policy::none())` and the app catches the first 302 as an auth failure).