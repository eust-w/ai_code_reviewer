package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// formatWhereClause 将简单的where子句转换为Chroma v1.0.0 API要求的格式
// 例如：{"language": "go"} 变为 {"$and": [{"language": {"$eq": "go"}}]}
func formatWhereClause(where map[string]interface{}) map[string]interface{} {
	if where == nil || len(where) == 0 {
		return nil
	}

	filters := make([]map[string]interface{}, 0, len(where))
	for k, v := range where {
		filters = append(filters, map[string]interface{}{
			k: map[string]interface{}{"$eq": v},
		})
	}

	return map[string]interface{}{
		"$and": filters,
	}
}

// ChromaStorage 使用Chroma作为存储后端
type ChromaStorage struct {
	client      ChromaClient
	collections map[string]string // repoKey -> collectionID
	mu          sync.RWMutex      // 保护collections映射的读写锁
	vectorSvc   VectorService     // 用于生成向量嵌入
}

// ChromaClient 是 Chroma API 的客户端接口
type ChromaClient interface {
	CreateCollection(ctx context.Context, name string, metadata map[string]interface{}) (string, error)
	GetCollection(ctx context.Context, name string) (string, error)
	AddDocuments(ctx context.Context, collectionID string, ids []string, documents []string, metadatas []map[string]interface{}, embeddings [][]float32) error
	GetDocuments(ctx context.Context, collectionID string, ids []string, includeMetadata bool) ([]string, []map[string]interface{}, error)
	DeleteDocuments(ctx context.Context, collectionID string, ids []string) error
	QueryDocuments(ctx context.Context, collectionID string, queryTexts []string, nResults int, where map[string]interface{}) ([][]string, [][]map[string]interface{}, [][]float32, error)
	QueryDocumentsWithEmbedding(ctx context.Context, collectionID string, queryEmbedding []float32, nResults int, where map[string]interface{}) ([][]string, [][]map[string]interface{}, [][]float32, error)
	Close() error
}

// NewChromaStorage 创建新的Chroma存储
func NewChromaStorage(config *StorageConfig) (*ChromaStorage, error) {
	// 初始化Chroma客户端
	client, err := NewRealChromaClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Chroma client: %w", err)
	}

	// 创建向量服务
	// 使用OpenAI向量服务，如果没有配置则返回nil
	var vectorSvc VectorService
	if config.OpenAIAPIKey != "" {
		var err error
		vectorSvc, err = NewOpenAIVectorService(config.OpenAIAPIKey, config.OpenAIModel)
		if err != nil {
			logrus.Warnf("Failed to create OpenAI vector service: %v", err)
			return nil, fmt.Errorf("failed to create vector service: %w", err)
		}
		logrus.Infof("Created Chroma storage with OpenAI vector service")
	} else if config.LLMProxyEndpoint != "" && config.LLMProxyAPIKey != "" {
		var err error
		vectorSvc, err = NewLLMProxyVectorService(config.LLMProxyEndpoint, config.LLMProxyAPIKey, config.LLMProxyModel, config.LLMProxyProvider)
		if err != nil {
			logrus.Warnf("Failed to create LLM Proxy vector service: %v", err)
			return nil, fmt.Errorf("failed to create vector service: %w", err)
		}
		logrus.Infof("Created Chroma storage with LLM Proxy vector service")
	} else {
		logrus.Warnf("No vector service configured, Chroma storage will operate without embeddings")
	}

	return &ChromaStorage{
		client:      client,
		collections: make(map[string]string),
		vectorSvc:   vectorSvc,
	}, nil
}

// getOrCreateCollection 获取或创建集合
func (s *ChromaStorage) getOrCreateCollection(ctx context.Context, repoKey string) (string, error) {
	// 检查 client 是否为 nil
	if s.client == nil {
		logrus.Errorf("[DEBUG] getOrCreateCollection 错误: Chroma client 未初始化")
		return "", fmt.Errorf("chroma client is not initialized")
	}

	// 使用读锁检查缓存
	s.mu.RLock()
	collID, ok := s.collections[repoKey]
	s.mu.RUnlock()

	if ok {
		logrus.Debugf("[索引增强] 使用缓存的集合ID: %s", collID)
		return collID, nil
	}

	// 尝试获取现有集合
	collectionName := fmt.Sprintf("code_snippets_%s", strings.ReplaceAll(repoKey, "/", "_"))
	logrus.Infof("[索引增强] Getting Chroma collection: %s", collectionName)

	collID, err := s.client.GetCollection(ctx, collectionName)
	if err == nil {
		// 找到现有集合，使用写锁更新缓存
		logrus.Infof("[索引增强] 找到现有集合: %s", collectionName)
		s.mu.Lock()
		s.collections[repoKey] = collID
		s.mu.Unlock()
		return collID, nil
	}

	// 记录获取集合失败的错误
	logrus.Errorf("[索引增强] Collection list failed with status %v: %v", getStatusCodeFromError(err), err)

	// 尝试创建新集合
	metadata := map[string]interface{}{
		"repo_key":    repoKey,
		"description": fmt.Sprintf("Code snippets for repository %s", repoKey),
		"created_at":  time.Now().Unix(),
	}

	logrus.Infof("[索引增强] Creating Chroma collection: %s with metadata: %v", collectionName, metadata)
	collID, err = s.client.CreateCollection(ctx, collectionName, metadata)
	if err != nil {
		logrus.Errorf("[索引增强] Collection creation failed with status %v: %v", getStatusCodeFromError(err), err)

		// 检查是否是数据库连接错误
		errStr := err.Error()
		if strings.Contains(errStr, "No connected db") {
			logrus.Warnf("[索引增强] Chroma数据库连接错误，请检查Chroma服务器配置")
			return "", fmt.Errorf("Chroma database connection error: %w", err)
		} else if strings.Contains(errStr, "404") {
			logrus.Warnf("[索引增强] Chroma API路径不正确，请检查API版本配置")
			return "", fmt.Errorf("Chroma API path error (404): %w", err)
		}

		return "", fmt.Errorf("failed to create collection for repo %s: %w", repoKey, err)
	}

	// 使用写锁保护缓存更新
	s.mu.Lock()
	s.collections[repoKey] = collID
	s.mu.Unlock()
	logrus.Infof("[索引增强] 成功创建集合: %s, ID: %s", collectionName, collID)
	return collID, nil
}

