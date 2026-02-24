package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// LoadConfig
// ---------------------------------------------------------------------------

func TestLoadConfig_ValidMinimal(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	os.WriteFile(p, []byte(`
kafka:
  brokers: ["localhost:9092"]
redis:
  url: "redis://localhost:6379"
`), 0644)

	cfg, err := LoadConfig(p)
	require.NoError(t, err)
	assert.Equal(t, "redis://localhost:6379", cfg.Redis.URL)
	assert.Equal(t, []string{"localhost:9092"}, cfg.Kafka.Brokers)
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/config.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	os.WriteFile(p, []byte(":::not[yaml"), 0644)

	_, err := LoadConfig(p)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse config file")
}

func TestLoadConfig_ValidationFails(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	os.WriteFile(p, []byte(`
redis:
  url: "redis://localhost:6379"
  max_memory: "notvalid_memory"
kafka:
  brokers: ["localhost:9092"]
`), 0644)

	_, err := LoadConfig(p)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config validation failed")
}

// ---------------------------------------------------------------------------
// setDefaults
// ---------------------------------------------------------------------------

func TestSetDefaults_AllZeroValues(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)

	// Ingestor
	assert.Equal(t, 100, cfg.Ingestor.RateLimit)
	assert.Equal(t, 200, cfg.Ingestor.BurstLimit)
	assert.Equal(t, 1*time.Second, cfg.Ingestor.ReconnectDelay)
	assert.Equal(t, 60*time.Second, cfg.Ingestor.MaxReconnectDelay)
	assert.Equal(t, 2112, cfg.Ingestor.MetricsPort)

	// Elasticsearch
	assert.Equal(t, "http://localhost:9200", cfg.Elasticsearch.URL)
	assert.Equal(t, 7, cfg.Elasticsearch.RetentionDays)
	assert.Equal(t, 10000, cfg.Elasticsearch.MaxDocsPerDay)

	// Redis
	assert.Equal(t, "redis://localhost:6379", cfg.Redis.URL)
	assert.Equal(t, "256mb", cfg.Redis.MaxMemory)
	assert.Equal(t, "allkeys-lru", cfg.Redis.EvictionPolicy)
	assert.Equal(t, 1000, cfg.Redis.HotPages.MaxTracked)
	assert.Equal(t, 5, cfg.Redis.HotPages.PromotionThreshold)
	assert.Equal(t, 15*time.Minute, cfg.Redis.HotPages.WindowDuration)

	// Kafka
	assert.Equal(t, []string{"localhost:9092"}, cfg.Kafka.Brokers)
	assert.Equal(t, "wikisurge", cfg.Kafka.ConsumerGroup)
	assert.Equal(t, 500, cfg.Kafka.MaxPollRecords)

	// API
	assert.Equal(t, 8080, cfg.API.Port)
	assert.Equal(t, 1000, cfg.API.RateLimit)

	// Auth
	assert.NotEmpty(t, cfg.Auth.JWTSecret)
	assert.Equal(t, 24*time.Hour, cfg.Auth.JWTExpiry)

	// Database
	assert.Equal(t, "data/wikisurge.db", cfg.Database.Path)

	// Email
	assert.Equal(t, "digest@wikisurge.net", cfg.Email.FromAddress)
	assert.Equal(t, "WikiSurge", cfg.Email.FromName)
	assert.Equal(t, 10, cfg.Email.MaxConcurrentSends)

	// LLM
	assert.Equal(t, "openai", cfg.LLM.Provider)
	assert.Equal(t, "gpt-4o-mini", cfg.LLM.Model)
	assert.Equal(t, 512, cfg.LLM.MaxTokens)

	// Logging
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)
}

func TestSetDefaults_DoesNotOverwriteExisting(t *testing.T) {
	cfg := &Config{}
	cfg.API.Port = 3000
	cfg.Redis.URL = "redis://custom:6380"
	cfg.Logging.Level = "debug"

	setDefaults(cfg)

	assert.Equal(t, 3000, cfg.API.Port)
	assert.Equal(t, "redis://custom:6380", cfg.Redis.URL)
	assert.Equal(t, "debug", cfg.Logging.Level)
}

