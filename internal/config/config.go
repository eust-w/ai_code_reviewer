package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

// Config holds all configuration for the application
type Config struct {
	// Platform selection
	Platform string

	// GitHub related
	GithubToken string

	// GitLab related
	GitlabToken   string
	GitlabBaseURL string

	// Gitea related
	GiteaToken   string
	GiteaBaseURL string

	// Common Git platform settings
	TargetLabel string
	
	// Code indexing related
	EnableIndexing bool

	// OpenAI related
	OpenAIAPIKey       string
	OpenAIAPIEndpoint  string
	Model              string
	Temperature        float32
	TopP               float32
	MaxTokens          int
	Language           string
	Prompt             string
	MaxPatchLength     int
	IgnorePatterns     []string
	IncludePatterns    []string
	IgnoreList         []string
	
	// Azure OpenAI related
	AzureAPIVersion    string
	AzureDeployment    string
	IsAzure            bool
	
	// Direct LLM Provider related
	DirectLLMEndpoint   string
	DirectLLMModelID    string
	DirectLLMAPIKey     string
	DirectLLMType       string
	IsDirectLLM         bool
	
	// LLM Proxy related
	LLMProxyEndpoint    string
	LLMProxyAPIKey      string
	
	// Claude model related
	ClaudeModelName     string
	ClaudeMaxTokens     int
	IsClaudeEnabled     bool
	
	// Deepseek model related
	DeepseekModelName   string
	IsDeepseekEnabled   bool
	
	// Code indexing related
	IndexerStorageType  string
	ChromaHost          string
	ChromaPort          int
	ChromaPath          string
	ChromaSSL           bool
	LocalStoragePath    string
	IndexerVectorType   string
}

