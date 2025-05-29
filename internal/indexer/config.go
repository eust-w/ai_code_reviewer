package indexer

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

// Config 索引器配置
type Config struct {
	// 存储配置
	StorageType     string // "chroma", "local"
	ChromaHost      string
	ChromaPort      int
	ChromaPath      string
	ChromaUsername  string
	ChromaPassword  string
	ChromaSSL       bool
	LocalStoragePath string

	// 向量服务配置
	VectorType      string // "openai", "local", "simple", "llm_proxy"
	OpenAIAPIKey    string
	OpenAIModel     string
	LocalModelPath  string
	
	// LLM代理配置
	LLMProxyEndpoint string
	LLMProxyAPIKey   string
	LLMProxyModel    string
	LLMProxyProvider string

	// 索引配置
	MaxFileSizeBytes int64
	ChunkSize        int
	SkipPatterns     []string
	IncludePatterns  []string
}

// NewConfigFromEnv 从环境变量创建配置
func NewConfigFromEnv() *Config {
	config := &Config{
		// 默认值
		StorageType:     "local",
		ChromaHost:      "localhost",
		ChromaPort:      8000,
		ChromaPath:      "",
		ChromaSSL:       false,
		LocalStoragePath: "./data/index",
		VectorType:      "simple",
		OpenAIModel:     "text-embedding-3-small",
		LLMProxyModel:   "text-embedding-3-large", // 默认使用Azure模型
		LLMProxyProvider: "azure",                  // 默认使用Azure提供商
		MaxFileSizeBytes: 1024 * 1024, // 1MB
		ChunkSize:       500,          // 500行
	}

	// 存储配置
	if val := os.Getenv("INDEXER_STORAGE_TYPE"); val != "" {
		config.StorageType = val
	}

	if val := os.Getenv("INDEXER_CHROMA_HOST"); val != "" {
		config.ChromaHost = val
	}

	if val := os.Getenv("INDEXER_CHROMA_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			config.ChromaPort = port
		}
	}

	if val := os.Getenv("INDEXER_CHROMA_PATH"); val != "" {
		config.ChromaPath = val
	}

	if val := os.Getenv("INDEXER_CHROMA_USERNAME"); val != "" {
		config.ChromaUsername = val
	}

	if val := os.Getenv("INDEXER_CHROMA_PASSWORD"); val != "" {
		config.ChromaPassword = val
	}

	if val := os.Getenv("INDEXER_CHROMA_SSL"); val != "" {
		config.ChromaSSL = (strings.ToLower(val) == "true" || val == "1")
	}

	if val := os.Getenv("INDEXER_LOCAL_STORAGE_PATH"); val != "" {
		config.LocalStoragePath = val
	}

	// 向量服务配置
	if val := os.Getenv("INDEXER_VECTOR_TYPE"); val != "" {
		config.VectorType = val
	}

	if val := os.Getenv("OPENAI_API_KEY"); val != "" {
		config.OpenAIAPIKey = val
	}

	if val := os.Getenv("INDEXER_OPENAI_MODEL"); val != "" {
		config.OpenAIModel = val
	}

	if val := os.Getenv("INDEXER_LOCAL_MODEL_PATH"); val != "" {
		config.LocalModelPath = val
	}
	
	// LLM代理配置
	if val := os.Getenv("LLM_PROXY_ENDPOINT"); val != "" {
		config.LLMProxyEndpoint = val
	}
	
	if val := os.Getenv("LLM_PROXY_API_KEY"); val != "" {
		config.LLMProxyAPIKey = val
	}
	
	if val := os.Getenv("INDEXER_LLM_PROXY_MODEL"); val != "" {
		config.LLMProxyModel = val
	}
	
	if val := os.Getenv("INDEXER_LLM_PROXY_PROVIDER"); val != "" {
		config.LLMProxyProvider = val
	}

	// 索引配置
	if val := os.Getenv("INDEXER_MAX_FILE_SIZE"); val != "" {
		if size, err := strconv.ParseInt(val, 10, 64); err == nil {
			config.MaxFileSizeBytes = size
		}
	}

	if val := os.Getenv("INDEXER_CHUNK_SIZE"); val != "" {
		if size, err := strconv.Atoi(val); err == nil {
			config.ChunkSize = size
		}
	}

	if val := os.Getenv("INDEXER_SKIP_PATTERNS"); val != "" {
		config.SkipPatterns = strings.Split(val, ",")
	}

	if val := os.Getenv("INDEXER_INCLUDE_PATTERNS"); val != "" {
		config.IncludePatterns = strings.Split(val, ",")
	}

	return config
}

