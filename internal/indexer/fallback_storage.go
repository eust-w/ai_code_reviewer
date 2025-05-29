package indexer

import (
	"context"
	"fmt"
	"github.com/sirupsen/logrus"
)

// FallbackStorage 是一个包装器，可以在主存储不可用时提供备用功能
type FallbackStorage struct {
	primary Storage
}

// NewFallbackStorage 创建一个新的FallbackStorage
func NewFallbackStorage(primary Storage) *FallbackStorage {
	return &FallbackStorage{
		primary: primary,
	}
}

// GetChromaClient 尝试从存储中获取ChromaClient，如果不可用则返回nil和错误
func (fs *FallbackStorage) GetChromaClient() (ChromaClient, error) {
	// 尝试将主存储转换为ChromaStorage
	if chromaStorage, ok := fs.primary.(*ChromaStorage); ok {
		return chromaStorage.client, nil
	}
	return nil, fmt.Errorf("primary storage is not ChromaStorage type")
}

// TryGetCollectionID 尝试获取集合ID，如果不可用则返回空字符串和错误
func (fs *FallbackStorage) TryGetCollectionID(ctx context.Context, repoKey string) (string, error) {
	// 尝试将主存储转换为ChromaStorage
	if chromaStorage, ok := fs.primary.(*ChromaStorage); ok {
		// 使用ChromaStorage的方法获取集合ID
		collName := fmt.Sprintf("%s_collection", repoKey)
		return chromaStorage.getOrCreateCollection(ctx, collName)
	}
	return "", fmt.Errorf("primary storage is not ChromaStorage type")
}

// QueryImportsFallback 提供一个降级的导入查询实现
func (fs *FallbackStorage) QueryImportsFallback(ctx context.Context, repoKey, filename, commitHash string) (map[string][]string, error) {
	// 尝试从主存储查询
	client, err := fs.GetChromaClient()
	if err != nil {
		logrus.Warnf("[索引增强] 无法获取Chroma客户端: %v - 返回空结果", err)
		return map[string][]string{}, nil
	}

	// 获取集合ID
	collID, err := fs.TryGetCollectionID(ctx, repoKey)
	if err != nil {
		logrus.Warnf("[索引增强] 无法获取集合ID: %v - 返回空结果", err)
		return map[string][]string{}, nil
	}

	// 构建查询条件
	whereClause := map[string]interface{}{
		"repo_key": repoKey,
		"filename": filename,
	}

	// 如果提供了commit hash，则查询特定版本的代码
	if commitHash != "" {
		whereClause["commit_hash"] = commitHash
	}

	// 获取所有文档
	documents, metadatas, err := client.GetDocuments(
		ctx,
		collID,
		[]string{}, // 不指定IDs，获取所有文档
		true,       // 包含元数据
	)

	if err != nil {
		logrus.Warnf("[索引增强] 查询导入语句失败: %v - 返回空结果", err)
		return map[string][]string{}, nil
	}

	// 从文档中提取导入语句
	result := make(map[string][]string)

	// 遍历所有文档及其元数据
	for i, doc := range documents {
		// 检查元数据，只收集类型为"import"的文档
		if i < len(metadatas) && metadatas[i]["type"] == "import" {
			// 检查文件名是否匹配
			if docFilename, ok := metadatas[i]["filename"].(string); ok && docFilename == filename {
				// 如果指定了commit hash，检查是否匹配
				if commitHash != "" {
					if docCommitHash, ok := metadatas[i]["commit_hash"].(string); ok && docCommitHash != commitHash {
						continue // 跳过不匹配的commit hash
					}
				}

				// 从元数据中获取导入的包名
				if packageName, ok := metadatas[i]["package"].(string); ok {
					// 将导入语句添加到结果中
					if _, exists := result[packageName]; !exists {
						result[packageName] = []string{}
					}
					result[packageName] = append(result[packageName], doc)
				}
			}
		}
	}

	return result, nil
}

