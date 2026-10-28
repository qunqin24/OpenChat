package httpx

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/qunqin24/openchat/apps/server/internal/logging"
)

// RequestLogger logs one structured line per request. Streaming responses are
// logged when the stream closes, so `duration` covers the whole stream.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		// Every line logged with this request's context carries its id — and
		// the user once the session is read (auth annotates it) — so one id
		// finds everything the request caused.
		id := middleware.GetReqID(r.Context())
		r = r.WithContext(logging.WithFields(r.Context(), slog.String("request_id", id)))
		// And the response says it, so the page can show it with an error for
		// the person to quote (chi's RequestID only reads the header).
		if id != "" {
			w.Header().Set("X-Request-Id", id)
		}

		// Only the completion line is logged at Info, which makes a request that
		// never finishes — hung, or an SSE stream still open — invisible. With
		// debug on there is a line when it arrives too, so an unanswered request
		// is one you can see rather than one you infer from silence.
		slog.DebugContext(r.Context(), "http request started",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"remote", r.RemoteAddr,
			"user_agent", r.UserAgent(),
			"request_id", middleware.GetReqID(r.Context()),
		)

		next.ServeHTTP(ww, r)

		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
		}
		// The conversation the request was about, from the route it matched.
		if rc := chi.RouteContext(r.Context()); rc != nil {
			if id := rc.URLParam("conversationId"); id != "" {
				attrs = append(attrs, "conversation_id", id)
			}
		}
		slog.InfoContext(r.Context(), "http request", attrs...)
	})
}
