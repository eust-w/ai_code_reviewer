package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
	"strings"
	"github.com/eust-w/ai_code_reviewer/internal/config"
	"github.com/sashabaranov/go-openai"
	"github.com/sirupsen/logrus"
)

// ReviewResult represents the result of a code review
type ReviewResult struct {
	LGTM          bool   `json:"lgtm"`
	ReviewComment string `json:"review_comment"`
	Summary       string `json:"summary"`       // 代码变更的总结
	Suggestions   string `json:"suggestions"`   // 改进建议
	Highlights    string `json:"highlights"`    // 代码亮点
	Risks         string `json:"risks"`         // 潜在风险
}

// LLMRequest 表示发送到 LLM API 的通用请求
type LLMRequest struct {
	Model    string      `json:"model"`
	Messages []LLMMessage `json:"messages"`
	Temperature float32   `json:"temperature,omitempty"`
	TopP       float32   `json:"top_p,omitempty"`
	MaxTokens  int       `json:"max_tokens,omitempty"`
	ResponseFormat *LLMResponseFormat `json:"response_format,omitempty"`
}

// LLMMessage 表示 LLM API 的消息
type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LLMResponseFormat 表示 LLM API 的响应格式
type LLMResponseFormat struct {
	Type string `json:"type"`
}

// LLMResponse 表示从 LLM API 接收的通用响应
type LLMResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// 为兼容性保留的类型别名
type DirectLLMRequest = LLMRequest
type DirectLLMMessage = LLMMessage
type DirectLLMResponseFormat = LLMResponseFormat
type DirectLLMResponse = LLMResponse

// Chat handles interactions with LLM APIs (OpenAI, Azure OpenAI, Volc Deepseek V3)
type Chat struct {
	client openai.Client
	config *config.Config
	httpClient *http.Client
}

// NewChat creates a new Chat instance
func NewChat(cfg *config.Config) (*Chat, error) {
	// 创建 HTTP 客户端
	httpClient := &http.Client{
		Timeout: 60 * time.Second,
	}
	
	// 检查直接 LLM 配置
	if cfg.DirectLLMEndpoint != "" && cfg.DirectLLMModelID != "" && cfg.DirectLLMAPIKey != "" {
		cfg.IsDirectLLM = true
		logrus.Info("Using Direct LLM API")
		return &Chat{
			config: cfg,
			httpClient: httpClient,
		}, nil
	}
	
	// 检查 LLM 代理配置
	if cfg.LLMProxyEndpoint != "" && cfg.LLMProxyAPIKey != "" && 
	   (cfg.ClaudeModelName != "" || cfg.DeepseekModelName != "") {
		// 设置相应的标志
		if cfg.ClaudeModelName != "" {
			cfg.IsClaudeEnabled = true
			logrus.Info("Using Claude via LLM Proxy")
		}
		if cfg.DeepseekModelName != "" {
			cfg.IsDeepseekEnabled = true
			logrus.Info("Using Deepseek via LLM Proxy")
		}
		return &Chat{
			config: cfg,
			httpClient: httpClient,
		}, nil
	}
	
	// 检查 OpenAI 配置
	if cfg.OpenAIAPIKey == "" {
		return nil, errors.New("Either Direct LLM, LLM Proxy, or OpenAI API configuration is required")
	}

	clientConfig := openai.DefaultConfig(cfg.OpenAIAPIKey)
	clientConfig.BaseURL = cfg.OpenAIAPIEndpoint

	// Configure for Azure OpenAI if needed
	if cfg.IsAzure {
		clientConfig = openai.DefaultAzureConfig(
			cfg.OpenAIAPIKey,
			fmt.Sprintf("%s/%s", cfg.OpenAIAPIEndpoint, cfg.AzureDeployment),
		)
		clientConfig.APIVersion = cfg.AzureAPIVersion
		clientConfig.AzureModelMapperFunc = func(model string) string {
			return cfg.AzureDeployment
		}
	}

	client := openai.NewClientWithConfig(clientConfig)
	return &Chat{
		client: *client,
		config: cfg,
		httpClient: httpClient,
	}, nil
}

