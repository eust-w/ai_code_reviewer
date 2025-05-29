package indexer

import (
	"fmt"
	"strings"
	
	"github.com/sirupsen/logrus"
)

// EnrichPatchWithContext 使用代码上下文信息增强补丁
// 这将帮助代码审查工具更好地理解代码变更的上下文
func EnrichPatchWithContext(patch string, context *CodeContext) string {
	if context == nil {
		logrus.Info("No code context available for enrichment")
		return patch
	}

	var enriched strings.Builder
	logrus.Info("开始增强补丁，添加代码上下文信息")

	// 添加导入信息
	if len(context.Imports) > 0 {
		logrus.Infof("添加 %d 个相关导入", len(context.Imports))
		for _, imp := range context.Imports {
			logrus.Debugf("导入: %s", imp)
		}
		
		enriched.WriteString("/* Relevant imports:\n")
		for _, imp := range context.Imports {
			enriched.WriteString(imp)
			enriched.WriteString("\n")
		}
		enriched.WriteString("*/\n\n")
	} else {
		logrus.Info("没有找到相关导入")
	}

	// 添加相关定义
	if len(context.Definitions) > 0 {
		logrus.Infof("添加 %d 个相关定义", len(context.Definitions))
		for name, def := range context.Definitions {
			shortDef := def
			if len(def) > 100 {
				shortDef = def[:100] + "..."
			}
			logrus.Debugf("定义: %s = %s", name, shortDef)
		}
		
		enriched.WriteString("/* Relevant definitions:\n")
		for name, def := range context.Definitions {
			enriched.WriteString(fmt.Sprintf("// %s\n%s\n\n", name, def))
		}
		enriched.WriteString("*/\n\n")
	} else {
		logrus.Info("没有找到相关定义")
	}

	// 添加引用信息
	if len(context.References) > 0 {
		logrus.Infof("添加 %d 个相关引用", len(context.References))
		for _, ref := range context.References {
			logrus.Debugf("引用: %s", ref)
		}
		
		enriched.WriteString("/* Relevant references:\n")
		for _, ref := range context.References {
			enriched.WriteString(ref)
			enriched.WriteString("\n")
		}
		enriched.WriteString("*/\n\n")
	} else {
		logrus.Info("没有找到相关引用")
	}

	// 添加依赖关系
	if len(context.Dependencies) > 0 {
		logrus.Infof("添加 %d 个依赖关系", len(context.Dependencies))
		for _, dep := range context.Dependencies {
			logrus.Debugf("依赖: %s", dep)
		}
		
		enriched.WriteString("/* Dependencies:\n")
		for _, dep := range context.Dependencies {
			enriched.WriteString(dep)
			enriched.WriteString("\n")
		}
		enriched.WriteString("*/\n\n")
	} else {
		logrus.Info("没有找到相关依赖")
	}

	// 添加相似代码片段
	if len(context.SimilarCode) > 0 {
		logrus.Infof("添加 %d 个相似代码片段", len(context.SimilarCode))
		for i, snippet := range context.SimilarCode {
			shortContent := snippet.Content
			if len(snippet.Content) > 100 {
				shortContent = snippet.Content[:100] + "..."
			}
			logrus.Debugf("相似代码 #%d: 文件=%s, 行=%d-%d, 相似度=%.2f", 
				i+1, snippet.Filename, snippet.LineStart, snippet.LineEnd, snippet.Similarity)
			logrus.Debugf("内容摘要: %s", shortContent)
		}
		
		enriched.WriteString("/* Similar code patterns:\n")
		for _, snippet := range context.SimilarCode {
			enriched.WriteString(fmt.Sprintf("From %s (lines %d-%d, similarity: %.2f):\n%s\n\n", 
				snippet.Filename, snippet.LineStart, snippet.LineEnd, snippet.Similarity, snippet.Content))
		}
		enriched.WriteString("*/\n\n")
	} else {
		logrus.Info("没有找到相似代码片段")
	}

	// 添加原始补丁
	enriched.WriteString(patch)
	
	logrus.Info("补丁增强完成，原始补丁大小: " + fmt.Sprintf("%d", len(patch)) + 
		" 字节，增强后大小: " + fmt.Sprintf("%d", enriched.Len()) + " 字节")

	return enriched.String()
}
