package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// VectorService 向量化服务接口
type VectorService interface {
	// EmbedCode 将代码片段转换为向量
	EmbedCode(ctx context.Context, language, content string) ([]float32, error)

	// EmbedQuery 将查询转换为向量
	EmbedQuery(ctx context.Context, query string) ([]float32, error)

	// Close 关闭服务
	Close() error
}

// VectorConfig 向量服务配置
type VectorConfig struct {
	// OpenAI配置
	OpenAIAPIKey string
	OpenAIModel  string

	// LLM代理配置
	LLMProxyEndpoint string
	LLMProxyAPIKey   string
	LLMProxyModel    string
	LLMProxyProvider string // "azure", "cohere", 等
}

// NewVectorService 创建向量服务
func NewVectorService(config *VectorConfig) (VectorService, error) {
	if config.LLMProxyEndpoint != "" && config.LLMProxyAPIKey != "" {
		// 使用LLM代理
		return NewLLMProxyVectorService(
			config.LLMProxyEndpoint,
			config.LLMProxyAPIKey,
			config.LLMProxyModel,
			config.LLMProxyProvider,
		)
	}

	if config.OpenAIAPIKey != "" {
		// 使用OpenAI
		return NewOpenAIVectorService(config.OpenAIAPIKey, config.OpenAIModel)
	}

	// 如果没有配置任何向量服务，返回错误
	return nil, fmt.Errorf("没有配置有效的向量服务，请配置OpenAI或LLM代理")
}

// normalizeValue 将值标准化到 0-1 范围
func normalizeValue(value, maxValue float32) float32 {
	normalized := value / maxValue
	if normalized > 1.0 {
		return 1.0
	}
	return normalized
}

// OpenAIVectorService 使用OpenAI API的向量服务
type OpenAIVectorService struct {
	apiKey string
	model  string
}

// NewOpenAIVectorService 创建OpenAI向量服务
func NewOpenAIVectorService(apiKey, model string) (*OpenAIVectorService, error) {
	if model == "" {
		model = "text-embedding-3-small" // 默认模型
	}

	return &OpenAIVectorService{
		apiKey: apiKey,
		model:  model,
	}, nil
}

// EmbedCode 将代码片段转换为向量
func (s *OpenAIVectorService) EmbedCode(ctx context.Context, language, content string) ([]float32, error) {
	logrus.Infof("Embedding code with OpenAI API (model: %s)", s.model)

	// 添加语言信息作为上下文
	input := fmt.Sprintf("Language: %s\n\n%s", language, content)

	// 准备请求数据
	reqData := OpenAIEmbeddingRequest{
		Model: s.model,
		Input: []string{input},
	}

	// 将请求数据转换为JSON
	reqBody, err := json.Marshal(reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request data: %w", err)
	}

	// 创建 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/embeddings", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	// 创建 HTTP 客户端并设置超时
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenAI API request failed with status code %d: %s", resp.StatusCode, string(respBody))
	}

	// 解析响应数据
	var embedResp OpenAIEmbeddingResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// 检查是否有嵌入数据
	if len(embedResp.Data) == 0 || len(embedResp.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("no embedding data in response")
	}

	logrus.Infof("Successfully obtained embedding vector with %d dimensions", len(embedResp.Data[0].Embedding))

	// 返回嵌入向量
	return embedResp.Data[0].Embedding, nil
}

// EmbedQuery 将查询转换为向量
func (s *OpenAIVectorService) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	logrus.Infof("Embedding query with OpenAI API (model: %s)", s.model)

	// 准备请求数据
	reqData := OpenAIEmbeddingRequest{
		Model: s.model,
		Input: []string{query},
	}

	// 将请求数据转换为JSON
	reqBody, err := json.Marshal(reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request data: %w", err)
	}

	// 创建 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/embeddings", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	// 创建 HTTP 客户端并设置超时
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenAI API request failed with status code %d: %s", resp.StatusCode, string(respBody))
	}

	// 解析响应数据
	var embedResp OpenAIEmbeddingResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// 检查是否有嵌入数据
	if len(embedResp.Data) == 0 || len(embedResp.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("no embedding data in response")
	}

	logrus.Infof("Successfully obtained embedding vector with %d dimensions", len(embedResp.Data[0].Embedding))

	// 返回嵌入向量
	return embedResp.Data[0].Embedding, nil
}

