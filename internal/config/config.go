package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the main configuration structure
type Config struct {
	Features      Features      `yaml:"features"`
	Ingestor      Ingestor      `yaml:"ingestor"`
	Elasticsearch Elasticsearch `yaml:"elasticsearch"`
	Redis         Redis         `yaml:"redis"`
	Kafka         Kafka         `yaml:"kafka"`
	API           API           `yaml:"api"`
	LLM           LLMConfig     `yaml:"llm"`
	Auth          AuthConfig    `yaml:"auth"`
	Database      DatabaseConfig `yaml:"database"`
	Email         EmailConfig   `yaml:"email"`
	Logging       Logging       `yaml:"logging"`
}

// AuthConfig configures JWT authentication.
type AuthConfig struct {
	JWTSecret string        `yaml:"jwt_secret"`
	JWTExpiry time.Duration `yaml:"jwt_expiry"`
}

// DatabaseConfig configures the SQLite user database.
type DatabaseConfig struct {
	Path string `yaml:"path"` // Path to SQLite database file
}

// EmailConfig configures the digest email sender.
type EmailConfig struct {
	Enabled            bool   `yaml:"enabled"`
	Provider           string `yaml:"provider"`             // "resend", "smtp", "log"
	APIKey             string `yaml:"api_key"`              // Resend API key
	SMTPHost           string `yaml:"smtp_host"`            // SMTP server hostname
	SMTPPort           int    `yaml:"smtp_port"`            // SMTP server port (587 for TLS)
	SMTPUser           string `yaml:"smtp_user"`            // SMTP username
	SMTPPass           string `yaml:"smtp_pass"`            // SMTP password
	FromAddress        string `yaml:"from_address"`
	FromName           string `yaml:"from_name"`
	DashboardURL       string `yaml:"dashboard_url"`        // Link back to WikiSurge
	DailySendHour      int    `yaml:"daily_send_hour"`      // UTC hour (0-23)
	WeeklySendDay      int    `yaml:"weekly_send_day"`      // 0=Sunday, 1=Monday, ...
	WeeklySendHour     int    `yaml:"weekly_send_hour"`     // UTC hour
	MaxConcurrentSends int    `yaml:"max_concurrent_sends"`
}

// LLMConfig configures the LLM provider used for edit war analysis.
type LLMConfig struct {
	Enabled     bool          `yaml:"enabled"`
	Provider    string        `yaml:"provider"`     // "openai", "anthropic", "ollama"
	APIKey      string        `yaml:"api_key"`      // API key (OpenAI / Anthropic)
	Model       string        `yaml:"model"`        // e.g. "gpt-4o-mini", "claude-3-haiku-20240307"
	BaseURL     string        `yaml:"base_url"`     // Override for Ollama or proxies
	MaxTokens      int           `yaml:"max_tokens"`
	Temperature    float64       `yaml:"temperature"`
	Timeout        time.Duration `yaml:"timeout"`
	CacheTTL       time.Duration `yaml:"cache_ttl"`        // How long to cache LLM results
	ReanalyzeEvery int           `yaml:"reanalyze_every"` // Re-run analysis every N edits (0=disabled)
}

// Features contains feature flags for each functionality
type Features struct {
	ElasticsearchIndexing bool `yaml:"elasticsearch_indexing"`
	Trending             bool `yaml:"trending"`
	EditWars             bool `yaml:"edit_wars"`
	Websockets           bool `yaml:"websockets"`
}

// Ingestor configuration for Wikipedia SSE client
type Ingestor struct {
	ExcludeBots       bool     `yaml:"exclude_bots"`
	AllowedLanguages  []string `yaml:"allowed_languages"`
	AllowedNamespaces []int    `yaml:"allowed_namespaces"` // Wikipedia namespaces: 0=Main, 1=Talk, 2=User, etc. Empty = all
	RateLimit         int      `yaml:"rate_limit"`
	BurstLimit        int      `yaml:"burst_limit"`
	ReconnectDelay    time.Duration `yaml:"reconnect_delay"`
	MaxReconnectDelay time.Duration `yaml:"max_reconnect_delay"`
	MetricsPort       int      `yaml:"metrics_port"`
}

// Elasticsearch configuration
type Elasticsearch struct {
	Enabled           bool              `yaml:"enabled"`
	URL               string            `yaml:"url"`
	RetentionDays     int               `yaml:"retention_days"`
	MaxDocsPerDay     int               `yaml:"max_docs_per_day"`
	SelectiveCriteria SelectiveCriteria `yaml:"selective_criteria"`
}

