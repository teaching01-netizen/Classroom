package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"qr-command-center/internal/service"
)

func NewRouter(rm *service.RoomManager) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(corsMiddleware)

	r.Get("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, successResponse(map[string]string{
			"message": "QR Command Center API is running!",
		}))
	})

	r.Route("/api/rooms", func(r chi.Router) {
		r.Get("/", getRoomsHandler(rm))
		r.Post("/", createRoomHandler(rm))
		r.Get("/{id}", getRoomHandler(rm))
		r.Delete("/{id}", deleteRoomHandler(rm))
		r.Post("/{id}/start", startRoomHandler(rm))
		r.Post("/{id}/stop", stopRoomHandler(rm))
	})

	r.Get("/ws", wsHandler(rm))

	r.Handle("/*", spaFallbackHandler())

	return r
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
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
		room, err := rm.CreateRoom(req.ClassID, req.Name)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(room))
	}
}

func getRoomHandler(rm *service.RoomManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid room id"))
			return
		}
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
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid room id"))
			return
		}
		if err := rm.DeleteRoom(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(nil))
	}
}

func startRoomHandler(rm *service.RoomManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid room id"))
			return
		}
		if err := rm.StartRoom(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(nil))
	}
}

func stopRoomHandler(rm *service.RoomManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid room id"))
			return
		}
		if err := rm.StopRoom(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(nil))
	}
}

func wsHandler(rm *service.RoomManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "WebSocket not implemented", http.StatusNotImplemented)
	}
}