// QueryDefinitionsFallback 提供一个降级的定义查询实现
func (fs *FallbackStorage) QueryDefinitionsFallback(ctx context.Context, repoKey, filename, commitHash string) (map[string]string, error) {
	// 尝试从主存储查询
	client, err := fs.GetChromaClient()
	if err != nil {
		logrus.Warnf("[索引增强] 无法获取Chroma客户端: %v - 返回空结果", err)
		return map[string]string{}, nil
	}

	// 获取集合ID
	collID, err := fs.TryGetCollectionID(ctx, repoKey)
	if err != nil {
		logrus.Warnf("[索引增强] 无法获取集合ID: %v - 返回空结果", err)
		return map[string]string{}, nil
	}

	// 构建查询条件
	whereClause := map[string]interface{}{
		"repo_key": repoKey,
		"filename": filename,
	}

	// 如果提供了commit hash，则查询特定版本的代码
	if commitHash != "" {
		whereClause["commit_hash"] = commitHash
	}

	// 构建查询文本，包含关键词"function"、"type"、"struct"等
	queryTexts := []string{"function", "type", "struct", "interface", "const", "var"}

	// 设置结果数量限制
	limit := 20

	// 查询文档
	ids, _, _, err := client.QueryDocuments(
		ctx,
		collID,
		queryTexts,
		limit,
		whereClause,
	)

	if err != nil {
		logrus.Warnf("[索引增强] 查询文档失败: %v - 返回空结果", err)
		return map[string]string{}, nil
	}

	// 初始化结果集
	definitions := make(map[string]string)

	// 检查我们是否有查询结果
	if len(ids) > 0 && len(ids[0]) > 0 {
		// 获取文档内容和元数据
		documents, metadatas, err := client.GetDocuments(
			ctx,
			collID,
			ids[0],
			true, // 包含元数据
		)

		if err != nil {
			logrus.Warnf("[索引增强] 获取文档内容失败: %v - 返回空结果", err)
			return map[string]string{}, nil
		}

		// 处理查询结果
		for i, doc := range documents {
			if i >= len(metadatas) {
				continue
			}

			// 从元数据中提取符号名称
			if symbolName, ok := metadatas[i]["symbol_name"].(string); ok && symbolName != "" {
				definitions[symbolName] = doc
			} else if symbolType, ok := metadatas[i]["symbol_type"].(string); ok && symbolType != "" {
				// 如果没有符号名称，尝试使用符号类型和行号作为键
				lineNum := ""
				if ln, ok := metadatas[i]["line_start"].(float64); ok {
					lineNum = fmt.Sprintf("L%d", int(ln))
				}
				key := fmt.Sprintf("%s_%s", symbolType, lineNum)
				definitions[key] = doc
			} else {
				// 使用文档ID作为键
				definitions[ids[0][i]] = doc
			}
		}
	}

	return definitions, nil
}

// QuerySimilarCodeFallback 提供一个降级的相似代码查询实现
func (fs *FallbackStorage) QuerySimilarCodeFallback(ctx context.Context, repoKey, language, code string, limit int) ([]CodeSnippet, error) {
	// 尝试从主存储查询
	client, err := fs.GetChromaClient()
	if err != nil {
		logrus.Warnf("[索引增强] 无法获取Chroma客户端: %v - 返回空结果", err)
		return []CodeSnippet{}, nil
	}

	// 获取集合ID
	collID, err := fs.TryGetCollectionID(ctx, repoKey)
	if err != nil {
		logrus.Warnf("[索引增强] 无法获取集合ID: %v - 返回空结果", err)
		return []CodeSnippet{}, nil
	}

	// 构建查询条件
	whereClause := map[string]interface{}{
		"repo_key": repoKey,
	}

	// 如果指定了语言，添加语言过滤条件
	if language != "" {
		whereClause["language"] = language
	}

	// 设置默认的结果数量限制
	if limit <= 0 {
		limit = 5
	}

	// 将代码切分为关键词进行查询
	keywords := extractKeywords(code)
	ids, _, distances, err := client.QueryDocuments(
		ctx,
		collID,
		keywords,
		limit,
		whereClause,
	)

	if err != nil {
		logrus.Warnf("[索引增强] 查询相似代码失败: %v - 返回空结果", err)
		return []CodeSnippet{}, nil
	}

	// 检查是否有结果
	if len(ids) == 0 || len(ids[0]) == 0 {
		logrus.Info("[索引增强] 没有找到相似代码")
		return []CodeSnippet{}, nil
	}

	// 获取文档内容和元数据
	documents, metadatas, err := client.GetDocuments(
		ctx,
		collID,
		ids[0],
		true,
	)

	if err != nil {
		logrus.Warnf("[索引增强] 获取文档内容失败: %v - 返回空结果", err)
		return []CodeSnippet{}, nil
	}

	// 将查询结果转换为CodeSnippet对象
	results := make([]CodeSnippet, 0)
	for i, docID := range ids[0] {
		if i >= len(distances[0]) {
			continue
		}

		// 计算相似度分数（将距离转换为相似度）
		similarity := 1.0 - float64(distances[0][i])
		if similarity < 0 {
			similarity = 0
		}

		// 获取文档内容和元数据
		var content string
		var metadata map[string]interface{}

		// 如果我们有文档内容
		if i < len(documents) {
			content = documents[i]
		}

		// 如果我们有元数据
		if i < len(metadatas) {
			metadata = metadatas[i]
		} else {
			metadata = map[string]interface{}{
				"repo_key": repoKey,
			}
			if language != "" {
				metadata["language"] = language
			}
		}

		// 从元数据中提取文件名和行号
		filename := ""
		lineStart := 0
		lineEnd := 0

		if fn, ok := metadata["filename"].(string); ok {
			filename = fn
		}

		if ls, ok := metadata["line_start"].(float64); ok {
			lineStart = int(ls)
		}

		if le, ok := metadata["line_end"].(float64); ok {
			lineEnd = int(le)
		}

		// 创建CodeSnippet对象
		// 注意：我们不使用docID，因为CodeSnippet结构体中没有ID字段
		_ = docID // 显式忽略未使用的变量
		snippet := CodeSnippet{
			Content:    content,
			Filename:   filename,
			LineStart:  lineStart,
			LineEnd:    lineEnd,
			Similarity: similarity,
		}

		results = append(results, snippet)
	}

	return results, nil
}
