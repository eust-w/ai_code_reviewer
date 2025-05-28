package bot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eust-w/ai_code_reviewer/internal/git"
	"github.com/eust-w/ai_code_reviewer/internal/indexer"
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
	
	// 如果启用了索引功能，确保仓库已被索引
	if b.indexer != nil {
		logrus.Infof("Checking if repository %s/%s is indexed", owner, repo)
		
		// 获取仓库索引器
		idxr, err := b.indexer.GetIndexer(owner, repo)
		if err != nil {
			logrus.Warnf("Failed to get indexer for %s/%s: %v - continuing without code context", owner, repo, err)
		} else {
			// 获取仓库路径和平台类型
			platformType := b.config.Platform
			var repoURL string
			
			// 根据不同平台构建仓库URL
			switch platformType {
			case "github":
				repoURL = fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
			case "gitlab":
				repoURL = fmt.Sprintf("https://gitlab.com/%s/%s.git", owner, repo)
			case "gitea":
				// 从配置中获取Gitea基础URL
				baseURL := b.config.GiteaBaseURL
				// 移除尾部斜杠
				baseURL = strings.TrimSuffix(baseURL, "/")
				repoURL = fmt.Sprintf("%s/%s/%s.git", baseURL, owner, repo)
			default:
				// 默认使用简单的路径格式
				repoURL = fmt.Sprintf("%s/%s", owner, repo)
			}
			
			// 使用仓库URL作为路径，索引器将使用这个来获取代码
			repoPath := repoURL
			
			// 尝试索引仓库（使用head commit作为分支/引用）
			logrus.Infof("Attempting to index repository %s/%s at commit %s", owner, repo, headSHA)
			
			// 设置环境变量以传递平台和凭证信息
			os.Setenv("PLATFORM", platformType)
			os.Setenv("GITHUB_TOKEN", b.config.GithubToken)
			os.Setenv("GITLAB_TOKEN", b.config.GitlabToken)
			os.Setenv("GITEA_TOKEN", b.config.GiteaToken)
			os.Setenv("GITEA_BASE_URL", b.config.GiteaBaseURL)
			
			indexErr := idxr.IndexRepository(ctx, repoPath, headSHA)
			if indexErr != nil {
				logrus.Warnf("Failed to index repository %s/%s: %v - continuing with partial or no context", owner, repo, indexErr)
			} else {
				logrus.Infof("Successfully indexed repository %s/%s", owner, repo)
			}
		}
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

		// 使用代码索引增强补丁信息（如果启用）
		enhancedPatch := patch
		logrus.Infof("indexer: %v", b.indexer)
		if b.indexer != nil {
			logrus.Infof("Using code indexing to enhance review context for %s", file.Filename)

			// 获取仓库信息
			repoInfo := indexer.RepoInfo{
				Owner:    owner,
				Name:     repo,
				Language: indexer.GetFileLanguage(file.Filename),
				Branch:   "main", // 默认分支，可能需要从PR中获取
			}

			// 查询相关代码上下文
			idxr, err := b.indexer.GetIndexer(owner, repo)
			if err != nil {
				logrus.Warnf("Failed to get indexer for %s/%s: %v - continuing without code context", owner, repo, err)
			} else {
				logrus.Debugf("Successfully obtained indexer for %s/%s", owner, repo)
				// 查询上下文
				codeContextMap, err := idxr.QueryContext(ctx, []*git.CommitFile{file}, repoInfo)
				if err != nil {
					logrus.Warnf("Failed to query code context for %s: %v - continuing without context enhancement", file.Filename, err)
				} else if codeContext, ok := codeContextMap[file.Filename]; ok && codeContext != nil {
					// 使用上下文丰富补丁 - 调用包级函数
					logrus.Debugf("Found code context for %s with %d imports, %d definitions, %d similar snippets",
						file.Filename, len(codeContext.Imports), len(codeContext.Definitions), len(codeContext.SimilarCode))
					enhancedPatch = indexer.EnrichPatchWithContext(patch, codeContext)
					logrus.Infof("Enhanced patch for %s with code context", file.Filename)
				} else {
					logrus.Debugf("No relevant code context found for %s", file.Filename)
				}
			}
		}

		// 使用增强的补丁进行代码审查
		result, err := b.chat.CodeReview(ctx, enhancedPatch)
		if err != nil {
			logrus.Errorf("Failed to review %s: %v", file.Filename, err)
			continue
		}

		// 构建完整的评论内容，包含所有字段
		commentBody := ""
		language := strings.ToLower(b.config.Language)
		// 添加总结
		if result.Summary != "" {
			if language == "english" {
				commentBody += fmt.Sprintf("## Summary\n%s\n\n", result.Summary)
			} else {
				commentBody += fmt.Sprintf("## 总结\n%s\n\n", result.Summary)
			}
		}
		
		// 添加详细评论
		if result.ReviewComment != "" {
			if language == "english" {
				commentBody += fmt.Sprintf("## Review Comment\n%s\n\n", result.ReviewComment)
			} else {
				commentBody += fmt.Sprintf("## 详细评论\n%s\n\n", result.ReviewComment)
			}
		}
		
		// 添加建议
		if result.Suggestions != "" || result.Risks != "" {
			if language == "english" {
				commentBody += fmt.Sprintf("## Suggestions\n%s\n\n", result.Suggestions)
			} else {
				commentBody += fmt.Sprintf("## 改进建议\n%s\n\n", result.Suggestions)
			}
		}else{
			result.LGTM = true
		}
		
		// // 添加亮点
		// if result.Highlights != "" {
		// 	if language == "english" {
		// 		commentBody += fmt.Sprintf("## Highlights\n%s\n\n", result.Highlights)
		// 	} else {
		// 		commentBody += fmt.Sprintf("## 代码亮点\n%s\n\n", result.Highlights)
		// 	}
		// }
		
		// 添加风险
		if result.Risks != "" {
			if language == "english" {
				commentBody += fmt.Sprintf("## Risks\n%s\n\n", result.Risks)
			} else {
				commentBody += fmt.Sprintf("## 潜在风险\n%s\n\n", result.Risks)
			}
		}
		
		// 添加 LGTM 状态
		if !result.LGTM {
			if language == "english" {
				commentBody = fmt.Sprintf("**LGTM: ✖️ Changes Required**\n\n%s", commentBody)
			} else {
				commentBody = fmt.Sprintf("**LGTM: ✖️ 需要修改**\n\n%s", commentBody)
			}
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