// Validate 验证配置
func (c *Config) Validate() error {
	// 验证存储配置
	switch c.StorageType {
	case "chroma":
		if c.ChromaHost == "" {
			return fmt.Errorf("chroma host is required when storage type is chroma")
		}
		if c.ChromaPort <= 0 {
			return fmt.Errorf("invalid chroma port: %d, must be greater than 0", c.ChromaPort)
		}
	case "local":
		if c.LocalStoragePath == "" {
			return fmt.Errorf("local storage path is required when storage type is local")
		}
	default:
		return fmt.Errorf("unsupported storage type: %s", c.StorageType)
	}

	// 验证向量服务配置
	switch c.VectorType {
	case "openai":
		if c.OpenAIAPIKey == "" {
			return fmt.Errorf("openai api key is required when vector type is openai")
		}
		if c.OpenAIModel == "" {
			c.OpenAIModel = "text-embedding-3-small" // 设置默认模型
		}
	case "local":
		if c.LocalModelPath == "" {
			return fmt.Errorf("local model path is required when vector type is local")
		}
	case "llm_proxy":
		if c.LLMProxyEndpoint == "" {
			return fmt.Errorf("llm proxy endpoint is required when vector type is llm_proxy")
		}
		if c.LLMProxyAPIKey == "" {
			return fmt.Errorf("llm proxy api key is required when vector type is llm_proxy")
		}
		if c.LLMProxyModel == "" {
			// 根据提供商设置默认模型
			switch c.LLMProxyProvider {
			case "azure":
				c.LLMProxyModel = "text-embedding-3-large"
			case "cohere":
				c.LLMProxyModel = "embed-multilingual-v3"
			default:
				c.LLMProxyModel = "text-embedding-3-large" // 默认使用Azure模型
			}
		}
	case "simple":
		// 简单向量服务不需要额外配置
	default:
		return fmt.Errorf("unsupported vector type: %s", c.VectorType)
	}

	// 设置其他默认值
	if c.MaxFileSizeBytes <= 0 {
		c.MaxFileSizeBytes = 1024 * 1024 // 默认为1MB
	}

	if c.ChunkSize <= 0 {
		c.ChunkSize = 100 // 默认为100行
	}

	return nil
}

// CreateStorage 创建存储
func (c *Config) CreateStorage() (Storage, error) {
	switch c.StorageType {
	case "chroma":
		storageConfig := &StorageConfig{
			ChromaHost:     c.ChromaHost,
			ChromaPort:     c.ChromaPort,
			ChromaPath:     c.ChromaPath,
			ChromaUsername: c.ChromaUsername,
			ChromaPassword: c.ChromaPassword,
			ChromaSSL:      c.ChromaSSL,
			// 添加向量服务相关配置
			OpenAIAPIKey:     c.OpenAIAPIKey,
			OpenAIModel:      c.OpenAIModel,
			LLMProxyEndpoint: c.LLMProxyEndpoint,
			LLMProxyAPIKey:   c.LLMProxyAPIKey,
			LLMProxyModel:    c.LLMProxyModel,
			LLMProxyProvider: c.LLMProxyProvider,
		}
		return NewChromaStorage(storageConfig)
	case "local":
		return NewLocalStorage(c.LocalStoragePath)
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", c.StorageType)
	}
}

// CreateVectorService 创建向量服务
func (c *Config) CreateVectorService() (VectorService, error) {
	switch c.VectorType {
	case "openai":
		vectorConfig := &VectorConfig{
			OpenAIAPIKey: c.OpenAIAPIKey,
			OpenAIModel:  c.OpenAIModel,
		}
		return NewVectorService(vectorConfig)
	case "llm_proxy":
		vectorConfig := &VectorConfig{
			LLMProxyEndpoint: c.LLMProxyEndpoint,
			LLMProxyAPIKey:   c.LLMProxyAPIKey,
			LLMProxyModel:    c.LLMProxyModel,
			LLMProxyProvider: c.LLMProxyProvider,
		}
		return NewVectorService(vectorConfig)
	default:
		return nil, fmt.Errorf("unsupported vector type: %s, please use 'openai' or 'llm_proxy'", c.VectorType)
	}
}