// SelectiveCriteria defines when to selectively index documents
type SelectiveCriteria struct {
	TrendingTopN     int     `yaml:"trending_top_n"`
	SpikeRatioMin    float64 `yaml:"spike_ratio_min"`
	EditWarEnabled   bool    `yaml:"edit_war_enabled"`
	SampleRate       float64 `yaml:"sample_rate"` // 0.0-1.0, percentage of all edits to index regardless of significance (0 = disabled)
}

// Redis configuration
type Redis struct {
	URL           string        `yaml:"url"`
	MaxMemory     string        `yaml:"max_memory"`
	EvictionPolicy string       `yaml:"eviction_policy"`
	HotPages      HotPages      `yaml:"hot_pages"`
	Trending      TrendingConfig `yaml:"trending"`
}

// HotPages configuration for tracking hot pages
type HotPages struct {
	MaxTracked          int           `yaml:"max_tracked"`
	PromotionThreshold  int           `yaml:"promotion_threshold"`
	WindowDuration      time.Duration `yaml:"window_duration"`
	MaxMembersPerPage   int           `yaml:"max_members_per_page"`
	HotThreshold        int           `yaml:"hot_threshold"`
	CleanupInterval     time.Duration `yaml:"cleanup_interval"`
}

// TrendingConfig for trending page functionality
type TrendingConfig struct {
	Enabled          bool          `yaml:"enabled"`
	MaxPages         int           `yaml:"max_pages"`
	HalfLifeMinutes  float64       `yaml:"half_life_minutes"`
	PruneInterval    time.Duration `yaml:"prune_interval"`
}

// Kafka configuration
type Kafka struct {
	Brokers        []string      `yaml:"brokers"`
	ConsumerGroup  string        `yaml:"consumer_group"`
	MaxPollRecords int           `yaml:"max_poll_records"`
	SessionTimeout time.Duration `yaml:"session_timeout"`
}

// API configuration
type API struct {
	Port                    int             `yaml:"port"`
	RateLimit               int             `yaml:"rate_limit"`
	MaxWebsocketConnections int             `yaml:"max_websocket_connections"`
	RateLimiting            APIRateLimiting `yaml:"rate_limiting"`
}

// APIRateLimiting configures the Redis-backed sliding-window rate limiter.
type APIRateLimiting struct {
	Enabled           bool     `yaml:"enabled"`
	RequestsPerMinute int      `yaml:"requests_per_minute"`
	BurstSize         int      `yaml:"burst_size"`
	KeyType           string   `yaml:"key_type"`
	Whitelist         []string `yaml:"whitelist"`
}

