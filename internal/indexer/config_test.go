package indexer

import (
	"testing"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid chroma config",
			config: &Config{
				StorageType:  "chroma",
				ChromaHost:   "localhost",
				ChromaPort:   8000,
				VectorType:   "simple",
				OpenAIAPIKey: "",
			},
			wantErr: false,
		},
		{
			name: "valid local config",
			config: &Config{
				StorageType:      "local",
				LocalStoragePath: "./data",
				VectorType:       "simple",
			},
			wantErr: false,
		},
		{
			name: "invalid storage type",
			config: &Config{
				StorageType: "invalid",
				VectorType:  "simple",
			},
			wantErr: true,
		},
		{
			name: "empty chroma host",
			config: &Config{
				StorageType: "chroma",
				ChromaHost:  "",
				ChromaPort:  8000,
				VectorType:  "simple",
			},
			wantErr: true,
		},
		{
			name: "invalid chroma port",
			config: &Config{
				StorageType: "chroma",
				ChromaHost:  "localhost",
				ChromaPort:  0,
				VectorType:  "simple",
			},
			wantErr: true,
		},
		{
			name: "empty local storage path",
			config: &Config{
				StorageType:      "local",
				LocalStoragePath: "",
				VectorType:       "simple",
			},
			wantErr: true,
		},
		{
			name: "invalid vector type",
			config: &Config{
				StorageType: "local",
				VectorType:  "invalid",
			},
			wantErr: true,
		},
		{
			name: "openai without api key",
			config: &Config{
				StorageType:  "local",
				VectorType:   "openai",
				OpenAIAPIKey: "",
			},
			wantErr: true,
		},
		{
			name: "local vector without model path",
			config: &Config{
				StorageType:    "local",
				VectorType:     "local",
				LocalModelPath: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultValues(t *testing.T) {
	config := &Config{
		StorageType:      "local",
		LocalStoragePath: "./data",
		VectorType:       "simple",
		MaxFileSizeBytes: 0,
		ChunkSize:        0,
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("Config.Validate() error = %v", err)
	}

	// 检查默认值是否已设置
	if config.MaxFileSizeBytes != 1024*1024 {
		t.Errorf("MaxFileSizeBytes default value not set, got %d", config.MaxFileSizeBytes)
	}

	if config.ChunkSize != 100 {
		t.Errorf("ChunkSize default value not set, got %d", config.ChunkSize)
	}
}
