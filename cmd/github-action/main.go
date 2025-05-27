package main

import (
	"context"
	"os"

	"github.com/eust-w/ai_code_reviewer/internal/bot"
	"github.com/eust-w/ai_code_reviewer/internal/chat"
	"github.com/eust-w/ai_code_reviewer/internal/config"
	"github.com/eust-w/ai_code_reviewer/internal/git/github"
	gh "github.com/google/go-github/v60/github"
	"github.com/sirupsen/logrus"
)

func main() {
	// Configure logging
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Set log level based on environment
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel != "" {
		level, err := logrus.ParseLevel(logLevel)
		if err == nil {
			logrus.SetLevel(level)
		}
	}

	// Load configuration
	cfg := config.LoadConfig()

	// Create GitHub client
	githubClient, err := github.NewClient(cfg)
	if err != nil {
		logrus.Fatalf("Failed to create GitHub client: %v", err)
	}

	// Create chat client
	chatClient, err := chat.NewChat(cfg)
	if err != nil {
		logrus.Fatalf("Failed to create chat client: %v", err)
	}

	// Create bot
	reviewBot := bot.NewBot(cfg, githubClient, chatClient)

	// Get GitHub event context from environment variables
	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	if eventPath == "" {
		logrus.Fatal("GITHUB_EVENT_PATH environment variable is not set")
	}

	// Read event file
	eventData, err := os.ReadFile(eventPath)
	if err != nil {
		logrus.Fatalf("Failed to read event file: %v", err)
	}

	// Parse event
	event, err := gh.ParseWebHook("pull_request", eventData)
	if err != nil {
		logrus.Fatalf("Failed to parse webhook: %v", err)
	}

	// Handle pull request event
	prEvent, ok := event.(*gh.PullRequestEvent)
	if !ok {
		logrus.Fatal("Event is not a pull request event")
	}

	// Handle the event
	ctx := context.Background()
	if err := reviewBot.HandlePullRequestEvent(ctx, prEvent); err != nil {
		logrus.Fatalf("Error handling pull request event: %v", err)
	}

	logrus.Info("GitHub Action completed successfully")
}
