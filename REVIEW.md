---
phase: code-review
reviewed: 2026-05-30T12:00:00Z
depth: deep
files_reviewed: 10
files_reviewed_list:
  - cmd/server/main.go
  - internal/api/routes.go
  - internal/api/handlers.go
  - internal/api/websocket.go
  - internal/db/db.go
  - internal/warwick/auth.go
  - Dockerfile
  - .env
  - web/.env
  - web/vite.config.js
findings:
  critical: 2
  warning: 4
  info: 5
  total: 11
status: issues_found
---

# Code Review: Railway Deployment Plan

**Reviewed:** 2026-05-30T12:00:00Z
**Depth:** deep
**Files Reviewed:** 10
**Status:** issues_found

## Summary

Reviewed proposed Railway deployment changes for Go + React/Vite monorepo (single service with frontend embedded via `embed.FS`). Found **2 CRITICAL** issues that will prevent compilation/deployment, 4 WARNINGs around edge cases in routing and env var handling, and 5 INFO observations. The WebSocket routing is confirmed safe, and the `godotenv.Load()` pattern is safe for Railway containers.

---

## Critical Issues

### CR-01: embed path `web/dist/*` resolves to wrong directory — won't compile

**File:** `internal/api/routes.go` (proposed embedded `spaFallbackHandler`)
**Line:** N/A (proposed change)

**Issue:** The `//go:embed` directive is relative to the source file's directory. From `internal/api/routes.go`, the path `web/dist/*` resolves to `internal/api/web/dist/*`. The actual `web/dist/` directory lives at the **project root** (`./web/dist/`). Go's `embed` package explicitly **forbids `..` in paths**. The result: compilation error `pattern web/dist/*: no matching files found`.

You **cannot** reach `../../web/dist/` from `internal/api/routes.go` — embed does not allow parent-directory traversal.

**Fix — option A (recommended):** Create `web/embed.go` at the project root:

```go
// web/embed.go
package web

import "embed"

//go:embed dist
var Dist embed.FS
```

Then in `routes.go`:

```go
import "qr-command-center/web"

func spaFallbackHandler() http.Handler {
    subFS, err := fs.Sub(web.Dist, "dist")
    if err != nil {
        panic(err) // or handle gracefully
    }
    fileServer := http.FileServer(http.FS(subFS))
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // try exact file first; fall back to index.html
        path := strings.TrimPrefix(r.URL.Path, "/")
        if _, err := fs.Stat(subFS, path); os.IsNotExist(err) {
            r.URL.Path = "/"
            fileServer.ServeHTTP(w, r)
            return
        }
        fileServer.ServeHTTP(w, r)
    })
}
```

**Fix — option B:** Create `internal/static/embed.go` with:
```go
// internal/static/embed.go
package static

import "embed"

//go:embed web/dist
var Dist embed.FS
```
But this **also won't work** — `internal/static/web/dist` still doesn't exist. The embed file **must live at a level where `web/dist/` is a valid relative path** (i.e., the project root). Only option A or a file in the project root directory will work.

---

### CR-02: `//go:embed web/dist/*` does not recurse — asset files missing

**File:** `internal/api/routes.go` (proposed)
**Line:** N/A

**Issue:** Even if the path problem (CR-01) is fixed, `//go:embed web/dist/*` (with `*` glob) only matches files **directly** in `dist/`. The Vite build produces:
```
web/dist/index.html
web/dist/assets/index-bkoxiY8-.js
```

The file `assets/index-bkoxiY8-.js` is **not matched** by `dist/*` — `*` in Go's embed patterns does **not** recurse into subdirectories.

**Fix:** Use `//go:embed dist` (without `/*`) which recursively embeds the entire directory tree. Example:
```go
//go:embed dist
var Dist embed.FS
```

This matches the current DB migration pattern in `internal/db/db.go` (line 16: `//go:embed migrations/*.sql` — although that pattern works because all `.sql` files are **directly** in `migrations/` without subdirectories).

---

## Warnings

### WR-01: PORT env var needs `":"` prefix — `os.Getenv("PORT")` returns `"8080"`, not `":8080"`

**File:** `cmd/server/main.go` (proposed change at line 76)
**Line:** 76