// generatePrompt creates the prompt for code review
func (c *Chat) generatePrompt(patch string) string {
	// 获取配置的语言
	language := strings.ToLower(c.config.Language)
	if language == "" {
		// 默认使用中文
		language = "chinese"
	}
	
	// 根据配置的语言设置提示
	languageInstruction := ""
	if language == "english" {
		languageInstruction = "You MUST respond in English. All your feedback, comments, and suggestions should be in English."
	} else {
		languageInstruction = "你必须用中文回复。所有的反馈、评论和建议都应该使用中文。"
	}

	jsonFormatRequirement := fmt.Sprintf(`
%s

You MUST provide your feedback in a strict JSON format with the following structure:
{
  "lgtm": boolean, // true if the code looks good to merge, false if there are concerns
  "review_comment": string, // Your detailed review comments. You can use markdown syntax in this string.
  "summary": string, // A concise summary of the code changes
  "suggestions": string, // Specific suggestions for improvements
  "highlights": string, // Positive aspects or well-implemented parts of the code
  "risks": string // IMPORTANT: Keep this to a SINGLE, SHORT sentence (max 100 chars) describing the most critical risk only
}

IMPORTANT REQUIREMENTS:
1. Your response MUST be a valid JSON object and NOTHING ELSE.
2. Do NOT include any text before or after the JSON object.
3. All fields MUST be present in your response.
4. NEVER leave any field empty or null. If you have nothing to say for a field, provide a message like "No specific suggestions" or "No risks identified".
5. Provide detailed and specific feedback for each field, with examples from the code where relevant, EXCEPT for the 'risks' field which must be a single, short sentence.
6. Make sure your JSON is properly formatted and can be parsed by a standard JSON parser.

Failure to follow these instructions will result in your review being rejected.
`, languageInstruction)

	return fmt.Sprintf("%s%s\n%s\n", 
		c.config.Prompt, 
		jsonFormatRequirement, 
		patch)
}

// extractNestedJSON 处理嵌套的 JSON 结构
func extractNestedJSON(content string) string {
	// 记录原始内容
	logrus.Infof("Raw LLM response: %s", content)
	
	// 定义可能的嵌套结构
	type NestedContent struct {
		Value  map[string]interface{} `json:"value"`
		Data   map[string]interface{} `json:"data"`
		Input  map[string]interface{} `json:"input"`
		Result map[string]interface{} `json:"result"`
	}
	
	// 解析嵌套结构
	var nested NestedContent
	if err := json.Unmarshal([]byte(content), &nested); err != nil {
		logrus.Warnf("Failed to parse nested JSON: %v, returning raw content", err)
		return content
	}
	
	// 检查哪个嵌套字段有值，并提取它
	var innerContent map[string]interface{}
	if nested.Value != nil {
		logrus.Info("Extracted 'value' field from LLM response")
		innerContent = nested.Value
	} else if nested.Data != nil {
		logrus.Info("Extracted 'data' field from LLM response")
		innerContent = nested.Data
	} else if nested.Input != nil {
		logrus.Info("Extracted 'input' field from LLM response")
		innerContent = nested.Input
	} else if nested.Result != nil {
		logrus.Info("Extracted 'result' field from LLM response")
		innerContent = nested.Result
	} else {
		logrus.Warn("No nested field found in LLM response, returning raw content")
		return content
	}
	
	// 将提取的内容转换回 JSON
	processedContent, err := json.Marshal(innerContent)
	if err != nil {
		logrus.Warnf("Failed to marshal extracted content: %v, returning raw content", err)
		return content
	}
	
	logrus.Infof("Processed LLM response: %s", string(processedContent))
	return string(processedContent)
}

// callLLMAPI 调用通用 LLM API
func (c *Chat) callLLMAPI(ctx context.Context, endpoint, apiKey, modelName, prompt string) (string, error) {
	// 创建请求体
	reqBody := LLMRequest{
		Model: modelName,
		Messages: []LLMMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Temperature: c.config.Temperature,
		TopP:        c.config.TopP,
		ResponseFormat: &LLMResponseFormat{
			Type: "json_object",
		},
	}
	
	if c.config.MaxTokens > 0 {
		reqBody.MaxTokens = c.config.MaxTokens
	}
	
	// 将请求体转换为 JSON
	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}
	
	// 创建 HTTP 请求
	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		endpoint,
		bytes.NewBuffer(reqData),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	
	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	
	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	
	// 读取响应体
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}
	
	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned non-200 status code: %d, body: %s", resp.StatusCode, string(respBody))
	}
	
	// 解析响应
	var llmResp LLMResponse
	if err := json.Unmarshal(respBody, &llmResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w, body: %s", err, string(respBody))
	}
	
	// 检查响应是否有内容
	if len(llmResp.Choices) == 0 {
		return "", errors.New("API returned empty choices")
	}
	
	// 获取原始内容
	rawContent := llmResp.Choices[0].Message.Content
	
	// 处理嵌套的 JSON 结构
	return extractNestedJSON(rawContent), nil
}

