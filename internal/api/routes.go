package api

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"qr-command-center/internal/domain"
	"qr-command-center/internal/metrics"
	"qr-command-center/internal/middleware"
	"qr-command-center/internal/service"
)

// RateLimiters holds per-route IP rate limiters created by NewRouter.
// Call Stop() on shutdown to prevent goroutine leaks.
type RateLimiters struct {
	teacher *middleware.IPRateLimiter
	toggle  *middleware.IPRateLimiter
	room    *middleware.IPRateLimiter
}

type RouterOptions struct {
	WSMaxConns       int64
	CORSOrigin       string
	ActivityRecorder service.ActivityRecorder
	EventHub         *service.EventHub
	SnapshotMetadata service.SnapshotMetadataStore
	ScraperRunner    ScraperRunner
	ScraperStatus    ScraperStatusReader
	ScraperHost      string
	ScraperTickLimit int
	ScraperToken     string
}

// Stop shuts down all rate limiter cleanup goroutines.
func (rl *RateLimiters) Stop() {
	rl.teacher.Stop()
	rl.toggle.Stop()
	rl.room.Stop()
}

func NewRouter(rm *service.RoomManager, ts *service.TeacherService, favSvc *service.FavouriteService, viewSvc *service.DashboardViewService, options RouterOptions) (*chi.Mux, *RateLimiters) {
	rl := &RateLimiters{
		teacher: middleware.NewIPRateLimiter(15, 40), // teacher/courses browsing: 15 req/s, burst 40
		toggle:  middleware.NewIPRateLimiter(8, 20),  // POST toggle-checkin: 8 req/s, burst 20
		room:    middleware.NewIPRateLimiter(25, 60), // rooms API: 25 req/s, burst 60
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(redactInternalQueryBeforeLogging)
	r.Use(chimiddleware.Logger)
	r.Use(corsMiddleware(options.CORSOrigin))
	r.Use(gzipMiddleware)

	r.Get("/api", healthHandler())
	r.Get("/api/", healthHandler())

	// Prometheus metrics endpoint.
	metricsHandler := promhttp.Handler()
	if options.ScraperStatus != nil {
		metricsHandler = scraperMetricsHandler(
			options.ScraperStatus,
			options.ScraperHost,
			time.Now,
			metricsHandler,
		)
	}
	r.Handle("/metrics", metricsHandler)

	r.Route("/api/rooms", func(r chi.Router) {
		r.Use(rl.room.Middleware)
		r.Use(admittedActivityMiddleware(options.ActivityRecorder))

		r.Get("/", getRoomsHandler(rm))
		r.Post("/", createRoomHandler(rm))
		r.Post("/from-session", createRoomFromSessionHandler(rm))
		r.Post("/from-session/start", startSessionRoomHandler(rm))
		r.Get("/{id}", getRoomHandler(rm))
		r.Delete("/{id}", deleteRoomHandler(rm))
		r.Post("/{id}/start", startRoomHandler(rm))
		r.Post("/{id}/stop", stopRoomHandler(rm))
	})

	r.Route("/api/teacher", func(r chi.Router) {
		r.Use(rl.teacher.Middleware)
		r.Use(admittedActivityMiddleware(options.ActivityRecorder))

		r.Post("/refresh", refreshAllDataHandler(ts))
		r.Get("/courses", getCoursesHandler(ts))
		r.Get("/courses/{courseId}", getCourseDetailHandler(ts))
		r.Get("/courses/{courseId}/sessions/{sessionId}", getSessionDetailHandler(ts))
		r.With(rl.toggle.Middleware).Post("/courses/{courseId}/sessions/{sessionId}/toggle-checkin", toggleCheckinHandler(ts))
		r.With(rl.toggle.Middleware).Put("/courses/{courseId}/sessions/{sessionId}/students/{studentId}/checkin", idempotentCheckinHandler(ts))
		r.Get("/courses/{courseId}/attendance-report", getCourseAttendanceReportHandler(ts))
		r.Post("/courses/{courseId}/attendance-report/export", attendanceExportHandler(ts))
		r.Post("/courses/attendance-batch", getBatchAttendanceHandler(ts))

		// Cross-course absence dashboard
		r.Get("/absence-dashboard", getAbsenceDashboardHandler(ts))

		r.Get("/favourites", getFavouritesHandler(favSvc))
		r.Post("/favourites", addFavouriteHandler(favSvc))
		r.Delete("/favourites/{courseId}", removeFavouriteHandler(favSvc))

		// Dashboard saved views
		r.Get("/dashboard-views", listDashboardViewsHandler(viewSvc))
		r.Post("/dashboard-views", createDashboardViewHandler(viewSvc))
		r.Get("/dashboard-views/{id}", getDashboardViewHandler(viewSvc))
		r.Put("/dashboard-views/{id}", updateDashboardViewHandler(viewSvc))
		r.Delete("/dashboard-views/{id}", deleteDashboardViewHandler(viewSvc))
		r.Post("/dashboard-views/{id}/use", touchDashboardViewHandler(viewSvc))
	})

	if options.ScraperRunner != nil &&
		options.ScraperStatus != nil &&
		options.ScraperToken != "" {
		r.Post(
			"/api/internal/scraper/tick",
			scraperTickHandler(
				options.ScraperRunner,
				options.ScraperTickLimit,
				options.ScraperToken,
			),
		)
		r.Get(
			"/api/internal/scraper/status",
			scraperStatusHandler(
				options.ScraperStatus,
				options.ScraperHost,
				options.ScraperToken,
				time.Now,
			),
		)
	}

	eventHub := options.EventHub
	if eventHub == nil && rm != nil {
		eventHub = rm.EventHub()
	}
	websocketHandler := wsHandlerWithSnapshots(
		rm,
		eventHub,
		options.SnapshotMetadata,
		options.WSMaxConns,
		options.ActivityRecorder,
	)
	r.Get("/ws", websocketHandler)
	r.Get("/ws/", websocketHandler)

	r.Handle("/*", spaFallbackHandler(filepath.Join("web", "dist")))

	return r, rl
}

func redactInternalQueryBeforeLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !strings.HasPrefix(request.URL.Path, "/api/internal/") ||
			request.URL.RawQuery == "" {
			next.ServeHTTP(w, request)
			return
		}
		sanitized := request.Clone(request.Context())
		sanitized.URL.RawQuery = ""
		sanitized.URL.ForceQuery = false
		sanitized.RequestURI = sanitized.URL.RequestURI()
		next.ServeHTTP(w, sanitized)
	})
}

type responseStatusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *responseStatusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseStatusRecorder) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(payload)
	w.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer, so
// Flusher/Hijacker assertions still work through this wrapper.
func (w *responseStatusRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// routeClass maps a request path to a bounded, low-cardinality metric label.
// Room IDs, course IDs, student IDs, and session IDs never appear in labels.
func routeClass(path string) string {
	const roomsPrefix = "/api/rooms"
	switch {
	case path == roomsPrefix || path == roomsPrefix+"/":
		return "rooms_list"
	case strings.HasPrefix(path, roomsPrefix+"/"):
		rest := strings.TrimPrefix(path, roomsPrefix+"/")
		if !strings.Contains(rest, "/") {
			return "room_detail"
		}
		return "rooms_other"
	case strings.HasPrefix(path, "/api/teacher"):
		return "teacher"
	default:
		return "other"
	}
}

func admittedActivityMiddleware(recorder service.ActivityRecorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			statusWriter := &responseStatusRecorder{ResponseWriter: w}
			var lease service.ActivityLease
			if tracker, ok := recorder.(service.ActivityTracker); ok {
				lease = tracker.BeginActivity()
			}
			if lease != nil {
				defer func() {
					status := statusWriter.status
					if status == 0 {
						status = http.StatusOK
					}
					lease.Finish(status >= 200 && status < 400)
				}()
			}
			next.ServeHTTP(statusWriter, r)
			// Recorded in a defer so a handler panic still accounts for the
			// bytes written before it unwound.
			defer func() {
				status := statusWriter.status
				if status == 0 {
					status = http.StatusOK
				}
				success := status >= 200 && status < 400
				if recorder != nil && lease == nil && success {
					recorder.RecordActivity()
				}
				metrics.HTTPResponseBytesTotal.
					WithLabelValues(routeClass(r.URL.Path)).
					Add(float64(statusWriter.bytes))
			}()
		})
	}
}

