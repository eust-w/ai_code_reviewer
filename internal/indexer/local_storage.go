package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// LocalStorage 使用本地文件系统作为存储后端
type LocalStorage struct {
	basePath string
	mu       sync.RWMutex
	metadata map[string]map[string]interface{} // id -> metadata
}

// NewLocalStorage 创建新的本地存储
func NewLocalStorage(basePath string) (*LocalStorage, error) {
	// 确保目录存在
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	return &LocalStorage{
		basePath: basePath,
		metadata: make(map[string]map[string]interface{}),
	}, nil
}

// getRepoPath 获取仓库目录路径
// 如果提供了commitHash，则按commit hash分库
func (s *LocalStorage) getRepoPath(repoKey string, commitHash ...string) string {
	// 将仓库键转换为有效的目录名
	safeName := strings.ReplaceAll(repoKey, "/", "_")
	
	// 如果提供了commit hash，则将其添加到路径中
	if len(commitHash) > 0 && commitHash[0] != "" {
		// 使用前8位作为目录名，足够区分大多数commit
		shortHash := commitHash[0]
		if len(shortHash) > 8 {
			shortHash = shortHash[:8]
		}
		return filepath.Join(s.basePath, safeName, "commits", shortHash)
	}
	
	// 如果没有提供或为空，则使用默认路径
	return filepath.Join(s.basePath, safeName, "default")
}

// getSnippetPath 获取代码片段文件路径
func (s *LocalStorage) getSnippetPath(id string, commitHash ...string) string {
	// 从定义提取仓库键
	parts := strings.Split(id, "_")
	if len(parts) < 2 {
		return filepath.Join(s.basePath, "unknown", id+".code")
	}
	
	repoKey := parts[0] + "/" + parts[1]
	
	// 使用commit hash构建路径
	var hash string
	if len(commitHash) > 0 && commitHash[0] != "" {
		hash = commitHash[0]
	}
	
	repoPath := s.getRepoPath(repoKey, hash)
	
	return filepath.Join(repoPath, id+".code")
}

// getMetadataPath 获取元数据文件路径
func (s *LocalStorage) getMetadataPath(id string, commitHash ...string) string {
	return s.getSnippetPath(id, commitHash...) + ".meta"
}

// SaveCodeSnippet 保存代码片段
func (s *LocalStorage) SaveCodeSnippet(ctx context.Context, repoKey, filename, content string, metadata map[string]interface{}) (string, error) {
	// 生成唯一ID
	id := fmt.Sprintf("%s_%s_%d", repoKey, strings.ReplaceAll(filename, "/", "_"), time.Now().UnixNano())
	
	// 检查元数据中是否包含commit hash
	var commitHash string
	if metadata != nil {
		if hash, ok := metadata["commit_hash"].(string); ok && hash != "" {
			commitHash = hash
		}
	}
	
	// 确保仓库目录存在，传入commit hash进行分库
	repoPath := s.getRepoPath(repoKey, commitHash)
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create repository directory: %w", err)
	}
	
	// 保存代码片段，传递commit hash
	snippetPath := s.getSnippetPath(id, commitHash)
	
	// 确保片段文件的父目录存在
	snippetDir := filepath.Dir(snippetPath)
	if err := os.MkdirAll(snippetDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create snippet directory: %w", err)
	}
	
	if err := os.WriteFile(snippetPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write code snippet: %w", err)
	}
	
	// 确保元数据包含必要字段
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["repo_key"] = repoKey
	metadata["filename"] = filename
	metadata["indexed_at"] = time.Now().Unix()
	
	// 保存元数据，传递commit hash
	metadataPath := s.getMetadataPath(id, commitHash)
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("failed to marshal metadata: %w", err)
	}
	
	// 确保元数据文件的父目录存在
	metadataDir := filepath.Dir(metadataPath)
	if err := os.MkdirAll(metadataDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create metadata directory: %w", err)
	}
	
	if err := os.WriteFile(metadataPath, metadataBytes, 0644); err != nil {
		return "", fmt.Errorf("failed to write metadata: %w", err)
	}
	
	// 缓存元数据
	s.mu.Lock()
	s.metadata[id] = metadata
	s.mu.Unlock()
	
	return id, nil
}

// GetCodeSnippet 获取代码片段
func (s *LocalStorage) GetCodeSnippet(ctx context.Context, id string) (string, map[string]interface{}, error) {
	// 读取代码片段
	snippetPath := s.getSnippetPath(id)
	content, err := os.ReadFile(snippetPath)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read code snippet: %w", err)
	}
	
	// 检查缓存中是否有元数据
	s.mu.RLock()
	metadata, exists := s.metadata[id]
	s.mu.RUnlock()
	
	if exists {
		return string(content), metadata, nil
	}
	
	// 读取元数据
	metadataPath := s.getMetadataPath(id)
	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		return string(content), nil, fmt.Errorf("failed to read metadata: %w", err)
	}
	
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return string(content), nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}
	
	// 缓存元数据
	s.mu.Lock()
	s.metadata[id] = metadata
	s.mu.Unlock()
	
	return string(content), metadata, nil
}

// DeleteCodeSnippet 删除代码片段
func (s *LocalStorage) DeleteCodeSnippet(ctx context.Context, id string) error {
	// 删除代码片段
	snippetPath := s.getSnippetPath(id)
	if err := os.Remove(snippetPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete code snippet: %w", err)
	}
	
	// 删除元数据
	metadataPath := s.getMetadataPath(id)
	if err := os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete metadata: %w", err)
	}
	
	// 从缓存中删除
	s.mu.Lock()
	delete(s.metadata, id)
	s.mu.Unlock()
	
	return nil
}

// ListSnippetsByFile 列出指定文件的所有代码片段
func (s *LocalStorage) ListSnippetsByFile(ctx context.Context, repoKey, filename string) ([]string, error) {
	repoPath := s.getRepoPath(repoKey)
	
	// 列出目录中的所有文件
	entries, err := os.ReadDir(repoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read repository directory: %w", err)
	}
	
	// 过滤出与指定文件相关的代码片段
	var snippets []string
	prefix := fmt.Sprintf("%s_%s", repoKey, strings.ReplaceAll(filename, "/", "_"))
	
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".code") {
			name := strings.TrimSuffix(entry.Name(), ".code")
			if strings.HasPrefix(name, prefix) {
				snippets = append(snippets, name)
			}
		}
	}
	
	return snippets, nil
}

// ListSnippetsByRepo 列出指定仓库的所有代码片段
func (s *LocalStorage) ListSnippetsByRepo(ctx context.Context, repoKey string) ([]string, error) {
	repoPath := s.getRepoPath(repoKey)
	
	// 列出目录中的所有文件
	entries, err := os.ReadDir(repoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read repository directory: %w", err)
	}
	
	// 过滤出代码片段
	var snippets []string
	
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".code") {
			name := strings.TrimSuffix(entry.Name(), ".code")
			snippets = append(snippets, name)
		}
	}
	
	return snippets, nil
}

// Close 关闭存储
func (s *LocalStorage) Close() error {
	// 本地存储不需要特殊的关闭操作
	logrus.Info("Closing local storage")
	return nil
}
