package indexer

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/eust-w/ai_code_reviewer/internal/git"
	"github.com/sirupsen/logrus"
)

// ChromaIndexer 使用Chroma实现的代码索引器
type ChromaIndexer struct {
	repoKey  string
	storage  Storage
	vector   VectorService
	indexMap sync.Map // 文件路径 -> 是否已索引
}

// NewChromaIndexer 创建新的Chroma索引器
func NewChromaIndexer(repoKey string, storage Storage, vector VectorService) *ChromaIndexer {
	return &ChromaIndexer{
		repoKey: repoKey,
		storage: storage,
		vector:  vector,
	}
}

// IndexRepository 索引整个代码库
func (idx *ChromaIndexer) IndexRepository(ctx context.Context, repoPath, ref string) error {
	start := time.Now()
	logrus.Infof("Starting indexing repository %s (ref: %s)", idx.repoKey, ref)

	// 解析仓库所有者和名称
	parts := strings.Split(idx.repoKey, "/")
	if len(parts) < 2 {
		return fmt.Errorf("invalid repository key format: %s, expected owner/repo", idx.repoKey)
	}
	owner := parts[0]
	repo := parts[1]

	// 检查是否需要从远程获取代码
	var localRepoPath string
	fileInfo, err := os.Stat(repoPath)
	
	// 如果路径不存在或不是目录，尝试从远程获取
	if os.IsNotExist(err) || (err == nil && !fileInfo.IsDir()) {
		logrus.Infof("Repository path %s does not exist or is not a directory, attempting to clone", repoPath)
		
		// 从配置中获取平台类型和凭证
		platform := os.Getenv("PLATFORM")
		if platform == "" {
			platform = "github" // 默认使用GitHub
		}
		
		// 凭证映射
		credentials := map[string]string{
			"github_token": os.Getenv("GITHUB_TOKEN"),
			"gitlab_token": os.Getenv("GITLAB_TOKEN"),
			"gitea_token": os.Getenv("GITEA_TOKEN"),
			"gitea_base_url": os.Getenv("GITEA_BASE_URL"),
		}
		
		// 克隆或更新仓库
		localRepoPath, err = CloneOrUpdateRepo(platform, owner, repo, ref, credentials)
		if err != nil {
			return fmt.Errorf("failed to clone or update repository: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to check repository path: %w", err)
	} else {
		// 使用提供的路径
		localRepoPath = repoPath
		logrus.Infof("Using existing repository path: %s", localRepoPath)
	}

	// 遍历仓库中的所有文件
	filesIndexed := 0
	snippetsIndexed := 0

	err = filepath.Walk(localRepoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录
		if info.IsDir() {
			// 跳过常见的不需要索引的目录
			basename := filepath.Base(path)
			if basename == ".git" || basename == "node_modules" || basename == "vendor" || basename == ".vscode" {
				return filepath.SkipDir
			}
			return nil
		}

		// 获取相对路径
		relPath, err := filepath.Rel(localRepoPath, path)
		if err != nil {
			logrus.Warnf("Failed to get relative path for %s: %v", path, err)
			return nil
		}

		// 跳过二进制文件和其他不需要索引的文件
		if !shouldIndexFile(relPath) {
			return nil
		}

		// 读取文件内容
		content, err := ioutil.ReadFile(path)
		if err != nil {
			logrus.Warnf("Failed to read file %s: %v", relPath, err)
			return nil
		}

		// 索引文件，传递commit hash
		snippets, err := idx.indexFile(ctx, relPath, string(content), ref)
		if err != nil {
			logrus.Warnf("Failed to index file %s: %v", relPath, err)
			return nil
		}

		filesIndexed++
		snippetsIndexed += snippets

		// 定期记录进度
		if filesIndexed%100 == 0 {
			logrus.Infof("Indexed %d files so far...", filesIndexed)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk repository: %w", err)
	}

	duration := time.Since(start)
	LogIndexStats(idx.repoKey, filesIndexed, snippetsIndexed, duration.String())

	return nil
}

// UpdateIndex 更新索引（增量）
func (idx *ChromaIndexer) UpdateIndex(ctx context.Context, repoPath, fromCommit, toCommit string) error {
	logrus.Infof("Updating index for repository %s (from %s to %s)", idx.repoKey, fromCommit, toCommit)

	// 在实际实现中，这里应该使用git命令或库获取变更的文件
	// 这里简化为重新索引整个仓库
	return idx.IndexRepository(ctx, repoPath, toCommit)
}

// QueryContext 查询与变更文件相关的代码上下文
func (idx *ChromaIndexer) QueryContext(ctx context.Context, changedFiles []*git.CommitFile, repoInfo RepoInfo) (map[string]*CodeContext, error) {
	logrus.Infof("Querying context for %d changed files in repository %s", len(changedFiles), idx.repoKey)
	
	// 获取commit hash，用于查询特定版本的代码上下文
	commitHash := ""
	if repoInfo.HeadSHA != "" {
		commitHash = repoInfo.HeadSHA
		logrus.Infof("Using commit hash %s for context queries", commitHash)
	}

	result := make(map[string]*CodeContext)

	for _, file := range changedFiles {
		filename := file.Filename
		language := GetFileLanguage(filename)

		// 创建代码上下文
		context := &CodeContext{
			Imports:     make([]string, 0),
			Definitions: make(map[string]string),
			References:  make([]string, 0),
			Dependencies: make([]string, 0),
			SimilarCode: make([]CodeSnippet, 0),
		}

		// 查询相关导入，传递commit hash
		imports, err := idx.queryImports(ctx, filename, commitHash)
		if err != nil {
			logrus.Warnf("Failed to query imports for %s: %v", filename, err)
		} else {
			context.Imports = imports
		}

		// 查询相关定义，传递commit hash
		definitions, err := idx.queryDefinitions(ctx, filename, commitHash)
		if err != nil {
			logrus.Warnf("Failed to query definitions for %s: %v", filename, err)
		} else {
			context.Definitions = definitions
		}

		// 查询相似代码，传递commit hash
		if file.Patch != "" {
			similarCode, err := idx.querySimilarCode(ctx, language, file.Patch, commitHash)
			if err != nil {
				logrus.Warnf("Failed to query similar code for %s: %v", filename, err)
			} else {
				context.SimilarCode = similarCode
			}
		}

		result[filename] = context
	}

	logrus.Infof("Context query completed")
	return result, nil
}

// Close 关闭索引器
func (idx *ChromaIndexer) Close() error {
	return idx.storage.Close()
}

// indexFile 索引单个文件
func (idx *ChromaIndexer) indexFile(ctx context.Context, filename, content string, commitHash string) (int, error) {
	// 跳过空文件
	if len(content) == 0 {
		return 0, nil
	}

	// 对于非常大的文件，可能需要分块处理
	if len(content) > 50000 {
		return idx.indexLargeFile(ctx, filename, GetFileLanguage(filename), content, commitHash)
	}

	language := GetFileLanguage(filename)
	
	// 将文件标记为已索引
	idx.indexMap.Store(filename, true)
	
	// 创建元数据
	metadata := map[string]interface{}{
		"repo_key":    idx.repoKey,
		"filename":    filename,
		"language":    language,
		"line_start":  1,
		"line_end":    strings.Count(content, "\n") + 1,
		"indexed_at":  time.Now().Unix(),
		"commit_hash": commitHash,
	}
	
	_, err := idx.storage.SaveCodeSnippet(ctx, idx.repoKey, filename, content, metadata)
	if err != nil {
		return 0, fmt.Errorf("failed to save code snippet: %w", err)
	}
	
	return 1, nil
}

// indexLargeFile 索引大文件（分块处理）
func (idx *ChromaIndexer) indexLargeFile(ctx context.Context, filename, language, content string, commitHash string) (int, error) {
	lines := strings.Split(content, "\n")
	
	// 每块最多500行
	const chunkSize = 500
	snippetsIndexed := 0
	
	for i := 0; i < len(lines); i += chunkSize {
		end := i + chunkSize
		if end > len(lines) {
			end = len(lines)
		}
		
		chunkContent := strings.Join(lines[i:end], "\n")
		
		metadata := map[string]interface{}{
			"repo_key":    idx.repoKey,
			"filename":    filename,
			"language":    language,
			"line_start":  i + 1,
			"line_end":    end,
			"indexed_at":  time.Now().Unix(),
			"commit_hash": commitHash,
		}
		
		_, err := idx.storage.SaveCodeSnippet(ctx, idx.repoKey, filename, chunkContent, metadata)
		if err != nil {
			return snippetsIndexed, fmt.Errorf("failed to save code chunk: %w", err)
		}
		
		snippetsIndexed++
	}
	
	return snippetsIndexed, nil
}

// queryImports 查询文件的导入语句
func (idx *ChromaIndexer) queryImports(ctx context.Context, filename string, commitHash string) ([]string, error) {
	// 在实际实现中，这里应该查询存储中的导入信息
	// 如果提供了commit hash，则应该查询特定版本的代码
	if commitHash != "" {
		logrus.Debugf("Querying imports for %s at commit %s", filename, commitHash)
	}
	
	// 这里返回模拟数据
	return []string{
		"import example/package",
		"import another/dependency",
	}, nil
}

// queryDefinitions 查询文件的定义
func (idx *ChromaIndexer) queryDefinitions(ctx context.Context, filename string, commitHash string) (map[string]string, error) {
	// 在实际实现中，这里应该查询存储中的定义信息
	// 如果提供了commit hash，则应该查询特定版本的代码
	if commitHash != "" {
		logrus.Debugf("Querying definitions for %s at commit %s", filename, commitHash)
	}
	
	// 这里返回模拟数据
	return map[string]string{
		"Function1": "func Function1() error",
		"Type1":     "type Type1 struct { ... }",
	}, nil
}

// querySimilarCode 查询与补丁相似的代码
func (idx *ChromaIndexer) querySimilarCode(ctx context.Context, language, patch string, commitHash string) ([]CodeSnippet, error) {
	// 在实际实现中，这里应该使用向量服务查询相似代码
	// 如果提供了commit hash，则应该查询特定版本的代码
	if commitHash != "" {
		logrus.Debugf("Querying similar code for patch at commit %s", commitHash)
	}
	
	// 这里返回模拟数据
	return []CodeSnippet{
		{
			Filename:   "similar/file1.go",
			Content:    "func SimilarFunction() { ... }",
			Similarity: 0.85,
			LineStart:  10,
			LineEnd:    20,
		},
		{
			Filename:   "similar/file2.go",
			Content:    "type SimilarType struct { ... }",
			Similarity: 0.75,
			LineStart:  30,
			LineEnd:    40,
		},
	}, nil
}

// shouldIndexFile 判断文件是否应该被索引
func shouldIndexFile(filename string) bool {
	// 跳过常见的不需要索引的文件
	skipExtensions := []string{
		".exe", ".bin", ".obj", ".o", ".a", ".so", ".dll", ".dylib",
		".jar", ".war", ".ear", ".class",
		".zip", ".tar", ".gz", ".bz2", ".7z", ".rar",
		".jpg", ".jpeg", ".png", ".gif", ".bmp", ".ico", ".svg",
		".mp3", ".mp4", ".avi", ".mov", ".wmv",
		".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".lock", ".sum",
	}
	
	for _, ext := range skipExtensions {
		if strings.HasSuffix(strings.ToLower(filename), ext) {
			return false
		}
	}
	
	// 跳过隐藏文件
	if strings.HasPrefix(filepath.Base(filename), ".") {
		return false
	}
	
	return true
}
