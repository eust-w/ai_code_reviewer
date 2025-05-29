package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eust-w/ai_code_reviewer/internal/bot"
	"github.com/eust-w/ai_code_reviewer/internal/chat"
	"github.com/eust-w/ai_code_reviewer/internal/config"
	"github.com/eust-w/ai_code_reviewer/internal/git"
	"github.com/eust-w/ai_code_reviewer/internal/git/gitea"
	"github.com/eust-w/ai_code_reviewer/internal/git/github"
	"github.com/eust-w/ai_code_reviewer/internal/git/gitlab"
	"github.com/sirupsen/logrus"
	
	// 外部SDK包
	ghSDK "github.com/google/go-github/v60/github"
	glSDK "github.com/xanzy/go-gitlab"
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

	// Create git platform client based on configuration
	platformFactory := git.NewFactory(cfg)
	platform, err := platformFactory.CreatePlatform()
	if err != nil {
		logrus.Fatalf("Failed to create git platform client: %v", err)
	}

	// Create chat client
	chatClient, err := chat.NewChat(cfg)
	if err != nil {
		logrus.Fatalf("Failed to create chat client: %v", err)
	}

	// Create bot
	reviewBot := bot.NewBot(cfg, platform, chatClient)
	
	// 如果索引器存在，确保在程序退出时关闭资源
	if indexManager := reviewBot.GetIndexManager(); indexManager != nil {
		defer func() {
			logrus.Info("Closing indexer resources...")
			if err := indexManager.Close(); err != nil {
				logrus.Warnf("Error closing indexer resources: %v", err)
			} else {
				logrus.Info("Indexer resources closed successfully")
			}
		}()
	}

	// Create webhook handler based on platform
	webhookSecret := os.Getenv("WEBHOOK_SECRET")
	
	// Create HTTP server
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	
	addr := fmt.Sprintf(":%s", port)
	mux := http.NewServeMux()
	
	// Register platform-specific webhook handlers
	switch cfg.Platform {
	case "github":
		webhookHandler := github.NewWebhookHandler(webhookSecret)
		webhookHandler.On("pull_request", func(payload interface{}) error {
			event, ok := payload.(*ghSDK.PullRequestEvent)
			if !ok {
				return fmt.Errorf("invalid payload type for pull_request event")
			}
			
			// Handle the event in a goroutine to avoid blocking the webhook handler
			go func() {
				// 添加错误恢复机制
				defer func() {
					if r := recover(); r != nil {
						logrus.Errorf("Recovered from panic in GitHub request handler: %v", r)
					}
				}()
				
				ctx := context.Background()
				if err := reviewBot.HandleGitHubPullRequest(ctx, event); err != nil {
					logrus.Errorf("Error handling GitHub pull request event: %v", err)
				}
			}()
			
			return nil
		})
		mux.HandleFunc("/webhook", webhookHandler.HandleWebhook)
	
	case "gitlab":
		webhookHandler := gitlab.NewWebhookHandler(webhookSecret)
		webhookHandler.On("Merge Request Hook", func(payload interface{}) error {
			event, ok := payload.(*glSDK.MergeEvent)
			if !ok {
				return fmt.Errorf("invalid payload type for merge request event")
			}
			
			// Handle the event in a goroutine to avoid blocking the webhook handler
			go func() {
				// 添加错误恢复机制
				defer func() {
					if r := recover(); r != nil {
						logrus.Errorf("Recovered from panic in GitLab request handler: %v", r)
					}
				}()
				
				ctx := context.Background()
				if err := reviewBot.HandleGitLabMergeRequest(ctx, event); err != nil {
					logrus.Errorf("Error handling GitLab merge request event: %v", err)
				}
			}()
			
			return nil
		})
		mux.HandleFunc("/webhook", webhookHandler.HandleWebhook)
	
	case "gitea":
		webhookHandler := gitea.NewWebhookHandler(webhookSecret)
		webhookHandler.On("pull_request", func(payload interface{}) error {
			event, ok := payload.(*gitea.HookPullRequestEvent)
			if !ok {
				return fmt.Errorf("invalid payload type for pull request event")
			}
			
			// Handle the event in a goroutine to avoid blocking the webhook handler
			go func() {
				// 添加错误恢复机制
				defer func() {
					if r := recover(); r != nil {
						logrus.Errorf("Recovered from panic in Gitea request handler: %v", r)
					}
				}()
				
				ctx := context.Background()
				if err := reviewBot.HandleGiteaPullRequest(ctx, event); err != nil {
					logrus.Errorf("Error handling Gitea pull request event: %v", err)
				}
			}()
			
			return nil
		})
		mux.HandleFunc("/webhook", webhookHandler.HandleWebhook)
	
	default:
		logrus.Fatalf("Unsupported platform: %s", cfg.Platform)
	}

	// Add health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Start server in a goroutine
	go func() {
		logrus.Infof("Starting server on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.Fatalf("Error starting server: %v", err)
		}
	}()

	// Wait for interrupt signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	// Graceful shutdown
	logrus.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// 注意：索引器资源关闭已经通过defer设置，将在程序退出时自动执行
	
	if err := server.Shutdown(ctx); err != nil {
		logrus.Fatalf("Error shutting down server: %v", err)
	}
	
	logrus.Info("Server stopped")
}