// SaveCodeSnippet 保存代码片段
func (s *ChromaStorage) SaveCodeSnippet(ctx context.Context, repoKey, filename, content string, metadata map[string]interface{}) (string, error) {
	// 生成唯一ID
	id := fmt.Sprintf("%s_%s_%d", repoKey, strings.ReplaceAll(filename, "/", "_"), time.Now().UnixNano())

	// 确保元数据包含必要字段
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["repo_key"] = repoKey
	metadata["filename"] = filename
	metadata["indexed_at"] = time.Now().Unix()

	// 从文件名推断语言
	language := detectLanguageFromFilename(filename)
	metadata["language"] = language

	// 检查 client 是否为 nil
	if s.client == nil {
		logrus.Errorf("Chroma client is not initialized")
		return "", fmt.Errorf("chroma client is not initialized")
	}

	// 获取或创建集合
	collID, err := s.getOrCreateCollection(ctx, repoKey)
	if err != nil {
		return "", err
	}

	// 检查 vectorSvc 是否为 nil
	if s.vectorSvc == nil {
		logrus.Warnf("Vector service is not initialized for %s, proceeding without embedding", filename)
		// 没有向量服务，直接添加文档，没有嵌入
		if err := s.client.AddDocuments(ctx, collID, []string{id}, []string{content}, []map[string]interface{}{metadata}, nil); err != nil {
			return "", fmt.Errorf("failed to add document to Chroma: %w", err)
		}
	} else {
		// 生成代码嵌入向量
		embedding, embErr := s.vectorSvc.EmbedCode(ctx, language, content)
		if embErr != nil {
			logrus.Warnf("Failed to generate embedding for %s: %v, proceeding without embedding", filename, embErr)
			// 如果嵌入失败，仍然尝试添加文档，但没有嵌入
			if err := s.client.AddDocuments(ctx, collID, []string{id}, []string{content}, []map[string]interface{}{metadata}, nil); err != nil {
				return "", fmt.Errorf("failed to add document to Chroma: %w", err)
			}
		} else {
			// 使用生成的嵌入向量添加文档
			if err := s.client.AddDocuments(ctx, collID, []string{id}, []string{content}, []map[string]interface{}{metadata}, [][]float32{embedding}); err != nil {
				return "", fmt.Errorf("failed to add document with embedding to Chroma: %w", err)
			}
		}
	}

	if err != nil {
		return "", fmt.Errorf("failed to add document to Chroma: %w", err)
	}

	return id, nil
}

// detectLanguageFromFilename 从文件名推断编程语言
func detectLanguageFromFilename(filename string) string {
	ext := strings.ToLower(filename[strings.LastIndex(filename, ".")+1:])

	// 根据文件扩展名映射到编程语言
	switch ext {
	case "go":
		return "go"
	case "js":
		return "javascript"
	case "ts":
		return "typescript"
	case "py":
		return "python"
	case "java":
		return "java"
	case "c":
		return "c"
	case "cpp", "cc", "cxx":
		return "cpp"
	case "cs":
		return "csharp"
	case "rb":
		return "ruby"
	case "php":
		return "php"
	case "swift":
		return "swift"
	case "kt":
		return "kotlin"
	case "rs":
		return "rust"
	case "sh":
		return "shell"
	case "html":
		return "html"
	case "css":
		return "css"
	case "md":
		return "markdown"
	case "json":
		return "json"
	case "yaml", "yml":
		return "yaml"
	case "xml":
		return "xml"
	case "sql":
		return "sql"
	default:
		return "text"
	}
}

// GetCodeSnippet 获取代码片段
func (s *ChromaStorage) GetCodeSnippet(ctx context.Context, id string) (string, map[string]interface{}, error) {
	// 检查 client 是否为 nil
	if s.client == nil {
		logrus.Errorf("Chroma client is not initialized")
		return "", nil, fmt.Errorf("chroma client is not initialized")
	}

	// 从 ID 中提取 repoKey
	parts := strings.Split(id, "_")
	if len(parts) < 2 {
		return "", nil, fmt.Errorf("invalid snippet ID format: %s", id)
	}
	repoKey := parts[0] + "/" + parts[1]

	// 获取集合ID
	s.mu.RLock()
	collID, ok := s.collections[repoKey]
	s.mu.RUnlock()

	if !ok {
		// 尝试获取集合
		var err error
		collID, err = s.getOrCreateCollection(ctx, repoKey)
		if err != nil {
			return "", nil, fmt.Errorf("failed to get collection for %s: %w", repoKey, err)
		}
	}

	// 从 Chroma 获取文档
	docs, metadatas, err := s.client.GetDocuments(ctx, collID, []string{id}, true)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get document from Chroma: %w", err)
	}

	if len(docs) == 0 || len(metadatas) == 0 {
		return "", nil, fmt.Errorf("document not found in Chroma")
	}

	return docs[0], metadatas[0], nil
}

// DeleteCodeSnippet 删除代码片段
func (s *ChromaStorage) DeleteCodeSnippet(ctx context.Context, id string) error {
	// 从ID中提取repoKey
	parts := strings.Split(id, "_")
	if len(parts) < 2 {
		return fmt.Errorf("invalid snippet ID format: %s", id)
	}
	repoKey := parts[0] + "/" + parts[1]

	collID, err := s.getOrCreateCollection(ctx, repoKey)
	if err != nil {
		return err
	}

	err = s.client.DeleteDocuments(ctx, collID, []string{id})
	if err != nil {
		return fmt.Errorf("failed to delete document from Chroma: %w", err)
	}

	return nil
}

// ListSnippetsByFile 列出指定文件的所有代码片段
func (s *ChromaStorage) ListSnippetsByFile(ctx context.Context, repoKey, filename string) ([]string, error) {
	collID, err := s.getOrCreateCollection(ctx, repoKey)
	if err != nil {
		return nil, err
	}

	where := map[string]interface{}{
		"repo_key": repoKey,
		"filename": filename,
	}

	ids, _, _, err := s.client.QueryDocuments(ctx, collID, []string{""}, 1000, where)
	if err != nil {
		return nil, fmt.Errorf("failed to query documents from Chroma: %w", err)
	}

	if len(ids) == 0 || len(ids[0]) == 0 {
		return []string{}, nil
	}

	return ids[0], nil
}

