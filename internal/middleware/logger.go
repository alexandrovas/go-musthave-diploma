package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// responseWriter оборачивает http.ResponseWriter, чтобы перехватить статус-код
// и размер тела ответа
type responseWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	size, err := rw.ResponseWriter.Write(b)
	rw.size += size
	return size, err
}

// Logger возвращает middleware, логирующую каждый HTTP-запрос
func Logger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rw, r)

			log := logger.With(
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"duration", time.Since(start),
				"size", rw.size)

			switch {
			case rw.status >= 400 && rw.status < 500:
				log.Debug("request")
			case rw.status >= 500:
				log.Error("request")
			default:
				log.Info("request")
			}
		})
	}
}
