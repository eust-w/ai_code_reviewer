package indexer

import (
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

// ChromaStorage 使用Chroma作为存储后端
type ChromaStorage struct {
	client      ChromaClient
	collections map[string]string // repoKey -> collectionID
	mu          sync.RWMutex      // 保护collections映射的读写锁
}

// ChromaClient 是Chroma API客户端的接口
// 这是一个接口，以便于我们可以在测试中模拟它
type ChromaClient interface {
	CreateCollection(ctx context.Context, name string, metadata map[string]interface{}) (string, error)
	GetCollection(ctx context.Context, name string) (string, error)
	AddDocuments(ctx context.Context, collectionID string, ids []string, documents []string, metadatas []map[string]interface{}, embeddings [][]float32) error
	GetDocuments(ctx context.Context, collectionID string, ids []string, includeMetadata bool) ([]string, []map[string]interface{}, error)
	DeleteDocuments(ctx context.Context, collectionID string, ids []string) error
	QueryDocuments(ctx context.Context, collectionID string, queryTexts []string, nResults int, where map[string]interface{}) ([][]string, [][]map[string]interface{}, [][]float32, error)
	Close() error
}

// NewChromaStorage 创建新的Chroma存储
func NewChromaStorage(config *StorageConfig) (*ChromaStorage, error) {
	// 初始化Chroma客户端
	client, err := NewRealChromaClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Chroma client: %w", err)
	}

	return &ChromaStorage{
		client:      client,
		collections: make(map[string]string),
	}, nil
}

// getOrCreateCollection 获取或创建集合
func (s *ChromaStorage) getOrCreateCollection(ctx context.Context, repoKey string) (string, error) {
	// 使用读锁检查缓存
	s.mu.RLock()
	collID, ok := s.collections[repoKey]
	s.mu.RUnlock()
	
	if ok {
		return collID, nil
	}

	// 尝试获取现有集合
	collectionName := fmt.Sprintf("code_snippets_%s", strings.ReplaceAll(repoKey, "/", "_"))
	collID, err := s.client.GetCollection(ctx, collectionName)
	if err == nil {
		// 找到现有集合，使用写锁更新缓存
		s.mu.Lock()
		s.collections[repoKey] = collID
		s.mu.Unlock()
		return collID, nil
	}

	// 创建新集合
	metadata := map[string]interface{}{
		"repo_key":    repoKey,
		"description": fmt.Sprintf("Code snippets for repository %s", repoKey),
		"created_at":  time.Now().Unix(),
	}

	collID, err = s.client.CreateCollection(ctx, collectionName, metadata)
	if err != nil {
		return "", fmt.Errorf("failed to create collection for repo %s: %w", repoKey, err)
	}

	// 使用写锁保护缓存更新
	s.mu.Lock()
	s.collections[repoKey] = collID
	s.mu.Unlock()
	return collID, nil
}

// SaveCodeSnippet 保存代码片段
func (s *ChromaStorage) SaveCodeSnippet(ctx context.Context, repoKey, filename, content string, metadata map[string]interface{}) (string, error) {
	collID, err := s.getOrCreateCollection(ctx, repoKey)
	if err != nil {
		return "", err
	}

	// 生成唯一ID
	id := fmt.Sprintf("%s_%s_%d", repoKey, strings.ReplaceAll(filename, "/", "_"), time.Now().UnixNano())

	// 确保元数据包含必要字段
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["repo_key"] = repoKey
	metadata["filename"] = filename
	metadata["indexed_at"] = time.Now().Unix()

	// 添加文档
	err = s.client.AddDocuments(ctx, collID, []string{id}, []string{content}, []map[string]interface{}{metadata}, nil)
	if err != nil {
		return "", fmt.Errorf("failed to add document to Chroma: %w", err)
	}

	return id, nil
}

// GetCodeSnippet 获取代码片段
func (s *ChromaStorage) GetCodeSnippet(ctx context.Context, id string) (string, map[string]interface{}, error) {
	// 从ID中提取repoKey
	parts := strings.Split(id, "_")
	if len(parts) < 2 {
		return "", nil, fmt.Errorf("invalid snippet ID format: %s", id)
	}
	repoKey := parts[0] + "/" + parts[1]

	collID, err := s.getOrCreateCollection(ctx, repoKey)
	if err != nil {
		return "", nil, err
	}

	docs, metas, err := s.client.GetDocuments(ctx, collID, []string{id}, true)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get document from Chroma: %w", err)
	}

	if len(docs) == 0 {
		return "", nil, fmt.Errorf("snippet not found: %s", id)
	}

	return docs[0], metas[0], nil
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

	if len(ids) == 0 {
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

	if len(ids) == 0 {
		return []string{}, nil
	}

	return ids[0], nil
}

// Close 关闭存储
func (s *ChromaStorage) Close() error {
	return s.client.Close()
}

