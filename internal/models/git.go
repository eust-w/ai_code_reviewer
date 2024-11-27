package models

import (
	"context"
)

// CommitFile represents a file changed in a commit
type CommitFile struct {
	Filename    string
	Status      string
	Patch       string
	ContentsURL string
}

// Commit represents a git commit
type Commit struct {
	SHA string
}

// PullRequest represents a pull request or merge request
type PullRequest struct {
	Number      int
	Title       string
	Description string
	State       string
	Locked      bool
	Labels      []string
	Base        Commit
	Head        Commit
	HTMLURL     string
}

// ReviewComment represents a comment on a pull request
type ReviewComment struct {
	Path     string
	Body     string
	Position int
}

// GitPlatform defines the interface for git hosting platforms
type GitPlatform interface {
	// GetPullRequest gets a pull request by number
	GetPullRequest(ctx context.Context, owner, repo string, number int) (*PullRequest, error)
	
	// GetPullRequestLabels gets the labels of a pull request
	GetPullRequestLabels(ctx context.Context, owner, repo string, number int) ([]string, error)
	
	// CompareCommits compares two commits and returns the files that changed
	CompareCommits(ctx context.Context, owner, repo, base, head string) ([]*CommitFile, []*Commit, error)
	
	// CreateReview creates a review on a pull request
	CreateReview(ctx context.Context, owner, repo string, number int, commitID string, comments []*ReviewComment, body string) error
	
	// CreatePRComment creates a comment on a pull request
	CreatePRComment(ctx context.Context, owner, repo string, number int, body string) error
	
	// GetRepoVariable gets a repository variable
	GetRepoVariable(ctx context.Context, owner, repo, name string) (string, error)
}
