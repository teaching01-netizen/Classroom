package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"qr-command-center/internal/middleware"
	"qr-command-center/internal/service"
	"qr-command-center/internal/warwick"
)

var (
	teacherLimiter = middleware.NewIPRateLimiter(5, 10)   // teacher/courses browsing: 5 req/s, burst 10
	toggleLimiter  = middleware.NewIPRateLimiter(2, 3)    // POST toggle-checkin: 2 req/s, burst 3
	roomLimiter    = middleware.NewIPRateLimiter(10, 20)  // rooms API: 10 req/s, burst 20
)

var allowedOrigins = map[string]bool{
	"http://localhost:3001": true,
	"http://localhost:3000": true,
}

func NewRouter(rm *service.RoomManager, cc *warwick.ClassroomClient) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(corsMiddleware)

	r.Get("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, successResponse(map[string]string{
			"message": "QR Command Center API is running!",
		}))
	})

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
	})

	r.Get("/ws", wsHandler(rm))

	r.Handle("/*", spaFallbackHandler())

	return r
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == "OPTIONS" {
			if allowedOrigins[origin] {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusNoContent)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
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


