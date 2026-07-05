package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"qr-command-center/internal/service"
)

type FavouriteRequest struct {
	CourseID string `json:"course_id"`
}

func getFavouritesHandler(svc *service.FavouriteService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ids, err := svc.GetAll(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		if ids == nil {
			ids = []string{}
		}
		writeJSON(w, http.StatusOK, successResponse(map[string][]string{"favourite_ids": ids}))
	}
}

func addFavouriteHandler(svc *service.FavouriteService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req FavouriteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse("invalid request body"))
			return
		}
		if req.CourseID == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse("course_id is required"))
			return
		}
		if err := svc.Add(r.Context(), req.CourseID); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusCreated, successResponse(nil))
	}
}

func removeFavouriteHandler(svc *service.FavouriteService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		courseID := chi.URLParam(r, "courseId")
		if courseID == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse("course_id is required"))
			return
		}
		err := svc.Remove(r.Context(), courseID)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				writeJSON(w, http.StatusNotFound, errorResponse(err.Error()))
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorResponse(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(nil))
	}
}
