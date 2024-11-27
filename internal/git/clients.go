package git

import (
	"github.com/eust-w/ai_code_reviewer/internal/config"
	"github.com/eust-w/ai_code_reviewer/internal/git/gitea"
	"github.com/eust-w/ai_code_reviewer/internal/git/github"
	"github.com/eust-w/ai_code_reviewer/internal/git/gitlab"
	"github.com/eust-w/ai_code_reviewer/internal/models"
)

// 创建GitHub客户端的工厂方法
func createGitHubClient(cfg *config.Config) (models.GitPlatform, error) {
	return github.NewClient(cfg)
}

// 创建GitLab客户端的工厂方法
func createGitLabClient(cfg *config.Config) (models.GitPlatform, error) {
	return gitlab.NewClient(cfg)
}

// 创建Gitea客户端的工厂方法
func createGiteaClient(cfg *config.Config) (models.GitPlatform, error) {
	return gitea.NewClient(cfg)
}

// 创建GitHub webhook处理程序的工厂方法
func createGitHubWebhookHandler(secret string) WebhookHandler {
	return github.NewWebhookHandler(secret)
}

// 创建GitLab webhook处理程序的工厂方法
func createGitLabWebhookHandler(secret string) WebhookHandler {
	return gitlab.NewWebhookHandler(secret)
}

// 创建Gitea webhook处理程序的工厂方法
func createGiteaWebhookHandler(secret string) WebhookHandler {
	return gitea.NewWebhookHandler(secret)
}
