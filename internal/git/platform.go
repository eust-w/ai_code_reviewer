package git

import (
	"github.com/eust-w/ai_code_reviewer/internal/models"
)

// 使用models包中定义的接口和数据结构
type Platform = models.GitPlatform
type CommitFile = models.CommitFile
type Commit = models.Commit
type PullRequest = models.PullRequest
type ReviewComment = models.ReviewComment
