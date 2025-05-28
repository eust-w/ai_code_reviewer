package indexer

import (
	"context"
	"fmt"
)

// Storage 索引存储接口
type Storage interface {
	// SaveCodeSnippet 保存代码片段
	SaveCodeSnippet(ctx context.Context, repoKey, filename, content string, metadata map[string]interface{}) (string, error)
	
	// GetCodeSnippet 获取代码片段
	GetCodeSnippet(ctx context.Context, id string) (string, map[string]interface{}, error)
	
	// DeleteCodeSnippet 删除代码片段
	DeleteCodeSnippet(ctx context.Context, id string) error
	
	// ListSnippetsByFile 列出指定文件的所有代码片段
	ListSnippetsByFile(ctx context.Context, repoKey, filename string) ([]string, error)
	
	// ListSnippetsByRepo 列出指定仓库的所有代码片段
	ListSnippetsByRepo(ctx context.Context, repoKey string) ([]string, error)
	
	// Close 关闭存储
	Close() error
}

// StorageConfig 存储配置
type StorageConfig struct {
	// Chroma配置
	ChromaHost     string
	ChromaPort     int
	ChromaPath     string
	ChromaUsername string
	ChromaPassword string
	ChromaSSL      bool
	
	// 本地存储配置
	LocalStoragePath string
	
	// 其他配置...
}

// NewStorage 创建存储实例
func NewStorage(config *StorageConfig) (Storage, error) {
	if config.ChromaHost != "" {
		// 使用Chroma作为存储
		return NewChromaStorage(config)
	}
	
	if config.LocalStoragePath != "" {
		// 使用本地存储
		return NewLocalStorage(config.LocalStoragePath)
	}
	
	return nil, fmt.Errorf("no valid storage configuration provided")
}

// SnippetMetadata 代码片段元数据
type SnippetMetadata struct {
	RepoKey     string   `json:"repo_key"`
	Filename    string   `json:"filename"`
	Language    string   `json:"language"`
	LineStart   int      `json:"line_start"`
	LineEnd     int      `json:"line_end"`
	Symbols     []string `json:"symbols"`      // 包含的符号（函数、类等）
	Imports     []string `json:"imports"`      // 导入语句
	References  []string `json:"references"`   // 引用的其他符号
	CommitID    string   `json:"commit_id"`    // 索引时的提交ID
	IndexedAt   int64    `json:"indexed_at"`   // 索引时间戳
	FileVersion string   `json:"file_version"` // 文件版本（哈希）
}
