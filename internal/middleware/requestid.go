package middleware

import (
	"log/slog"
	"net/http"
)

// RequestID echoes the inbound X-Request-ID header onto the response so a
// client-generated correlation ID survives the round trip, and logs it with
// the request context. Requests without the header pass through untouched.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestID := r.Header.Get("X-Request-ID"); requestID != "" {
			w.Header().Set("X-Request-ID", requestID)
			slog.Debug("request", "request_id", requestID, "method", r.Method, "path", r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}
