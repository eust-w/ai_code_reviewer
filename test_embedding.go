package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/eust-w/ai_code_reviewer/internal/indexer"
)

func main() {
	// 设置日志级别
	logrus.SetLevel(logrus.InfoLevel)
	
	// 打印环境变量
	fmt.Println("环境变量:")
	fmt.Println("INDEXER_LLM_PROXY_ENDPOINT =", os.Getenv("INDEXER_LLM_PROXY_ENDPOINT"))
	fmt.Println("INDEXER_LLM_PROXY_API_KEY =", os.Getenv("INDEXER_LLM_PROXY_API_KEY"))
	fmt.Println("INDEXER_LLM_PROXY_MODEL =", os.Getenv("INDEXER_LLM_PROXY_MODEL"))
	fmt.Println("INDEXER_LLM_PROXY_PROVIDER =", os.Getenv("INDEXER_LLM_PROXY_PROVIDER"))
	
	// 创建向量服务
	config := indexer.VectorConfig{
		LLMProxyEndpoint:  os.Getenv("INDEXER_LLM_PROXY_ENDPOINT"),
		LLMProxyAPIKey:    os.Getenv("INDEXER_LLM_PROXY_API_KEY"),
		LLMProxyModel:     os.Getenv("INDEXER_LLM_PROXY_MODEL"),
		LLMProxyProvider:  os.Getenv("INDEXER_LLM_PROXY_PROVIDER"),
	}
	
	vectorService, err := indexer.NewVectorService(&config)
	if err != nil {
		log.Fatalf("创建向量服务失败: %v", err)
	}
	defer vectorService.Close()
	
	// 测试嵌入代码
	code := `
	func main() {
		fmt.Println("Hello, World!")
	}
	`
	
	fmt.Println("\n测试嵌入代码...")
	embedding, err := vectorService.EmbedCode(context.Background(), "go", code)
	if err != nil {
		log.Fatalf("嵌入代码失败: %v", err)
	}
	
	fmt.Printf("成功获取嵌入向量，维度: %d\n", len(embedding))
	
	// 测试嵌入查询
	query := "打印问候消息"
	
	fmt.Println("\n测试嵌入查询...")
	queryEmbedding, err := vectorService.EmbedQuery(context.Background(), query)
	if err != nil {
		log.Fatalf("嵌入查询失败: %v", err)
	}
	
	fmt.Printf("成功获取查询嵌入向量，维度: %d\n", len(queryEmbedding))
	
	fmt.Println("\n测试成功!")
}