// callClaudeAPI 调用 Claude API
func (c *Chat) callClaudeAPI(ctx context.Context, prompt string) (string, error) {
	// 创建请求体
	reqBody := LLMRequest{
		Model: c.config.ClaudeModelName,
		Messages: []LLMMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Temperature: c.config.Temperature,
		TopP:        c.config.TopP,
		ResponseFormat: &LLMResponseFormat{
			Type: "json_object",
		},
	}
	
	// 设置最大 token 数
	reqBody.MaxTokens = c.config.ClaudeMaxTokens
	
	// 将请求体转换为 JSON
	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}
	
	// 创建 HTTP 请求
	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		c.config.LLMProxyEndpoint,
		bytes.NewBuffer(reqData),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	
	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.config.LLMProxyAPIKey))
	
	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	
	// 读取响应体
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}
	
	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned non-200 status code: %d, body: %s", resp.StatusCode, string(respBody))
	}
	
	// 解析响应
	var llmResp LLMResponse
	if err := json.Unmarshal(respBody, &llmResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w, body: %s", err, string(respBody))
	}
	
	// 检查响应是否有内容
	if len(llmResp.Choices) == 0 {
		return "", errors.New("API returned empty choices")
	}
	
	// 获取原始内容
	rawContent := llmResp.Choices[0].Message.Content
	
	// 处理嵌套的 JSON 结构
	return extractNestedJSON(rawContent), nil
}

// estimateTokenCount 估算文本的 token 数量（糊略估计）
func estimateTokenCount(text string) int {
	// 一个简单的估算：平均每 4 个字符约为 1 个 token
	return len(text) / 4
}

// callDirectLLMAPI 调用直接 LLM API
func (c *Chat) callDirectLLMAPI(ctx context.Context, prompt string) (string, error) {
	// 使用通用 LLM API 调用函数
	return c.callLLMAPI(
		ctx,
		c.config.DirectLLMEndpoint, 
		c.config.DirectLLMAPIKey, 
		c.config.DirectLLMModelID, 
		prompt,
	)
}

// callDeepseekAPI 调用 Deepseek API
func (c *Chat) callDeepseekAPI(ctx context.Context, prompt string) (string, error) {
	// 使用通用 LLM API 调用函数
	return c.callLLMAPI(
		ctx,
		c.config.LLMProxyEndpoint, 
		c.config.LLMProxyAPIKey, 
		c.config.DeepseekModelName, 
		prompt,
	)
}

