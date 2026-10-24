package main

import (
	"fmt"
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
	services.InitStems()

	util.OnCookieRefreshNeeded = alerts.CookieIssue
	util.OnSessionTokenFailed = alerts.SessionTokenFailed
	util.OnSessionTokenRecovered = alerts.SessionTokenRecovered

	util.CheckDependencies()
	if services.StemsAvailable() {
		fmt.Printf("✓ demucs available: %s (own cgroup: %t)\n", services.StemsPython, services.StemsSandboxed())
	} else {
		fmt.Println("- demucs not found (optional, needed for vocal stems)")
	}
	if config.RequireTempMount {
		if err := util.EnsureTempDirMounted(); err != nil {
			log.Fatal(err)
		}
	}
	server.EnsureTempDirs()
	loadedJobs, err := services.Global.LoadPendingJobs()
	if err != nil {
		log.Printf("[Recovery] Failed to load pending jobs: %v", err)
	} else if loadedJobs > 0 {
		log.Printf("[Recovery] Restored %d pending job(s)", loadedJobs)
	}
	loadedAsyncJobs, err := services.Global.LoadCompletedAsyncJobs()
	if err != nil {
		log.Printf("[Recovery] Failed to load completed async jobs: %v", err)
	} else if loadedAsyncJobs > 0 {
		log.Printf("[Recovery] Restored %d completed async job(s)", loadedAsyncJobs)
	}
	server.PrintBanner()

	services.Global.StartSessionCleanup(util.CleanupJobFiles)
	services.Global.StartCounterReconciliation()
	services.Global.StartAsyncJobExpiry()
	middleware.StartRateLimitCleanup()
	routes.StartChunkedUploadCleanup()
	routes.StartBotDownloadExpiry()
	routes.StartPlaylistDownloadExpiry()
	routes.StartRecoveredDownloadExpiry()
	routes.ResumeActivePlaylists()
	startIdleTempCleanup()
	if os.Getenv("SESSION_GENERATOR_URL") != "" {
		util.StartSessionTokenRefresh()
	}

	srv := server.New()
	go func() {
		time.Sleep(time.Second)
		routes.ResumePendingDownloads()
	}()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		fmt.Printf("\nReceived %s, shutting down...\n", sig)
		alerts.ServerStopping()
		services.Global.SetShuttingDown(true)
		srv.Close()
	}()

	log.Printf("listening on :%s", config.Port)
	alerts.ServerStarted()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func startIdleTempCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	go func() {
		for range ticker.C {
			active := activeJobCount()
			if active == 0 {
				protected := readyPendingOutputPaths(services.Global.PendingJobs())
				protected = append(protected, services.Global.CompletedAsyncOutputPaths()...)
				util.CleanupTempFiles(services.Global.KeepTempFile, protected...)
			} else {
				log.Printf("[Cleanup] Skipping temp sweep; %d active jobs", active)
			}
		}
	}()
}

func readyPendingOutputPaths(jobs []*services.PendingJob) []string {
	paths := make([]string, 0)
	for _, job := range jobs {
		if job.Ready && job.OutputPath != "" {
			paths = append(paths, job.OutputPath)
		}
	}
	return paths
}

func activeJobCount() int {
	total := 0
	for _, count := range services.Global.GetJobsByType() {
		total += count
	}
	return total
}
