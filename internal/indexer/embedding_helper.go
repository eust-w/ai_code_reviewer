package indexer

import (
	"fmt"
	"math"
	"os"
	"strings"
	"unicode/utf8"

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

// SplitTextIntoChunks 将文本分割成多个块，以适应模型的上下文窗口大小
// maxTokens: 模型的最大上下文窗口大小（以token为单位）
// overlap: 相邻块之间的重叠token数，以保持上下文连贯性
func SplitTextIntoChunks(text string, maxTokens int, overlap int) []string {
	if maxTokens <= 0 {
		maxTokens = 8000 // 默认使用8000作为安全值
	}
	
	if overlap < 0 {
		overlap = 0
	}
	
	// 如果overlap大于maxTokens的一半，则将其限制为maxTokens的一半
	if overlap > maxTokens/2 {
		overlap = maxTokens/2
	}
	
	// 估算文本的token数量
	// 这是一个简单的估算，实际token数量取决于模型的分词器
	// 一般来说，对于英文文本，1个token约等于4个字符
	// 对于中文文本，1个token约等于1-2个字符
	// 这里我们使用一个保守的估计：平均每个UTF-8字符约等于1个token
	runeCount := utf8.RuneCountInString(text)
	
	// 如果文本的token数量小于maxTokens，则直接返回整个文本
	if runeCount <= maxTokens {
		return []string{text}
	}
	
	// 计算每个块的大小（以字符为单位）
	chunkSize := maxTokens - overlap
	
	// 计算需要多少个块
	numChunks := int(math.Ceil(float64(runeCount) / float64(chunkSize)))
	
	// 将文本分割成多个块
	chunks := make([]string, 0, numChunks)
	runes := []rune(text)
	
	for i := 0; i < numChunks; i++ {
		start := i * chunkSize
		if i > 0 {
			// 对于除第一个块外的所有块，从前一个块的末尾减去overlap开始
			start = i*chunkSize - overlap
		}
		
		end := start + maxTokens
		if end > len(runes) {
			end = len(runes)
		}
		
		chunk := string(runes[start:end])
		chunks = append(chunks, chunk)
		
		// 如果已经处理到文本末尾，则退出循环
		if end == len(runes) {
			break
		}
	}
	
	logrus.Infof("Split text into %d chunks (original size: %d characters, max tokens: %d, overlap: %d)",
		len(chunks), runeCount, maxTokens, overlap)
	
	return chunks
}

// EstimateTokenCount 估算文本的token数量
// 这是一个简单的估算，实际token数量取决于模型的分词器
func EstimateTokenCount(text string) int {
	// 对于英文文本，1个token约等于4个字符
	// 对于中文文本，1个token约等于1-2个字符
	// 这里我们使用一个保守的估计：平均每个UTF-8字符约等于1个token
	return utf8.RuneCountInString(text)
}
