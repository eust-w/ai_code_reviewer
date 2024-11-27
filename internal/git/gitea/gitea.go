package gitea

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"code.gitea.io/sdk/gitea"
	"github.com/eust-w/ai_code_reviewer/internal/config"
	"github.com/eust-w/ai_code_reviewer/internal/models"
	"github.com/sirupsen/logrus"
)

// Client implements the models.GitPlatform interface for Gitea
type Client struct {
	client *gitea.Client
	config *config.Config
}

// NewClient creates a new Gitea client
func NewClient(cfg *config.Config) (*Client, error) {
	// 只有当选择的平台是Gitea时，才检查令牌和基础URL
	if cfg.Platform == "gitea" {
		if cfg.GiteaToken == "" {
			return nil, errors.New("Gitea token is required when using Gitea platform")
		}

		if cfg.GiteaBaseURL == "" {
			return nil, errors.New("Gitea base URL is required when using Gitea platform")
		}
	}

	client, err := gitea.NewClient(cfg.GiteaBaseURL, gitea.SetToken(cfg.GiteaToken))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gitea client: %w", err)
	}
	
	return &Client{
		client: client,
		config: cfg,
	}, nil
}

// GetPullRequest gets a pull request by number
func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (*models.PullRequest, error) {
	pr, _, err := c.client.GetPullRequest(owner, repo, int64(number))
	if err != nil {
		return nil, err
	}
	
	labels := make([]string, 0, len(pr.Labels))
	for _, label := range pr.Labels {
		labels = append(labels, label.Name)
	}
	
	return &models.PullRequest{
		Number:      int(pr.Index),
		Title:       pr.Title,
		Description: pr.Body,
		State:       string(pr.State),
		Locked:      false, // Gitea doesn't have a direct equivalent to locked
		Labels:      labels,
		Base: models.Commit{
			SHA: pr.Base.Sha,
		},
		Head: models.Commit{
			SHA: pr.Head.Sha,
		},
		HTMLURL: pr.HTMLURL,
	}, nil
}

// GetPullRequestLabels gets the labels of a pull request
func (c *Client) GetPullRequestLabels(ctx context.Context, owner, repo string, number int) ([]string, error) {
	pr, _, err := c.client.GetPullRequest(owner, repo, int64(number))
	if err != nil {
		return nil, err
	}
	
	labels := make([]string, 0, len(pr.Labels))
	for _, label := range pr.Labels {
		labels = append(labels, label.Name)
	}
	
	return labels, nil
}

