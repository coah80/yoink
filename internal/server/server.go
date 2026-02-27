package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/middleware"
	"github.com/coah80/yoink/internal/routes"
	"github.com/coah80/yoink/internal/util"
)

func New() *http.Server {
	r := chi.NewRouter()

	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(securityHeaders)
	r.Use(middleware.LoadCORS())
	r.Use(middleware.RateLimit)

	routes.CoreRoutes(r)
	routes.DownloadRoutes(r)
	routes.PlaylistRoutes(r)
	routes.ConvertRoutes(r)
	routes.GalleryRoutes(r)
	routes.TranscribeRoutes(r)
	routes.BotRoutes(r)

	publicDir := filepath.Join(filepath.Dir(os.Args[0]), "public")
	if info, err := os.Stat(publicDir); err == nil && info.IsDir() {
		fileServer := http.FileServer(http.Dir(publicDir))
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			cleaned := filepath.Clean(filepath.Join(publicDir, strings.TrimPrefix(r.URL.Path, "/")))
			if !strings.HasPrefix(cleaned, publicDir) {
				http.NotFound(w, r)
				return
			}
			if _, err := os.Stat(cleaned); os.IsNotExist(err) {
				http.ServeFile(w, r, filepath.Join(publicDir, "index.html"))
				return
			}
			fileServer.ServeHTTP(w, r)
		})
	}

	return &http.Server{
		Addr:              ":" + config.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func EnsureTempDirs() {
	util.ClearTempDir()
}

func PrintBanner() {
	fmt.Printf(`
  ┌──────────────────────────────────┐
  │         yoink-go %s          │
  │    media download api server     │
  └──────────────────────────────────┘
`, padVersion(config.Version))
}

func padVersion(v string) string {
	for len(v) < 10 {
		v += " "
	}
	return v
}
