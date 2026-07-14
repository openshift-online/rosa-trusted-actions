package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/sirupsen/logrus"
)

// NewLogger creates a new structured logging middleware
func NewLogger(logger *logrus.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			defer func() {
				duration := time.Since(start)

				logger.WithFields(logrus.Fields{
					"method":        r.Method,
					"uri":           r.RequestURI,
					"status":        ww.Status(),
					"bytes":         ww.BytesWritten(),
					"duration_ms":   duration.Milliseconds(),
					"user_agent":    r.UserAgent(),
					"remote_addr":   r.RemoteAddr,
					"request_id":    r.Context().Value("request_id"),
				}).Info("Request completed")
			}()

			next.ServeHTTP(ww, r)
		})
	}
}