// CompareCommits compares two commits and returns the files that changed
func (c *Client) CompareCommits(ctx context.Context, owner, repo, base, head string) ([]*models.CommitFile, []*models.Commit, error) {
	// 获取PR列表，找到匹配base和head的PR
	logrus.Debugf("Comparing commits for %s/%s: base=%s, head=%s", owner, repo, base, head)
	
	// 获取仓库的所有PR
	prs, _, err := c.client.ListRepoPullRequests(owner, repo, gitea.ListPullRequestsOptions{
		State: gitea.StateOpen,
		Sort: "recentupdate",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list pull requests: %w", err)
	}
	
	// 寻找匹配的PR
	var prIndex int64
	for _, pr := range prs {
		if pr.Head.Sha == head {
			prIndex = pr.Index
			break
		}
	}
	
	if prIndex == 0 {
		logrus.Warnf("No matching PR found for head SHA: %s", head)
		// 如果找不到匹配的PR，返回一个基本的比较结果
		gitCommits := []*models.Commit{
			{
				SHA: head,
			},
			{
				SHA: base,
			},
		}
		return []*models.CommitFile{}, gitCommits, nil
	}
	
	// 获取PR的文件变更
	logrus.Debugf("Found matching PR #%d", prIndex)
	
	// Gitea SDK没有直接提供获取PR文件的方法
	// 我们需要使用GetPullRequest获取PR信息，然后手动构建文件列表
	pr, _, err := c.client.GetPullRequest(owner, repo, prIndex)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get PR details: %w", err)
	}
	
	// 由于Gitea SDK没有直接提供获取PR文件的方法
	// 我们将使用PR的diff信息来获取文件变更
	logrus.Debugf("Attempting to get PR diff for PR #%d", prIndex)
	
	// 创建文件列表
	files := make([]*models.CommitFile, 0)
	
	// 获取PR的diff
	diffURL := fmt.Sprintf("%s/api/v1/repos/%s/%s/pulls/%d.diff", 
		c.config.GiteaBaseURL, 
		owner, 
		repo, 
		prIndex)
	
	logrus.Debugf("Fetching diff from: %s", diffURL)
	
	// 创建HTTP请求
	req, err := http.NewRequest("GET", diffURL, nil)
	if err != nil {
		logrus.Errorf("Failed to create request for PR diff: %v", err)
		// 如果无法获取diff，返回空文件列表
		return files, []*models.Commit{{SHA: head}, {SHA: base}}, nil
	}
	
	// 添加认证信息
	req.Header.Add("Authorization", fmt.Sprintf("token %s", c.config.GiteaToken))
	
	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logrus.Errorf("Failed to get PR diff: %v", err)
		// 如果无法获取diff，返回空文件列表
		return files, []*models.Commit{{SHA: head}, {SHA: base}}, nil
	}
	defer resp.Body.Close()
	
	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		logrus.Errorf("Failed to get PR diff, status code: %d", resp.StatusCode)
		// 如果无法获取diff，返回空文件列表
		return files, []*models.Commit{{SHA: head}, {SHA: base}}, nil
	}
	
	// 读取diff内容
	diffContent, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Errorf("Failed to read PR diff: %v", err)
		// 如果无法读取diff，返回空文件列表
		return files, []*models.Commit{{SHA: head}, {SHA: base}}, nil
	}
	
	// 解析diff内容，提取文件信息
	// 这里使用一个简化的方法来解析diff
	// 在实际生产环境中，您可能需要使用更复杂的diff解析器
	
	// 将diff内容转换为字符串
	diffString := string(diffContent)
	
	// 打印diff内容的前100个字符，帮助调试
	if len(diffString) > 0 {
		previewLen := 100
		if len(diffString) < previewLen {
			previewLen = len(diffString)
		}
		logrus.Debugf("Diff content preview: %s...", diffString[:previewLen])
	} else {
		logrus.Warn("Empty diff content received")
		return files, []*models.Commit{{SHA: head}, {SHA: base}}, nil
	}

	// 使用更简单的方法提取文件名
	// 将diff按文件分割
	fileDiffs := strings.Split(diffString, "diff --git ")
	
	// 第一个元素是空的，跳过
	if len(fileDiffs) > 0 {
		fileDiffs = fileDiffs[1:]
	}
	
	logrus.Debugf("Found %d file diffs", len(fileDiffs))
	
	// 如果没有文件变更，直接返回空列表
	if len(fileDiffs) == 0 {
		logrus.Warn("No file diffs found")
		return files, []*models.Commit{{SHA: head}, {SHA: base}}, nil
	}
	
	for _, fileDiff := range fileDiffs {
		// 从文件diff中提取文件名
		// 格式例如: "a/file.txt b/file.txt"
		parts := strings.Split(fileDiff, " ")
		if len(parts) < 1 {
			continue
		}
		
		// 提取文件名，去除 "a/" 前缀
		filePath := parts[0]
		if strings.HasPrefix(filePath, "a/") {
			filePath = filePath[2:]
		}
		
		filename := filePath
		
		// 确定文件状态
		status := "modified" // 默认为修改
		
		// 检查是否是新文件
		if strings.Contains(fileDiff, "new file mode") {
			status = "added"
		}
		
		// 检查是否是删除的文件
		if strings.Contains(fileDiff, "deleted file mode") {
			status = "removed"
		}
		
		// 提取patch部分
		// 找到第一个@@标记，这是patch的开始
		patchStart := strings.Index(fileDiff, "@@")
		if patchStart == -1 {
			// 如果没有@@标记，跳过这个文件
			logrus.Debugf("No patch found for file %s", filename)
			continue
		}
		
		// 提取patch部分
		patch := fileDiff[patchStart:]
		
		// 创建文件对象
		files = append(files, &models.CommitFile{
			Filename:    filename,
			Status:      status,
			Patch:       patch,
			ContentsURL: fmt.Sprintf("%s/api/v1/repos/%s/%s/contents/%s?ref=%s", 
				c.config.GiteaBaseURL, 
				owner, 
				repo, 
				filename,
				head),
		})
	}
	
	logrus.Debugf("Found %d changed files in PR #%d", len(files), prIndex)
	
	// 获取提交信息
	pr, _, err = c.client.GetPullRequest(owner, repo, prIndex)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get PR details: %w", err)
	}
	
	// 创建提交列表
	gitCommits := []*models.Commit{
		{
			SHA: pr.Head.Sha,
		},
		{
			SHA: pr.Base.Sha,
		},
	}
	
	return files, gitCommits, nil
}

