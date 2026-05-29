package api

import (
	"encoding/json"
	"net/http"
)

type ApiResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
	Error   string      `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, resp ApiResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

func successResponse(data interface{}) ApiResponse {
	return ApiResponse{Success: true, Data: data}
}

func errorResponse(msg string) ApiResponse {
	return ApiResponse{Success: false, Error: msg}
}
