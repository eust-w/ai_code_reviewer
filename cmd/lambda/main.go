package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/eust-w/ai_code_reviewer/internal/bot"
	"github.com/eust-w/ai_code_reviewer/internal/chat"
	"github.com/eust-w/ai_code_reviewer/internal/config"
	"github.com/eust-w/ai_code_reviewer/internal/git/github"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	gh "github.com/google/go-github/v60/github"
	"github.com/sirupsen/logrus"
)

func main() {
	// Configure logging
	logrus.SetFormatter(&logrus.JSONFormatter{})
	
	// Set log level based on environment
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel != "" {
		level, err := logrus.ParseLevel(logLevel)
		if err == nil {
			logrus.SetLevel(level)
		}
	}
	
	lambda.Start(handleRequest)
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	logrus.Info("Received Lambda request")
	
	// Load configuration
	cfg := config.LoadConfig()
	
	// Create GitHub client
	githubClient, err := github.NewClient(cfg)
	if err != nil {
		logrus.Errorf("Failed to create GitHub client: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Body:       fmt.Sprintf("Failed to create GitHub client: %v", err),
		}, nil
	}
	
	// Create chat client
	chatClient, err := chat.NewChat(cfg)
	if err != nil {
		logrus.Errorf("Failed to create chat client: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Body:       fmt.Sprintf("Failed to create chat client: %v", err),
		}, nil
	}
	
	// Create bot
	reviewBot := bot.NewBot(cfg, githubClient, chatClient)
	
	// Get GitHub event type from headers
	eventType := request.Headers["X-GitHub-Event"]
	if eventType == "" {
		eventType = request.Headers["x-github-event"]
	}
	
	// Handle ping event
	if eventType == "ping" {
		return events.APIGatewayProxyResponse{
			StatusCode: 200,
			Body:       "pong",
		}, nil
	}
	
	// Only handle pull request events
	if eventType != "pull_request" {
		logrus.Infof("Ignoring event type: %s", eventType)
		return events.APIGatewayProxyResponse{
			StatusCode: 200,
			Body:       fmt.Sprintf("Event type %s not supported", eventType),
		}, nil
	}
	
	// Parse pull request event
	var prEvent gh.PullRequestEvent
	if err := json.Unmarshal([]byte(request.Body), &prEvent); err != nil {
		logrus.Errorf("Failed to parse pull request event: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Body:       fmt.Sprintf("Failed to parse pull request event: %v", err),
		}, nil
	}
	
	// Handle the event
	if err := reviewBot.HandlePullRequestEvent(ctx, &prEvent); err != nil {
		logrus.Errorf("Error handling pull request event: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Body:       fmt.Sprintf("Error handling pull request event: %v", err),
		}, nil
	}
	
	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       "Success",
	}, nil
}