**Issue:** The proposed change is `addr := os.Getenv("PORT")`. Railway injects `PORT` as a bare number string (e.g., `"8080"`). Go's `http.Server.Addr` expects `":8080"` or `"0.0.0.0:8080"`. The server will attempt to listen on `"8080"` which will fail or bind incorrectly.

**Fix:**
```go
port := os.Getenv("PORT")
if port == "" {
    port = os.Getenv("SERVER_ADDR")
    if port == "" {
        port = ":3000"
    }
} else {
    port = ":" + port
}
addr := port
```

Or cleaner with `net.JoinHostPort`:
```go
port := os.Getenv("PORT")
if port == "" {
    addr = os.Getenv("SERVER_ADDR")
    if addr == "" {
        addr = ":3000"
    }
} else {
    addr = net.JoinHostPort("0.0.0.0", port)
}
```

---

### WR-02: Invalid API paths (`/api/foo`) return HTML `index.html`, not JSON 404

**File:** `internal/api/routes.go:71`
**Line:** 71

**Issue:** Chi's `/*` catch-all is registered on line 71 — after all API routes. An unmatched API path like `GET /api/foo` won't match any specific route, so Chi falls through to `/*` and returns `index.html`. API clients receive HTML instead of `{"success": false, "error": "not found"}`.

This is a **pre-existing issue**, not introduced by the deployment changes, but relevant because the proposed `spaFallbackHandler` would preserve this behavior.

**Fix:** Add a catch-all for `/api/` prefix before the SPA handler:
```go
// Return JSON 404 for unmatched API routes
r.Route("/api", func(r chi.Router) {
    r.Handle("/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        writeJSON(w, http.StatusNotFound, errorResponse("endpoint not found"))
    }))
})
// Then the SPA catch-all
r.Handle("/*", spaFallbackHandler())
```

---

### WR-03: CORS `allowedOrigins` hardcoded to localhost — production requests from custom domains will be blocked

**File:** `internal/api/routes.go:32-35`
**Line:** 32-35

