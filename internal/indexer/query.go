package indexer

import (
	"context"
	"fmt"
	"strings"
	"github.com/sirupsen/logrus"
)

// Query 表示代码查询
type Query struct {
	Text     string            // 查询文本
	Filters  map[string]string // 过滤条件
	Language string            // 代码语言
	Limit    int               // 结果数量限制
}

// QueryResult 表示查询结果
type QueryResult struct {
	ID         string                 // 代码片段ID
	Content    string                 // 代码内容
	Metadata   map[string]interface{} // 元数据
	Similarity float64                // 相似度分数
}

// QueryService 代码查询服务
type QueryService struct {
	storage Storage
	vector  VectorService
}

// NewQueryService 创建新的查询服务
func NewQueryService(storage Storage, vector VectorService) *QueryService {
	return &QueryService{
		storage: storage,
		vector:  vector,
	}
}

// QuerySimilarCode 查询相似代码
func (s *QueryService) QuerySimilarCode(ctx context.Context, repoKey, language, code string, limit int) ([]QueryResult, error) {
	logrus.Infof("Querying similar code in repository %s (language: %s)", repoKey, language)

	// 将代码转换为向量
	// 注意：由于我们使用QueryDocuments而不是直接使用向量查询，
	// 我们不需要实际使用向量，但仍然需要检查向量服务是否可用
	_, err := s.vector.EmbedCode(ctx, language, code)
	if err != nil {
		logrus.Warnf("Failed to embed code: %v - will use text search instead", err)
	}

	// 使用Chroma的向量搜索功能
	// 将QueryService的storage转换为ChromaStorage
	chromaStorage, ok := s.storage.(*ChromaStorage)
	if !ok {
		return nil, fmt.Errorf("storage is not ChromaStorage type")
	}

	// 获取集合ID
	// 注意：ChromaStorage没有GetCollectionID方法
	// 我们需要构造集合ID
	collID := fmt.Sprintf("%s_collection", repoKey)

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

	// 使用Chroma客户端进行文本查询
	// 注意：我们使用QueryDocuments进行文本查询
	// 将代码切分为关键词进行查询
	keywords := extractKeywords(code)
	ids, _, distances, err := chromaStorage.client.QueryDocuments(
		ctx,
		collID,
		keywords, // 使用从代码中提取的关键词进行查询
		limit,
		whereClause,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to query documents: %w", err)
	}

	// 获取文档内容
	// 注意：ChromaClient没有GetMetadatas方法，使用GetDocuments获取文档内容
	documents, metadatas, err := chromaStorage.client.GetDocuments(
		ctx,
		collID,
		ids[0], // 使用第一组查询结果的IDs
		true,   // 包含元数据
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get metadatas: %w", err)
	}

	// 将查询结果转换为QueryResult对象
	results := make([]QueryResult, 0)
	
	// 检查我们是否有查询结果
	if len(ids) > 0 && len(ids[0]) > 0 {
		// 遍历所有文档
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
			
			result := QueryResult{
				ID:         docID,
				Content:    content,
				Metadata:   metadata,
				Similarity: similarity,
			}
			
			results = append(results, result)
		}
	}

	logrus.Infof("Found %d similar code snippets", len(results))
	return results, nil
}

