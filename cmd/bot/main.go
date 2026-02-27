package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/coah80/yoink/internal/bot"
)

func main() {
	godotenv.Load()

	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Fatal("DISCORD_TOKEN is required")
	}
	appID := os.Getenv("DISCORD_APP_ID")
	if appID == "" {
		log.Fatal("DISCORD_APP_ID is required")
	}
	apiURL := os.Getenv("YOINK_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:3003"
	}
	publicURL := os.Getenv("YOINK_PUBLIC_URL")
	if publicURL == "" {
		publicURL = apiURL
	}
	botSecret := os.Getenv("BOT_SECRET")
	if botSecret == "" {
		log.Fatal("BOT_SECRET is required")
	}

	b, err := bot.New(bot.Config{
		Token:     token,
		AppID:     appID,
		APIURL:    apiURL,
		PublicURL: publicURL,
		BotSecret: botSecret,
	})
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	if err := b.Start(); err != nil {
		log.Fatalf("Failed to start bot: %v", err)
	}

	fmt.Println("Bot is running. Press Ctrl+C to stop.")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\nShutting down bot...")
	b.Stop()
	fmt.Println("Bot stopped.")
}
