package indexer

import (
	"fmt"
	"strings"
)

// EnrichPatchWithContext 使用代码上下文信息增强补丁
// 这将帮助代码审查工具更好地理解代码变更的上下文
func EnrichPatchWithContext(patch string, context *CodeContext) string {
	if context == nil {
		return patch
	}

	var enriched strings.Builder

	// 添加导入信息
	if len(context.Imports) > 0 {
		enriched.WriteString("/* Relevant imports:\n")
		for _, imp := range context.Imports {
			enriched.WriteString(imp)
			enriched.WriteString("\n")
		}
		enriched.WriteString("*/\n\n")
	}

	// 添加相关定义
	if len(context.Definitions) > 0 {
		enriched.WriteString("/* Relevant definitions:\n")
		for name, def := range context.Definitions {
			enriched.WriteString(fmt.Sprintf("// %s\n%s\n\n", name, def))
		}
		enriched.WriteString("*/\n\n")
	}

	// 添加引用信息
	if len(context.References) > 0 {
		enriched.WriteString("/* Relevant references:\n")
		for _, ref := range context.References {
			enriched.WriteString(ref)
			enriched.WriteString("\n")
		}
		enriched.WriteString("*/\n\n")
	}

	// 添加依赖关系
	if len(context.Dependencies) > 0 {
		enriched.WriteString("/* Dependencies:\n")
		for _, dep := range context.Dependencies {
			enriched.WriteString(dep)
			enriched.WriteString("\n")
		}
		enriched.WriteString("*/\n\n")
	}

	// 添加相似代码片段
	if len(context.SimilarCode) > 0 {
		enriched.WriteString("/* Similar code patterns:\n")
		for _, snippet := range context.SimilarCode {
			enriched.WriteString(fmt.Sprintf("From %s (lines %d-%d, similarity: %.2f):\n%s\n\n", 
				snippet.Filename, snippet.LineStart, snippet.LineEnd, snippet.Similarity, snippet.Content))
		}
		enriched.WriteString("*/\n\n")
	}

	// 添加原始补丁
	enriched.WriteString(patch)

	return enriched.String()
}