// QueryBySymbol 按符号查询代码
func (s *QueryService) QueryBySymbol(ctx context.Context, repoKey, symbol string) ([]QueryResult, error) {
	logrus.Infof("Querying code by symbol %s in repository %s", symbol, repoKey)

	// 将QueryService的storage转换为ChromaStorage
	chromaStorage, ok := s.storage.(*ChromaStorage)
	if !ok {
		return nil, fmt.Errorf("storage is not ChromaStorage type")
	}

	// 获取集合ID
	collID := fmt.Sprintf("%s_collection", repoKey)

	// 构建查询条件，按符号过滤
	whereClause := map[string]interface{}{
		"repo_key": repoKey,
		"symbols":  symbol, // 注意：这取决于存储符号的方式，可能需要调整
	}

	// 设置默认的结果数量限制
	limit := 5

	// 使用Chroma客户端进行查询
	ids, _, _, err := chromaStorage.client.QueryDocuments(
		ctx,
		collID,
		[]string{symbol}, // 使用符号作为查询文本
		limit,
		whereClause,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to query documents: %w", err)
	}

	// 获取文档内容
	var results []QueryResult

	// 检查我们是否有查询结果
	if len(ids) > 0 && len(ids[0]) > 0 {
		// 获取文档内容和元数据
		documents, metadatas, err := chromaStorage.client.GetDocuments(
			ctx,
			collID,
			ids[0],
			true, // 包含元数据
		)

		if err != nil {
			return nil, fmt.Errorf("failed to get documents: %w", err)
		}

		// 将查询结果转换为QueryResult对象
		results = make([]QueryResult, 0, len(documents))
		for i, docID := range ids[0] {
			if i >= len(documents) || i >= len(metadatas) {
				continue
			}

			result := QueryResult{
				ID:         docID,
				Content:    documents[i],
				Metadata:   metadatas[i],
				Similarity: 1.0, // 符号查询的相似度始终为1.0，因为它是精确匹配
			}

			results = append(results, result)
		}
	}

	logrus.Infof("Found %d code snippets for symbol %s", len(results), symbol)
	return results, nil
}

// QueryByFile 按文件查询代码
func (s *QueryService) QueryByFile(ctx context.Context, repoKey, filename string) ([]QueryResult, error) {
	logrus.Infof("Querying code in file %s of repository %s", filename, repoKey)

	// 获取文件的所有代码片段ID
	snippetIDs, err := s.storage.ListSnippetsByFile(ctx, repoKey, filename)
	if err != nil {
		return nil, fmt.Errorf("failed to list snippets: %w", err)
	}

	var results []QueryResult

	// 获取每个代码片段的内容和元数据
	for _, id := range snippetIDs {
		content, metadata, err := s.storage.GetCodeSnippet(ctx, id)
		if err != nil {
			logrus.Warnf("Failed to get snippet %s: %v", id, err)
			continue
		}

		results = append(results, QueryResult{
			ID:         id,
			Content:    content,
			Metadata:   metadata,
			Similarity: 1.0, // 精确匹配
		})
	}

	logrus.Infof("Found %d code snippets in file %s", len(results), filename)
	return results, nil
}

// ExtractCodeContext 从查询结果中提取代码上下文
func (s *QueryService) ExtractCodeContext(results []QueryResult) *CodeContext {
	if len(results) == 0 {
		return nil
	}

	context := &CodeContext{
		Imports:      make([]string, 0),
		Definitions:  make(map[string]string),
		References:   make([]string, 0),
		Dependencies: make([]string, 0),
		SimilarCode:  make([]CodeSnippet, 0),
	}

	for _, result := range results {
		// 提取导入语句
		imports := extractImports(result.Content, getStringValue(result.Metadata, "language"))
		context.Imports = append(context.Imports, imports...)

		// 提取定义
		defs := extractDefinitions(result.Content, getStringValue(result.Metadata, "language"))
		for name, def := range defs {
			context.Definitions[name] = def
		}

		// 添加相似代码
		if result.Similarity < 1.0 {
			snippet := CodeSnippet{
				Filename:    getStringValue(result.Metadata, "filename"),
				Content:     result.Content,
				Similarity:  result.Similarity,
				LineStart:   getIntValue(result.Metadata, "line_start"),
				LineEnd:     getIntValue(result.Metadata, "line_end"),
			}
			context.SimilarCode = append(context.SimilarCode, snippet)
		}
	}

	return context
}

// 辅助函数：从元数据中获取字符串值
func getStringValue(metadata map[string]interface{}, key string) string {
	if val, ok := metadata[key]; ok {
		if strVal, ok := val.(string); ok {
			return strVal
		}
	}
	return ""
}

// 辅助函数：从元数据中获取整数值
func getIntValue(metadata map[string]interface{}, key string) int {
	if val, ok := metadata[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case string:
			if i, err := parseInt(v); err == nil {
				return i
			}
		}
	}
	return 0
}

// 辅助函数：解析整数
func parseInt(s string) (int, error) {
	var i int
	_, err := fmt.Sscanf(s, "%d", &i)
	return i, err
}