// ---------------------------------------------------------------------------
// overrideWithEnv
// ---------------------------------------------------------------------------

func TestOverrideWithEnv_KafkaBrokers(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)

	t.Setenv("KAFKA_BROKERS", "broker1:9092,broker2:9092")
	overrideWithEnv(cfg)

	assert.Equal(t, []string{"broker1:9092", "broker2:9092"}, cfg.Kafka.Brokers)
}

func TestOverrideWithEnv_RedisURL(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)

	t.Setenv("REDIS_URL", "redis://prod:6379")
	overrideWithEnv(cfg)

	assert.Equal(t, "redis://prod:6379", cfg.Redis.URL)
}

func TestOverrideWithEnv_ESURL(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)

	t.Setenv("ES_URL", "http://es-cluster:9200")
	overrideWithEnv(cfg)

	assert.Equal(t, "http://es-cluster:9200", cfg.Elasticsearch.URL)
}

func TestOverrideWithEnv_LogLevel(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)

	t.Setenv("LOG_LEVEL", "debug")
	overrideWithEnv(cfg)

	assert.Equal(t, "debug", cfg.Logging.Level)
}

func TestOverrideWithEnv_LLM(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)

	t.Setenv("LLM_PROVIDER", "anthropic")
	t.Setenv("LLM_API_KEY", "sk-test-key")
	t.Setenv("LLM_MODEL", "claude-3-haiku")
	t.Setenv("LLM_BASE_URL", "http://proxy:8000")
	overrideWithEnv(cfg)

	assert.Equal(t, "anthropic", cfg.LLM.Provider)
	assert.Equal(t, "sk-test-key", cfg.LLM.APIKey)
	assert.Equal(t, "claude-3-haiku", cfg.LLM.Model)
	assert.Equal(t, "http://proxy:8000", cfg.LLM.BaseURL)
	assert.True(t, cfg.LLM.Enabled)
}

func TestOverrideWithEnv_LLMEnabledFlag(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)

	t.Setenv("LLM_ENABLED", "true")
	overrideWithEnv(cfg)
	assert.True(t, cfg.LLM.Enabled)
}

func TestOverrideWithEnv_Auth(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)

	t.Setenv("JWT_SECRET", "super-secret")
	overrideWithEnv(cfg)

	assert.Equal(t, "super-secret", cfg.Auth.JWTSecret)
}

func TestOverrideWithEnv_Database(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)

	t.Setenv("DB_PATH", "/data/prod.db")
	overrideWithEnv(cfg)

	assert.Equal(t, "/data/prod.db", cfg.Database.Path)
}

func TestOverrideWithEnv_Email(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)

	t.Setenv("EMAIL_API_KEY", "re_test_key")
	t.Setenv("EMAIL_FROM", "noreply@example.com")
	t.Setenv("DASHBOARD_URL", "https://wikisurge.net")
	overrideWithEnv(cfg)

	assert.True(t, cfg.Email.Enabled)
	assert.Equal(t, "resend", cfg.Email.Provider) // auto-selected
	assert.Equal(t, "re_test_key", cfg.Email.APIKey)
	assert.Equal(t, "noreply@example.com", cfg.Email.FromAddress)
	assert.Equal(t, "https://wikisurge.net", cfg.Email.DashboardURL)
}

func TestOverrideWithEnv_EmailSMTP(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)

	t.Setenv("EMAIL_PROVIDER", "smtp")
	t.Setenv("EMAIL_SMTP_HOST", "smtp.gmail.com")
	t.Setenv("EMAIL_SMTP_PORT", "587")
	t.Setenv("EMAIL_SMTP_USER", "user@gmail.com")
	t.Setenv("EMAIL_SMTP_PASS", "app-password")
	overrideWithEnv(cfg)

	assert.Equal(t, "smtp", cfg.Email.Provider)
	assert.Equal(t, "smtp.gmail.com", cfg.Email.SMTPHost)
	assert.Equal(t, 587, cfg.Email.SMTPPort)
	assert.Equal(t, "user@gmail.com", cfg.Email.SMTPUser)
	assert.Equal(t, "app-password", cfg.Email.SMTPPass)
}

