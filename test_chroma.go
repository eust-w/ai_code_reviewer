package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/d-robotics/ai_code_reviewer/internal/config"
	"github.com/d-robotics/ai_code_reviewer/internal/indexer"
)

func main() {
	// 设置日志级别
	logrus.SetLevel(logrus.InfoLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// 加载配置
	cfg := config.LoadConfig()
	logrus.Infof("Loaded config: %+v", cfg)

	// 创建存储配置
	storageConfig := &indexer.StorageConfig{
		IndexerStorageType: cfg.IndexerStorageType,
		ChromaHost:         cfg.ChromaHost,
		ChromaPort:         cfg.ChromaPort,
		ChromaPath:         cfg.ChromaPath,
		ChromaSSL:          cfg.ChromaSSL,
		LocalStoragePath:   cfg.LocalStoragePath,
		OpenAIAPIKey:       cfg.OpenAIAPIKey,
		OpenAIModel:        cfg.OpenAIModel,
		LLMProxyEndpoint:   cfg.LLMProxyEndpoint,
		LLMProxyAPIKey:     cfg.LLMProxyAPIKey,
		LLMProxyModel:      cfg.LLMProxyModel,
		LLMProxyProvider:   cfg.LLMProxyProvider,
	}

	// 打印存储配置
	logrus.Infof("Storage config: %+v", storageConfig)

	// 测试直接连接到 Chroma
	client, err := indexer.NewRealChromaClient(storageConfig)
	if err != nil {
		logrus.Errorf("Failed to create Chroma client: %v", err)
		os.Exit(1)
	}
	logrus.Infof("Successfully created Chroma client")

	// 测试创建集合
	ctx := context.Background()
	collectionName := "test_collection"
	collectionID, err := client.CreateCollection(ctx, collectionName, map[string]interface{}{
		"description": "Test collection",
	})
	if err != nil {
		logrus.Errorf("Failed to create collection: %v", err)
	} else {
		logrus.Infof("Successfully created collection with ID: %s", collectionID)
	}

	// 测试获取集合
	collectionID, err = client.GetCollection(ctx, collectionName)
	if err != nil {
		logrus.Errorf("Failed to get collection: %v", err)
	} else {
		logrus.Infof("Successfully retrieved collection with ID: %s", collectionID)
	}

	// 关闭客户端
	if err := client.Close(); err != nil {
		logrus.Errorf("Failed to close client: %v", err)
	}
}