// Logging configuration
type Logging struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// LoadConfig loads configuration from file and environment variables
func LoadConfig(configPath string) (*Config, error) {
	// Read the config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set default values
	setDefaults(&config)

	// Override with environment variables
	overrideWithEnv(&config)

	// Validate configuration
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

// setDefaults sets default values for optional fields
func setDefaults(config *Config) {
	// Ingestor defaults
	if config.Ingestor.RateLimit == 0 {
		config.Ingestor.RateLimit = 100
	}
	if config.Ingestor.BurstLimit == 0 {
		config.Ingestor.BurstLimit = 200
	}
	if config.Ingestor.ReconnectDelay == 0 {
		config.Ingestor.ReconnectDelay = 1 * time.Second
	}
	if config.Ingestor.MaxReconnectDelay == 0 {
		config.Ingestor.MaxReconnectDelay = 60 * time.Second
	}
	if config.Ingestor.MetricsPort == 0 {
		config.Ingestor.MetricsPort = 2112
	}

	// Elasticsearch defaults
	if config.Elasticsearch.URL == "" {
		config.Elasticsearch.URL = "http://localhost:9200"
	}
	if config.Elasticsearch.RetentionDays == 0 {
		config.Elasticsearch.RetentionDays = 7
	}
	if config.Elasticsearch.MaxDocsPerDay == 0 {
		config.Elasticsearch.MaxDocsPerDay = 10000
	}
	if config.Elasticsearch.SelectiveCriteria.TrendingTopN == 0 {
		config.Elasticsearch.SelectiveCriteria.TrendingTopN = 100
	}
	if config.Elasticsearch.SelectiveCriteria.SpikeRatioMin == 0 {
		config.Elasticsearch.SelectiveCriteria.SpikeRatioMin = 2.0
	}

	// Redis defaults
	if config.Redis.URL == "" {
		config.Redis.URL = "redis://localhost:6379"
	}
	if config.Redis.MaxMemory == "" {
		config.Redis.MaxMemory = "256mb"
	}
	if config.Redis.EvictionPolicy == "" {
		config.Redis.EvictionPolicy = "allkeys-lru"
	}
	if config.Redis.HotPages.MaxTracked == 0 {
		config.Redis.HotPages.MaxTracked = 1000
	}
	if config.Redis.HotPages.PromotionThreshold == 0 {
		config.Redis.HotPages.PromotionThreshold = 5
	}
	if config.Redis.HotPages.WindowDuration == 0 {
		config.Redis.HotPages.WindowDuration = 15 * time.Minute
	}
	if config.Redis.HotPages.MaxMembersPerPage == 0 {
		config.Redis.HotPages.MaxMembersPerPage = 100
	}
	if config.Redis.Trending.MaxPages == 0 {
		config.Redis.Trending.MaxPages = 1000
	}
	if config.Redis.Trending.HalfLifeMinutes == 0 {
		config.Redis.Trending.HalfLifeMinutes = 30.0
	}
	if config.Redis.Trending.PruneInterval == 0 {
		config.Redis.Trending.PruneInterval = 5 * time.Minute
	}

	// Kafka defaults
	if len(config.Kafka.Brokers) == 0 {
		config.Kafka.Brokers = []string{"localhost:9092"}
	}
	if config.Kafka.ConsumerGroup == "" {
		config.Kafka.ConsumerGroup = "wikisurge"
	}
	if config.Kafka.MaxPollRecords == 0 {
		config.Kafka.MaxPollRecords = 500
	}
	if config.Kafka.SessionTimeout == 0 {
		config.Kafka.SessionTimeout = 30 * time.Second
	}

	// API defaults
	if config.API.Port == 0 {
		config.API.Port = 8080
	}
	if config.API.RateLimit == 0 {
		config.API.RateLimit = 1000
	}
	if config.API.MaxWebsocketConnections == 0 {
		config.API.MaxWebsocketConnections = 1000
	}

	// Rate limiting defaults
	if config.API.RateLimiting.RequestsPerMinute == 0 {
		config.API.RateLimiting.RequestsPerMinute = 1000
	}
	if config.API.RateLimiting.BurstSize == 0 {
		config.API.RateLimiting.BurstSize = 100
	}
	if config.API.RateLimiting.KeyType == "" {
		config.API.RateLimiting.KeyType = "ip"
	}

	// Auth defaults
	if config.Auth.JWTSecret == "" {
		config.Auth.JWTSecret = "wikisurge-dev-secret-change-in-production"
	}
	if config.Auth.JWTExpiry == 0 {
		config.Auth.JWTExpiry = 24 * time.Hour
	}

	// Database defaults
	if config.Database.Path == "" {
		config.Database.Path = "data/wikisurge.db"
	}

	// Email defaults
	if config.Email.FromAddress == "" {
		config.Email.FromAddress = "digest@wikisurge.app"
	}
	if config.Email.FromName == "" {
		config.Email.FromName = "WikiSurge"
	}
	if config.Email.DashboardURL == "" {
		config.Email.DashboardURL = "http://localhost:5173"
	}
	if config.Email.DailySendHour == 0 {
		config.Email.DailySendHour = 8
	}
	if config.Email.WeeklySendDay == 0 {
		config.Email.WeeklySendDay = 1 // Monday
	}
	if config.Email.WeeklySendHour == 0 {
		config.Email.WeeklySendHour = 8
	}
	if config.Email.MaxConcurrentSends == 0 {
		config.Email.MaxConcurrentSends = 10
	}

	// LLM defaults
	if config.LLM.Provider == "" {
		config.LLM.Provider = "openai"
	}
	if config.LLM.Model == "" {
		config.LLM.Model = "gpt-4o-mini"
	}
	if config.LLM.MaxTokens == 0 {
		config.LLM.MaxTokens = 512
	}
	if config.LLM.Temperature == 0 {
		config.LLM.Temperature = 0.3
	}
	if config.LLM.Timeout == 0 {
		config.LLM.Timeout = 30 * time.Second
	}
	if config.LLM.CacheTTL == 0 {
		config.LLM.CacheTTL = 5 * time.Minute
	}

	// Logging defaults
	if config.Logging.Level == "" {
		config.Logging.Level = "info"
	}
	if config.Logging.Format == "" {
		config.Logging.Format = "json"
	}
}

// overrideWithEnv overrides configuration with environment variables
func overrideWithEnv(config *Config) {
	if brokers := os.Getenv("KAFKA_BROKERS"); brokers != "" {
		config.Kafka.Brokers = strings.Split(brokers, ",")
	}
	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		config.Redis.URL = redisURL
	}
	if esURL := os.Getenv("ES_URL"); esURL != "" {
		config.Elasticsearch.URL = esURL
	}
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		config.Logging.Level = logLevel
	}
	// LLM overrides
	if llmProvider := os.Getenv("LLM_PROVIDER"); llmProvider != "" {
		config.LLM.Provider = llmProvider
	}
	if llmKey := os.Getenv("LLM_API_KEY"); llmKey != "" {
		config.LLM.APIKey = llmKey
		config.LLM.Enabled = true
	}
	if llmModel := os.Getenv("LLM_MODEL"); llmModel != "" {
		config.LLM.Model = llmModel
	}
	if llmURL := os.Getenv("LLM_BASE_URL"); llmURL != "" {
		config.LLM.BaseURL = llmURL
	}
	if llmEnabled := os.Getenv("LLM_ENABLED"); llmEnabled == "true" || llmEnabled == "1" {
		config.LLM.Enabled = true
	}

	// Auth overrides
	if jwtSecret := os.Getenv("JWT_SECRET"); jwtSecret != "" {
		config.Auth.JWTSecret = jwtSecret
	}

	// Database overrides
	if dbPath := os.Getenv("DB_PATH"); dbPath != "" {
		config.Database.Path = dbPath
	}

	// Email overrides
	if emailKey := os.Getenv("EMAIL_API_KEY"); emailKey != "" {
		config.Email.APIKey = emailKey
		config.Email.Enabled = true
		if config.Email.Provider == "" || config.Email.Provider == "log" {
			config.Email.Provider = "resend" // auto-select resend when API key is set
		}
	}
	if emailProvider := os.Getenv("EMAIL_PROVIDER"); emailProvider != "" {
		config.Email.Provider = emailProvider
	}
	if smtpHost := os.Getenv("EMAIL_SMTP_HOST"); smtpHost != "" {
		config.Email.SMTPHost = smtpHost
	}
	if smtpPort := os.Getenv("EMAIL_SMTP_PORT"); smtpPort != "" {
		if p, err := strconv.Atoi(smtpPort); err == nil {
			config.Email.SMTPPort = p
		}
	}
	if smtpUser := os.Getenv("EMAIL_SMTP_USER"); smtpUser != "" {
		config.Email.SMTPUser = smtpUser
	}
	if smtpPass := os.Getenv("EMAIL_SMTP_PASS"); smtpPass != "" {
		config.Email.SMTPPass = smtpPass
	}
	if emailFrom := os.Getenv("EMAIL_FROM"); emailFrom != "" {
		config.Email.FromAddress = emailFrom
	}
	if dashURL := os.Getenv("DASHBOARD_URL"); dashURL != "" {
		config.Email.DashboardURL = dashURL
	}
}

