package bot

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/eust-w/ai_code_reviewer/internal/chat"
	"github.com/eust-w/ai_code_reviewer/internal/config"
	"github.com/eust-w/ai_code_reviewer/internal/git"
	"github.com/gobwas/glob"
	"github.com/google/go-github/v60/github"
	"github.com/sirupsen/logrus"
)

// Bot handles the code review logic
type Bot struct {
	config   *config.Config
	platform git.Platform
	chat     *chat.Chat
}

// NewBot creates a new Bot instance
func NewBot(cfg *config.Config, platform git.Platform, chat *chat.Chat) *Bot {
	return &Bot{
		config:   cfg,
		platform: platform,
		chat:     chat,
	}
}

// HandlePullRequestEvent handles GitHub pull request events
func (b *Bot) HandlePullRequestEvent(ctx context.Context, event *github.PullRequestEvent) error {
	// Skip if not opened or synchronized
	action := event.GetAction()
	if action != "opened" && action != "synchronize" {
		logrus.Infof("Skipping event with action: %s", action)
		return nil
	}

	pr := event.GetPullRequest()
	if pr.GetState() == "closed" || pr.GetLocked() {
		logrus.Info("Pull request is closed or locked, skipping")
		return nil
	}

	repo := event.GetRepo()
	owner := repo.GetOwner().GetLogin()
	repoName := repo.GetName()
	prNumber := pr.GetNumber()

	// Check if target label is required and present
	if b.config.TargetLabel != "" {
		labels, err := b.platform.GetPullRequestLabels(ctx, owner, repoName, prNumber)
		if err != nil {
			return fmt.Errorf("failed to get PR labels: %w", err)
		}

		hasTargetLabel := false
		for _, label := range labels {
			if label == b.config.TargetLabel {
				hasTargetLabel = true
				break
			}
		}

		if !hasTargetLabel {
			logrus.Info("Target label not attached, skipping")
			return nil
		}
	}

	// Compare commits to get changed files
	base := pr.GetBase().GetSHA()
	head := pr.GetHead().GetSHA()

	logrus.Debugf("Comparing commits: base=%s, head=%s", base, head)
	changedFiles, commits, err := b.platform.CompareCommits(ctx, owner, repoName, base, head)
	if err != nil {
		return fmt.Errorf("failed to compare commits: %w", err)
	}

	// For synchronize events, only review files changed in the latest commit
	if action == "synchronize" && len(commits) >= 2 {
		lastCommitBase := commits[len(commits)-2].SHA
		lastCommitHead := commits[len(commits)-1].SHA

		logrus.Debugf("Comparing latest commits: base=%s, head=%s", lastCommitBase, lastCommitHead)
		changedFiles, _, err = b.platform.CompareCommits(ctx, owner, repoName, lastCommitBase, lastCommitHead)
		if err != nil {
			return fmt.Errorf("failed to compare latest commits: %w", err)
		}
	}

	// Filter files based on patterns
	filteredFiles := b.filterFiles(changedFiles)
	if len(filteredFiles) == 0 {
		logrus.Info("No files to review after filtering")
		return nil
	}

	// Review each file
	start := time.Now()
	reviewComments := make([]*git.ReviewComment, 0)

	for _, file := range filteredFiles {
		if file.Status != "modified" && file.Status != "added" {
			continue
		}

		patch := file.Patch
		if patch == "" || (b.config.MaxPatchLength > 0 && len(patch) > b.config.MaxPatchLength) {
			logrus.Infof("Skipping %s: empty patch or too large", file.Filename)
			continue
		}

		result, err := b.chat.CodeReview(ctx, patch)
		if err != nil {
			logrus.Errorf("Failed to review %s: %v", file.Filename, err)
			continue
		}

		// 构建评论内容
		commentBody := ""
		
		// 根据配置的语言选择评论模板
		language := strings.ToLower(b.config.Language)
		if language == "" {
			// 默认使用中文
			language = "chinese"
		}
		
		// 添加 LGTM 状态
		// 如果result为空，则视为LGTM通过
		isEmptyResult := result.Summary == "" && result.ReviewComment == "" && 
		               result.Suggestions == "" && result.Highlights == "" && 
		               result.Risks == ""
		
		if !result.LGTM && !isEmptyResult {
			if language == "english" {
				commentBody += "**LGTM: ✖️ Changes Required**\n\n"
			} else {
				commentBody += "**LGTM: ✖️ 需要修改**\n\n"
			}
		} else {
			if language == "english" {
				commentBody += "**LGTM: ✅ Code Looks Good**\n\n"
			} else {
				commentBody += "**LGTM: ✅ 代码看起来不错**\n\n"
			}
		}
		
		// 添加总结（仅当内容非空时）
		if result.Summary != "" && result.Summary != "没有提供代码变更总结" && result.Summary != "No code changes detected." {
			if language == "english" {
				commentBody += fmt.Sprintf("## Summary\n%s\n\n", result.Summary)
			} else {
				commentBody += fmt.Sprintf("## 总结\n%s\n\n", result.Summary)
			}
		}
		
		// 添加详细评论
		if result.ReviewComment != "" {
			if language == "english" {
				commentBody += fmt.Sprintf("## Detailed Comments\n%s\n\n", result.ReviewComment)
			} else {
				commentBody += fmt.Sprintf("## 详细评论\n%s\n\n", result.ReviewComment)
			}
		}
		
		// 添加建议（仅当内容非空且非默认值时）
		if result.Suggestions != "" && result.Suggestions != "没有特定的改进建议" && result.Suggestions != "No specific suggestions" {
			if language == "english" {
				commentBody += fmt.Sprintf("## Improvement Suggestions\n%s\n\n", result.Suggestions)
			} else {
				commentBody += fmt.Sprintf("## 改进建议\n%s\n\n", result.Suggestions)
			}
		}
		
		// 添加亮点（暂时注释掉）
		/*
		if result.Highlights != "" && result.Highlights != "没有特别指出的代码亮点" && result.Highlights != "No highlights identified" {
			if language == "english" {
				commentBody += fmt.Sprintf("## Code Highlights\n%s\n\n", result.Highlights)
			} else {
				commentBody += fmt.Sprintf("## 代码亮点\n%s\n\n", result.Highlights)
			}
		}
		*/
		
		// 添加风险（简化为一句话）
		if result.Risks != "" && result.Risks != "没有发现明显的风险" && result.Risks != "No risks identified" {
			if language == "english" {
				commentBody += fmt.Sprintf("**Potential Risks**: %s\n\n", result.Risks)
			} else {
				commentBody += fmt.Sprintf("**潜在风险**: %s\n\n", result.Risks)
			}
		}
		
		// 如果评论内容只包含LGTM状态，则添加简短说明
		if commentBody == "**LGTM: ✅ Code Looks Good**\n\n" || commentBody == "**LGTM: ✅ 代码看起来不错**\n\n" {
			if language == "english" {
				commentBody += "The code changes look good with no specific issues identified."
			} else {
				commentBody += "代码变更看起来不错，没有发现特定问题。"
			}
		}
		
		patchLines := len(strings.Split(patch, "\n"))
		reviewComments = append(reviewComments, &git.ReviewComment{
			Path:     file.Filename,
			Body:     commentBody,
			Position: patchLines - 1,
		})
	}

	// Create the review
	body := "LGTM 👍"
	if len(reviewComments) > 0 {
		body = "Code review by ChatGPT"
	}

	latestCommitSHA := commits[len(commits)-1].SHA
	err = b.platform.CreateReview(ctx, owner, repoName, prNumber, latestCommitSHA, reviewComments, body)
	if err != nil {
		return fmt.Errorf("failed to create review: %w", err)
	}

	logrus.Infof("Successfully reviewed PR #%d in %s", prNumber, time.Since(start))
	return nil
}

