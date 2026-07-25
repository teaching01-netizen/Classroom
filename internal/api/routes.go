package api

import (
	"encoding/json"
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
		teacher: middleware.NewIPRateLimiter(5, 10),  // teacher/courses browsing: 5 req/s, burst 10
		toggle:  middleware.NewIPRateLimiter(2, 3),   // POST toggle-checkin: 2 req/s, burst 3
		room:    middleware.NewIPRateLimiter(10, 20), // rooms API: 10 req/s, burst 20
	}

	r := chi.NewRouter()
	r.Use(chimiddleware.Logger)
	r.Use(corsMiddleware(options.CORSOrigin))

	r.Get("/api", healthHandler())
	r.Get("/api/", healthHandler())

	// Prometheus metrics endpoint.
	r.Handle("/metrics", promhttp.Handler())

	r.Route("/api/rooms", func(r chi.Router) {
		r.Use(rl.room.Middleware)
		r.Use(admittedActivityMiddleware(options.ActivityRecorder))

		r.Get("/", getRoomsHandler(rm))
		r.Post("/", createRoomHandler(rm))
		r.Post("/from-session", createRoomFromSessionHandler(rm))
		r.Get("/{id}", getRoomHandler(rm))
		r.Delete("/{id}", deleteRoomHandler(rm))
		r.Post("/{id}/start", startRoomHandler(rm))
		r.Post("/{id}/stop", stopRoomHandler(rm))
	})

	r.Route("/api/teacher", func(r chi.Router) {
		r.Use(rl.teacher.Middleware)
		r.Use(admittedActivityMiddleware(options.ActivityRecorder))

		r.Get("/courses", getCoursesHandler(ts))
		r.Get("/courses/{courseId}", getCourseDetailHandler(ts))
		r.Get("/courses/{courseId}/sessions/{sessionId}", getSessionDetailHandler(ts))
		r.With(rl.toggle.Middleware).Post("/courses/{courseId}/sessions/{sessionId}/toggle-checkin", toggleCheckinHandler(ts))
		r.Get("/courses/{courseId}/attendance-report", getCourseAttendanceReportHandler(ts))
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

	r.Handle("/*", spaFallbackHandler())

	return r, rl
}

type responseStatusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *responseStatusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseStatusRecorder) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(payload)
}

func admittedActivityMiddleware(recorder service.ActivityRecorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if recorder == nil {
				next.ServeHTTP(w, r)
				return
			}
			var lease service.ActivityLease
			if tracker, ok := recorder.(service.ActivityTracker); ok {
				lease = tracker.BeginActivity()
			}
			statusWriter := &responseStatusRecorder{ResponseWriter: w}
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
			status := statusWriter.status
			if status == 0 {
				status = http.StatusOK
			}
			success := status >= 200 && status < 400
			if lease == nil && success {
				recorder.RecordActivity()
			}
		})
	}
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
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
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

func spaFallbackHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join("web", "dist", r.URL.Path)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			http.ServeFile(w, r, filepath.Join("web", "dist", "index.html"))
			return
		}
		http.FileServer(http.Dir(filepath.Join("web", "dist"))).ServeHTTP(w, r)
	})
}

func getRoomsHandler(rm *service.RoomManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lite := r.URL.Query().Get("lite")
		rooms := rm.GetAllRooms()

		if lite == "true" || lite == "1" {
			liteRooms := make([]domain.RoomLite, 0, len(rooms))
			for _, room := range rooms {
				liteRooms = append(liteRooms, domain.RoomLite{
					RoomID:    room.RoomID,
					ClassID:   room.ClassID,
					Name:      room.Name,
					Status:    room.Status,
					QRURL:     room.QRURL,
					ExpiresAt: room.ExpiresAt,
				})
			}
			writeJSON(w, http.StatusOK, successResponse(liteRooms))
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
