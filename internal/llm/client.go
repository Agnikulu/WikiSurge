package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Provider identifies the LLM backend.
type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
	ProviderOllama    Provider = "ollama" // local/self-hosted
)

// Config holds provider-specific settings.
type Config struct {
	Provider    Provider `yaml:"provider"`
	APIKey      string   `yaml:"api_key"`       // OpenAI / Anthropic key
	Model       string   `yaml:"model"`          // e.g. "gpt-4o-mini", "claude-3-haiku-20240307", "llama3"
	BaseURL     string   `yaml:"base_url"`       // Override for Ollama or proxies (e.g. "http://localhost:11434")
	MaxTokens   int      `yaml:"max_tokens"`     // max response tokens
	Temperature float64  `yaml:"temperature"`    // 0.0-1.0
	Timeout     time.Duration `yaml:"timeout"`   // HTTP timeout
}

// defaultConfig returns sane defaults.
func defaultConfig() Config {
	return Config{
		Provider:    ProviderOpenAI,
		Model:       "gpt-4o-mini",
		MaxTokens:   512,
		Temperature: 0.3,
		Timeout:     30 * time.Second,
	}
}

// Client is a lightweight, provider-agnostic LLM HTTP client.
type Client struct {
	cfg    Config
	http   *http.Client
	logger zerolog.Logger
}

// NewClient creates a new LLM client from the given config.
func NewClient(cfg Config, logger zerolog.Logger) *Client {
	d := defaultConfig()
	if cfg.Model == "" {
		cfg.Model = d.Model
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = d.MaxTokens
	}
	if cfg.Temperature == 0 {
		cfg.Temperature = d.Temperature
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = d.Timeout
	}
	if cfg.Provider == "" {
		cfg.Provider = d.Provider
	}
	return &Client{
		cfg: cfg,
		http: &http.Client{
			Timeout: cfg.Timeout,
		},
		logger: logger.With().Str("component", "llm").Logger(),
	}
}

// Complete sends a prompt to the configured LLM and returns the response text.
func (c *Client) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	switch c.cfg.Provider {
	case ProviderOpenAI:
		return c.completeOpenAI(ctx, systemPrompt, userPrompt)
	case ProviderAnthropic:
		return c.completeAnthropic(ctx, systemPrompt, userPrompt)
	case ProviderOllama:
		return c.completeOllama(ctx, systemPrompt, userPrompt)
	default:
		return "", fmt.Errorf("unsupported LLM provider: %s", c.cfg.Provider)
	}
}

// Enabled returns true if the LLM client is configured with a usable provider.
func (c *Client) Enabled() bool {
	switch c.cfg.Provider {
	case ProviderOpenAI, ProviderAnthropic:
		return c.cfg.APIKey != ""
	case ProviderOllama:
		return c.cfg.BaseURL != ""
	default:
		return false
	}
}

// ─── OpenAI / OpenAI-compatible ─────────────────────────────────────────────

func (c *Client) completeOpenAI(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	baseURL := c.cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	url := strings.TrimRight(baseURL, "/") + "/v1/chat/completions"

	body := map[string]interface{}{
		"model": c.cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"max_tokens":  c.cfg.MaxTokens,
		"temperature": c.cfg.Temperature,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("unmarshal openai response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("openai returned no choices")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

// ─── Anthropic (Claude) ─────────────────────────────────────────────────────

func (c *Client) completeAnthropic(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	baseURL := c.cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	url := strings.TrimRight(baseURL, "/") + "/v1/messages"

	body := map[string]interface{}{
		"model":      c.cfg.Model,
		"max_tokens": c.cfg.MaxTokens,
		"system":     systemPrompt,
		"messages": []map[string]string{
			{"role": "user", "content": userPrompt},
		},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("unmarshal anthropic response: %w", err)
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("anthropic returned no content")
	}
	return strings.TrimSpace(result.Content[0].Text), nil
}

// ─── Ollama (local) ─────────────────────────────────────────────────────────

func (c *Client) completeOllama(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	baseURL := c.cfg.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	url := strings.TrimRight(baseURL, "/") + "/api/chat"

	body := map[string]interface{}{
		"model": c.cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"stream": false,
		"options": map[string]interface{}{
			"temperature": c.cfg.Temperature,
			"num_predict": c.cfg.MaxTokens,
		},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("unmarshal ollama response: %w", err)
	}
	return strings.TrimSpace(result.Message.Content), nil
}