// gzipMiddleware compresses responses when the client advertises gzip support
// and the response Content-Type is compressible. Compression starts lazily on
// the first body write so handler-set headers are honored, and informational
// statuses (incl. the 101 WebSocket upgrade), 204/304, existing
// Content-Encoding, and empty responses pass through untouched.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(strings.ToLower(r.Header.Get("Accept-Encoding")), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gz := &gzipResponseWriter{ResponseWriter: w}
		defer gz.close()
		next.ServeHTTP(gz, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	status      int
	headerSent  bool
	passthrough bool
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if w.headerSent {
		return
	}
	w.status = status
	// Informational (incl. 101 WebSocket upgrade), 204, and 304 responses
	// never carry a compressible body; send them through untouched.
	if status < http.StatusOK ||
		status == http.StatusNoContent ||
		status == http.StatusNotModified ||
		w.Header().Get("Content-Encoding") != "" {
		w.passthrough = true
		w.forwardHeader()
	}
	// Body-producing statuses are staged until the first Write so empty-body
	// responses are never wrapped in gzip.
}

func (w *gzipResponseWriter) forwardHeader() {
	if w.headerSent {
		return
	}
	w.headerSent = true
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(payload []byte) (int, error) {
	if w.passthrough {
		return w.ResponseWriter.Write(payload)
	}
	if w.gz == nil {
		w.startGzip(payload)
	}
	if w.passthrough {
		return w.ResponseWriter.Write(payload)
	}
	return w.gz.Write(payload)
}

func (w *gzipResponseWriter) startGzip(payload []byte) {
	contentType := w.Header().Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(payload)
	}
	if !compressibleContentType(contentType) {
		w.passthrough = true
		w.forwardHeader()
		return
	}
	w.Header().Del("Content-Length")
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Add("Vary", "Accept-Encoding")
	w.gz = gzip.NewWriter(w.ResponseWriter)
	w.forwardHeader()
}

// Flush forwards flushes through the gzip stream and the underlying writer so
// streaming handlers keep working.
func (w *gzipResponseWriter) Flush() {
	if w.gz != nil {
		w.gz.Flush()
	} else if !w.passthrough && !w.headerSent {
		// Headers are being flushed before any body bytes: compressing now
		// would violate header ordering, so serve this response uncompressed.
		w.passthrough = true
		w.forwardHeader()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack passes the connection through so WebSocket upgrades keep working.
func (w *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("underlying ResponseWriter does not implement http.Hijacker")
	}
	return hj.Hijack()
}

func (w *gzipResponseWriter) close() {
	if w.gz != nil {
		_ = w.gz.Close()
		return
	}
	if !w.headerSent {
		w.forwardHeader()
	}
}

// compressibleContentType reports whether a response Content-Type is worth
// gzip compression.
func compressibleContentType(contentType string) bool {
	mimeType := contentType
	if idx := strings.IndexByte(mimeType, ';'); idx != -1 {
		mimeType = mimeType[:idx]
	}
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	switch mimeType {
	case "application/json",
		"application/javascript",
		"text/javascript",
		"text/css",
		"text/html",
		"application/openmetrics-text":
		return true
	}
	return strings.HasPrefix(mimeType, "text/")
}

func corsMiddleware(corsOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/internal/") {
				next.ServeHTTP(w, r)
				return
			}
			if corsOrigin == "" {
				next.ServeHTTP(w, r)
				return
			}
			origin := r.Header.Get("Origin")
			if corsOrigin == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if origin == corsOrigin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			} else {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "86400")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type healthResponse struct {
	Message string `json:"message"`
}

func healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, successResponse(healthResponse{
			Message: "QR Command Center API is running!",
		}))
	}
}

func spaFallbackHandler(distDir string) http.Handler {
	indexFile := filepath.Join(distDir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := filepath.Clean(r.URL.Path)

		// Vite emits content-hashed build assets: safe to cache forever as
		// immutable. A missing asset means a stale index.html referenced a chunk
		// from an older deploy — return a real 404 instead of SPA-fallback HTML,
		// which would be served as text/html and MIME-block the module script.
		if strings.HasPrefix(cleanPath, "/assets/") {
			if _, err := os.Stat(filepath.Join(distDir, cleanPath)); os.IsNotExist(err) {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			http.FileServer(http.Dir(distDir)).ServeHTTP(w, r)
			return
		}

		// App shell (root or a client-side route): never cache, so a deploy
		// always reaches users with the newest asset manifest. Heuristic caching
		// of index.html is what left browsers on stale bundles.
		if cleanPath == "/" {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, indexFile)
			return
		}
		if _, err := os.Stat(filepath.Join(distDir, cleanPath)); os.IsNotExist(err) {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, indexFile)
			return
		}
		http.FileServer(http.Dir(distDir)).ServeHTTP(w, r)
	})
}

func getRoomsHandler(rm *service.RoomManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lite := r.URL.Query().Get("lite")
		rooms := rm.GetAllRooms()

		if lite == "true" || lite == "1" {
			writeJSON(w, http.StatusOK, successResponse(domain.RoomsToLite(rooms)))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(rooms))
	}
}

func createRoomHandler(rm *service.RoomManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ClassID string  `json:"class_id"`
			Name    *string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid request body"))
			return
		}
		if req.ClassID == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse("class_id is required"))
			return
		}
		room, err := rm.CreateRoom(uuid.New().String(), req.ClassID, req.Name)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(room))
	}
}

func getRoomHandler(rm *service.RoomManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		room := rm.GetRoom(id)
		if room == nil {
			writeJSON(w, http.StatusNotFound, errorResponse("Room not found"))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(room))
	}
}

func deleteRoomHandler(rm *service.RoomManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := rm.DeleteRoom(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(nil))
	}
}

func startRoomHandler(rm *service.RoomManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := rm.StartRoom(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(nil))
	}
}

func stopRoomHandler(rm *service.RoomManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := rm.StopRoom(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(nil))
	}
}

func createRoomFromSessionHandler(rm *service.RoomManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid request body"))
			return
		}
		if req.SessionID == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse("session_id is required"))
			return
		}
		room, err := rm.CreateRoom(req.SessionID, req.SessionID, nil)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(room))
	}
}

// startSessionRoomHandler combines find-or-create, start, and first QR fetch
// into one idempotent operation. It returns the full room with a valid QR URL
// when available, or 202 Accepted with a retry hint while the QR is being
// generated.
func startSessionRoomHandler(rm *service.RoomManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SessionID string `json:"session_id"`
			CourseID  string `json:"course_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid request body"))
			return
		}
		if req.SessionID == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse("session_id is required"))
			return
		}
		room, err := rm.EnsureSessionRoom(req.SessionID, req.CourseID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		if room.QRURL != nil && *room.QRURL != "" &&
			room.ExpiresAt != nil && room.ExpiresAt.After(time.Now()) {
			writeJSON(w, http.StatusOK, successResponse(room))
			return
		}
		writeJSON(w, http.StatusAccepted, successResponse(map[string]any{
			"status":         "starting",
			"room_id":        room.RoomID,
			"retry_after_ms": 500,
		}))
	}
}