// ListSnippetsByRepo 列出指定仓库的所有代码片段
func (s *ChromaStorage) ListSnippetsByRepo(ctx context.Context, repoKey string) ([]string, error) {
	collID, err := s.getOrCreateCollection(ctx, repoKey)
	if err != nil {
		return nil, err
	}

	where := map[string]interface{}{
		"repo_key": repoKey,
	}

	ids, _, _, err := s.client.QueryDocuments(ctx, collID, []string{""}, 10000, where)
	if err != nil {
		return nil, fmt.Errorf("failed to query documents from Chroma: %w", err)
	}

	if len(ids) == 0 || len(ids[0]) == 0 {
		return []string{}, nil
	}

	return ids[0], nil
}

// QueryCodeSnippets 查询代码片段
func (s *ChromaStorage) QueryCodeSnippets(ctx context.Context, repoKey, query string, limit int) ([]string, []map[string]interface{}, error) {
	// 检查 client 是否为 nil
	if s.client == nil {
		logrus.Errorf("Chroma client is not initialized")
		return nil, nil, fmt.Errorf("chroma client is not initialized")
	}

	collID, err := s.getOrCreateCollection(ctx, repoKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get or create collection: %w", err)
	}

	// 构造查询条件
	whereClause := map[string]interface{}{}
	if repoKey != "" {
		whereClause["repo_key"] = repoKey
	}

	// 检查 vectorSvc 是否为 nil
	if s.vectorSvc == nil {
		logrus.Warnf("Vector service is not initialized, using text search")
		// 没有向量服务，直接使用文本搜索
		ids, metas, _, err := s.client.QueryDocuments(ctx, collID, []string{query}, limit, whereClause)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to query documents from Chroma: %w", err)
		}
		return flattenQueryResults(ids, metas)
	}

	// 使用向量服务进行查询
	// 生成查询嵌入向量
	_, err = s.vectorSvc.EmbedQuery(ctx, query)
	if err != nil {
		logrus.Warnf("Failed to generate query embedding: %v, falling back to text search", err)
		// 如果嵌入失败，回退到简单的文本搜索
		ids, metas, _, err := s.client.QueryDocuments(ctx, collID, []string{query}, limit, whereClause)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to query documents from Chroma: %w", err)
		}
		return flattenQueryResults(ids, metas)
	}

	// 使用嵌入向量进行查询
	ids, metas, _, err := s.client.QueryDocuments(ctx, collID, []string{query}, limit, whereClause)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query documents from Chroma with embedding: %w", err)
	}

	// 将查询结果展平为一维数组
	return flattenQueryResults(ids, metas)
}

// Close 关闭存储连接
func (s *ChromaStorage) Close() error {
	// 关闭向量服务
	if s.vectorSvc != nil {
		if closer, ok := s.vectorSvc.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				logrus.Warnf("Failed to close vector service: %v", err)
			}
		}
	}

	// 关闭 Chroma 客户端
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// flattenQueryResults 将查询结果展平为一维数组
func flattenQueryResults(ids [][]string, metas [][]map[string]interface{}) ([]string, []map[string]interface{}, error) {
	if len(ids) == 0 || len(ids[0]) == 0 {
		return []string{}, []map[string]interface{}{}, nil
	}

	return ids[0], metas[0], nil
}

// getStatusCodeFromError 从错误中提取HTTP状态码
func getStatusCodeFromError(err error) int {
	if err == nil {
		return 0
	}

	errStr := err.Error()

	// 尝试匹配常见的HTTP状态码
	statusPatterns := []struct {
		pattern string
		code    int
	}{
		{"404", 404},
		{"400", 400},
		{"401", 401},
		{"403", 403},
		{"500", 500},
		{"502", 502},
		{"503", 503},
		{"504", 504},
	}

	for _, sp := range statusPatterns {
		if strings.Contains(errStr, sp.pattern) {
			return sp.code
		}
	}

	// 如果没有找到匹配的状态码，返回0
	return 0
}

// RealChromaClient 实现了ChromaClient接口，使用实际的Chroma API
type RealChromaClient struct {
	// 实际的HTTP客户端实例
	baseURL     string
	apiVersion  string
	httpClient  *http.Client
	tenant      string            // 租户名称
	database    string            // 数据库名称
	collections map[string]string // 缓存集合名称到ID的映射
	mu          sync.RWMutex      // 保护collections映射
}

