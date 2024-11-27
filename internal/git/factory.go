package git

import (
	"fmt"
	"strings"

	"github.com/eust-w/ai_code_reviewer/internal/config"
	"github.com/eust-w/ai_code_reviewer/internal/models"
	"github.com/sirupsen/logrus"
)

// Factory creates platform clients based on configuration
type Factory struct {
	config *config.Config
}

// NewFactory creates a new platform factory
func NewFactory(cfg *config.Config) *Factory {
	return &Factory{
		config: cfg,
	}
}

// PlatformType represents the type of git platform
type PlatformType string

const (
	GitHubPlatform PlatformType = "github"
	GitLabPlatform PlatformType = "gitlab"
	GiteaPlatform  PlatformType = "gitea"
)

// CreatePlatform creates a platform client based on configuration
func (f *Factory) CreatePlatform() (models.GitPlatform, error) {
	platform := strings.ToLower(f.config.Platform)
	
	switch platform {
	case string(GitHubPlatform):
		logrus.Info("Creating GitHub platform client")
		// 使用动态导入的方式避免导入循环
		return createGitHubClient(f.config)
	case string(GitLabPlatform):
		logrus.Info("Creating GitLab platform client")
		return createGitLabClient(f.config)
	case string(GiteaPlatform):
		logrus.Info("Creating Gitea platform client")
		return createGiteaClient(f.config)
	default:
		return nil, fmt.Errorf("unsupported platform: %s", platform)
	}
}

// WebhookHandler is an interface for webhook handlers
type WebhookHandler interface{}

// CreateWebhookHandler creates a webhook handler for the specified platform
func (f *Factory) CreateWebhookHandler(secret string) (WebhookHandler, error) {
	platform := strings.ToLower(f.config.Platform)
	
	switch platform {
	case string(GitHubPlatform):
		return createGitHubWebhookHandler(secret), nil
	case string(GitLabPlatform):
		return createGitLabWebhookHandler(secret), nil
	case string(GiteaPlatform):
		return createGiteaWebhookHandler(secret), nil
	default:
		return nil, fmt.Errorf("unsupported platform: %s", platform)
	}
}