func TestOverrideWithEnv_EmailSMTPPortInvalid(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)

	t.Setenv("EMAIL_SMTP_PORT", "notanumber")
	overrideWithEnv(cfg)

	assert.Equal(t, 0, cfg.Email.SMTPPort) // unchanged
}

// ---------------------------------------------------------------------------
// validateConfig
// ---------------------------------------------------------------------------

func TestValidateConfig_Valid(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)
	assert.NoError(t, validateConfig(cfg))
}

func TestValidateConfig_EmptyBrokers(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)
	cfg.Kafka.Brokers = nil
	assert.ErrorContains(t, validateConfig(cfg), "kafka brokers must not be empty")
}

func TestValidateConfig_EmptyRedisURL(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)
	cfg.Redis.URL = ""
	assert.ErrorContains(t, validateConfig(cfg), "redis URL must not be empty")
}

func TestValidateConfig_BadRetentionDays(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)
	cfg.Elasticsearch.RetentionDays = -1
	assert.ErrorContains(t, validateConfig(cfg), "elasticsearch retention days must be positive")
}

func TestValidateConfig_BadMemorySize(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)
	cfg.Redis.MaxMemory = "lotsa_ram"
	assert.ErrorContains(t, validateConfig(cfg), "redis max_memory must be valid size string")
}

func TestValidateConfig_HotPagesTooHigh(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)
	cfg.Redis.HotPages.MaxTracked = 200000
	assert.ErrorContains(t, validateConfig(cfg), "hot pages max_tracked")
}

func TestValidateConfig_HotPagesZero(t *testing.T) {
	cfg := &Config{}
	setDefaults(cfg)
	cfg.Redis.HotPages.MaxTracked = 0
	assert.ErrorContains(t, validateConfig(cfg), "hot pages max_tracked")
}

// ---------------------------------------------------------------------------
// isValidMemorySize
// ---------------------------------------------------------------------------

func TestIsValidMemorySize(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"256mb", true},
		{"1gb", true},
		{"512kb", true},
		{"1024", true},
		{"2.5gb", true},
		{"", false},
		{"notmemory", false},
		{"mb", false},
		{"256MB", true},  // case insensitive
		{"1GB", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.valid, isValidMemorySize(tt.input))
		})
	}
}

// ---------------------------------------------------------------------------
// Integration: LoadConfig with full file
// ---------------------------------------------------------------------------

func TestLoadConfig_FullConfig(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	os.WriteFile(p, []byte(`
features:
  elasticsearch_indexing: true
  trending: true
  edit_wars: true
  websockets: true
ingestor:
  exclude_bots: true
  allowed_languages: ["en", "es"]
  rate_limit: 50
kafka:
  brokers: ["kafka:9092"]
  consumer_group: "test-group"
redis:
  url: "redis://redis:6379"
  max_memory: "512mb"
  hot_pages:
    max_tracked: 500
elasticsearch:
  enabled: true
  url: "http://es:9200"
  retention_days: 14
api:
  port: 9090
  rate_limit: 500
logging:
  level: "debug"
  format: "text"
`), 0644)

	cfg, err := LoadConfig(p)
	require.NoError(t, err)
	assert.True(t, cfg.Features.ElasticsearchIndexing)
	assert.True(t, cfg.Features.Trending)
	assert.True(t, cfg.Ingestor.ExcludeBots)
	assert.Equal(t, []string{"en", "es"}, cfg.Ingestor.AllowedLanguages)
	assert.Equal(t, 50, cfg.Ingestor.RateLimit)
	assert.Equal(t, "test-group", cfg.Kafka.ConsumerGroup)
	assert.Equal(t, 9090, cfg.API.Port)
	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, 14, cfg.Elasticsearch.RetentionDays)
	assert.Equal(t, 500, cfg.Redis.HotPages.MaxTracked)
}
