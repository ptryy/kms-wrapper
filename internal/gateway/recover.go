package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
)

// recoverPanic catches any panic raised by a downstream handler, logs it
// with the request ID in scope, increments kms_panics_total, and emits a
// canonical HTTP 500 JSON envelope containing the request ID so the caller
// can report it back to operators. The process keeps running — a panic in
// one handler must not take down the listener.
//
// The middleware is mounted **immediately inside** requestID so that the
// request context already carries the ID by the time the recover fires.
func recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				stack := string(debug.Stack())
				slog.ErrorContext(r.Context(), "panic in handler",
					"panic", fmt.Sprintf("%v", rec),
					"stack", stack,
				)
				kmsPanicsTotal.WithLabelValues(r.URL.Path).Inc()
				body, _ := json.Marshal(map[string]string{
					"error":      "internal server error",
					"request_id": RequestIDFromContext(r.Context()),
				})
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write(body)
				_, _ = w.Write([]byte{'\n'})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
