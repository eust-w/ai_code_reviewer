package indexer

import (
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

// extractKeywords 从代码片段中提取关键词
// 这个函数用于在无法使用向量搜索时，提取代码中的关键词进行文本搜索
func extractKeywords(code string) []string {
	logrus.Infof("从代码片段中提取关键词")
	
	// 移除注释
	commentRegex := regexp.MustCompile(`(?m)//.*$|/\*[\s\S]*?\*/`)
	codeWithoutComments := commentRegex.ReplaceAllString(code, "")
	
	// 移除字符串字面量
	stringRegex := regexp.MustCompile(`"[^"]*"`)
	codeWithoutStrings := stringRegex.ReplaceAllString(codeWithoutComments, "")
	
	// 分割代码为单词
	wordRegex := regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9_]*`)
	words := wordRegex.FindAllString(codeWithoutStrings, -1)
	
	// 过滤常见的关键字和短单词
	keywords := make([]string, 0)
	commonKeywords := map[string]bool{
		"if": true, "else": true, "for": true, "while": true, "return": true,
		"func": true, "var": true, "const": true, "type": true, "struct": true,
		"interface": true, "package": true, "import": true, "map": true, "chan": true,
		"go": true, "select": true, "case": true, "default": true, "switch": true,
		"break": true, "continue": true, "goto": true, "defer": true, "range": true,
		"true": true, "false": true, "nil": true, "int": true, "string": true,
		"bool": true, "float": true, "byte": true, "error": true,
	}
	
	// 使用map去重
	uniqueWords := make(map[string]bool)
	
	for _, word := range words {
		// 过滤短单词和常见关键字
		if len(word) <= 2 || commonKeywords[word] {
			continue
		}
		
		// 转换为小写并添加到唯一单词集合
		lowerWord := strings.ToLower(word)
		uniqueWords[lowerWord] = true
	}
	
	// 将唯一单词转换为切片
	for word := range uniqueWords {
		keywords = append(keywords, word)
	}
	
	// 限制关键词数量，避免查询过大
	maxKeywords := 10
	if len(keywords) > maxKeywords {
		keywords = keywords[:maxKeywords]
	}
	
	logrus.Infof("提取的关键词: %v", keywords)
	return keywords
}
