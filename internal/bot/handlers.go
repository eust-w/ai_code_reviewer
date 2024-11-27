package bot

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/eust-w/ai_code_reviewer/internal/git"
	"github.com/eust-w/ai_code_reviewer/internal/git/gitea"
	"github.com/google/go-github/v60/github"
	"github.com/sirupsen/logrus"
	"github.com/xanzy/go-gitlab"
)

// HandleGitHubPullRequest handles GitHub pull request events
func (b *Bot) HandleGitHubPullRequest(ctx context.Context, event *github.PullRequestEvent) error {
	// Skip if not opened or synchronized
	action := event.GetAction()
	if action != "opened" && action != "synchronize" {
		logrus.Infof("Skipping GitHub event with action: %s", action)
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

	return b.handlePullRequest(ctx, owner, repoName, prNumber, pr.GetBase().GetSHA(), pr.GetHead().GetSHA(), action)
}

// HandleGitLabMergeRequest handles GitLab merge request events
func (b *Bot) HandleGitLabMergeRequest(ctx context.Context, event *gitlab.MergeEvent) error {
	// Skip if not opened or synchronized
	action := event.ObjectAttributes.Action
	if action != "open" && action != "update" {
		logrus.Infof("Skipping GitLab event with action: %s", action)
		return nil
	}

	mr := event.ObjectAttributes
	if mr.State == "closed" || mr.WorkInProgress {
		logrus.Info("Merge request is closed or work in progress, skipping")
		return nil
	}

	owner := event.Project.Namespace
	repoName := event.Project.Name
	mrNumber := mr.IID

	return b.handlePullRequest(ctx, owner, repoName, mrNumber, mr.OldRev, mr.LastCommit.ID, action)
}

// HandleGiteaPullRequest handles Gitea pull request events
func (b *Bot) HandleGiteaPullRequest(ctx context.Context, event *gitea.HookPullRequestEvent) error {
	// Skip if not opened or synchronized
	action := event.Action
	if action != gitea.HookIssueOpened && action != gitea.HookIssueSynchronized {
		logrus.Infof("Skipping Gitea event with action: %s", action)
		return nil
	}

	pr := event.PullRequest
	if pr.State == "closed" {
		logrus.Info("Pull request is closed, skipping")
		return nil
	}

	owner := event.Repository.Owner.Username
	repoName := event.Repository.Name
	prNumber := int(pr.Number)

	return b.handlePullRequest(ctx, owner, repoName, prNumber, pr.Base.Sha, pr.Head.Sha, string(action))
}

// Common handler for pull requests from any platform
func (b *Bot) handlePullRequest(ctx context.Context, owner, repo string, number int, baseSHA, headSHA, action string) error {
	// 移除标签检查，让所有PR都能触发代码审查
	// 注释掉原来的代码，保留以备将来可能需要恢复
	/*
	// Check if target label is required and present
	if b.config.TargetLabel != "" {
		labels, err := b.platform.GetPullRequestLabels(ctx, owner, repo, number)
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
	*/

	// Compare commits to get changed files
	logrus.Debugf("Comparing commits: base=%s, head=%s", baseSHA, headSHA)
	changedFiles, commits, err := b.platform.CompareCommits(ctx, owner, repo, baseSHA, headSHA)
	if err != nil {
		return fmt.Errorf("failed to compare commits: %w", err)
	}

	// For synchronize/update events, only review files changed in the latest commit
	if (action == "synchronize" || action == "update" || action == gitea.HookIssueSynchronized) && len(commits) >= 2 {
		lastCommitBase := commits[len(commits)-2].SHA
		lastCommitHead := commits[len(commits)-1].SHA

		logrus.Debugf("Comparing latest commits: base=%s, head=%s", lastCommitBase, lastCommitHead)
		changedFiles, _, err = b.platform.CompareCommits(ctx, owner, repo, lastCommitBase, lastCommitHead)
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

		// 构建完整的评论内容，包含所有字段
		commentBody := ""
		
		// 添加总结
		if result.Summary != "" {
			commentBody += fmt.Sprintf("## 总结\n%s\n\n", result.Summary)
		}
		
		// 添加详细评论
		if result.ReviewComment != "" {
			commentBody += fmt.Sprintf("## 详细评论\n%s\n\n", result.ReviewComment)
		}
		
		// 添加建议
		if result.Suggestions != "" {
			commentBody += fmt.Sprintf("## 改进建议\n%s\n\n", result.Suggestions)
		}
		
		// 添加亮点
		if result.Highlights != "" {
			commentBody += fmt.Sprintf("## 代码亮点\n%s\n\n", result.Highlights)
		}
		
		// 添加风险
		if result.Risks != "" {
			commentBody += fmt.Sprintf("## 潜在风险\n%s\n\n", result.Risks)
		}
		
		// 添加 LGTM 状态
		if !result.LGTM {
			commentBody = fmt.Sprintf("**LGTM: ✖️ 需要修改**\n\n%s", commentBody)
		} else {
			commentBody = fmt.Sprintf("**LGTM: ✅ 代码看起来不错**\n\n%s", commentBody)
		}
		
		// 即使评论内容为空，也添加到评论列表，确保显示 LGTM 状态
		patchLines := len(strings.Split(patch, "\n"))
		reviewComments = append(reviewComments, &git.ReviewComment{
			Path:     file.Filename,
			Body:     commentBody,
			Position: patchLines - 1,
		})
	}

	// Create the review with detailed information
	// 始终创建一个有信息量的总结，不再仅显示 "LGTM 👍"
	// 即使没有评论，也会显示一个基本的总结
	
	// 收集所有文件的审查结果
	allSummaries := []string{}
	allLGTM := true
	
	// 遍历所有评论，提取摘要信息
	for _, comment := range reviewComments {
		// 从评论中提取文件名
		fileName := filepath.Base(comment.Path)
		
		// 检查评论中是否包含 "LGTM: false"
		if strings.Contains(comment.Body, "LGTM: false") || strings.Contains(comment.Body, "\"lgtm\": false") || strings.Contains(comment.Body, "✖️ 需要修改") {
			allLGTM = false
			allSummaries = append(allSummaries, fmt.Sprintf("❌ `%s` 需要修改", fileName))
		} else {
			allSummaries = append(allSummaries, fmt.Sprintf("✅ `%s` 看起来不错", fileName))
		}
	}
	// 创建总结
	body := ""
	if len(reviewComments) == 0 {
		// 如果没有评论，说明没有需要审查的文件或所有文件都已过滤
		body = "## 代码审查结果 ℹ️\n\n没有发现需要审查的文件。这可能是因为所有文件都被过滤或者变更太小。"
	} else if allLGTM {
		// 如果所有文件都通过了审查
		body = "## 代码审查通过 ✅\n\n所有文件都通过了审查，请查看各文件的详细评论获取更多信息。"
	} else {
		// 如果有文件需要修改
		body = "## 代码审查发现问题 ⚠️\n\n一些文件需要修改，请查看各文件的详细评论获取更多信息。"
	}
	// 添加文件摘要
	if len(allSummaries) > 0 {
		body += "\n\n### 文件摘要:\n" + strings.Join(allSummaries, "\n")
	}
	
	// 添加署名
	body += "\n\n---\n*由 AI 代码审查助手自动生成*"

	latestCommitSHA := commits[len(commits)-1].SHA
	err = b.platform.CreateReview(ctx, owner, repo, number, latestCommitSHA, reviewComments, body)
	if err != nil {
		return fmt.Errorf("failed to create review: %w", err)
	}

	logrus.Infof("Successfully reviewed PR #%d in %s", number, time.Since(start))
	return nil
}