// validateConfig validates the configuration
func validateConfig(config *Config) error {
	// Kafka validation
	if len(config.Kafka.Brokers) == 0 {
		return fmt.Errorf("kafka brokers must not be empty")
	}

	// Redis URL validation
	if config.Redis.URL == "" {
		return fmt.Errorf("redis URL must not be empty")
	}

	// Retention days validation
	if config.Elasticsearch.RetentionDays <= 0 {
		return fmt.Errorf("elasticsearch retention days must be positive")
	}

	// Max memory validation (basic check for format)
	if !isValidMemorySize(config.Redis.MaxMemory) {
		return fmt.Errorf("redis max_memory must be valid size string (e.g., '256mb', '1gb')")
	}

	// Hot pages validation
	if config.Redis.HotPages.MaxTracked <= 0 || config.Redis.HotPages.MaxTracked >= 100000 {
		return fmt.Errorf("hot pages max_tracked must be > 0 and < 100000")
	}

	return nil
}

// isValidMemorySize checks if memory size string is valid
func isValidMemorySize(size string) bool {
	if size == "" {
		return false
	}
	
	// Simple validation for memory size format
	size = strings.ToLower(size)
	if strings.HasSuffix(size, "mb") || strings.HasSuffix(size, "gb") || strings.HasSuffix(size, "kb") {
		numStr := size[:len(size)-2]
		_, err := strconv.ParseFloat(numStr, 64)
		return err == nil
	}
	
	// Also allow pure numbers (bytes)
	_, err := strconv.ParseInt(size, 10, 64)
	return err == nil
}