// CreateIndexManager creates an index manager based on configuration
func (c *Config) CreateIndexManager() (*IndexManager, error) {
	// 验证配置
	if err := c.Validate(); err != nil {
		return nil, err
	}
	
	// 创建存储对象
	var storage Storage
	var err error
	
	switch c.StorageType {
	case "chroma":
		logrus.Infof("Creating Chroma storage with host: %s, port: %d", c.ChromaHost, c.ChromaPort)
		storageConfig := &StorageConfig{
			ChromaHost: c.ChromaHost,
			ChromaPort: c.ChromaPort,
			ChromaPath: c.ChromaPath,
			ChromaSSL:  c.ChromaSSL,
			// 添加向量服务相关配置
			OpenAIAPIKey:     c.OpenAIAPIKey,
			OpenAIModel:      c.OpenAIModel,
			LLMProxyEndpoint: c.LLMProxyEndpoint,
			LLMProxyAPIKey:   c.LLMProxyAPIKey,
			LLMProxyModel:    c.LLMProxyModel,
			LLMProxyProvider: c.LLMProxyProvider,
		}
		storage, err = NewChromaStorage(storageConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create Chroma storage: %w", err)
		}
	case "local":
		logrus.Infof("Creating local storage at: %s", c.LocalStoragePath)
		storage, err = NewLocalStorage(c.LocalStoragePath)
		if err != nil {
			return nil, fmt.Errorf("failed to create local storage: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", c.StorageType)
	}
	
	// 创建向量服务
	logrus.Infof("Creating vector service with type: %s", c.VectorType)
	vector, err := c.CreateVectorService()
	if err != nil {
		return nil, fmt.Errorf("failed to create vector service: %w", err)
	}
	
	return NewIndexManager(storage, vector), nil
}

// LogConfig 记录配置信息
func (c *Config) LogConfig() {
	logrus.Info("Indexer configuration:")
	logrus.Infof("  Storage type: %s", c.StorageType)
	
	if c.StorageType == "chroma" {
		logrus.Infof("  Chroma host: %s", c.ChromaHost)
		logrus.Infof("  Chroma port: %d", c.ChromaPort)
		logrus.Infof("  Chroma path: %s", c.ChromaPath)
		logrus.Infof("  Chroma SSL: %v", c.ChromaSSL)
	} else if c.StorageType == "local" {
		logrus.Infof("  Local storage path: %s", c.LocalStoragePath)
	}
	
	logrus.Info("Vector Service Configuration:")
	logrus.Infof("  Vector Type: %s", c.VectorType)
	switch c.VectorType {
	case "openai":
		logrus.Infof("  OpenAI Model: %s", c.OpenAIModel)
		if c.OpenAIAPIKey != "" {
			logrus.Info("  OpenAI API Key: [REDACTED]")
		} else {
			logrus.Warn("  OpenAI API Key not set!")
		}
	case "local":
		logrus.Infof("  Local Model Path: %s", c.LocalModelPath)
	case "llm_proxy":
		logrus.Infof("  LLM Proxy Endpoint: %s", c.LLMProxyEndpoint)
		logrus.Info("  LLM Proxy API Key: [REDACTED]")
		logrus.Infof("  LLM Proxy Provider: %s", c.LLMProxyProvider)
		logrus.Infof("  LLM Proxy Model: %s", c.LLMProxyModel)
	case "simple":
		logrus.Info("  Using simple rule-based vector embeddings")
	}
	
	logrus.Infof("  Max file size: %d bytes", c.MaxFileSizeBytes)
	logrus.Infof("  Chunk size: %d lines", c.ChunkSize)
	
	if len(c.SkipPatterns) > 0 {
		logrus.Infof("  Skip patterns: %s", strings.Join(c.SkipPatterns, ", "))
	}
	
	if len(c.IncludePatterns) > 0 {
		logrus.Infof("  Include patterns: %s", strings.Join(c.IncludePatterns, ", "))
	}
}