**Issue:** The `allowedOrigins` map only contains `localhost:3000` and `localhost:3001`. In production on Railway, the app is served from a Railway domain (e.g., `qr-command-center.up.railway.app`) or a custom domain. The `corsMiddleware` at line 79 checks `allowedOrigins[origin]` — if the origin doesn't match, no `Access-Control-Allow-Origin` header is set. Same-origin requests from the embedded frontend don't need CORS (they're served from the same host:port), so this works. However:
1. If Railway's proxy presents a different origin to the browser
2. If custom domains are configured
3. Any cross-origin scenario will be silently blocked

The proposed `CORS_ORIGIN` env var addition addresses this — but it must be supported in the middleware.

**Fix:** Replace hardcoded map with env-based config:
```go
var allowedOrigins []string

func init() {
    corsOrigin := os.Getenv("CORS_ORIGIN")
    if corsOrigin != "" {
        allowedOrigins = strings.Split(corsOrigin, ",")
    } else {
        allowedOrigins = []string{"http://localhost:3000", "http://localhost:3001"}
    }
}

func corsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        origin := r.Header.Get("Origin")
        allowed := false
        for _, ao := range allowedOrigins {
            if strings.TrimSpace(ao) == origin {
                allowed = true
                break
            }
        }
        if allowed {
            w.Header().Set("Access-Control-Allow-Origin", origin)
            // ... rest of headers
        }
    })
}
```

For production same-origin: no CORS needed at all. The `CORS_ORIGIN` env var should be left **unset** in production, and the middleware should skip header injection entirely when origin is empty (request is same-origin). This is the safest approach.

---

### WR-04: Warwick auth failure at startup prevents container from serving — crash loop during Railway deploy

**File:** `cmd/server/main.go:36-40`
**Line:** 36-40

**Issue:** `main()` calls `auth.GetValidSession()` and `os.Exit(1)` on failure **before** `ListenAndServe`. If Warwick is down during deployment (maintenance, network blip), the container exits immediately. Railway's ALWAYS restart policy will retry, but if Warwick is down for minutes, the deploy enters a crash loop. The healthcheck at `/api/` never runs because the server never starts.

This is acceptable as a design choice (fail-fast on critical external dependency), but the plan should document this risk. Consider a degraded-mode startup where the server starts even if Warwick fails, serving a maintenance page.

---

## Info

### IN-01: `godotenv.Load()` error is discarded — safe for Railway containers

**File:** `cmd/server/main.go:21`
**Line:** 21

**Observation:** `_ = godotenv.Load()` will fail in the Docker container because no `.env` file is present. The error is explicitly discarded with `_`, so the app continues safely. **No action needed.** If the code is refactored later to use `err` instead of `_`, this would break instantly in Railway. Consider a conditional load:
```go
if _, err := os.Stat(".env"); err == nil {
    _ = godotenv.Load()
}
```

---

### IN-02: `EXPOSE 3000` is inconsistent with default `SERVER_ADDR=:3001`

**File:** `Dockerfile:25`
**Line:** 25

**Observation:** The `EXPOSE 3000` directive doesn't match the default `SERVER_ADDR=0.0.0.0:3001` in `.env`. Railway ignores `EXPOSE` (uses `PORT` env var), but this inconsistency can confuse developers. After the PORT change, the default should align.

---

### IN-03: `COPY . .` in Go builder invalidates Docker layer cache on any file change

**File:** `Dockerfile:14`
**Line:** 14

**Observation:** `COPY . .` copies the entire monorepo (including `web/`, `node_modules/`) into the Go builder stage. Any change to any file — frontend or backend — invalidates the Go build cache. This adds 30-60s to every Railway rebuild when only frontend files changed.

**Mitigation:** Add a `.dockerignore` with:
```
node_modules/
web/node_modules/
.env
.git/
```

Or restructure the Go builder to only copy Go source:
```dockerfile
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
# Don't copy web/ — it's not needed for Go build (with embed, it IS needed)
```
(If using embed from root-level file, `web/` must be copied. Use `COPY web/ ./web/` explicitly.)

---

### IN-04: Pre-existing — `dist/assets/` CSS file missing from build output

**File:** `web/dist/index.html:9`
**Line:** 9

**Observation:** The built `dist/index.html` references `<link rel="stylesheet" crossorigin href="/assets/index-BlLwlJQl.css">` but no `.css` file exists in `web/dist/assets/` — only the `.js` bundle is present. And `/vite.svg` (favicon) is also missing. The CSS may be inlined in the JS, but the `index.html` still references the external CSS file path. This is a **pre-existing build issue** that will cause the app to render without styles in production if the CSS truly is external. Verify by checking if `main.jsx` (or any entry) imports `index.css` and whether Vite extracts or inlines it.

This is not specific to Railway, but it will affect the deployed app's appearance.

---

### IN-05: No `public/` directory — `/vite.svg` missing from dist

**File:** `web/dist/index.html:5`
**Line:** 5

**Observation:** The built `index.html` references `href="/vite.svg"` but there is no `web/public/` directory, so Vite doesn't copy this file to `dist/`. The favicon will 404 in production. Minor, but visible in browser tabs.

---

## Routing Analysis Summary

| Concern | Status | Details |
|---------|--------|---------|
| WebSocket `/ws` vs `/*` catch-all | ✅ Safe | Chi registers `/ws` (line 69) before `/*` (line 71). Exact match takes priority. No conflict. |
| API routes vs `/*` catch-all | ⚠️ See WR-02 | Unmatched `/api/foo` falls through to `/*` → returns HTML instead of JSON 404 |
| `/*` catch-all serving index.html | ⚠️ Pre-existing | Works correctly for client-side routing; breaks API error responses |
| Embed.FS with Chi `/*` | 🔴 CR-01/CR-02 | Path resolution fails; glob pattern doesn't recurse |

## Missing from Plan

1. **`.dockerignore`** — Not mentioned. Essential for build cache efficiency on Railway.
2. **Embed file location** — The plan says "Replace `spaFallbackHandler` with `//go:embed`" but doesn't specify *where* the embed file lives. Must be at project root (see CR-01).
3. **Healthcheck timing** — No consideration of Warwick dependency vs healthcheck endpoint timing.
4. **Graceful degradation** — No fallback if Warwick auth fails (app exits vs serves degraded).

---

_Reviewed: 2026-05-30T12:00:00Z_
_Reviewer: code-review-agent (gsd-code-reviewer)_
_Depth: deep_