// Close 关闭服务
func (s *OpenAIVectorService) Close() error {
	return nil
}

// LLMProxyVectorService 使用LLM代理的向量服务
type LLMProxyVectorService struct {
	endpoint string
	apiKey   string
	model    string
	provider string
}

// NewLLMProxyVectorService 创建LLM代理向量服务
func NewLLMProxyVectorService(endpoint, apiKey, model, provider string) (*LLMProxyVectorService, error) {
	// 如果没有指定模型，根据提供商设置默认模型
	if model == "" {
		switch provider {
		case "azure":
			model = "text-embedding-3-large"
		case "cohere":
			model = "embed-multilingual-v3"
		default:
			model = "text-embedding-3-large" // 默认使用Azure模型
		}
	}

	logrus.Infof("Creating LLM Proxy vector service with provider: %s, model: %s", provider, model)

	return &LLMProxyVectorService{
		endpoint: endpoint,
		apiKey:   apiKey,
		model:    model,
		provider: provider,
	}, nil
}

// OpenAIEmbeddingRequest OpenAI兼容的嵌入请求结构
type OpenAIEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// OpenAIEmbeddingResponse OpenAI兼容的嵌入响应结构
type OpenAIEmbeddingResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// EmbedCode 将代码片段转换为向量
func (s *LLMProxyVectorService) EmbedCode(ctx context.Context, language, content string) ([]float32, error) {
	// 从环境变量中获取模型和提供者
	model := os.Getenv("INDEXER_LLM_PROXY_MODEL")
	if model == "" {
		model = s.model
	}
	
	provider := os.Getenv("INDEXER_LLM_PROXY_PROVIDER")
	if provider == "" {
		provider = s.provider
	}
	
	// 格式化模型名称
	formattedModel := model
	if provider != "" && !strings.Contains(model, "/") {
		formattedModel = provider + "/" + model
	}

	logrus.Infof("Embedding code with LLM Proxy API (provider: %s, model: %s)", provider, formattedModel)

	// 准备请求数据
	// 添加语言信息作为上下文
	input := fmt.Sprintf("Language: %s\n\n%s", language, content)

	// 调用嵌入API
	return s.getEmbedding(ctx, formattedModel, input)
}

// EmbedQuery 将查询转换为向量
func (s *LLMProxyVectorService) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	// 从环境变量中获取模型和提供者
	model := os.Getenv("INDEXER_LLM_PROXY_MODEL")
	if model == "" {
		model = s.model
	}
	
	provider := os.Getenv("INDEXER_LLM_PROXY_PROVIDER")
	if provider == "" {
		provider = s.provider
	}
	
	// 格式化模型名称
	formattedModel := model
	if provider != "" && !strings.Contains(model, "/") {
		formattedModel = provider + "/" + model
	}

	logrus.Infof("Embedding query with LLM Proxy API (provider: %s, model: %s)", provider, formattedModel)

	// 调用嵌入API
	return s.getEmbedding(ctx, formattedModel, query)
}

// getEmbedding 从 LLM 代理获取嵌入向量
func (s *LLMProxyVectorService) getEmbedding(ctx context.Context, model, input string) ([]float32, error) {
	// 准备请求数据
	reqData := OpenAIEmbeddingRequest{
		Model: model,
		Input: []string{input},
	}

	// 将请求数据转换为JSON
	reqBody, err := json.Marshal(reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request data: %w", err)
	}
	
	// 使用辅助函数获取嵌入模型的端点和API密钥
	embeddingEndpoint := GetEmbeddingEndpoint(s.endpoint)
	// 使用GetEmbeddingAPIKey函数获取API密钥
	apiKey := GetEmbeddingAPIKey(s.apiKey)

	req, err := http.NewRequestWithContext(ctx, "POST", embeddingEndpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// 创建 HTTP 客户端并设置超时
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status code %d: %s", resp.StatusCode, string(respBody))
	}

	// 解析响应数据
	var embedResp OpenAIEmbeddingResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// 检查是否有嵌入数据
	if len(embedResp.Data) == 0 || len(embedResp.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("no embedding data in response")
	}

	logrus.Infof("Successfully obtained embedding vector with %d dimensions", len(embedResp.Data[0].Embedding))

	// 返回嵌入向量
	return embedResp.Data[0].Embedding, nil
}

// Close 关闭服务
func (s *LLMProxyVectorService) Close() error {
	return nil
}
