package middleware

import (
	"log"
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// RequestLogger logs method, path, status and timing. It leaves out the
// client IP and the query string (which carries the URLs people download),
// so the logs match what the privacy page promises.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		defer func() {
			log.Printf("%s %s - %d %dB in %s", r.Method, r.URL.Path, ww.Status(), ww.BytesWritten(), time.Since(start))
		}()
		next.ServeHTTP(ww, r)
	})
}
