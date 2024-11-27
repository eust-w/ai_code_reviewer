package github

import (
	"context"
	"errors"
	"net/http"

	"github.com/eust-w/ai_code_reviewer/internal/config"
	"github.com/eust-w/ai_code_reviewer/internal/models"
	"github.com/google/go-github/v60/github"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

// Client implements the models.GitPlatform interface for GitHub
type Client struct {
	client *github.Client
	config *config.Config
}

// NewClient creates a new GitHub client
func NewClient(cfg *config.Config) (*Client, error) {
	// 只有当选择的平台是GitHub时，才检查令牌
	if cfg.Platform == "github" && cfg.GithubToken == "" {
		return nil, errors.New("GitHub token is required when using GitHub platform")
	}

	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: cfg.GithubToken},
	)
	tc := oauth2.NewClient(context.Background(), ts)
	
	return &Client{
		client: github.NewClient(tc),
		config: cfg,
	}, nil
}

// GetPullRequest gets a pull request by number
func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (*models.PullRequest, error) {
	pr, _, err := c.client.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	
	return &models.PullRequest{
		Number:      pr.GetNumber(),
		Title:       pr.GetTitle(),
		Description: pr.GetBody(),
		State:       pr.GetState(),
		Locked:      pr.GetLocked(),
		Labels:      extractLabels(pr.Labels),
		Base: models.Commit{
			SHA: pr.GetBase().GetSHA(),
		},
		Head: models.Commit{
			SHA: pr.GetHead().GetSHA(),
		},
		HTMLURL: pr.GetHTMLURL(),
	}, nil
}

// GetPullRequestLabels gets the labels of a pull request
func (c *Client) GetPullRequestLabels(ctx context.Context, owner, repo string, number int) ([]string, error) {
	pr, _, err := c.client.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	
	return extractLabels(pr.Labels), nil
}

// CompareCommits compares two commits and returns the files that changed
func (c *Client) CompareCommits(ctx context.Context, owner, repo, base, head string) ([]*models.CommitFile, []*models.Commit, error) {
	comparison, _, err := c.client.Repositories.CompareCommits(ctx, owner, repo, base, head, &github.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	
	files := make([]*models.CommitFile, 0, len(comparison.Files))
	for _, file := range comparison.Files {
		files = append(files, &models.CommitFile{
			Filename:    file.GetFilename(),
			Status:      file.GetStatus(),
			Patch:       file.GetPatch(),
			ContentsURL: file.GetContentsURL(),
		})
	}
	
	commits := make([]*models.Commit, 0, len(comparison.Commits))
	for _, commit := range comparison.Commits {
		commits = append(commits, &models.Commit{
			SHA: commit.GetSHA(),
		})
	}
	
	return files, commits, nil
}

// CreateReview creates a review on a pull request
func (c *Client) CreateReview(ctx context.Context, owner, repo string, number int, commitID string, comments []*models.ReviewComment, body string) error {
	ghComments := make([]*github.DraftReviewComment, 0, len(comments))
	for _, comment := range comments {
		ghComments = append(ghComments, &github.DraftReviewComment{
			Path:     github.String(comment.Path),
			Body:     github.String(comment.Body),
			Position: github.Int(comment.Position),
		})
	}
	
	_, _, err := c.client.PullRequests.CreateReview(ctx, owner, repo, number, &github.PullRequestReviewRequest{
		CommitID: github.String(commitID),
		Body:     github.String(body),
		Event:    github.String("COMMENT"),
		Comments: ghComments,
	})
	
	return err
}

// CreatePRComment creates a comment on a pull request
func (c *Client) CreatePRComment(ctx context.Context, owner, repo string, number int, body string) error {
	_, _, err := c.client.Issues.CreateComment(ctx, owner, repo, number, &github.IssueComment{
		Body: github.String(body),
	})
	return err
}

// GetRepoVariable gets a repository variable
func (c *Client) GetRepoVariable(ctx context.Context, owner, repo, name string) (string, error) {
	variable, _, err := c.client.Actions.GetRepoVariable(ctx, owner, repo, name)
	if err != nil {
		return "", err
	}
	
	return variable.Value, nil
}

// IsNotFound checks if an error is a 404 Not Found error
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	
	rerr, ok := err.(*github.ErrorResponse)
	return ok && rerr.Response.StatusCode == http.StatusNotFound
}

// Helper function to extract labels from GitHub labels
func extractLabels(ghLabels []*github.Label) []string {
	labels := make([]string, 0, len(ghLabels))
	for _, label := range ghLabels {
		labels = append(labels, label.GetName())
	}
	return labels
}

// CheckRateLimit logs the current rate limit status
func (c *Client) CheckRateLimit(ctx context.Context) {
	rate, _, err := c.client.RateLimits(ctx)
	if err != nil {
		logrus.Warnf("Error checking rate limit: %v", err)
		return
	}
	
	logrus.Infof("GitHub API rate limit: %d/%d, resets at %s", 
		rate.Core.Remaining, 
		rate.Core.Limit,
		rate.Core.Reset.Time.String())
}
