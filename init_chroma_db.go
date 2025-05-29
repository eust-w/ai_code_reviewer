package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

func main() {
	// 设置日志级别
	logrus.SetLevel(logrus.InfoLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// 从环境变量获取配置，如果没有则使用默认值
	chromaHost := getEnvWithDefault("INDEXER_CHROMA_HOST", "localhost")
	chromaPort := getEnvWithDefault("INDEXER_CHROMA_PORT", "8012")
	chromaSSL := getEnvWithDefault("INDEXER_CHROMA_SSL", "false") == "true"

	// 确定协议
	protocol := "http"
	if chromaSSL {
		protocol = "https"
	}

	// 构建基础URL
	baseURL := fmt.Sprintf("%s://%s:%s/api/v2", protocol, chromaHost, chromaPort)
	logrus.Infof("Connecting to Chroma at %s", baseURL)

	// 创建HTTP客户端
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	// 1. 检查是否可以连接到服务器
	heartbeatURL := fmt.Sprintf("%s/heartbeat", baseURL)
	logrus.Infof("Testing connection to Chroma: %s", heartbeatURL)
	resp, err := httpClient.Get(heartbeatURL)
	if err != nil {
		logrus.Fatalf("Failed to connect to Chroma server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logrus.Fatalf("Chroma server returned non-OK status: %d", resp.StatusCode)
	}
	logrus.Infof("Successfully connected to Chroma server")

	// 2. 检查默认租户是否存在
	tenantsURL := fmt.Sprintf("%s/tenants", baseURL)
	logrus.Infof("Checking if default tenant exists: %s", tenantsURL)
	resp, err = httpClient.Get(tenantsURL)
	if err != nil {
		logrus.Fatalf("Failed to get tenants: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		logrus.Fatalf("Failed to read response body: %v", err)
	}

	// 解析响应
	var tenants []map[string]interface{}
	if err := json.Unmarshal(body, &tenants); err != nil {
		logrus.Fatalf("Failed to parse tenants response: %v", err)
	}

	// 检查默认租户是否存在
	defaultTenantExists := false
	for _, tenant := range tenants {
		if name, ok := tenant["name"].(string); ok && name == "default" {
			defaultTenantExists = true
			break
		}
	}

	// 如果默认租户不存在，创建它
	if !defaultTenantExists {
		logrus.Infof("Default tenant does not exist, creating it")
		createDefaultTenant(baseURL, httpClient)
	} else {
		logrus.Infof("Default tenant already exists")
	}

	// 3. 检查默认数据库是否存在
	databasesURL := fmt.Sprintf("%s/tenants/default/databases", baseURL)
	logrus.Infof("Checking if default database exists: %s", databasesURL)
	resp, err = httpClient.Get(databasesURL)
	if err != nil {
		logrus.Fatalf("Failed to get databases: %v", err)
	}
	body, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		logrus.Fatalf("Failed to read response body: %v", err)
	}

	// 解析响应
	var databases []map[string]interface{}
	if err := json.Unmarshal(body, &databases); err != nil {
		logrus.Fatalf("Failed to parse databases response: %v", err)
	}

	// 检查默认数据库是否存在
	defaultDatabaseExists := false
	for _, db := range databases {
		if name, ok := db["name"].(string); ok && name == "default" {
			defaultDatabaseExists = true
			break
		}
	}

	// 如果默认数据库不存在，创建它
	if !defaultDatabaseExists {
		logrus.Infof("Default database does not exist, creating it")
		createDefaultDatabase(baseURL, httpClient)
	} else {
		logrus.Infof("Default database already exists")
	}

	logrus.Infof("Chroma database initialization completed successfully")
}

// createDefaultTenant 创建默认租户
func createDefaultTenant(baseURL string, httpClient *http.Client) {
	url := fmt.Sprintf("%s/tenants", baseURL)
	logrus.Infof("Creating default tenant at: %s", url)

	// 准备请求体
	reqBody := map[string]interface{}{
		"name": "default",
	}

	// 将请求体转换为JSON
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		logrus.Fatalf("Failed to marshal request body: %v", err)
	}

	// 创建请求
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		logrus.Fatalf("Failed to create request: %v", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := httpClient.Do(req)
	if err != nil {
		logrus.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Fatalf("Failed to read response body: %v", err)
	}

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		logrus.Fatalf("Tenant creation failed with status %d: %s", resp.StatusCode, string(body))
	}

	logrus.Infof("Successfully created default tenant")
}

// createDefaultDatabase 创建默认数据库
func createDefaultDatabase(baseURL string, httpClient *http.Client) {
	url := fmt.Sprintf("%s/tenants/default/databases", baseURL)
	logrus.Infof("Creating default database at: %s", url)

	// 准备请求体
	reqBody := map[string]interface{}{
		"name": "default",
	}

	// 将请求体转换为JSON
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		logrus.Fatalf("Failed to marshal request body: %v", err)
	}

	// 创建请求
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		logrus.Fatalf("Failed to create request: %v", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := httpClient.Do(req)
	if err != nil {
		logrus.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Fatalf("Failed to read response body: %v", err)
	}

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		logrus.Fatalf("Database creation failed with status %d: %s", resp.StatusCode, string(body))
	}

	logrus.Infof("Successfully created default database")
}

// getEnvWithDefault 获取环境变量，如果不存在则返回默认值
func getEnvWithDefault(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		// 尝试使用替代前缀
		altKey := "CHROMA_" + key[len("INDEXER_CHROMA_"):]
		value = os.Getenv(altKey)
		if value == "" {
			return defaultValue
		}
	}
	return value
}
