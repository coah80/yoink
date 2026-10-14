package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/coah80/yoink/internal/alerts"
	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/middleware"
	"github.com/coah80/yoink/internal/routes"
	"github.com/coah80/yoink/internal/server"
	"github.com/coah80/yoink/internal/services"
	"github.com/coah80/yoink/internal/util"
)

func main() {
	godotenv.Load()
	config.Load()
	util.InitCookiePaths()
	routes.InitTranscription()

	util.OnCookieRefreshNeeded = alerts.CookieIssue
	util.OnSessionTokenFailed = alerts.SessionTokenFailed
	util.OnSessionTokenRecovered = alerts.SessionTokenRecovered

	server.PrintBanner()
	server.EnsureTempDirs()

	util.StartCleanupInterval(services.Global.KeepTempFile)
	middleware.StartRateLimitCleanup()
	routes.StartChunkedUploadCleanup()
	routes.StartBotDownloadExpiry()
	routes.StartPlaylistDownloadExpiry()
	services.Global.StartSessionCleanup(util.CleanupJobFiles)
	services.Global.StartCounterReconciliation()
	services.Global.StartAsyncJobExpiry()
	if os.Getenv("SESSION_GENERATOR_URL") != "" {
		util.StartSessionTokenRefresh()
	}

	srv := server.New()
	go func() {
		log.Printf("listening on :%s", config.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()
	alerts.ServerStarted()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down...")
	alerts.ServerStopping()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
