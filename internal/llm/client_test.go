package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() zerolog.Logger {
	return zerolog.Nop()
}

func TestNewClient_Defaults(t *testing.T) {
	c := NewClient(Config{}, testLogger())
	assert.Equal(t, ProviderOpenAI, c.cfg.Provider)
	assert.Equal(t, "gpt-4o-mini", c.cfg.Model)
	assert.Equal(t, 512, c.cfg.MaxTokens)
	assert.Equal(t, 0.3, c.cfg.Temperature)
	assert.Equal(t, 30*time.Second, c.cfg.Timeout)
}

func TestNewClient_CustomOverrides(t *testing.T) {
	c := NewClient(Config{
		Provider:    ProviderAnthropic,
		Model:       "claude-3-haiku-20240307",
		MaxTokens:   256,
		Temperature: 0.5,
		Timeout:     10 * time.Second,
	}, testLogger())
	assert.Equal(t, ProviderAnthropic, c.cfg.Provider)
	assert.Equal(t, "claude-3-haiku-20240307", c.cfg.Model)
	assert.Equal(t, 256, c.cfg.MaxTokens)
	assert.Equal(t, 0.5, c.cfg.Temperature)
}

func TestClient_Enabled(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		enabled bool
	}{
		{"openai with key", Config{Provider: ProviderOpenAI, APIKey: "sk-test"}, true},
		{"openai no key", Config{Provider: ProviderOpenAI}, false},
		{"anthropic with key", Config{Provider: ProviderAnthropic, APIKey: "sk-ant-test"}, true},
		{"anthropic no key", Config{Provider: ProviderAnthropic}, false},
		{"ollama with url", Config{Provider: ProviderOllama, BaseURL: "http://localhost:11434"}, true},
		{"ollama no url", Config{Provider: ProviderOllama}, false},
		{"unknown provider", Config{Provider: Provider("unknown")}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient(tt.cfg, testLogger())
			assert.Equal(t, tt.enabled, c.Enabled())
		})
	}
}

func TestClient_CompleteOpenAI(t *testing.T) {
	// Mock OpenAI server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "gpt-4o-mini", req["model"])

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{"summary":"Test conflict about X","sides":[{"position":"A wants X","editors":[]},{"position":"B wants Y","editors":[]}],"content_area":"politics"}`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient(Config{
		Provider: ProviderOpenAI,
		APIKey:   "test-key",
		BaseURL:  server.URL,
		Model:    "gpt-4o-mini",
	}, testLogger())

	result, err := c.Complete(context.Background(), "system", "user prompt")
	require.NoError(t, err)
	assert.Contains(t, result, "Test conflict about X")
}

func TestClient_CompleteAnthropic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/messages", r.URL.Path)
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))

		resp := map[string]interface{}{
			"content": []map[string]interface{}{
				{"text": `{"summary":"Anthropic test","sides":[{"position":"X","editors":[]},{"position":"Y","editors":[]}],"content_area":"science"}`},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient(Config{
		Provider: ProviderAnthropic,
		APIKey:   "test-key",
		BaseURL:  server.URL,
		Model:    "claude-3-haiku",
	}, testLogger())

	result, err := c.Complete(context.Background(), "system", "user")
	require.NoError(t, err)
	assert.Contains(t, result, "Anthropic test")
}

func TestClient_CompleteOllama(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/chat", r.URL.Path)

		resp := map[string]interface{}{
			"message": map[string]interface{}{
				"content": `{"summary":"Local model test","sides":[{"position":"A","editors":[]},{"position":"B","editors":[]}],"content_area":"technology"}`,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient(Config{
		Provider: ProviderOllama,
		BaseURL:  server.URL,
		Model:    "llama3",
	}, testLogger())

	result, err := c.Complete(context.Background(), "system", "user")
	require.NoError(t, err)
	assert.Contains(t, result, "Local model test")
}

func TestClient_CompleteOpenAIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error": {"message": "rate limited"}}`))
	}))
	defer server.Close()

	c := NewClient(Config{
		Provider: ProviderOpenAI,
		APIKey:   "test-key",
		BaseURL:  server.URL,
	}, testLogger())

	_, err := c.Complete(context.Background(), "system", "user")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

func TestClient_CompleteUnsupportedProvider(t *testing.T) {
	c := NewClient(Config{Provider: Provider("unsupported")}, testLogger())
	_, err := c.Complete(context.Background(), "system", "user")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestClient_OpenAINoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"choices": []interface{}{}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewClient(Config{
		Provider: ProviderOpenAI,
		APIKey:   "test-key",
		BaseURL:  server.URL,
	}, testLogger())

	_, err := c.Complete(context.Background(), "system", "user")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no choices")
}
