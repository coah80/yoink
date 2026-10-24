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
)

func New() *http.Server {
	r := chi.NewRouter()

	r.Use(middleware.RealIP)
	r.Use(middleware.RequestLogger)
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
	routes.StemsRoutes(r)
	routes.BotRoutes(r)

	publicDir := filepath.Join(filepath.Dir(os.Args[0]), "public")
	if _, err := os.Stat(publicDir); os.IsNotExist(err) {
		publicDir = filepath.Join("frontend", "public")
	}
	if info, err := os.Stat(publicDir); err == nil && info.IsDir() {
		registerStaticRoutes(r, publicDir)
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

func spaHandler(publicDir string) http.HandlerFunc {
	root, _ := filepath.Abs(publicDir)
	fileServer := http.FileServer(http.Dir(root))
	return func(w http.ResponseWriter, r *http.Request) {
		cleaned := filepath.Clean(filepath.Join(root, strings.TrimPrefix(r.URL.Path, "/")))
		relative, err := filepath.Rel(root, cleaned)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			http.NotFound(w, r)
			return
		}
		if info, err := os.Stat(cleaned); err == nil && !info.IsDir() {
			if relative == "index.html" || relative == "sw.js" {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") || filepath.Ext(r.URL.Path) != "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
	}
}

func registerStaticRoutes(router chi.Router, publicDir string) {
	handler := spaHandler(publicDir)
	router.Get("/*", handler)
	router.Head("/*", handler)
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
	for _, dir := range config.TempDirs {
		_ = os.MkdirAll(dir, 0o755)
	}
	fmt.Println("✓ Temp directories ready")
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
