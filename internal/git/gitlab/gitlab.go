package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/eust-w/ai_code_reviewer/internal/config"
	"github.com/eust-w/ai_code_reviewer/internal/models"
	"github.com/sirupsen/logrus"
	"github.com/xanzy/go-gitlab"
)

// String returns a pointer to the string value passed in.
func String(v string) *string {
	return &v
}

// Int returns a pointer to the int value passed in.
func Int(v int) *int {
	return &v
}

// Client implements the models.GitPlatform interface for GitLab
type Client struct {
	client *gitlab.Client
	config *config.Config
}

// NewClient creates a new GitLab client
func NewClient(cfg *config.Config) (*Client, error) {
	// 只有当选择的平台是GitLab时，才检查令牌
	if cfg.Platform == "gitlab" && cfg.GitlabToken == "" {
		return nil, errors.New("GitLab token is required when using GitLab platform")
	}

	baseURL := cfg.GitlabBaseURL
	if baseURL == "" {
		baseURL = "https://gitlab.com/api/v4"
	}
	
	client, err := gitlab.NewClient(cfg.GitlabToken, gitlab.WithBaseURL(baseURL))
	if err != nil {
		return nil, fmt.Errorf("failed to create GitLab client: %w", err)
	}
	
	return &Client{
		client: client,
		config: cfg,
	}, nil
}

// GetPullRequest gets a merge request by number (iid in GitLab)
func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (*models.PullRequest, error) {
	projectPath := fmt.Sprintf("%s/%s", owner, repo)
	
	mr, _, err := c.client.MergeRequests.GetMergeRequest(projectPath, number, &gitlab.GetMergeRequestsOptions{})
	if err != nil {
		return nil, err
	}
	
	labels := make([]string, len(mr.Labels))
	copy(labels, mr.Labels)
	
	return &models.PullRequest{
		Number:      mr.IID,
		Title:       mr.Title,
		Description: mr.Description,
		State:       mr.State,
		Locked:      mr.WorkInProgress, // Use WorkInProgress as a proxy for locked
		Labels:      labels,
		Base: models.Commit{
			SHA: mr.DiffRefs.BaseSha,
		},
		Head: models.Commit{
			SHA: mr.DiffRefs.HeadSha,
		},
		HTMLURL: mr.WebURL,
	}, nil
}

// GetPullRequestLabels gets the labels of a merge request
func (c *Client) GetPullRequestLabels(ctx context.Context, owner, repo string, number int) ([]string, error) {
	projectPath := fmt.Sprintf("%s/%s", owner, repo)
	
	mr, _, err := c.client.MergeRequests.GetMergeRequest(projectPath, number, &gitlab.GetMergeRequestsOptions{})
	if err != nil {
		return nil, err
	}
	
	labels := make([]string, len(mr.Labels))
	copy(labels, mr.Labels)
	
	return labels, nil
}

// CompareCommits compares two commits and returns the files that changed
func (c *Client) CompareCommits(ctx context.Context, owner, repo, base, head string) ([]*models.CommitFile, []*models.Commit, error) {
	projectPath := fmt.Sprintf("%s/%s", owner, repo)
	
	// Get the comparison
	comparison, _, err := c.client.Repositories.Compare(projectPath, &gitlab.CompareOptions{
		From: &base,
		To:   &head,
	})
	if err != nil {
		return nil, nil, err
	}
	
	// Get the commits
	gitlabCommits, _, err := c.client.Commits.ListCommits(projectPath, &gitlab.ListCommitsOptions{
		RefName: &head,
	})
	if err != nil {
		return nil, nil, err
	}
	
	// Filter commits between base and head
	filteredCommits := make([]*gitlab.Commit, 0)
	for _, commit := range gitlabCommits {
		if commit.ID == base {
			break
		}
		filteredCommits = append(filteredCommits, commit)
	}
	
	// Convert commits to our format
	commits := make([]*models.Commit, len(filteredCommits))
	for i, commit := range filteredCommits {
		commits[i] = &models.Commit{
			SHA: commit.ID,
		}
	}
	
	// Convert diffs to our format
	files := make([]*models.CommitFile, len(comparison.Diffs))
	for i, diff := range comparison.Diffs {
		status := "modified"
		if diff.NewFile {
			status = "added"
		} else if diff.DeletedFile {
			status = "deleted"
		} else if diff.RenamedFile {
			status = "renamed"
		}
		
		files[i] = &models.CommitFile{
			Filename:    diff.NewPath,
			Status:      status,
			Patch:       diff.Diff,
			ContentsURL: fmt.Sprintf("%s/api/v4/projects/%s/repository/files/%s/raw?ref=%s", 
				c.config.GitlabBaseURL, 
				projectPath, 
				strings.ReplaceAll(diff.NewPath, "/", "%2F"),
				head),
		}
	}
	
	return files, commits, nil
}