// filterFiles filters files based on include/ignore patterns
func (b *Bot) filterFiles(files []*git.CommitFile) []*git.CommitFile {
	logrus.Debugf("Filtering %d files", len(files))
	logrus.Debugf("Include patterns: %v", b.config.IncludePatterns)
	logrus.Debugf("Ignore patterns: %v", b.config.IgnorePatterns)
	logrus.Debugf("Ignore list: %v", b.config.IgnoreList)
	
	if len(files) == 0 {
		logrus.Debug("No files to filter")
		return files
	}

	filtered := make([]*git.CommitFile, 0, len(files))
	for _, file := range files {
		filename := file.Filename
		logrus.Debugf("Checking file: %s, status: %s", filename, file.Status)
		
		// Check ignore list
		ignored := false
		for _, ignoreItem := range b.config.IgnoreList {
			if ignoreItem == filename {
				logrus.Debugf("File %s ignored by ignore list", filename)
				ignored = true
				break
			}
		}
		if ignored {
			continue
		}

		// Get pathname from contents_url for pattern matching
		contentsURL := file.ContentsURL
		logrus.Debugf("File contents URL: %s", contentsURL)
		u, err := url.Parse(contentsURL)
		if err != nil {
			logrus.Warnf("Failed to parse contents URL: %v", err)
			continue
		}
		pathname := u.Path
		logrus.Debugf("Parsed pathname: %s", pathname)

		// Check include patterns
		if len(b.config.IncludePatterns) > 0 {
			included := matchPatterns(b.config.IncludePatterns, pathname)
			logrus.Debugf("File %s include pattern match: %v", filename, included)
			if !included {
				logrus.Debugf("File %s excluded by include patterns", filename)
				continue
			}
		}

		// Check ignore patterns
		if len(b.config.IgnorePatterns) > 0 {
			ignored := matchPatterns(b.config.IgnorePatterns, pathname)
			logrus.Debugf("File %s ignore pattern match: %v", filename, ignored)
			if ignored {
				logrus.Debugf("File %s excluded by ignore patterns", filename)
				continue
			}
		}

		filtered = append(filtered, file)
	}

	return filtered
}

// matchPatterns checks if a path matches any of the patterns
func matchPatterns(patterns []string, path string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}

		// Adjust pattern format
		if pattern == "*" {
			// 特殊处理 "*" 模式，匹配所有内容
			return true
		} else if strings.HasPrefix(pattern, "/") {
			pattern = "**" + pattern
		} else if !strings.HasPrefix(pattern, "**") {
			pattern = "**/" + pattern
		}

		// Try glob pattern matching
		g, err := glob.Compile(pattern)
		if err == nil {
			if g.Match(path) {
				return true
			}
			continue
		}

		// Try regex matching as fallback
		// Note: In Go, we're not implementing regex fallback as it would require
		// importing the regexp package and adding complexity.
		// Instead, we're focusing on glob pattern matching which covers most use cases.
	}

	return false
}
