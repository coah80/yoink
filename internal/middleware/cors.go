package middleware

import (
	"bufio"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/cors"
)

func LoadCORS() func(http.Handler) http.Handler {
	origins := loadCORSOrigins()

	if len(origins) > 0 {
		log.Printf("✓ Loaded %d CORS origins from cors-origins.txt", len(origins))
		return cors.Handler(cors.Options{
			AllowedOrigins:   origins,
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"*"},
			AllowCredentials: true,
			MaxAge:           86400,
		})
	}

	log.Println("[CORS] WARNING: No cors-origins.txt found, allowing all origins (credentials disabled). Create cors-origins.txt to restrict.")
	return cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: false,
		MaxAge:           86400,
	})
}

func loadCORSOrigins() []string {
	f, err := os.Open("cors-origins.txt")
	if err != nil {
		return nil
	}
	defer f.Close()

	var origins []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			origins = append(origins, line)
		}
	}
	return origins
}
