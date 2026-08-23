package api

import (
	"net/http"

	"qr-command-center/internal/service"
)

func refreshAllDataHandler(ts *service.TeacherService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := ts.RefreshAllData(r.Context())
		if err != nil {
			if mapServiceError(w, err) {
				return
			}
			writeJSON(w, http.StatusServiceUnavailable, errorResponse("data refresh failed"))
			return
		}
		writeJSON(w, http.StatusOK, successResponse(result))
	}
}
