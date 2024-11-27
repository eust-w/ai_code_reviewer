package github

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/eust-w/ai_code_reviewer/internal/config"
	"github.com/google/go-github/v60/github"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

// Client wraps the GitHub API client
type Client struct {
	client *github.Client
	config *config.Config
}

// NewClient creates a new GitHub client
func NewClient(cfg *config.Config) (*Client, error) {
	if cfg.GithubToken == "" {
		return nil, errors.New("GitHub token is required")
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

// GetRepoVariable gets a repository variable
func (c *Client) GetRepoVariable(ctx context.Context, owner, repo, name string) (string, error) {
	variable, _, err := c.client.Actions.GetRepoVariable(ctx, owner, repo, name)
	if err != nil {
		return "", err
	}
	
	return variable.Value, nil
}

// CreatePRComment creates a comment on a pull request
func (c *Client) CreatePRComment(ctx context.Context, owner, repo string, number int, body string) error {
	_, _, err := c.client.Issues.CreateComment(ctx, owner, repo, number, &github.IssueComment{
		Body: github.String(body),
	})
	return err
}

// CompareCommits compares two commits and returns the files that changed
func (c *Client) CompareCommits(ctx context.Context, owner, repo, base, head string) ([]*github.CommitFile, []*github.RepositoryCommit, error) {
	comparison, _, err := c.client.Repositories.CompareCommits(ctx, owner, repo, base, head, &github.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	
	return comparison.Files, comparison.Commits, nil
}

// CreateReview creates a review on a pull request
func (c *Client) CreateReview(ctx context.Context, owner, repo string, number int, commitID string, comments []*github.DraftReviewComment, body string) error {
	event := "COMMENT"
	
	_, _, err := c.client.PullRequests.CreateReview(ctx, owner, repo, number, &github.PullRequestReviewRequest{
		CommitID: github.String(commitID),
		Body:     github.String(body),
		Event:    github.String(event),
		Comments: comments,
	})
	
	return err
}

// GetPullRequestLabels gets the labels of a pull request
func (c *Client) GetPullRequestLabels(ctx context.Context, owner, repo string, number int) ([]*github.Label, error) {
	pr, _, err := c.client.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	
	return pr.Labels, nil
}

// ExtractOwnerAndRepo extracts the owner and repo from a repository URL
func ExtractOwnerAndRepo(repoURL string) (string, string, error) {
	if repoURL == "" {
		return "", "", errors.New("empty repository URL")
	}
	
	// Handle SSH URLs like git@github.com:owner/repo.git
	if strings.HasPrefix(repoURL, "git@") {
		parts := strings.Split(strings.TrimSuffix(strings.Split(repoURL, ":")[1], ".git"), "/")
		if len(parts) != 2 {
			return "", "", errors.New("invalid repository URL format")
		}
		return parts[0], parts[1], nil
	}
	
	// Handle HTTPS URLs
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", "", err
	}
	
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", errors.New("invalid repository URL format")
	}
	
	return parts[0], parts[1], nil
}

// IsLabelAttached checks if a specific label is attached to the PR
func HasTargetLabel(labels []*github.Label, targetLabel string) bool {
	if targetLabel == "" {
		return true // No target label specified, so consider it as matched
	}
	
	for _, label := range labels {
		if label.GetName() == targetLabel {
			return true
		}
	}
	
	return false
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

// IsNotFound checks if an error is a 404 Not Found error
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	
	rerr, ok := err.(*github.ErrorResponse)
	return ok && rerr.Response.StatusCode == http.StatusNotFound
}