// 辅助函数：提取导入语句
func extractImports(content, language string) []string {
	var imports []string

	switch language {
	case "go":
		// 简单的Go导入提取
		lines := strings.Split(content, "\n")
		inImport := false

		for _, line := range lines {
			line = strings.TrimSpace(line)

			if strings.HasPrefix(line, "import (") {
				inImport = true
				continue
			}

			if inImport && line == ")" {
				inImport = false
				continue
			}

			if inImport && line != "" {
				imports = append(imports, "import "+line)
			} else if strings.HasPrefix(line, "import ") {
				imports = append(imports, line)
			}
		}

	case "javascript", "typescript":
		// 简单的JS/TS导入提取
		lines := strings.Split(content, "\n")

		for _, line := range lines {
			line = strings.TrimSpace(line)

			if strings.HasPrefix(line, "import ") || strings.HasPrefix(line, "require(") {
				imports = append(imports, line)
			}
		}

	case "python":
		// 简单的Python导入提取
		lines := strings.Split(content, "\n")

		for _, line := range lines {
			line = strings.TrimSpace(line)

			if strings.HasPrefix(line, "import ") || strings.HasPrefix(line, "from ") {
				imports = append(imports, line)
			}
		}
	}

	return imports
}

// 辅助函数：提取定义
func extractDefinitions(content, language string) map[string]string {
	defs := make(map[string]string)

	switch language {
	case "go":
		// 简单的Go定义提取
		lines := strings.Split(content, "\n")

		for _, line := range lines {
			line = strings.TrimSpace(line)

			if strings.HasPrefix(line, "func ") {
				// 提取函数名
				parts := strings.Split(line, "(")
				if len(parts) > 0 {
					funcName := strings.TrimPrefix(parts[0], "func ")
					funcName = strings.TrimSpace(funcName)
					
					// 处理方法
					if strings.Contains(funcName, ")") {
						parts = strings.Split(funcName, ")")
						if len(parts) > 1 {
							funcName = strings.TrimSpace(parts[1])
						}
					}
					
					if funcName != "" {
						defs[funcName] = line
					}
				}
			} else if strings.HasPrefix(line, "type ") {
				// 提取类型名
				parts := strings.Split(line, " ")
				if len(parts) > 2 {
					typeName := parts[1]
					defs[typeName] = line
				}
			}
		}

	case "javascript", "typescript":
		// 简单的JS/TS定义提取
		lines := strings.Split(content, "\n")

		for _, line := range lines {
			line = strings.TrimSpace(line)

			if strings.HasPrefix(line, "function ") {
				// 提取函数名
				parts := strings.Split(line, "(")
				if len(parts) > 0 {
					funcName := strings.TrimPrefix(parts[0], "function ")
					funcName = strings.TrimSpace(funcName)
					if funcName != "" {
						defs[funcName] = line
					}
				}
			} else if strings.HasPrefix(line, "class ") {
				// 提取类名
				parts := strings.Split(line, " ")
				if len(parts) > 1 {
					className := parts[1]
					if strings.Contains(className, "{") {
						className = strings.Split(className, "{")[0]
					}
					className = strings.TrimSpace(className)
					if className != "" {
						defs[className] = line
					}
				}
			}
		}

	case "python":
		// 简单的Python定义提取
		lines := strings.Split(content, "\n")

		for _, line := range lines {
			line = strings.TrimSpace(line)

			if strings.HasPrefix(line, "def ") {
				// 提取函数名
				parts := strings.Split(line, "(")
				if len(parts) > 0 {
					funcName := strings.TrimPrefix(parts[0], "def ")
					funcName = strings.TrimSpace(funcName)
					if funcName != "" {
						defs[funcName] = line
					}
				}
			} else if strings.HasPrefix(line, "class ") {
				// 提取类名
				parts := strings.Split(line, "(")
				if len(parts) > 0 {
					className := strings.TrimPrefix(parts[0], "class ")
					className = strings.TrimSpace(className)
					if strings.Contains(className, ":") {
						className = strings.Split(className, ":")[0]
					}
					className = strings.TrimSpace(className)
					if className != "" {
						defs[className] = line
					}
				}
			}
		}
	}

	return defs
}
