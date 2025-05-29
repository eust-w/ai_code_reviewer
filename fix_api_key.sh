#!/bin/bash

# 这个脚本用于修复vector.go文件中的API密钥部分

# 备份原始文件
cp internal/indexer/vector.go internal/indexer/vector.go.bak

# 使用sed命令修改API密钥部分
sed -i '' 's/req.Header.Set("Authorization", "Bearer "+s.apiKey)/\
	\/* 从环境变量中获取API密钥 *\/\
	apiKey := os.Getenv("INDEXER_LLM_PROXY_API_KEY")\
	if apiKey == "" {\
		apiKey = s.apiKey\
	}\
	req.Header.Set("Authorization", "Bearer "+apiKey)/g' internal/indexer/vector.go

echo "API密钥部分已修复。"