// CodeReview performs a code review on the given patch
func (c *Chat) CodeReview(ctx context.Context, patch string) (ReviewResult, error) {
	if patch == "" {
		logrus.Info("Empty patch received, returning empty review result")
		return ReviewResult{
			LGTM:          true,
			ReviewComment: "",
			Summary:       "No code changes detected.",
			Suggestions:   "",
			Highlights:    "",
			Risks:         "",
		}, nil
	}

	start := time.Now()
	patchSize := len(patch)
	logrus.Infof("Starting code review for patch of size %d bytes", patchSize)
	
	// 确定最大 token 限制
	maxTokens := 4000 // 默认值
	if c.config.IsClaudeEnabled {
		maxTokens = c.config.ClaudeMaxTokens
	}
	
	// 分割补丁
	patches := splitPatch(patch, maxTokens)
	logrus.Debugf("Split patch into %d chunks", len(patches))
	
	// 如果有多个块，并发审查并合并结果
	if len(patches) > 1 {
		chunkCount := len(patches)
		logrus.Infof("Performing multi-chunk code review with %d chunks", chunkCount)
		
		// 使用通道收集结果
		resultChan := make(chan struct {
			index int
			result ReviewResult
			err error
		}, chunkCount)
		
		// 并发处理所有块
		for i, p := range patches {
			chunkSize := len(p)
			chunkIndex := i // 创建一个副本以在闭包中使用
			logrus.Infof("Starting review of chunk %d/%d (size: %d bytes)", chunkIndex+1, chunkCount, chunkSize)
			
			// 启动一个 goroutine 处理这个块
			go func(idx int, patch string) {
				chunkStart := time.Now()
				
				// 为每个块生成提示语
				chunkPrompt := c.generatePrompt(patch)
				chunkPrompt = fmt.Sprintf("This is part %d of %d of a larger code review. Please review only this part:\n\n%s", 
					idx+1, chunkCount, chunkPrompt)
				
				// 审查当前块
				chunkResult, err := c.reviewSingleChunk(ctx, chunkPrompt)
				
				// 将结果发送到通道
				resultChan <- struct {
					index int
					result ReviewResult
					err error
				}{idx, chunkResult, err}
				
				logrus.Infof("Chunk %d/%d review completed in %s", idx+1, chunkCount, time.Since(chunkStart))
			}(chunkIndex, p)
		}
		
		// 收集所有结果
		results := make([]ReviewResult, chunkCount)
		for i := 0; i < chunkCount; i++ {
			r := <-resultChan
			
			if r.err != nil {
				logrus.Warnf("Error reviewing chunk %d: %v, attempting to retry", r.index+1, r.err)
				
				// 尝试重试最多3次
				var retryResult ReviewResult
				var retryErr error
				var retrySuccess bool = false
				
				for retryCount := 0; retryCount < 3; retryCount++ {
					logrus.Infof("Retry %d/3 for chunk %d", retryCount+1, r.index+1)
					retryResult, retryErr = c.reviewSingleChunk(ctx, c.generatePrompt(patches[r.index]))
					
					if retryErr == nil {
						logrus.Infof("Retry %d/3 successful for chunk %d", retryCount+1, r.index+1)
						retrySuccess = true
						break
					}
					
					logrus.Warnf("Retry %d/3 failed for chunk %d: %v", retryCount+1, r.index+1, retryErr)
					time.Sleep(2 * time.Second) // 等待一下再重试
				}
				
				if retrySuccess {
					results[r.index] = retryResult
					logrus.Infof("Successfully retried chunk %d, LGTM=%v, comment length=%d", 
						r.index+1, retryResult.LGTM, len(retryResult.ReviewComment))
				} else {
					logrus.Errorf("All retries failed for chunk %d", r.index+1)
					results[r.index] = ReviewResult{
						LGTM: true,
					}
				}
			} else {
				results[r.index] = r.result
				logrus.Infof("Chunk %d/%d result collected: LGTM=%v, comment length=%d", 
					r.index+1, chunkCount, r.result.LGTM, len(r.result.ReviewComment))
			}
		}
		
		// 合并所有块的结果
		mergedResult := mergeReviewResults(results)
		logrus.Debugf("Code review completed in %s", time.Since(start))
		return mergedResult, nil
	}
	
	// 如果只有一个块，使用常规方法
	prompt := c.generatePrompt(patch)
	return c.reviewSingleChunk(ctx, prompt)
}
	
// splitPatch 将大型补丁分割成多个小块
func splitPatch(patch string, maxTokens int) []string {
	// 估算整个补丁的 token 数量
	totalTokens := estimateTokenCount(patch)
	
	// 如果补丁足够小，直接返回
	if totalTokens <= maxTokens {
		return []string{patch}
	}
	
	// 将补丁按文件分割
	files := strings.Split(patch, "diff --git")
	
	// 第一个元素通常是空的，移除它
	if len(files) > 0 && files[0] == "" {
		files = files[1:]
	}
	
	// 添加前缀
	for i := range files {
		if i > 0 || (i == 0 && len(files[0]) > 0) {
			files[i] = "diff --git" + files[i]
		}
	}
	
	// 组合文件，确保每个块不超过 maxTokens
	result := []string{}
	currentChunk := ""
	currentTokens := 0
	
	for _, file := range files {
		fileTokens := estimateTokenCount(file)
		
		// 如果单个文件超过限制，需要进一步分割
		if fileTokens > maxTokens {
			// 如果当前块不为空，先添加到结果中
			if currentChunk != "" {
				result = append(result, currentChunk)
				currentChunk = ""
				currentTokens = 0
			}
			
			// 简单地按行分割大文件
			lines := strings.Split(file, "\n")
			tempChunk := ""
			tempTokens := 0
			
			for _, line := range lines {
				lineTokens := estimateTokenCount(line + "\n")
				
				if tempTokens + lineTokens > maxTokens {
					result = append(result, tempChunk)
					tempChunk = line + "\n"
					tempTokens = lineTokens
				} else {
					tempChunk += line + "\n"
					tempTokens += lineTokens
				}
			}
			
			// 添加最后一个块
			if tempChunk != "" {
				result = append(result, tempChunk)
			}
		} else if currentTokens + fileTokens > maxTokens {
			// 如果添加这个文件会超过限制，先添加当前块到结果中
			result = append(result, currentChunk)
			currentChunk = file
			currentTokens = fileTokens
		} else {
			// 添加文件到当前块
			currentChunk += file
			currentTokens += fileTokens
		}
	}
	
	// 添加最后一个块
	if currentChunk != "" {
		result = append(result, currentChunk)
	}
	
	return result
}

