package api

import (
	"encoding/json"
	"net/http"
	"time"

	"qr-command-center/internal/domain"
)

type SnapshotVersionInfo struct {
	Version       int64                  `json:"version"`
	GeneratedAt   string                 `json:"generatedAt"`
	Quality       domain.DataQualityState `json:"quality,omitempty"`
	Complete      bool                   `json:"complete,omitempty"`
	ParserVersion string                 `json:"parserVersion,omitempty"`
	Stale         bool                   `json:"stale,omitempty"`
	AgeSeconds    int64                  `json:"ageSeconds,omitempty"`
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
	now := time.Now().UTC()
	ageSeconds := int64(0)
	if !metadata.ValidatedAt.IsZero() {
		ageSeconds = int64(now.Sub(metadata.ValidatedAt).Seconds())
		if ageSeconds < 0 {
			ageSeconds = 0
		}
	}
	return ApiResponse{
		Success: true,
		Data:    data,
		Snapshot: &SnapshotVersionInfo{
			Version:       metadata.Version,
			GeneratedAt:   metadata.ValidatedAt.UTC().Format(time.RFC3339),
			Quality:       metadata.QualityState,
			Complete:      metadata.Complete,
			ParserVersion: metadata.ParserVersion,
			Stale:         metadata.Stale,
			AgeSeconds:    ageSeconds,
		},
	}
}

func errorResponse(msg string) ApiResponse {
	return ApiResponse{Success: false, Error: msg}
}