// RealChromaClient 实现了ChromaClient接口，使用实际的Chroma API
type RealChromaClient struct {
	// 实际的HTTP客户端实例
	baseURL     string
	apiVersion  string
	httpClient  *http.Client
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
	
	// 确保使用v2 API
	apiPath := config.ChromaPath
	if apiPath == "" {
		apiPath = "/api"
	}
	
	// 确定API版本
	apiVersion := "v2"
	if !strings.Contains(apiPath, "/v") {
		apiPath = fmt.Sprintf("%s/%s", apiPath, apiVersion)
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
	
	baseURL := fmt.Sprintf("%s://%s:%d%s", protocol, config.ChromaHost, config.ChromaPort, apiPath)
	
	logrus.Infof("Connecting to Chroma at %s (API version: %s)", baseURL, apiVersion)
	
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
	testURL := fmt.Sprintf("%s://%s:%d/api/version", protocol, config.ChromaHost, config.ChromaPort)
	req, err := http.NewRequest("GET", testURL, nil)
	if err != nil {
		logrus.Errorf("Failed to create request for Chroma version check: %v", err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	resp, err := httpClient.Do(req)
	if err != nil {
		logrus.Errorf("Failed to connect to Chroma server: %v", err)
		return nil, fmt.Errorf("failed to connect to Chroma server: %w", err)
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
	
	return &RealChromaClient{
		baseURL:     baseURL,
		apiVersion:  apiVersion,
		httpClient:  httpClient,
		collections: make(map[string]string),
	}, nil
}

// CreateCollection 创建集合
func (c *RealChromaClient) CreateCollection(ctx context.Context, name string, metadata map[string]interface{}) (string, error) {
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
		"name": name,
		"metadata": metadata,
	}
	
	jsonData, err := json.Marshal(reqData)
	if err != nil {
		logrus.Errorf("Failed to marshal collection creation request: %v", err)
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}
	
	// 构建请求URL
	url := fmt.Sprintf("%s/collections", c.baseURL)
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

// GetCollection 获取集合
func (c *RealChromaClient) GetCollection(ctx context.Context, name string) (string, error) {
	logrus.Infof("Getting Chroma collection: %s", name)
	
	// 检查缓存
	c.mu.RLock()
	if id, ok := c.collections[name]; ok {
		c.mu.RUnlock()
		logrus.Debugf("Collection %s found in cache with ID: %s", name, id)
		return id, nil
	}
	c.mu.RUnlock()
	
	// 构建请求URL
	url := fmt.Sprintf("%s/collections", c.baseURL)
	logrus.Debugf("GET request to: %s", url)
	
	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		logrus.Errorf("Failed to parse collection list response: %v", err)
		return "", fmt.Errorf("failed to parse response: %w", err)
	}
	
	// 提取集合列表
	collections, ok := result["collections"]
	if !ok {
		logrus.Errorf("No collections field in response")
		return "", fmt.Errorf("no collections field in response")
	}
	
	collectionsList, ok := collections.([]interface{})
	if !ok {
		logrus.Errorf("Collections field is not a list")
		return "", fmt.Errorf("collections field is not a list")
	}
	
	// 查找指定名称的集合
	for _, coll := range collectionsList {
		collMap, ok := coll.(map[string]interface{})
		if !ok {
			continue
		}
		
		collName, ok := collMap["name"]
		if !ok {
			continue
		}
		
		if collName == name {
			id, ok := collMap["id"]
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
	logrus.Infof("Adding %d documents to Chroma collection: %s", len(documents), collectionID)
	
	// 准备请求数据
	reqData := map[string]interface{}{
		"ids": ids,
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
	
	// 构建URL
	url := fmt.Sprintf("%s/collections/%s/documents", c.baseURL, collectionID)
	logrus.Debugf("POST request to: %s", url)
	
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
	logrus.Infof("Getting %d documents from Chroma collection: %s", len(ids), collectionID)
	
	// 准备请求数据
	reqData := map[string]interface{}{
		"ids": ids,
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
	
	// 构建URL
	url := fmt.Sprintf("%s/collections/%s/documents/get", c.baseURL, collectionID)
	logrus.Debugf("POST request to: %s with data: %s", url, string(jsonData))
	
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
	
	// 构建URL
	url := fmt.Sprintf("%s/collections/%s/documents", c.baseURL, collectionID)
	logrus.Debugf("DELETE request to: %s with data: %s", url, string(jsonData))
	
	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, strings.NewReader(string(jsonData)))
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

// QueryDocuments 查询文档
func (c *RealChromaClient) QueryDocuments(ctx context.Context, collectionID string, queryTexts []string, nResults int, where map[string]interface{}) ([][]string, [][]map[string]interface{}, [][]float32, error) {
	whereJSON, _ := json.Marshal(where)
	logrus.Infof("Querying Chroma collection %s with filter: %s", collectionID, string(whereJSON))
	
	// 准备请求数据
	reqData := map[string]interface{}{
		"query_texts": queryTexts,
		"n_results": nResults,
		"include": []string{"documents", "metadatas", "distances"},
	}
	
	// 添加过滤条件（如果有）
	if where != nil && len(where) > 0 {
		reqData["where"] = where
	}
	
	jsonData, err := json.Marshal(reqData)
	if err != nil {
		logrus.Errorf("Failed to marshal query documents request: %v", err)
		return nil, nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	
	// 构建URL
	url := fmt.Sprintf("%s/collections/%s/query", c.baseURL, collectionID)
	logrus.Debugf("POST request to: %s with data: %s", url, string(jsonData))
	
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
