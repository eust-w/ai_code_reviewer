package indexer

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/eust-w/ai_code_reviewer/internal/git"
	"github.com/sirupsen/logrus"
)

// ChromaIndexer 使用Chroma实现的代码索引器
type ChromaIndexer struct {
	repoKey  string
	storage  Storage
	vector   VectorService
	indexMap sync.Map // 文件路径 -> 是否已索引
}

// NewChromaIndexer 创建新的Chroma索引器
func NewChromaIndexer(repoKey string, storage Storage, vector VectorService) *ChromaIndexer {
	return &ChromaIndexer{
		repoKey: repoKey,
		storage: storage,
		vector:  vector,
	}
}

// IndexRepository 索引整个代码库
func (idx *ChromaIndexer) IndexRepository(ctx context.Context, repoPath, ref string) error {
	start := time.Now()
	logrus.Infof("Starting indexing repository %s (ref: %s)", idx.repoKey, ref)

	// 解析仓库所有者和名称
	parts := strings.Split(idx.repoKey, "/")
	if len(parts) < 2 {
		return fmt.Errorf("invalid repository key format: %s, expected owner/repo", idx.repoKey)
	}
	owner := parts[0]
	repo := parts[1]

	// 检查是否需要从远程获取代码
	var localRepoPath string
	fileInfo, err := os.Stat(repoPath)

	// 如果路径不存在或不是目录，尝试从远程获取
	if os.IsNotExist(err) || (err == nil && !fileInfo.IsDir()) {
		logrus.Infof("Repository path %s does not exist or is not a directory, attempting to clone", repoPath)

		// 从配置中获取平台类型和凭证
		platform := os.Getenv("PLATFORM")
		if platform == "" {
			platform = "github" // 默认使用GitHub
		}

		// 凭证映射
		credentials := map[string]string{
			"github_token":   os.Getenv("GITHUB_TOKEN"),
			"gitlab_token":   os.Getenv("GITLAB_TOKEN"),
			"gitea_token":    os.Getenv("GITEA_TOKEN"),
			"gitea_base_url": os.Getenv("GITEA_BASE_URL"),
		}

		// 克隆或更新仓库
		localRepoPath, err = CloneOrUpdateRepo(platform, owner, repo, ref, credentials)
		if err != nil {
			return fmt.Errorf("failed to clone or update repository: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to check repository path: %w", err)
	} else {
		// 使用提供的路径
		localRepoPath = repoPath
		logrus.Infof("Using existing repository path: %s", localRepoPath)
	}

	// 遍历仓库中的所有文件
	filesIndexed := 0
	snippetsIndexed := 0

	err = filepath.Walk(localRepoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录
		if info.IsDir() {
			// 跳过常见的不需要索引的目录
			basename := filepath.Base(path)
			if basename == ".git" || basename == "node_modules" || basename == "vendor" || basename == ".vscode" {
				return filepath.SkipDir
			}
			return nil
		}

		// 获取相对路径
		relPath, err := filepath.Rel(localRepoPath, path)
		if err != nil {
			logrus.Warnf("Failed to get relative path for %s: %v", path, err)
			return nil
		}

		// 跳过二进制文件和其他不需要索引的文件
		if !shouldIndexFile(relPath) {
			return nil
		}

		// 读取文件内容
		content, err := ioutil.ReadFile(path)
		if err != nil {
			logrus.Warnf("Failed to read file %s: %v", relPath, err)
			return nil
		}

		// 记录文件信息
		logrus.Debugf("[DEBUG] 准备索引文件: %s (大小: %d 字节)", relPath, len(content))
	
		// 检查索引器字段
		logrus.Debugf("[DEBUG] 索引器状态检查: storage=%v, vector=%v", idx.storage != nil, idx.vector != nil)
	
		// 索引文件，传递commit hash
		snippets, err := idx.indexFile(ctx, relPath, string(content), ref)
		if err != nil {
			logrus.Warnf("[DEBUG] 索引文件失败 %s: %v", relPath, err)
			return nil
		}

		filesIndexed++
		snippetsIndexed += snippets

		// 定期记录进度
		if filesIndexed%100 == 0 {
			logrus.Infof("Indexed %d files so far...", filesIndexed)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk repository: %w", err)
	}

	duration := time.Since(start)
	LogIndexStats(idx.repoKey, filesIndexed, snippetsIndexed, duration.String())

	return nil
}

// UpdateIndex 更新索引（增量）
func (idx *ChromaIndexer) UpdateIndex(ctx context.Context, repoPath, fromCommit, toCommit string) error {
	logrus.Infof("Updating index for repository %s (from %s to %s)", idx.repoKey, fromCommit, toCommit)

	// 在实际实现中，这里应该使用git命令或库获取变更的文件
	// 这里简化为重新索引整个仓库
	return idx.IndexRepository(ctx, repoPath, toCommit)
}

// QueryContext 查询与变更文件相关的代码上下文
func (idx *ChromaIndexer) QueryContext(ctx context.Context, changedFiles []*git.CommitFile, repoInfo RepoInfo) (map[string]*CodeContext, error) {
	logrus.Infof("Querying context for %d changed files in repository %s", len(changedFiles), idx.repoKey)

	// 获取commit hash，用于查询特定版本的代码上下文
	commitHash := ""
	if repoInfo.HeadSHA != "" {
		commitHash = repoInfo.HeadSHA
		logrus.Infof("Using commit hash %s for context queries", commitHash)
	}

	result := make(map[string]*CodeContext)

	for _, file := range changedFiles {
		filename := file.Filename
		language := GetFileLanguage(filename)

		// 创建代码上下文
		context := &CodeContext{
			Imports:      make([]string, 0),
			Definitions:  make(map[string]string),
			References:   make([]string, 0),
			Dependencies: make([]string, 0),
			SimilarCode:  make([]CodeSnippet, 0),
		}

		// 查询相关导入，传递commit hash
		importMap, err := idx.queryImports(ctx, filename, commitHash)
		if err != nil {
			logrus.Warnf("Failed to query imports for %s: %v", filename, err)
		} else {
			// 将map[string][]string转换为[]string
			imports := make([]string, 0)
			for pkg, stmts := range importMap {
				for _, stmt := range stmts {
					imports = append(imports, fmt.Sprintf("%s: %s", pkg, stmt))
				}
			}
			context.Imports = imports
		}

		// 查询相关定义，传递commit hash
		definitions, err := idx.queryDefinitions(ctx, filename, commitHash)
		if err != nil {
			logrus.Warnf("Failed to query definitions for %s: %v", filename, err)
		} else {
			context.Definitions = definitions
		}

		// 查询相似代码，传递commit hash
		if file.Patch != "" {
			similarCode, err := idx.querySimilarCode(ctx, language, file.Patch, commitHash)
			if err != nil {
				logrus.Warnf("Failed to query similar code for %s: %v", filename, err)
			} else {
				context.SimilarCode = similarCode
			}
		}

		result[filename] = context
	}

	logrus.Infof("Context query completed")
	return result, nil
}

// Close 关闭索引器
func (idx *ChromaIndexer) Close() error {
	return idx.storage.Close()
}

// indexFile 索引单个文件
func (idx *ChromaIndexer) indexFile(ctx context.Context, filename, content string, commitHash string) (int, error) {
	// 跳过空文件
	if len(content) == 0 {
		return 0, nil
	}

	// 对于非常大的文件，可能需要分块处理
	if len(content) > 50000 {
		return idx.indexLargeFile(ctx, filename, GetFileLanguage(filename), content, commitHash)
	}

	language := GetFileLanguage(filename)

	// 将文件标记为已索引
	idx.indexMap.Store(filename, true)

	// 创建元数据
	metadata := map[string]interface{}{
		"repo_key":    idx.repoKey,
		"filename":    filename,
		"language":    language,
		"line_start":  1,
		"line_end":    strings.Count(content, "\n") + 1,
		"indexed_at":  time.Now().Unix(),
		"commit_hash": commitHash,
	}

	// 检查 storage 是否为 nil
	if idx.storage == nil {
		logrus.Errorf("[DEBUG] indexFile 错误: storage 未初始化")
		return 0, fmt.Errorf("storage is not initialized")
	}

	logrus.Debugf("[DEBUG] 准备调用 SaveCodeSnippet: repoKey=%s, filename=%s, metadata=%v", 
		idx.repoKey, filename, metadata)
	
	// 记录 storage 类型和状态
	logrus.Debugf("[DEBUG] Storage 类型: %T", idx.storage)
	
	_, err := idx.storage.SaveCodeSnippet(ctx, idx.repoKey, filename, content, metadata)
	if err != nil {
		return 0, fmt.Errorf("failed to save code snippet: %w", err)
	}

	return 1, nil
}

// indexLargeFile 索引大文件（分块处理）
func (idx *ChromaIndexer) indexLargeFile(ctx context.Context, filename, language, content string, commitHash string) (int, error) {
	logrus.Debugf("[DEBUG] 开始索引大文件: %s (大小: %d 字节, 语言: %s)", 
		filename, len(content), language)

	lines := strings.Split(content, "\n")
	logrus.Debugf("[DEBUG] 文件行数: %d, 将分块处理", len(lines))

	// 每块最多500行
	const chunkSize = 500
	snippetsIndexed := 0

	for i := 0; i < len(lines); i += chunkSize {
		end := i + chunkSize
		if end > len(lines) {
			end = len(lines)
		}

		chunkContent := strings.Join(lines[i:end], "\n")

		metadata := map[string]interface{}{
			"repo_key":    idx.repoKey,
			"filename":    filename,
			"language":    language,
			"line_start":  i + 1,
			"line_end":    end,
			"indexed_at":  time.Now().Unix(),
			"commit_hash": commitHash,
		}

		// 检查 storage 是否为 nil
		if idx.storage == nil {
			logrus.Errorf("[DEBUG] indexLargeFile 错误: storage 未初始化")
			return snippetsIndexed, fmt.Errorf("storage is not initialized")
		}

		logrus.Debugf("[DEBUG] 处理大文件分块 #%d: 行 %d-%d, 大小: %d 字节", 
			snippetsIndexed+1, i+1, end, len(chunkContent))

		_, err := idx.storage.SaveCodeSnippet(ctx, idx.repoKey, filename, chunkContent, metadata)
		if err != nil {
			return snippetsIndexed, fmt.Errorf("failed to save code chunk: %w", err)
		}

		snippetsIndexed++
	}

	return snippetsIndexed, nil
}

// queryImports 查询文件的导入语句
func (idx *ChromaIndexer) queryImports(ctx context.Context, filename, commitHash string) (map[string][]string, error) {
	logrus.Infof("[索引增强] 查询文件 %s 的导入语句", filename)

	// 获取集合ID
	collID, err := idx.getCollectionID(ctx)
	if err != nil {
		logrus.Warnf("[索引增强] 获取集合ID失败: %v - 返回空结果", err)
		return map[string][]string{}, nil
	}

	// 将Storage接口转换为ChromaStorage以访问其客户端
	chromaStorage, ok := idx.storage.(*ChromaStorage)
	if !ok {
		logrus.Warnf("[索引增强] 存储不是ChromaStorage类型 - 返回空结果")
		return map[string][]string{}, nil
	}

	// 构建查询条件
	whereClause := map[string]interface{}{
		"repo_key": idx.repoKey,
		"filename": filename,
	}

	// 如果提供了commit hash，则查询特定版本的代码
	if commitHash != "" {
		logrus.Infof("[索引增强] 使用提交哈希 %s 查询导入", commitHash)
		whereClause["commit_hash"] = commitHash
	}

	// 根据类型过滤条件构建查询
	// 首先获取所有文档
	documents, metadatas, err := chromaStorage.client.GetDocuments(
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

	logrus.Infof("[索引增强] 为文件 %s 找到 %d 个导入包", filename, len(result))
	return result, nil
}

// queryDefinitions 查询文件的定义
func (idx *ChromaIndexer) queryDefinitions(ctx context.Context, filename string, commitHash string) (map[string]string, error) {
	logrus.Infof("[索引增强] 查询文件 %s 的定义", filename)

	// 构建查询条件
	whereClause := map[string]interface{}{
		"repo_key": idx.repoKey,
		"filename": filename,
	}

	// 如果提供了commit hash，则查询特定版本的代码
	if commitHash != "" {
		logrus.Infof("[索引增强] 使用提交哈希 %s 查询定义", commitHash)
		whereClause["commit_hash"] = commitHash
	}

	// 尝试从存储中查询
	logrus.Infof("[索引增强] 尝试从存储中查询 %s 的定义", filename)

	// 获取集合ID
	collID, err := idx.getCollectionID(ctx)
	if err != nil {
		// 如果无法获取集合ID，返回空结果
		logrus.Warnf("[索引增强] 无法获取集合ID: %v - 返回空结果", err)
		return map[string]string{}, nil
	}

	// 将storage转换为ChromaStorage
	chromaStorage, ok := idx.storage.(*ChromaStorage)
	if !ok {
		// 如果不是ChromaStorage类型，返回空结果
		logrus.Warnf("[索引增强] 存储不是ChromaStorage类型 - 返回空结果")
		return map[string]string{}, nil
	}

	// 使用Chroma客户端进行查询
	// 构建查询文本，包含关键词“function”、“type”、“struct”等
	queryTexts := []string{"function", "type", "struct", "interface", "const", "var"}

	// 设置结果数量限制
	limit := 20

	// 查询文档
	ids, _, _, err := chromaStorage.client.QueryDocuments(
		ctx,
		collID,
		queryTexts,
		limit,
		whereClause,
	)

	if err != nil {
		logrus.Warnf("[索引增强] 查询文档失败: %v", err)
		return map[string]string{}, nil
	}

	// 初始化结果集
	definitions := make(map[string]string)

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

	logrus.Infof("[索引增强] 为文件 %s 找到 %d 个定义", filename, len(definitions))
	for name, def := range definitions {
		shortDef := def
		if len(def) > 100 {
			shortDef = def[:100] + "..."
		}
		logrus.Infof("[索引增强] 定义: %s = %s", name, shortDef)
	}

	return definitions, nil
}

// getCollectionID 获取当前仓库的集合ID
func (idx *ChromaIndexer) getCollectionID(ctx context.Context) (string, error) {
	// 检查 storage 是否为 nil
	if idx.storage == nil {
		logrus.Warn("[索引增强] 存储服务未初始化，无法获取集合ID")
		return "", fmt.Errorf("存储服务未初始化")
	}

	// 将storage转换为ChromaStorage
	chromaStorage, ok := idx.storage.(*ChromaStorage)
	if !ok {
		// 如果不是ChromaStorage类型，返回错误
		logrus.Warnf("[索引增强] 存储不是ChromaStorage类型，无法获取集合ID")
		return "", fmt.Errorf("存储不是ChromaStorage类型")
	}

	// 使用ChromaStorage的getOrCreateCollection方法获取集合ID
	collID, err := chromaStorage.getOrCreateCollection(ctx, idx.repoKey)
	if err != nil {
		// 如果获取集合ID失败，记录详细错误信息
		logrus.Warnf("[索引增强] 获取集合ID失败: %v", err)
		
		// 检查错误类型，如果是数据库连接错误，记录更详细的信息
		if strings.Contains(err.Error(), "No connected db") {
			logrus.Warnf("[索引增强] Chroma数据库连接错误，请检查Chroma服务器配置")
		} else if strings.Contains(err.Error(), "404") {
			logrus.Warnf("[索引增强] Chroma API路径不正确，请检查API版本配置")
		}
		
		return "", err
	}
	
	return collID, nil
}

// querySimilarCode 查询与补丁相似的代码
func (idx *ChromaIndexer) querySimilarCode(ctx context.Context, language, patch string, commitHash string) ([]CodeSnippet, error) {
	logrus.Infof("[索引增强] 查询与补丁相似的代码 (语言: %s)", language)

	// 如果没有向量服务，则无法查询相似代码
	if idx.vector == nil {
		logrus.Warn("[索引增强] 没有配置向量服务，无法查询相似代码")
		return nil, nil
	}

	// 记录补丁信息
	patchPreview := patch
	if len(patch) > 200 {
		patchPreview = patch[:200] + "..."
	}
	logrus.Infof("[索引增强] 补丁预览: %s", patchPreview)

	// 使用向量服务将补丁转换为向量
	logrus.Infof("[索引增强] 开始将补丁转换为向量")

	// 尝试生成向量，但如果失败，不返回错误，而是使用简单的文本匹配
	var useVectorSearch bool = true
	var patchEmbedding []float32

	patchEmbedding, err := idx.vector.EmbedCode(ctx, language, patch)
	if err != nil {
		logrus.Warnf("[索引增强] 将补丁转换为向量失败: %v - 将使用简单文本匹配代替", err)
		useVectorSearch = false
	} else {
		logrus.Infof("[索引增强] 成功生成补丁向量，维度: %d", len(patchEmbedding))
	}

	// 构建查询条件
	whereClause := map[string]interface{}{
		"repo_key": idx.repoKey,
	}

	// 添加语言过滤条件（如果有指定）
	if language != "" {
		whereClause["language"] = language
	}

	// 如果提供了commit hash，则查询特定版本的代码
	if commitHash != "" {
		logrus.Infof("[索引增强] 使用提交哈希 %s 查询相似代码", commitHash)
		whereClause["commit_hash"] = commitHash
	}

	// 使用Chroma存储查询相似代码
	logrus.Infof("[索引增强] 开始使用向量查询相似代码")

	// 检查存储是否为 nil
	if idx.storage == nil {
		logrus.Warn("[索引增强] 存储服务未初始化，无法查询相似代码")
		return []CodeSnippet{}, nil
	}

	// 检查存储类型，如果不是ChromaStorage，则使用FallbackStorage
	chromaStorage, ok := idx.storage.(*ChromaStorage)
	if !ok {
		// 创建FallbackStorage
		fallback := NewFallbackStorage(idx.storage)
		// 使用FallbackStorage查询相似代码
		result, err := fallback.QuerySimilarCodeFallback(ctx, idx.repoKey, language, patch, 5)
		if err != nil {
			logrus.Warnf("[索引增强] 使用FallbackStorage查询相似代码失败: %v - 返回空结果", err)
			return []CodeSnippet{}, nil
		}
		return result, nil
	}

	// 获取集合ID
	collID, err := idx.getCollectionID(ctx)
	if err != nil {
		logrus.Warnf("[索引增强] 获取集合ID失败: %v - 返回空结果", err)
		return []CodeSnippet{}, nil
	}

	// 使用ChromaClient查询相似文档
	// 设置查询参数
	nResults := 5 // 限制返回的相似代码数量

	logrus.Infof("[索引增强] 开始查询集合 %s 中的相似文档，条件: %v", collID, whereClause)

	// 根据是否使用向量搜索选择不同的查询方式
	var ids [][]string
	var distances [][]float64

	if useVectorSearch {
		// 使用向量搜索
		logrus.Infof("[索引增强] 使用向量搜索查询相似代码")
		
		// 获取补丁的向量嵌入
		embedding, embedErr := idx.vector.EmbedCode(ctx, "text", patch)
		if embedErr != nil {
			logrus.Errorf("[索引增强] 获取补丁向量嵌入失败: %v - 将尝试文本查询", embedErr)
			useVectorSearch = false
		} else {
			// 执行查询 - 使用真实的向量嵌入
			result, _, resultDistances, queryErr := chromaStorage.client.QueryDocumentsWithEmbedding(
				ctx,
				collID,
				embedding,
				nResults,
				whereClause,
			)
			if queryErr != nil {
				logrus.Errorf("[索引增强] 向量查询失败: %v - 将尝试文本查询", queryErr)
				useVectorSearch = false
			} else {
				// 类型转换
				ids = result

				// 将float32转换为float64
				distances = make([][]float64, len(resultDistances))
				for i, distRow := range resultDistances {
					distances[i] = make([]float64, len(distRow))
					for j, dist := range distRow {
						distances[i][j] = float64(dist)
					}
				}
			}
		}
	}

	// 如果向量搜索失败或不可用，使用文本搜索
	if !useVectorSearch {
		logrus.Infof("[索引增强] 使用文本搜索查询相关代码")

		// 从补丁中提取关键词作为查询条件
		keywords := extractKeywords(patch)
		logrus.Infof("[索引增强] 从补丁中提取的关键词: %v", keywords)

		// 使用关键词进行文本查询
		result, _, resultDistances, queryErr := chromaStorage.client.QueryDocuments(
			ctx,
			collID,
			keywords,
			nResults,
			whereClause,
		)

		if queryErr == nil {
			// 类型转换
			ids = result

			// 将float32转换为float64
			distances = make([][]float64, len(resultDistances))
			for i, distRow := range resultDistances {
				distances[i] = make([]float64, len(distRow))
				for j, dist := range distRow {
					distances[i][j] = float64(dist)
				}
			}
		}
		if queryErr != nil {
			logrus.Warnf("[索引增强] 文本查询也失败: %v - 返回空结果", queryErr)
			return []CodeSnippet{}, nil
		}
	}

	// 检查查询结果
	logrus.Infof("[索引增强] 查询结果: ids=%v, distances=%v", ids, distances)

	// 检查是否有结果
	if len(ids) == 0 || len(ids[0]) == 0 {
		logrus.Info("[索引增强] 没有找到相似代码")
		return []CodeSnippet{}, nil
	}

	logrus.Infof("[索引增强] 查询返回了 %d 个结果", len(ids[0]))

	// 获取文档内容
	documents, docMetadatas, err := chromaStorage.client.GetDocuments(ctx, collID, ids[0], true)
	if err != nil {
		logrus.Errorf("[索引增强] 获取文档内容失败: %v", err)
		return nil, fmt.Errorf("获取文档内容失败: %w", err)
	}

	// 构建相似代码片段列表
	similarCode := make([]CodeSnippet, 0, len(documents))
	
	// 创建一个集合来跟踪已经添加的文档内容
	// 使用文件名+行号范围+文档内容的哈希作为唯一标识
	addedDocs := make(map[string]bool)

	for i, doc := range documents {
		// 获取元数据
		var meta map[string]interface{}
		if i < len(docMetadatas) {
			meta = docMetadatas[i]
		} else {
			meta = make(map[string]interface{})
		}

		// 获取行号信息
		lineStart := 1
		lineEnd := 1
		if val, ok := meta["line_start"].(float64); ok {
			lineStart = int(val)
		}
		if val, ok := meta["line_end"].(float64); ok {
			lineEnd = int(val)
		}

		// 获取文件名
		filename := ""
		if val, ok := meta["filename"].(string); ok {
			filename = val
		}
		
		// 创建文档的唯一标识（文件名+行号范围）
		docKey := fmt.Sprintf("%s:%d-%d", filename, lineStart, lineEnd)
		
		// 检查是否已经添加过这个文档
		if addedDocs[docKey] {
			logrus.Debugf("[索引增强] 跳过重复的文档: %s", docKey)
			continue // 跳过重复的文档
		}
		
		// 标记该文档已经被添加
		addedDocs[docKey] = true
		
		// 计算相似度分数（将距离转换为相似度）
		// 注意：Chroma返回的是距离，距离越小表示越相似
		// 将距离转换为0-1范围的相似度分数
		var similarity float32 = 0.0
		if len(distances) > 0 && i < len(distances[0]) {
			// 距离转换为相似度（距离越小越相似）
			similarity = 1.0 - float32(distances[0][i])
			if similarity < 0 {
				similarity = 0
			}
			
			// 为了避免完全相同的相似度分数，添加小的随机浮动
			// 这样可以确保即使距离相同，最终的相似度分数也有微小的差异
			// 使用索引作为随机因子的来源，确保结果可重复
			randomFactor := float32(i) * 0.001 // 每个索引增加0.001的差异
			similarity = similarity - randomFactor
		}

		// 创建代码片段
		snippet := CodeSnippet{
			Filename:   filename,
			Content:    doc,
			Similarity: float64(similarity), // 转换为float64类型以匹配结构体定义
			LineStart:  lineStart,
			LineEnd:    lineEnd,
		}

		similarCode = append(similarCode, snippet)
	}

	// 按相似度降序排序
	sort.Slice(similarCode, func(i, j int) bool {
		return similarCode[i].Similarity > similarCode[j].Similarity
	})

	logrus.Infof("[索引增强] 找到 %d 个相似代码片段", len(similarCode))
	for i, snippet := range similarCode {
		logrus.Infof("[索引增强] 相似代码 #%d: 文件=%s, 行=%d-%d, 相似度=%.2f",
			i+1, snippet.Filename, snippet.LineStart, snippet.LineEnd, snippet.Similarity)

		contentPreview := snippet.Content
		if len(snippet.Content) > 100 {
			contentPreview = snippet.Content[:100] + "..."
		}
		logrus.Infof("[索引增强] 内容预览: %s", contentPreview)
	}

	return similarCode, nil
}

// shouldIndexFile 判断文件是否应该被索引
func shouldIndexFile(filename string) bool {
	// 跳过常见的不需要索引的文件
	skipExtensions := []string{
		".exe", ".bin", ".obj", ".o", ".a", ".so", ".dll", ".dylib",
		".jar", ".war", ".ear", ".class",
		".zip", ".tar", ".gz", ".bz2", ".7z", ".rar",
		".jpg", ".jpeg", ".png", ".gif", ".bmp", ".ico", ".svg",
		".mp3", ".mp4", ".avi", ".mov", ".wmv",
		".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".lock", ".sum",
	}

	for _, ext := range skipExtensions {
		if strings.HasSuffix(strings.ToLower(filename), ext) {
			return false
		}
	}

	// 跳过隐藏文件
	if strings.HasPrefix(filepath.Base(filename), ".") {
		return false
	}

	return true
}
