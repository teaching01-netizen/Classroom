package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"qr-command-center/internal/cache"
	"qr-command-center/internal/db"
	"qr-command-center/internal/middleware"
	"qr-command-center/internal/service"
	"qr-command-center/internal/warwick"
)

var (
	teacherLimiter = middleware.NewIPRateLimiter(5, 10)   // teacher/courses browsing: 5 req/s, burst 10
	toggleLimiter  = middleware.NewIPRateLimiter(2, 3)    // POST toggle-checkin: 2 req/s, burst 3
	roomLimiter    = middleware.NewIPRateLimiter(10, 20)  // rooms API: 10 req/s, burst 20
)

// StopRateLimiters stops all package-level rate limiter goroutines.
// Must be called during server shutdown to prevent goroutine leaks.
func StopRateLimiters() {
	teacherLimiter.Stop()
	toggleLimiter.Stop()
	roomLimiter.Stop()
}

var allowedOrigin string

func init() {
	allowedOrigin = os.Getenv("CORS_ORIGIN")
}

func NewRouter(rm *service.RoomManager, cc *warwick.ClassroomClient, favRepo db.FavouriteRepository, c *cache.Cache, refresher *service.DataRefresher) *chi.Mux {
	r := chi.NewRouter()
	r.Use(chimiddleware.Logger)
	r.Use(corsMiddleware)

	r.Get("/api", healthHandler(c, refresher))
	r.Get("/api/", healthHandler(c, refresher))

	r.Route("/api/rooms", func(r chi.Router) {
		r.Use(roomLimiter.Middleware)

		r.Get("/", getRoomsHandler(rm))
		r.Post("/", createRoomHandler(rm))
		r.Post("/from-session", createRoomFromSessionHandler(rm))
		r.Get("/{id}", getRoomHandler(rm))
		r.Delete("/{id}", deleteRoomHandler(rm))
		r.Post("/{id}/start", startRoomHandler(rm))
		r.Post("/{id}/stop", stopRoomHandler(rm))
	})

	r.Route("/api/teacher", func(r chi.Router) {
		r.Use(teacherLimiter.Middleware)

		r.Get("/courses", getCoursesHandler(cc))
		r.Get("/courses/{courseId}", getCourseDetailHandler(cc))
		r.Get("/courses/{courseId}/sessions/{sessionId}", getSessionDetailHandler(cc))
		r.With(toggleLimiter.Middleware).Post("/courses/{courseId}/sessions/{sessionId}/toggle-checkin", toggleCheckinHandler(cc))

		r.Get("/favourites", getFavouritesHandler(favRepo))
		r.Post("/favourites", addFavouriteHandler(favRepo))
		r.Delete("/favourites/{courseId}", removeFavouriteHandler(favRepo))
	})

	r.Get("/ws", wsHandler(rm))
	r.Get("/ws/", wsHandler(rm))

	r.Handle("/*", spaFallbackHandler())

	return r
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowedOrigin == "" {
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if allowedOrigin == "*" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else if origin == allowedOrigin {
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

func healthHandler(c *cache.Cache, refresher *service.DataRefresher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cacheSize := 0
		cacheWarm := false
		if c != nil {
			cacheSize = c.Size()
		}
		if refresher != nil {
			cacheWarm = refresher.IsWarm()
		}
		writeJSON(w, http.StatusOK, successResponse(map[string]interface{}{
			"message": "QR Command Center API is running!",
			"cache": map[string]interface{}{
				"size": cacheSize,
				"warm": cacheWarm,
			},
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
		rooms := rm.GetAllRooms()
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


