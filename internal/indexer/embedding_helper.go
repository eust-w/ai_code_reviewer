package indexer

import (
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

// GetEmbeddingEndpoint 从环境变量中获取嵌入模型的端点
func GetEmbeddingEndpoint(defaultEndpoint string) string {
	// 从环境变量中获取嵌入模型的端点
	embeddingEndpoint := os.Getenv("INDEXER_LLM_PROXY_ENDPOINT")
	
	// 如果环境变量中没有设置端点，则使用默认端点
	if embeddingEndpoint == "" {
		embeddingEndpoint = defaultEndpoint
	}
	
	// 确保端点以"/embeddings"结尾
	if !strings.HasSuffix(embeddingEndpoint, "/embeddings") {
		if strings.HasSuffix(embeddingEndpoint, "/") {
			embeddingEndpoint += "embeddings"
		} else {
			embeddingEndpoint += "/embeddings"
		}
	}
	
	logrus.Infof("Using embedding endpoint: %s", embeddingEndpoint)
	return embeddingEndpoint
}

// GetEmbeddingAPIKey 从环境变量中获取嵌入模型的API密钥
func GetEmbeddingAPIKey(defaultAPIKey string) string {
	// 从环境变量中获取API密钥
	apiKey := os.Getenv("INDEXER_LLM_PROXY_API_KEY")
	if apiKey == "" {
		apiKey = defaultAPIKey
	}
	
	// 不要在日志中打印API密钥，只打印是否使用了环境变量中的API密钥
	if apiKey != defaultAPIKey {
		logrus.Infof("Using API key from environment variable")
	} else {
		logrus.Infof("Using default API key")
	}
	
	return apiKey
}

// GetEmbeddingModel 从环境变量中获取嵌入模型的名称
func GetEmbeddingModel(defaultModel string) string {
	// 从环境变量中获取模型名称
	model := os.Getenv("INDEXER_LLM_PROXY_MODEL")
	if model == "" {
		model = defaultModel
	}
	
	logrus.Infof("Using embedding model: %s", model)
	return model
}

// GetEmbeddingProvider 从环境变量中获取嵌入模型的提供者
func GetEmbeddingProvider(defaultProvider string) string {
	// 从环境变量中获取提供者
	provider := os.Getenv("INDEXER_LLM_PROXY_PROVIDER")
	if provider == "" {
		provider = defaultProvider
	}
	
	logrus.Infof("Using embedding provider: %s", provider)
	return provider
}

// FormatEmbeddingModel 格式化嵌入模型的名称
func FormatEmbeddingModel(model, provider string) string {
	if provider != "" && !strings.Contains(model, "/") {
		return fmt.Sprintf("%s/%s", provider, model)
	}
	return model
}
