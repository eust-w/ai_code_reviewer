package indexer

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/eust-w/ai_code_reviewer/internal/git"
	"github.com/sirupsen/logrus"
)

// CodeContext 表示代码上下文信息
type CodeContext struct {
	Imports     []string          // 导入语句
	Definitions map[string]string // 函数/类定义
	References  []string          // 相关引用
	Dependencies []string         // 依赖关系
	SimilarCode []CodeSnippet     // 相似代码片段
}

// CodeSnippet 表示代码片段
type CodeSnippet struct {
	Filename    string  // 文件名
	Content     string  // 代码内容
	Similarity  float64 // 相似度分数
	LineStart   int     // 开始行号
	LineEnd     int     // 结束行号
}

// RepoInfo 仓库信息
type RepoInfo struct {
	Owner    string
	Name     string
	Language string
	Branch   string
	HeadSHA  string // 当前提交的SHA，用于指定查询特定版本的代码上下文
}

// Indexer 代码索引器接口
type Indexer interface {
	// IndexRepository 索引整个代码库
	IndexRepository(ctx context.Context, repoPath, branch string) error
	
	// UpdateIndex 更新索引（增量）
	UpdateIndex(ctx context.Context, repoPath, fromCommit, toCommit string) error
	
	// QueryContext 查询与变更文件相关的代码上下文
	QueryContext(ctx context.Context, changedFiles []*git.CommitFile, repoInfo RepoInfo) (map[string]*CodeContext, error)
	
	// Close 关闭索引器
	Close() error
}

// IndexManager 管理代码库索引
type IndexManager struct {
	storage  Storage
	vector   VectorService
	indexers map[string]Indexer
	mu       sync.RWMutex
}

// NewIndexManager 创建新的索引管理器
func NewIndexManager(storage Storage, vector VectorService) *IndexManager {
	return &IndexManager{
		storage:  storage,
		vector:   vector,
		indexers: make(map[string]Indexer),
	}
}

// GetIndexer 获取或创建指定仓库的索引器
func (im *IndexManager) GetIndexer(repoOwner, repoName string) (Indexer, error) {
	repoKey := fmt.Sprintf("%s/%s", repoOwner, repoName)
	
	im.mu.RLock()
	indexer, exists := im.indexers[repoKey]
	im.mu.RUnlock()
	
	if exists {
		return indexer, nil
	}
	
	// 创建新的索引器
	im.mu.Lock()
	defer im.mu.Unlock()
	
	// 再次检查，避免竞态条件
	if indexer, exists = im.indexers[repoKey]; exists {
		return indexer, nil
	}
	
	indexer = NewChromaIndexer(repoKey, im.storage, im.vector)
	im.indexers[repoKey] = indexer
	
	return indexer, nil
}

// Close 关闭所有索引器
func (im *IndexManager) Close() error {
	im.mu.Lock()
	defer im.mu.Unlock()
	
	var errs []string
	for key, indexer := range im.indexers {
		if err := indexer.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("failed to close indexer %s: %v", key, err))
		}
	}
	
	if len(errs) > 0 {
		return fmt.Errorf("errors closing indexers: %s", strings.Join(errs, "; "))
	}
	
	return nil
}

// GetFileLanguage 根据文件扩展名确定语言
func GetFileLanguage(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".go":
		return "go"
	case ".js", ".jsx", ".ts", ".tsx":
		return "javascript"
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".c", ".cpp", ".cc", ".h", ".hpp":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".rs":
		return "rust"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	default:
		return "text"
	}
}

// 注意：EnrichPatchWithContext函数已移动到patch_utils.go文件中

// LogIndexStats 记录索引统计信息
func LogIndexStats(repoKey string, filesIndexed, snippetsIndexed int, duration string) {
	logrus.Infof("Indexed repository %s: %d files, %d code snippets in %s", 
		repoKey, filesIndexed, snippetsIndexed, duration)
}