// CreateReview creates a review on a pull request
func (c *Client) CreateReview(ctx context.Context, owner, repo string, number int, commitID string, comments []*models.ReviewComment, body string) error {
	// 如果没有评论和正文，则不执行任何操作
	if len(comments) == 0 && body == "" {
		return nil
	}
	
	// 如果有评论，则将所有评论合并到一个评论中
	if len(comments) > 0 {
		// 获取配置的语言
		language := strings.ToLower(c.config.Language)
		if language == "" {
			// 默认使用中文
			language = "chinese"
		}
		
		// 创建一个合并的评论
		combinedBody := ""
		if language == "english" {
			combinedBody = "## Code Review Results\n\n"
		} else {
			combinedBody = "## 代码审查结果\n\n"
		}
		
		// 如果有通用评论，添加到合并评论的开头
		if body != "" && body != "Code review by ChatGPT" && body != "LGTM 👍" {
			combinedBody += fmt.Sprintf("%s\n\n", body)
		}
		
		// 为每个文件创建一个部分
		for i, comment := range comments {
			// 添加文件标题
			if language == "english" {
				combinedBody += fmt.Sprintf("### File: %s\n\n%s", comment.Path, comment.Body)
			} else {
				combinedBody += fmt.Sprintf("### 文件: %s\n\n%s", comment.Path, comment.Body)
			}
			
			// 如果不是最后一个评论，添加分隔符
			if i < len(comments)-1 {
				combinedBody += "\n\n---\n\n"
			}
		}
		
		// 创建合并的评论
		_, _, err := c.client.CreateIssueComment(owner, repo, int64(number), gitea.CreateIssueCommentOption{
			Body: combinedBody,
		})
		
		if err != nil {
			return fmt.Errorf("failed to create combined comment: %w", err)
		}
	} else if body != "" {
		// 如果只有通用评论，创建一个普通评论
		_, _, err := c.client.CreateIssueComment(owner, repo, int64(number), gitea.CreateIssueCommentOption{
			Body: body,
		})
		if err != nil {
			return err
		}
	}
	
	return nil
}

// CreatePRComment creates a comment on a pull request
func (c *Client) CreatePRComment(ctx context.Context, owner, repo string, number int, body string) error {
	_, _, err := c.client.CreateIssueComment(owner, repo, int64(number), gitea.CreateIssueCommentOption{
		Body: body,
	})
	
	return err
}

// GetRepoVariable gets a repository variable
// Note: Gitea doesn't have a direct equivalent to GitHub's repository variables
// We'll use repository secrets as a proxy
func (c *Client) GetRepoVariable(ctx context.Context, owner, repo, name string) (string, error) {
	// Gitea doesn't have an API to get secret values, only to set them
	// This is a limitation of the platform
	return "", fmt.Errorf("getting repository variables is not supported in Gitea")
}

// IsNotFound checks if an error is a 404 Not Found error
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	
	return strings.Contains(err.Error(), "404") || 
		   strings.Contains(err.Error(), "not found")
}
