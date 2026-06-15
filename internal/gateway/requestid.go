package gateway

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/google/uuid"
)

type requestIDKey struct{}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// RequestIDFromContext returns the canonical request ID stored by the
// requestID middleware, or "" if none has been attached (typically because
// the middleware was not in the chain).
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

// requestID is the outermost middleware: it normalises the inbound
// `X-Request-ID` (replacing anything that fails validation with a fresh
// UUIDv4), echoes it back via the response header, and stores it in the
// request context for downstream handlers/logging.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || !requestIDPattern.MatchString(id) {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requestIDLogHandler wraps an slog.Handler so that any context-aware log
// call (slog.*Context) automatically picks up the request ID from the
// request context and emits it as a `request_id=<id>` field.
type requestIDLogHandler struct{ slog.Handler }

// NewRequestIDLogHandler wraps h so every log record handled with a context
// gets a `request_id` attribute (if the context carries one).
func NewRequestIDLogHandler(h slog.Handler) slog.Handler {
	return requestIDLogHandler{Handler: h}
}

func (h requestIDLogHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestIDFromContext(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h requestIDLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return requestIDLogHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h requestIDLogHandler) WithGroup(name string) slog.Handler {
	return requestIDLogHandler{Handler: h.Handler.WithGroup(name)}
}