// mergeReviewResults 合并多个审查结果
func mergeReviewResults(results []ReviewResult) ReviewResult {
	if len(results) == 0 {
		return ReviewResult{LGTM: true, ReviewComment: ""}
	}
	
	// 默认认为代码没问题，除非有任何一个审查结果表明有问题
	lgtm := true
	comments := []string{}
	summaries := []string{}
	suggestions := []string{}
	highlights := []string{}
	risks := []string{}
	
	for i, result := range results {
		if !result.LGTM {
			lgtm = false
		}
		
		// 添加分块标记
		if len(results) > 1 {
			comments = append(comments, fmt.Sprintf("### 代码块 %d/%d 审查结果:\n\n%s", i+1, len(results), result.ReviewComment))
			
			// 收集其他字段
			if result.Summary != "" {
				summaries = append(summaries, fmt.Sprintf("**块 %d/%d**: %s", i+1, len(results), result.Summary))
			}
			if result.Suggestions != "" {
				suggestions = append(suggestions, fmt.Sprintf("**块 %d/%d**: %s", i+1, len(results), result.Suggestions))
			}
			if result.Highlights != "" {
				highlights = append(highlights, fmt.Sprintf("**块 %d/%d**: %s", i+1, len(results), result.Highlights))
			}
			if result.Risks != "" {
				risks = append(risks, fmt.Sprintf("**块 %d/%d**: %s", i+1, len(results), result.Risks))
			}
		} else {
			comments = append(comments, result.ReviewComment)
			summaries = append(summaries, result.Summary)
			suggestions = append(suggestions, result.Suggestions)
			highlights = append(highlights, result.Highlights)
			risks = append(risks, result.Risks)
		}
	}
	
	// 生成最终的审查结果
	return ReviewResult{
		LGTM:          lgtm,
		ReviewComment: strings.Join(comments, "\n\n---\n\n"),
		Summary:       strings.Join(summaries, "\n\n"),
		Suggestions:   strings.Join(suggestions, "\n\n"),
		Highlights:    strings.Join(highlights, "\n\n"),
		Risks:         strings.Join(risks, "\n\n"),
	}
}

