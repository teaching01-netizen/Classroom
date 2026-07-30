package api

import (
	"encoding/json"
	"net/http"
	"time"

	"qr-command-center/internal/domain"
)

type SnapshotVersionInfo struct {
	Version     int64  `json:"version"`
	GeneratedAt string `json:"generatedAt"`
}

type ApiResponse struct {
	Success  bool                `json:"success"`
	Data     interface{}         `json:"data"`
	Error    string              `json:"error"`
	Snapshot *SnapshotVersionInfo `json:"snapshot,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, resp ApiResponse) {
	w.Header().Set("Content-Type", "application/json")
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	}
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

func successResponse(data interface{}) ApiResponse {
	return ApiResponse{Success: true, Data: data}
}

func versionedResponse(data interface{}, metadata domain.SnapshotMetadata) ApiResponse {
	return ApiResponse{
		Success: true,
		Data:    data,
		Snapshot: &SnapshotVersionInfo{
			Version:     metadata.Version,
			GeneratedAt: metadata.ValidatedAt.UTC().Format(time.RFC3339),
		},
	}
}

func errorResponse(msg string) ApiResponse {
	return ApiResponse{Success: false, Error: msg}
}