// CreateReview creates a review on a merge request
func (c *Client) CreateReview(ctx context.Context, owner, repo string, number int, commitID string, comments []*models.ReviewComment, body string) error {
	projectPath := fmt.Sprintf("%s/%s", owner, repo)
	
	// First, create the general comment
	if body != "" {
		_, _, err := c.client.Notes.CreateMergeRequestNote(projectPath, number, &gitlab.CreateMergeRequestNoteOptions{
			Body: &body,
		})
		if err != nil {
			return err
		}
	}
	
	// Then create individual file comments
	for _, comment := range comments {
		// GitLab requires line numbers instead of positions
		// We'll use a simplified approach here
		lineNum := 1 // Default to line 1 if we can't determine
		
		// Create the comment
		commentBody := comment.Body
		
		// 创建讨论
		_, _, err := c.client.Discussions.CreateMergeRequestDiscussion(
			projectPath, 
			number, 
			&gitlab.CreateMergeRequestDiscussionOptions{
				Body: &commentBody,
				Position: &gitlab.PositionOptions{
					BaseSHA:      String("base"),  // 使用占位符
					StartSHA:     String("start"), // 使用占位符
					HeadSHA:      String(commitID),
					NewPath:      String(comment.Path),
					NewLine:      Int(lineNum),
				},
			},
		)
		
		if err != nil {
			logrus.Errorf("Failed to create comment on file %s: %v", comment.Path, err)
			// Continue with other comments
		}
	}
	
	return nil
}

// CreatePRComment creates a comment on a merge request
func (c *Client) CreatePRComment(ctx context.Context, owner, repo string, number int, body string) error {
	projectPath := fmt.Sprintf("%s/%s", owner, repo)
	
	_, _, err := c.client.Notes.CreateMergeRequestNote(projectPath, number, &gitlab.CreateMergeRequestNoteOptions{
		Body: &body,
	})
	
	return err
}

// GetRepoVariable gets a repository variable
func (c *Client) GetRepoVariable(ctx context.Context, owner, repo, name string) (string, error) {
	projectPath := fmt.Sprintf("%s/%s", owner, repo)
	
	// Try to get project ID
	project, _, err := c.client.Projects.GetProject(projectPath, &gitlab.GetProjectOptions{})
	if err != nil {
		return "", err
	}
	
	// Get variable
	variable, _, err := c.client.ProjectVariables.GetVariable(project.ID, name, nil)
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
	
	if response, ok := err.(*gitlab.ErrorResponse); ok {
		return response.Response.StatusCode == http.StatusNotFound
	}
	
	return false
}

// ExtractProjectID extracts the project ID from a project path
func ExtractProjectID(projectPath string) (int, error) {
	// Try to parse as integer first
	id, err := strconv.Atoi(projectPath)
	if err == nil {
		return id, nil
	}
	
	// Otherwise, assume it's a path
	parts := strings.Split(projectPath, "/")
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid project path: %s", projectPath)
	}
	
	return 0, fmt.Errorf("project ID not found for path: %s", projectPath)
}