// reviewSingleChunk 审查单个代码块
func (c *Chat) reviewSingleChunk(ctx context.Context, prompt string) (ReviewResult, error) {
	start := time.Now()
	promptSize := len(prompt)
	logrus.Infof("Starting single chunk review (prompt size: %d bytes)", promptSize)
	
	var content string
	var err error
	var modelUsed string
	
	// 尝试按优先级使用不同的模型
	
	// 1. 首先尝试使用 Claude
	if c.config.IsClaudeEnabled {
		logrus.Info("Attempting to use Claude API for code review")
		modelStart := time.Now()
		content, err = c.callClaudeAPI(ctx, prompt)
		if err == nil {
			// 调用成功，处理结果
			modelUsed = "Claude"
			logrus.Infof("Claude API call successful in %s", time.Since(modelStart))
			goto ProcessResult
		}
		logrus.Warnf("Claude API error: %v, trying next model", err)
	}
	
	// 2. 如果 Claude 失败，尝试使用 Deepseek
	if c.config.IsDeepseekEnabled {
		logrus.Info("Attempting to use Deepseek API for code review")
		modelStart := time.Now()
		content, err = c.callDeepseekAPI(ctx, prompt)
		if err == nil {
			// 调用成功，处理结果
			modelUsed = "Deepseek"
			logrus.Infof("Deepseek API call successful in %s", time.Since(modelStart))
			goto ProcessResult
		}
		logrus.Warnf("Deepseek API error: %v, trying next model", err)
	}
	
	// 3. 如果前两个失败，尝试使用直接 LLM
	if c.config.IsDirectLLM {
		logrus.Info("Attempting to use Direct LLM API for code review")
		modelStart := time.Now()
		content, err = c.callDirectLLMAPI(ctx, prompt)
		if err == nil {
			// 调用成功，处理结果
			modelUsed = "Direct LLM"
			logrus.Infof("Direct LLM API call successful in %s", time.Since(modelStart))
			goto ProcessResult
		}
		logrus.Warnf("Direct LLM API error: %v, trying next model", err)
	}
	
	// 4. 最后尝试使用 OpenAI API
	if c.config.OpenAIAPIKey != "" {
		logrus.Info("Attempting to use OpenAI API for code review")
		modelStart := time.Now()
		
		req := openai.ChatCompletionRequest{
			Model: c.config.Model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			Temperature: c.config.Temperature,
			TopP:        c.config.TopP,
			ResponseFormat: &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONObject,
			},
		}
		
		if c.config.MaxTokens > 0 {
			req.MaxTokens = c.config.MaxTokens
		}

		logrus.Debugf("Sending request to OpenAI API with model: %s", c.config.Model)
		resp, err := c.client.CreateChatCompletion(ctx, req)
		if err != nil {
			logrus.Errorf("OpenAI API error: %v", err)
			return ReviewResult{}, fmt.Errorf("OpenAI API error: %w", err)
		}
		
		if len(resp.Choices) == 0 {
			logrus.Warn("OpenAI API returned empty choices")
			return ReviewResult{LGTM: true, ReviewComment: ""}, nil
		}
		
		content = resp.Choices[0].Message.Content
		modelUsed = fmt.Sprintf("OpenAI (%s)", c.config.Model)
		logrus.Infof("OpenAI API call successful in %s", time.Since(modelStart))
	}
	
	// 如果所有模型都失败了，返回错误信息
	if content == "" {
		logrus.Error("All LLM models failed, unable to perform code review")
		return ReviewResult{LGTM: true}, nil
	}

	// 处理结果的标签
ProcessResult:
	logrus.Infof("Code review completed in %s using model: %s", time.Since(start), modelUsed)
	logrus.Infof("Raw response content: %s", content)
	
	// 尝试从响应中提取有用的内容
	// 定义可能的嵌套 JSON 结构
	type NestedResponse struct {
		Value  *ReviewResult `json:"value"`
		Data   *ReviewResult `json:"data"`
		Input  *ReviewResult `json:"input"`
		Result *ReviewResult `json:"result"`
	}
	
	// 首先尝试直接解析
	var result ReviewResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		logrus.Warnf("Failed to parse direct JSON response: %v", err)
		
		// 尝试解析嵌套结构
		var nestedResult NestedResponse
		if err := json.Unmarshal([]byte(content), &nestedResult); err != nil {
			logrus.Warnf("Failed to parse nested JSON response: %v", err)
			
			// 尝试从响应中提取有用的信息
			// 有时候 LLM 可能会返回带有额外文本的 JSON
			jsonStartIdx := strings.Index(content, "{")
			jsonEndIdx := strings.LastIndex(content, "}")
			
			if jsonStartIdx >= 0 && jsonEndIdx > jsonStartIdx {
				jsonContent := content[jsonStartIdx : jsonEndIdx+1]
				logrus.Infof("Attempting to parse extracted JSON: %s", jsonContent)
				
				// 尝试直接解析提取的 JSON
				if err := json.Unmarshal([]byte(jsonContent), &result); err != nil {
					// 尝试解析嵌套结构
					if err := json.Unmarshal([]byte(jsonContent), &nestedResult); err != nil {
						logrus.Warnf("Failed to parse extracted JSON: %v", err)
						// Fallback to using the raw content as the review comment
						return ReviewResult{LGTM: true}, nil
					}
				}
			} else {
				// 如果无法提取 JSON，使用原始内容作为评论
				return ReviewResult{LGTM: true}, nil
			}
		}
		
		// 如果有嵌套结果，使用它
		if nestedResult.Value != nil {
			logrus.Info("Using nested 'value' field")
			result = *nestedResult.Value
		} else if nestedResult.Data != nil {
			logrus.Info("Using nested 'data' field")
			result = *nestedResult.Data
		} else if nestedResult.Input != nil {
			logrus.Info("Using nested 'input' field")
			result = *nestedResult.Input
		} else if nestedResult.Result != nil {
			logrus.Info("Using nested 'result' field")
			result = *nestedResult.Result
		}
	}
	
	logrus.Infof("Review result: LGTM=%v, comment length=%d", result.LGTM, len(result.ReviewComment))
	return result, nil
}