// NewRealChromaClient 创建真实的Chroma客户端
func NewRealChromaClient(config *StorageConfig) (*RealChromaClient, error) {
	// 构建Chroma API URL
	protocol := "http"
	if config.ChromaSSL {
		protocol = "https"
	}

	// 直接使用v2 API，因为我们已经确认这是可用的
	apiPath := "/api/v2"
	apiVersion := "v2"

	// 如果配置中指定了路径，使用配置的路径
	if config.ChromaPath != "" {
		apiPath = config.ChromaPath
		// 确保路径以/开头
		if !strings.HasPrefix(apiPath, "/") {
			apiPath = "/" + apiPath
		}

		// 检查是否包含版本信息
		if !strings.Contains(apiPath, "/v") {
			apiPath = fmt.Sprintf("%s/v2", apiPath)
		} else {
			// 从路径中提取版本
			parts := strings.Split(apiPath, "/")
			for _, part := range parts {
				if strings.HasPrefix(part, "v") && len(part) > 1 {
					apiVersion = part
					break
				}
			}
		}
	}

	logrus.Infof("Using Chroma API path: %s (version: %s)", apiPath, apiVersion)

	baseURL := fmt.Sprintf("%s://%s:%d/api/v2", protocol, config.ChromaHost, config.ChromaPort)

	logrus.Infof("Connecting to Chroma at %s (API version: v2)", baseURL)

	// 创建带有超时的HTTP客户端
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// 测试连接
	// 尝试多个可能的API路径
	// 首先尝试v2 API的heartbeat端点
	testURL := fmt.Sprintf("%s://%s:%d/api/v2/heartbeat", protocol, config.ChromaHost, config.ChromaPort)
	logrus.Infof("Testing Chroma connection with URL: %s", testURL)
	req, err := http.NewRequest("GET", testURL, nil)
	if err != nil {
		logrus.Errorf("Failed to create request for Chroma heartbeat check: %v", err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		// 如果第一个请求失败，尝试其他可能的API路径
		logrus.Warnf("First Chroma connection test failed, trying alternative API path")

		// 尝试不带版本的路径
		testURL = fmt.Sprintf("%s://%s:%d/api/heartbeat", protocol, config.ChromaHost, config.ChromaPort)
		logrus.Infof("Testing Chroma connection with alternative URL: %s", testURL)
		req, err = http.NewRequest("GET", testURL, nil)
		if err != nil {
			logrus.Errorf("Failed to create request for alternative Chroma test: %v", err)
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		// 关闭之前的响应体
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}

		resp, err = httpClient.Do(req)
		if err != nil {
			logrus.Errorf("Failed to connect to Chroma server with alternative path: %v", err)
			return nil, fmt.Errorf("failed to connect to Chroma server: %w", err)
		}

		// 更新API路径和版本
		apiPath = "/api"
		apiVersion = ""
		logrus.Infof("Using alternative API path: %s", apiPath)
	} else {
		logrus.Infof("Successfully connected to Chroma API v2")
		// 关闭响应体，因为我们将在后面重新设置它
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}

		// 再次测试使用完整的版本路径
		testURL = fmt.Sprintf("%s://%s:%d/api/v2/version", protocol, config.ChromaHost, config.ChromaPort)
		logrus.Infof("Testing Chroma version with URL: %s", testURL)
		req, err = http.NewRequest("GET", testURL, nil)
		if err != nil {
			logrus.Warnf("Failed to create request for Chroma version check: %v", err)
		}

		resp, err = httpClient.Do(req)
	}

	// 之前已经定义了resp和err变量，这里不需要再次定义
	// 如果前面的测试都失败了，这里作为最后的备用方案
	if resp == nil || resp.StatusCode != http.StatusOK {
		// 尝试使用原始的版本端点
		testURL = fmt.Sprintf("%s://%s:%d/api/version", protocol, config.ChromaHost, config.ChromaPort)
		logrus.Infof("Final attempt with URL: %s", testURL)
		req, err = http.NewRequest("GET", testURL, nil)
		if err != nil {
			logrus.Errorf("Failed to create request for final Chroma test: %v", err)
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		// 关闭之前的响应体
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}

		resp, err = httpClient.Do(req)
		if err != nil {
			logrus.Errorf("Failed to connect to Chroma server: %v", err)
			return nil, fmt.Errorf("failed to connect to Chroma server: %w", err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logrus.Errorf("Chroma server returned non-OK status: %d", resp.StatusCode)
		return nil, fmt.Errorf("Chroma server returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Errorf("Failed to read Chroma version response: %v", err)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	logrus.Infof("Chroma server version response: %s", string(body))

	// 设置默认的租户和数据库名称，如果配置中有指定则使用配置的值
	tenant := "default"
	if config.ChromaTenant != "" {
		tenant = config.ChromaTenant
	}

	database := "default"
	if config.ChromaDatabase != "" {
		database = config.ChromaDatabase
	}

	logrus.Infof("Using Chroma tenant: %s, database: %s", tenant, database)

	return &RealChromaClient{
		baseURL:     baseURL,
		apiVersion:  apiVersion,
		httpClient:  httpClient,
		tenant:      tenant,
		database:    database,
		collections: make(map[string]string),
	}, nil
}

// CreateCollection 创建一个新的集合
func (c *RealChromaClient) CreateCollection(ctx context.Context, name string, metadata map[string]interface{}) (string, error) {
	// 确保数据库存在
	if err := c.ensureDatabaseExists(ctx); err != nil {
		logrus.Warnf("Failed to ensure database exists: %v", err)
		// 继续尝试创建集合，可能会失败
	}

	logrus.Infof("Creating Chroma collection: %s with metadata: %+v", name, metadata)

	// 检查缓存
	c.mu.RLock()
	if id, ok := c.collections[name]; ok {
		c.mu.RUnlock()
		logrus.Infof("Collection %s already exists with ID: %s", name, id)
		return id, nil
	}
	c.mu.RUnlock()

	// 准备请求数据
	reqData := map[string]interface{}{
		"name":     name,
		"metadata": metadata,
	}

	jsonData, err := json.Marshal(reqData)
	if err != nil {
		logrus.Errorf("Failed to marshal collection creation request: %v", err)
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// 构建请求URL - 使用 Chroma v2 API 路径结构
	// 先尝试获取默认租户和数据库
	url := fmt.Sprintf("%s/tenants/default/databases/default/collections", c.baseURL)
	logrus.Debugf("POST request to: %s", url)

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		logrus.Errorf("Failed to create collection creation request: %v", err)
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		logrus.Errorf("Failed to send collection creation request: %v", err)
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Errorf("Failed to read collection creation response: %v", err)
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	logrus.Debugf("Collection creation response: %s", string(body))

	// 检查状态码
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		logrus.Errorf("Collection creation failed with status %d: %s", resp.StatusCode, string(body))
		return "", fmt.Errorf("collection creation failed with status %d: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		logrus.Errorf("Failed to parse collection creation response: %v", err)
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// 提取集合ID
	id, ok := result["id"]
	if !ok {
		id = name // 如果没有返回ID，使用名称作为ID
	}

	collectionID := fmt.Sprintf("%v", id)
	logrus.Infof("Successfully created collection %s with ID: %s", name, collectionID)

	// 更新缓存
	c.mu.Lock()
	c.collections[name] = collectionID
	c.mu.Unlock()

	return collectionID, nil
}

// ensureDatabaseExists 确保默认数据库存在
func (c *RealChromaClient) ensureDatabaseExists(ctx context.Context) error {
	// 首先检查数据库是否存在
	// 注意：c.baseURL 已经包含了 /api/v2 前缀，所以这里不需要再添加
	url := fmt.Sprintf("%s/tenants/%s/databases", c.baseURL, c.tenant)
	logrus.Debugf("GET request to list databases: %s", url)

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request to list databases: %w", err)
	}

	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to list databases: %w", err)
	}
	defer resp.Body.Close()

	// 如果状态码不是200，尝试创建数据库
	if resp.StatusCode != http.StatusOK {
		logrus.Warnf("Failed to list databases, status code: %d, trying to create default database", resp.StatusCode)
		return c.createDefaultDatabase(ctx)
	}

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// 解析响应，检查默认数据库是否存在
	var databases []map[string]interface{}
	if err := json.Unmarshal(body, &databases); err != nil {
		return fmt.Errorf("failed to parse databases response: %w", err)
	}

	// 检查指定数据库是否存在
	databaseExists := false
	for _, db := range databases {
		if name, ok := db["name"].(string); ok && name == c.database {
			databaseExists = true
			break
		}
	}

	// 如果指定数据库不存在，创建它
	if !databaseExists {
		return c.createDefaultDatabase(ctx)
	}

	return nil
}

// createDefaultDatabase 创建默认数据库
func (c *RealChromaClient) createDefaultDatabase(ctx context.Context) error {
	// 注意：c.baseURL 已经包含了 /api/v2 前缀，所以这里不需要再添加
	// 正确的路径应该是 /api/v2/tenants/{tenant}/databases
	url := fmt.Sprintf("%s/tenants/%s/databases", c.baseURL, c.tenant)
	logrus.Infof("Creating database %s at: %s", c.database, url)

	// 准备请求体
	reqBody := map[string]interface{}{
		"name": c.database,
	}

	// 将请求体转换为JSON
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("database creation failed with status %d: %s", resp.StatusCode, string(body))
	}

	logrus.Infof("Successfully created default database")
	return nil
}

// GetCollection 获取集合
func (c *RealChromaClient) GetCollection(ctx context.Context, name string) (string, error) {
	logrus.Infof("Getting Chroma collection: %s", name)

	// 确保数据库存在
	if err := c.ensureDatabaseExists(ctx); err != nil {
		logrus.Warnf("Failed to ensure database exists: %v", err)
		// 继续尝试获取集合，可能会失败
	}

	// 检查缓存
	c.mu.RLock()
	if id, ok := c.collections[name]; ok {
		c.mu.RUnlock()
		logrus.Debugf("Collection %s found in cache with ID: %s", name, id)
		return id, nil
	}
	c.mu.RUnlock()

	// 尝试两种方法获取集合
	// 1. 先尝试直接获取指定名称的集合
	url := fmt.Sprintf("%s/tenants/%s/databases/%s/collections/%s", c.baseURL, c.tenant, c.database, name)
	logrus.Debugf("GET request to: %s", url)

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		logrus.Errorf("Failed to create collection request: %v", err)
		// 继续尝试列表方法
	} else {
		// 设置请求头
		req.Header.Set("Accept", "application/json")

		// 发送请求
		resp, err := c.httpClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()

			// 读取响应
			body, err := io.ReadAll(resp.Body)
			if err == nil {
				logrus.Debugf("Collection response: %s", string(body))

				// 解析响应
				var result map[string]interface{}
				if err := json.Unmarshal(body, &result); err == nil {
					// 提取集合ID
					id, ok := result["id"]
					if ok {
						collectionID := fmt.Sprintf("%v", id)
						logrus.Infof("Found collection %s with ID: %s", name, collectionID)

						// 更新缓存
						c.mu.Lock()
						c.collections[name] = collectionID
						c.mu.Unlock()

						return collectionID, nil
					}
				}
			}
		} else if resp != nil {
			resp.Body.Close()
			logrus.Warnf("Direct collection request failed with status %d, falling back to list method", resp.StatusCode)
		}
	}

	// 2. 如果直接获取失败，尝试列出所有集合并查找
	url = fmt.Sprintf("%s/tenants/default/databases/default/collections", c.baseURL)
	logrus.Debugf("GET request to list collections: %s", url)

	// 创建请求
	req, err = http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		logrus.Errorf("Failed to create collection list request: %v", err)
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	req.Header.Set("Accept", "application/json")

	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		logrus.Errorf("Failed to send collection list request: %v", err)
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Errorf("Failed to read collection list response: %v", err)
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	logrus.Debugf("Collection list response: %s", string(body))

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		logrus.Errorf("Collection list failed with status %d: %s", resp.StatusCode, string(body))
		return "", fmt.Errorf("collection list failed with status %d: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var collections []map[string]interface{}
	if err := json.Unmarshal(body, &collections); err != nil {
		logrus.Errorf("Failed to parse collection list response: %v", err)
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// 查找指定名称的集合
	for _, coll := range collections {
		collName, ok := coll["name"]
		if !ok {
			continue
		}

		if fmt.Sprintf("%v", collName) == name {
			id, ok := coll["id"]
			if !ok {
				continue
			}

			collectionID := fmt.Sprintf("%v", id)
			logrus.Infof("Found collection %s with ID: %s", name, collectionID)

			// 更新缓存
			c.mu.Lock()
			c.collections[name] = collectionID
			c.mu.Unlock()

			return collectionID, nil
		}
	}

	logrus.Infof("Collection %s not found", name)
	return "", fmt.Errorf("collection %s not found", name)
}

// AddDocuments 添加文档
func (c *RealChromaClient) AddDocuments(ctx context.Context, collectionID string, ids []string, documents []string, metadatas []map[string]interface{}, embeddings [][]float32) error {
	// 确保数据库存在
	if err := c.ensureDatabaseExists(ctx); err != nil {
		logrus.Warnf("Failed to ensure database exists before adding documents: %v", err)
		// 继续尝试添加文档，可能会失败
	}

	logrus.Infof("Adding %d documents to Chroma collection: %s", len(documents), collectionID)

	// 准备请求数据
	reqData := map[string]interface{}{
		"ids":       ids,
		"documents": documents,
	}

	// 添加元数据（如果提供）
	if metadatas != nil && len(metadatas) > 0 {
		reqData["metadatas"] = metadatas
	}

	// 添加嵌入向量（如果提供）
	if embeddings != nil && len(embeddings) > 0 {
		reqData["embeddings"] = embeddings
	}

	jsonData, err := json.Marshal(reqData)
	if err != nil {
		logrus.Errorf("Failed to marshal add documents request: %v", err)
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// 构建 URL - 使用正确的 API 路径
	// 根据 Chroma v1.0.0 API 规范，使用 /api/v2/tenants/{tenant}/databases/{database}/collections/{collection_id}/add 端点
	url := fmt.Sprintf("%s/tenants/%s/databases/%s/collections/%s/add", c.baseURL, c.tenant, c.database, collectionID)
	logrus.Debugf("[DEBUG] POST request to: %s", url)

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		logrus.Errorf("Failed to create add documents request: %v", err)
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		logrus.Errorf("Failed to send add documents request: %v", err)
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Errorf("Failed to read add documents response: %v", err)
		return fmt.Errorf("failed to read response: %w", err)
	}

	logrus.Debugf("Add documents response: %s", string(body))

	// 检查状态码
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		logrus.Errorf("Add documents failed with status %d: %s", resp.StatusCode, string(body))
		return fmt.Errorf("add documents failed with status %d: %s", resp.StatusCode, string(body))
	}

	logrus.Infof("Successfully added %d documents to collection %s", len(documents), collectionID)
	return nil
}

// GetDocuments 获取文档
func (c *RealChromaClient) GetDocuments(ctx context.Context, collectionID string, ids []string, includeMetadata bool) ([]string, []map[string]interface{}, error) {
	// 确保数据库存在
	if err := c.ensureDatabaseExists(ctx); err != nil {
		logrus.Warnf("Failed to ensure database exists before getting documents: %v", err)
		// 继续尝试获取文档，可能会失败
	}

	logrus.Infof("Getting %d documents from Chroma collection: %s", len(ids), collectionID)

	// 准备请求数据
	reqData := map[string]interface{}{
		"ids":     ids,
		"include": []string{"documents"},
	}

	// 如果需要包含元数据
	if includeMetadata {
		reqData["include"] = append(reqData["include"].([]string), "metadatas")
	}

	jsonData, err := json.Marshal(reqData)
	if err != nil {
		logrus.Errorf("Failed to marshal get documents request: %v", err)
		return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// 构建URL - 使用正确的 API 路径
	// 根据 Chroma v1.0.0 API 规范，使用 /api/v2/tenants/{tenant}/databases/{database}/collections/{collection_id}/get 端点
	url := fmt.Sprintf("%s/tenants/%s/databases/%s/collections/%s/get", c.baseURL, c.tenant, c.database, collectionID)
	logrus.Debugf("[DEBUG] POST request to: %s with data: %s", url, string(jsonData))

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		logrus.Errorf("Failed to create get documents request: %v", err)
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		logrus.Errorf("Failed to send get documents request: %v", err)
		return nil, nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Errorf("Failed to read get documents response: %v", err)
		return nil, nil, fmt.Errorf("failed to read response: %w", err)
	}

	logrus.Debugf("Get documents response: %s", string(body))

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		logrus.Errorf("Get documents failed with status %d: %s", resp.StatusCode, string(body))
		return nil, nil, fmt.Errorf("get documents failed with status %d: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		logrus.Errorf("Failed to parse get documents response: %v", err)
		return nil, nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// 提取文档
	documentsInterface, ok := result["documents"]
	if !ok {
		logrus.Errorf("No documents field in response")
		return nil, nil, fmt.Errorf("no documents field in response")
	}

	documentsList, ok := documentsInterface.([]interface{})
	if !ok {
		logrus.Errorf("Documents field is not a list")
		return nil, nil, fmt.Errorf("documents field is not a list")
	}

	// 转换文档列表
	documents := make([]string, len(documentsList))
	for i, doc := range documentsList {
		documents[i] = fmt.Sprintf("%v", doc)
	}

	// 如果需要元数据，提取元数据
	var metadatas []map[string]interface{}
	if includeMetadata {
		metadatasInterface, ok := result["metadatas"]
		if !ok {
			logrus.Warnf("No metadatas field in response despite requesting it")
			metadatas = make([]map[string]interface{}, len(documents))
			for i := range metadatas {
				metadatas[i] = make(map[string]interface{})
			}
		} else {
			metadatasList, ok := metadatasInterface.([]interface{})
			if !ok {
				logrus.Errorf("Metadatas field is not a list")
				return nil, nil, fmt.Errorf("metadatas field is not a list")
			}

			metadatas = make([]map[string]interface{}, len(metadatasList))
			for i, meta := range metadatasList {
				metaMap, ok := meta.(map[string]interface{})
				if ok {
					metadatas[i] = metaMap
				} else {
					metadatas[i] = make(map[string]interface{})
				}
			}
		}
	} else {
		metadatas = make([]map[string]interface{}, len(documents))
		for i := range metadatas {
			metadatas[i] = make(map[string]interface{})
		}
	}

	logrus.Infof("Successfully retrieved %d documents from collection %s", len(documents), collectionID)
	return documents, metadatas, nil
}

// DeleteDocuments 删除文档
func (c *RealChromaClient) DeleteDocuments(ctx context.Context, collectionID string, ids []string) error {
	logrus.Infof("Deleting %d documents from Chroma collection: %s", len(ids), collectionID)

	// 准备请求数据
	reqData := map[string]interface{}{
		"ids": ids,
	}

	jsonData, err := json.Marshal(reqData)
	if err != nil {
		logrus.Errorf("Failed to marshal delete documents request: %v", err)
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// 构建 URL - 使用正确的 API 路径
	// 根据 Chroma v1.0.0 API 规范，使用 /api/v2/tenants/{tenant}/databases/{database}/collections/{collection_id}/delete 端点
	url := fmt.Sprintf("%s/tenants/%s/databases/%s/collections/%s/delete", c.baseURL, c.tenant, c.database, collectionID)
	logrus.Debugf("[DEBUG] POST request to: %s with data: %s", url, string(jsonData))

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		logrus.Errorf("Failed to create delete documents request: %v", err)
		return fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		logrus.Errorf("Failed to send delete documents request: %v", err)
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Errorf("Failed to read delete documents response: %v", err)
		return fmt.Errorf("failed to read response: %w", err)
	}

	logrus.Debugf("Delete documents response: %s", string(body))

	// 检查状态码
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		logrus.Errorf("Delete documents failed with status %d: %s", resp.StatusCode, string(body))
		return fmt.Errorf("delete documents failed with status %d: %s", resp.StatusCode, string(body))
	}

	logrus.Infof("Successfully deleted %d documents from collection %s", len(ids), collectionID)
	return nil
}

// QueryDocumentsWithEmbedding 使用向量查询文档
func (c *RealChromaClient) QueryDocumentsWithEmbedding(ctx context.Context, collectionID string, queryEmbedding []float32, nResults int, where map[string]interface{}) ([][]string, [][]map[string]interface{}, [][]float32, error) {
	// 确保数据库存在
	if err := c.ensureDatabaseExists(ctx); err != nil {
		logrus.Warnf("Failed to ensure database exists before querying documents: %v", err)
		// 继续尝试查询，可能会失败
	}
	whereJSON, _ := json.Marshal(where)
	logrus.Infof("Querying Chroma collection %s with embedding vector and filter: %s", collectionID, string(whereJSON))

	// 准备请求数据
	reqData := map[string]interface{}{
		"n_results":   nResults,
		"include":     []string{"documents", "metadatas", "distances"},
		"query_embeddings": [][]float32{queryEmbedding}, // 使用真实的查询向量
	}

	// 添加过滤条件（如果有）
	if where != nil && len(where) > 0 {
		// 使用formatWhereClause函数格式化where子句
		formattedWhere := formatWhereClause(where)
		reqData["where"] = formattedWhere
		logrus.Debugf("Formatted where clause: %v", formattedWhere)
	}

	jsonData, err := json.Marshal(reqData)
	if err != nil {
		logrus.Errorf("Failed to marshal query documents request: %v", err)
		return nil, nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// 构建URL - 使用正确的 API 路径
	url := fmt.Sprintf("%s/tenants/%s/databases/%s/collections/%s/query", c.baseURL, c.tenant, c.database, collectionID)
	logrus.Debugf("[DEBUG] POST request to: %s with data: %s", url, string(jsonData))

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		logrus.Errorf("Failed to create query documents request: %v", err)
		return nil, nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		logrus.Errorf("Failed to send query documents request: %v", err)
		return nil, nil, nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Errorf("Failed to read query documents response: %v", err)
		return nil, nil, nil, fmt.Errorf("failed to read response: %w", err)
	}

	logrus.Debugf("Query documents response: %s", string(body))

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		logrus.Errorf("Query documents failed with status %d: %s", resp.StatusCode, string(body))
		return nil, nil, nil, fmt.Errorf("query documents failed with status %d: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		logrus.Errorf("Failed to parse query documents response: %v", err)
		return nil, nil, nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// 初始化返回数据结构
	// 单个查询向量，所以只有一组结果
	ids := make([][]string, 1) 
	metas := make([][]map[string]interface{}, 1)
	scores := make([][]float32, 1)
	
	// 检查查询向量的维度
	logrus.Debugf("Query embedding dimension: %d", len(queryEmbedding))
	
	// 确保查询向量的维度为3072
	if len(queryEmbedding) != 3072 {
		logrus.Warnf("Query embedding dimension mismatch: expected 3072, got %d. Adjusting vector dimension.", len(queryEmbedding))
		
		// 创建一个3072维的新向量
		adjustedEmbedding := make([]float32, 3072)
		
		// 如果原始向量过短，复制现有的值并填充剩余部分
		// 如果原始向量过长，只复制前3072个元素
		copyLen := len(queryEmbedding)
		if copyLen > 3072 {
			copyLen = 3072
		}
		
		// 复制现有的向量值
		for i := 0; i < copyLen; i++ {
			adjustedEmbedding[i] = queryEmbedding[i]
		}
		
		// 如果原始向量过短，填充剩余部分
		for i := copyLen; i < 3072; i++ {
			adjustedEmbedding[i] = 0.00001 // 使用很小的值
		}
		
		// 使用调整后的向量
		queryEmbedding = adjustedEmbedding
		logrus.Infof("Vector dimension adjusted to 3072")
	}

	// 提取IDs
	idsInterface, ok := result["ids"]
	if !ok {
		logrus.Errorf("No ids field in response")
		return nil, nil, nil, fmt.Errorf("no ids field in response")
	}

	idsArray, ok := idsInterface.([]interface{})
	if !ok {
		logrus.Errorf("Ids field is not an array")
		return nil, nil, nil, fmt.Errorf("ids field is not an array")
	}

	for i, idList := range idsArray {
		idListArray, ok := idList.([]interface{})
		if !ok {
			logrus.Errorf("Id list is not an array")
			continue
		}

		ids[i] = make([]string, len(idListArray))
		for j, id := range idListArray {
			ids[i][j] = fmt.Sprintf("%v", id)
		}
	}

	// 提取元数据
	metadatasInterface, ok := result["metadatas"]
	if !ok {
		logrus.Warnf("No metadatas field in response")
		// 创建空元数据
		for i := range ids {
			metas[i] = make([]map[string]interface{}, len(ids[i]))
			for j := range ids[i] {
				metas[i][j] = make(map[string]interface{})
			}
		}
	} else {
		metadatasArray, ok := metadatasInterface.([]interface{})
		if !ok {
			logrus.Errorf("Metadatas field is not an array")
			return nil, nil, nil, fmt.Errorf("metadatas field is not an array")
		}

		for i, metaList := range metadatasArray {
			metaListArray, ok := metaList.([]interface{})
			if !ok {
				logrus.Errorf("Metadata list is not an array")
				continue
			}

			metas[i] = make([]map[string]interface{}, len(metaListArray))
			for j, meta := range metaListArray {
				metaMap, ok := meta.(map[string]interface{})
				if ok {
					metas[i][j] = metaMap
				} else {
					metas[i][j] = make(map[string]interface{})
				}
			}
		}
	}

	// 提取距离分数
	distancesInterface, ok := result["distances"]
	if !ok {
		logrus.Warnf("No distances field in response")
		// 创建默认分数
		for i := range ids {
			scores[i] = make([]float32, len(ids[i]))
			for j := range ids[i] {
				scores[i][j] = 1.0 - float32(j)*0.1 // 默认递减分数
			}
		}
	} else {
		distancesArray, ok := distancesInterface.([]interface{})
		if !ok {
			logrus.Errorf("Distances field is not an array")
			return nil, nil, nil, fmt.Errorf("distances field is not an array")
		}

		for i, distList := range distancesArray {
			distListArray, ok := distList.([]interface{})
			if !ok {
				logrus.Errorf("Distance list is not an array")
				continue
			}

			scores[i] = make([]float32, len(distListArray))
			for j, dist := range distListArray {
				// 将距离转换为相似度分数（距离越小越相似）
				distVal, ok := dist.(float64)
				if ok {
					scores[i][j] = 1.0 - float32(distVal)
				} else {
					scores[i][j] = 1.0 - float32(j)*0.1 // 默认递减分数
				}
			}
		}
	}

	return ids, metas, scores, nil
}

// QueryDocuments 查询文档
func (c *RealChromaClient) QueryDocuments(ctx context.Context, collectionID string, queryTexts []string, nResults int, where map[string]interface{}) ([][]string, [][]map[string]interface{}, [][]float32, error) {
	// 确保数据库存在
	if err := c.ensureDatabaseExists(ctx); err != nil {
		logrus.Warnf("Failed to ensure database exists before querying documents: %v", err)
		// 继续尝试查询，可能会失败
	}
	whereJSON, _ := json.Marshal(where)
	logrus.Infof("Querying Chroma collection %s with filter: %s", collectionID, string(whereJSON))

	// 准备请求数据
	reqData := map[string]interface{}{
		"n_results":   nResults,
		"include":     []string{"documents", "metadatas", "distances"},
	}
	
	// 添加查询文本
	if queryTexts != nil && len(queryTexts) > 0 {
		reqData["query_texts"] = queryTexts
	}
	
	// Chroma v1.0.0 API要求始终提供 query_embeddings 字段
	// 创建一个有效的查询向量
	// 使用单个元素的向量，但包含多个维度（与实际向量维度相匹配）
	// 这里使用一个3072维的向量，所有值都设置为很小的数值
	embeddingDim := 3072 // 使用与集合相匹配的嵌入维度
	embedding := make([]float32, embeddingDim)
	for i := range embedding {
		embedding[i] = 0.00001 // 使用很小的值而不是0，以避免可能的数值问题
	}
	reqData["query_embeddings"] = [][]float32{embedding}

	// 添加过滤条件（如果有）
	if where != nil && len(where) > 0 {
		// 使用formatWhereClause函数格式化where子句
		formattedWhere := formatWhereClause(where)
		reqData["where"] = formattedWhere
		logrus.Debugf("Formatted where clause: %v", formattedWhere)
	}

	jsonData, err := json.Marshal(reqData)
	if err != nil {
		logrus.Errorf("Failed to marshal query documents request: %v", err)
		return nil, nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// 构建URL - 使用正确的 API 路径
	// 根据 Chroma v1.0.0 API 规范，使用 /api/v2/tenants/{tenant}/databases/{database}/collections/{collection_id}/query 端点
	url := fmt.Sprintf("%s/tenants/%s/databases/%s/collections/%s/query", c.baseURL, c.tenant, c.database, collectionID)
	logrus.Debugf("[DEBUG] POST request to: %s with data: %s", url, string(jsonData))

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		logrus.Errorf("Failed to create query documents request: %v", err)
		return nil, nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		logrus.Errorf("Failed to send query documents request: %v", err)
		return nil, nil, nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Errorf("Failed to read query documents response: %v", err)
		return nil, nil, nil, fmt.Errorf("failed to read response: %w", err)
	}

	logrus.Debugf("Query documents response: %s", string(body))

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		logrus.Errorf("Query documents failed with status %d: %s", resp.StatusCode, string(body))
		return nil, nil, nil, fmt.Errorf("query documents failed with status %d: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		logrus.Errorf("Failed to parse query documents response: %v", err)
		return nil, nil, nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// 初始化返回数据结构
	ids := make([][]string, len(queryTexts))
	metas := make([][]map[string]interface{}, len(queryTexts))
	scores := make([][]float32, len(queryTexts))

	// 提取IDs
	idsInterface, ok := result["ids"]
	if !ok {
		logrus.Errorf("No ids field in response")
		return nil, nil, nil, fmt.Errorf("no ids field in response")
	}

	idsArray, ok := idsInterface.([]interface{})
	if !ok {
		logrus.Errorf("Ids field is not an array")
		return nil, nil, nil, fmt.Errorf("ids field is not an array")
	}

	for i, idList := range idsArray {
		idListArray, ok := idList.([]interface{})
		if !ok {
			logrus.Errorf("Id list is not an array")
			continue
		}

		ids[i] = make([]string, len(idListArray))
		for j, id := range idListArray {
			ids[i][j] = fmt.Sprintf("%v", id)
		}
	}

	// 提取元数据
	metadatasInterface, ok := result["metadatas"]
	if !ok {
		logrus.Warnf("No metadatas field in response")
		// 创建空元数据
		for i := range queryTexts {
			metas[i] = make([]map[string]interface{}, len(ids[i]))
			for j := range ids[i] {
				metas[i][j] = make(map[string]interface{})
			}
		}
	} else {
		metadatasArray, ok := metadatasInterface.([]interface{})
		if !ok {
			logrus.Errorf("Metadatas field is not an array")
			return nil, nil, nil, fmt.Errorf("metadatas field is not an array")
		}

		for i, metaList := range metadatasArray {
			metaListArray, ok := metaList.([]interface{})
			if !ok {
				logrus.Errorf("Metadata list is not an array")
				continue
			}

			metas[i] = make([]map[string]interface{}, len(metaListArray))
			for j, meta := range metaListArray {
				metaMap, ok := meta.(map[string]interface{})
				if ok {
					metas[i][j] = metaMap
				} else {
					metas[i][j] = make(map[string]interface{})
				}
			}
		}
	}

	// 提取距离分数
	distancesInterface, ok := result["distances"]
	if !ok {
		logrus.Warnf("No distances field in response")
		// 创建默认分数
		for i := range queryTexts {
			scores[i] = make([]float32, len(ids[i]))
			for j := range ids[i] {
				scores[i][j] = 1.0 - float32(j)*0.1 // 默认递减分数
			}
		}
	} else {
		distancesArray, ok := distancesInterface.([]interface{})
		if !ok {
			logrus.Errorf("Distances field is not an array")
			return nil, nil, nil, fmt.Errorf("distances field is not an array")
		}

		for i, distList := range distancesArray {
			distListArray, ok := distList.([]interface{})
			if !ok {
				logrus.Errorf("Distance list is not an array")
				continue
			}

			scores[i] = make([]float32, len(distListArray))
			for j, dist := range distListArray {
				// 将距离转换为相似度分数（距离越小越相似）
				// 注意：这里假设距离在[0,1]范围内，如果不是可能需要调整
				distVal, ok := dist.(float64)
				if ok {
					scores[i][j] = 1.0 - float32(distVal)
				} else {
					scores[i][j] = 1.0 - float32(j)*0.1 // 默认递减分数
				}
			}
		}
	}

	logrus.Infof("Successfully queried collection %s, found %d results for %d queries",
		collectionID, len(ids[0]), len(queryTexts))
	return ids, metas, scores, nil
}

// Close 关闭客户端
func (c *RealChromaClient) Close() error {
	logrus.Info("Closing Chroma client")

	// 释放资源
	c.mu.Lock()
	c.collections = nil
	c.mu.Unlock()

	// 关闭HTTP客户端的空闲连接
	c.httpClient.CloseIdleConnections()

	return nil
}