// LoadConfig loads configuration from environment variables
func LoadConfig() *Config {
	// Load .env file if it exists
	_ = godotenv.Load()

	config := &Config{
		// Platform selection (default to GitHub if not specified)
		Platform:           getEnvWithDefault("PLATFORM", "github"),

		// GitHub configuration
		GithubToken:        os.Getenv("GITHUB_TOKEN"),

		// GitLab configuration
		GitlabToken:        os.Getenv("GITLAB_TOKEN"),
		GitlabBaseURL:      getEnvWithDefault("GITLAB_BASE_URL", "https://gitlab.com/api/v4"),

		// Gitea configuration
		GiteaToken:         os.Getenv("GITEA_TOKEN"),
		GiteaBaseURL:       os.Getenv("GITEA_BASE_URL"),

		// Common Git platform settings
		TargetLabel:        os.Getenv("TARGET_LABEL"),

		// OpenAI configuration
		OpenAIAPIKey:       os.Getenv("OPENAI_API_KEY"),
		OpenAIAPIEndpoint:  getEnvWithDefault("OPENAI_API_ENDPOINT", "https://api.openai.com/v1"),
		Model:              getEnvWithDefault("MODEL", "gpt-4o-mini"),
		Language:           os.Getenv("LANGUAGE"),
		Prompt:             getEnvWithDefault("PROMPT", "Please review the following code patch. Focus on potential bugs, risks, and improvement suggestions."),
		AzureAPIVersion:    os.Getenv("AZURE_API_VERSION"),
		AzureDeployment:    os.Getenv("AZURE_DEPLOYMENT"),
		IgnorePatterns:     splitAndTrim(os.Getenv("IGNORE_PATTERNS"), ","),
		IncludePatterns:    splitAndTrim(os.Getenv("INCLUDE_PATTERNS"), ","),
		IgnoreList:         splitAndTrim(os.Getenv("IGNORE"), "\n"),
	}

	// Parse numeric values
	config.Temperature = parseFloat32(getEnvWithDefault("temperature", "1"))
	config.TopP = parseFloat32(getEnvWithDefault("top_p", "1"))
	config.MaxTokens = parseInt(os.Getenv("max_tokens"), 0)
	config.MaxPatchLength = parseInt(os.Getenv("MAX_PATCH_LENGTH"), 0)

	// Check if Azure OpenAI is configured
	config.IsAzure = config.AzureAPIVersion != "" && config.AzureDeployment != ""
	
	// Load Direct LLM Provider configuration
	config.DirectLLMEndpoint = os.Getenv("DIRECT_LLM_ENDPOINT")
	config.DirectLLMModelID = os.Getenv("DIRECT_LLM_MODEL_ID")
	config.DirectLLMAPIKey = os.Getenv("DIRECT_LLM_API_KEY")
	config.DirectLLMType = os.Getenv("DIRECT_LLM_PROVIDER_TYPE")
	config.IsDirectLLM = config.DirectLLMEndpoint != "" && config.DirectLLMModelID != "" && config.DirectLLMAPIKey != ""
	
	// Load LLM Proxy configuration
	config.LLMProxyEndpoint = os.Getenv("LLM_PROXY_ENDPOINT")
	config.LLMProxyAPIKey = os.Getenv("LLM_PROXY_API_KEY")
	
	// Load Claude model configuration
	config.ClaudeModelName = os.Getenv("CLAUDE_MODEL_NAME")
	config.ClaudeMaxTokens = parseInt(os.Getenv("CLAUDE_MAX_TOKENS"), 4000)
	config.IsClaudeEnabled = config.LLMProxyEndpoint != "" && config.LLMProxyAPIKey != "" && config.ClaudeModelName != ""
	
	// Load Deepseek model configuration
	config.DeepseekModelName = os.Getenv("DEEPSEEK_MODEL_NAME")
	config.IsDeepseekEnabled = os.Getenv("DEEPSEEK_ENABLED") == "true"
	
	// Load code indexing configuration
	config.EnableIndexing = os.Getenv("ENABLE_INDEXING") == "true"
	config.IndexerStorageType = getEnvWithDefault("INDEXER_STORAGE_TYPE", "local")
	// 支持两种环境变量前缀：INDEXER_CHROMA_* 和 CHROMA_*
	config.ChromaHost = getEnvWithDefault("INDEXER_CHROMA_HOST", getEnvWithDefault("CHROMA_HOST", "localhost"))
	config.ChromaPort = getEnvIntWithDefault("INDEXER_CHROMA_PORT", getEnvIntWithDefault("CHROMA_PORT", 8000))
	config.ChromaPath = getEnvWithDefault("INDEXER_CHROMA_PATH", os.Getenv("CHROMA_PATH"))
	config.ChromaSSL = os.Getenv("INDEXER_CHROMA_SSL") == "true" || os.Getenv("CHROMA_SSL") == "true"
	config.LocalStoragePath = getEnvWithDefault("INDEXER_LOCAL_STORAGE_PATH", "./data/index")
	config.IndexerVectorType = getEnvWithDefault("INDEXER_VECTOR_TYPE", "simple")

	return config
}

// Helper functions
func getEnvWithDefault(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func parseFloat32(value string) float32 {
	if value == "" {
		return 0
	}
	
	f, err := strconv.ParseFloat(value, 32)
	if err != nil {
		logrus.Warnf("Failed to parse float value: %s, using default 0", value)
		return 0
	}
	return float32(f)
}

func parseInt(value string, defaultValue int) int {
	if value == "" {
		return defaultValue
	}
	
	i, err := strconv.Atoi(value)
	if err != nil {
		logrus.Warnf("Failed to parse int value: %s, using default %d", value, defaultValue)
		return defaultValue
	}
	return i
}

func getEnvIntWithDefault(key string, defaultValue int) int {
	value := os.Getenv(key)
	return parseInt(value, defaultValue)
}

func splitAndTrim(value, separator string) []string {
	if value == "" {
		return []string{}
	}
	
	parts := strings.Split(value, separator)
	result := make([]string, 0, len(parts))
	
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	
	return result
}
