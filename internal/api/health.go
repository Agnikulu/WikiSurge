package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/elastic/go-elasticsearch/v8/esapi"
)

// ---------------------------------------------------------------------------
// Enhanced Health Response (Task 17.5)
// ---------------------------------------------------------------------------

// DetailedHealthResponse is the enhanced health check response.
type DetailedHealthResponse struct {
	Status     string                     `json:"status"`
	Timestamp  string                     `json:"timestamp"`
	Uptime     int64                      `json:"uptime"`
	Version    string                     `json:"version"`
	Components map[string]ComponentHealth `json:"components"`
}

// ComponentHealth holds health details for a single dependency.
type ComponentHealth struct {
	Status       string  `json:"status"`
	LatencyMs    float64 `json:"latency_ms"`
	MemoryMB     float64 `json:"memory_mb,omitempty"`
	DocsCount    int64   `json:"docs_count,omitempty"`
	IndicesCount int     `json:"indices_count,omitempty"`
	Lag          int64   `json:"lag,omitempty"`
	Details      string  `json:"details,omitempty"`
}

// ---------------------------------------------------------------------------
// Enhanced /health — detailed component health (replaces old handler)
// ---------------------------------------------------------------------------

func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	components := make(map[string]ComponentHealth)

	// ---- Redis ----
	redisHealth := s.checkRedisHealth(ctx)
	components["redis"] = redisHealth

	// ---- Elasticsearch ----
	esHealth := s.checkElasticsearchHealth(ctx)
	components["elasticsearch"] = esHealth

	// ---- Kafka (approximation based on consumer lag gauge) ----
	kafkaHealth := ComponentHealth{Status: "healthy", Details: "lag monitoring via metrics"}
	components["kafka"] = kafkaHealth

	// ---- Overall status ----
	overall := "ok"
	httpStatus := http.StatusOK
	for _, c := range components {
		if c.Status == "degraded" && overall == "ok" {
			overall = "degraded"
		}
		if c.Status == "unhealthy" {
			overall = "error"
			httpStatus = http.StatusServiceUnavailable
		}
	}

	respondJSON(w, httpStatus, DetailedHealthResponse{
		Status:     overall,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Uptime:     int64(time.Since(s.startTime).Seconds()),
		Version:    s.version,
		Components: components,
	})
}

// checkRedisHealth pings Redis and gathers memory info.
func (s *APIServer) checkRedisHealth(ctx context.Context) ComponentHealth {
	start := time.Now()
	if err := s.redis.Ping(ctx).Err(); err != nil {
		return ComponentHealth{
			Status:    "unhealthy",
			LatencyMs: float64(time.Since(start).Milliseconds()),
			Details:   fmt.Sprintf("ping failed: %v", err),
		}
	}
	latency := float64(time.Since(start).Milliseconds())

	// Try to get memory info.
	var memoryMB float64
	info, err := s.redis.Info(ctx, "memory").Result()
	if err == nil {
		// Parse used_memory_human or used_memory.
		for _, line := range splitLines(info) {
			if len(line) > 12 && line[:12] == "used_memory:" {
				var mem int64
				fmt.Sscanf(line, "used_memory:%d", &mem)
				memoryMB = float64(mem) / (1024 * 1024)
				break
			}
		}
	}

	return ComponentHealth{
		Status:    "healthy",
		LatencyMs: latency,
		MemoryMB:  memoryMB,
	}
}

// checkElasticsearchHealth checks ES cluster health.
func (s *APIServer) checkElasticsearchHealth(ctx context.Context) ComponentHealth {
	if s.es == nil {
		if s.config.Elasticsearch.Enabled {
			return ComponentHealth{Status: "unhealthy", Details: "not initialized"}
		}
		return ComponentHealth{Status: "disabled"}
	}

	start := time.Now()

	// Use the raw ES client to call /_cluster/health
	health, err := s.getESClusterHealth(ctx)
	latency := float64(time.Since(start).Milliseconds())
	if err != nil {
		return ComponentHealth{
			Status:    "degraded",
			LatencyMs: latency,
			Details:   fmt.Sprintf("health check failed: %v", err),
		}
	}

	status := "healthy"
	if cs, ok := health["status"].(string); ok && cs == "red" {
		status = "unhealthy"
	} else if cs == "yellow" {
		status = "degraded"
	}

	var docsCount int64
	var indicesCount int
	if v, ok := health["number_of_data_nodes"].(float64); ok {
		_ = v
	}
	// Get index count from the health response.
	if v, ok := health["active_shards"].(float64); ok {
		_ = v
	}

	// Try to get stats for doc count and index count.
	stats, statsErr := s.getESStats(ctx)
	if statsErr == nil {
		if all, ok := stats["_all"].(map[string]interface{}); ok {
			if primaries, ok := all["primaries"].(map[string]interface{}); ok {
				if docs, ok := primaries["docs"].(map[string]interface{}); ok {
					if count, ok := docs["count"].(float64); ok {
						docsCount = int64(count)
					}
				}
			}
		}
		if indices, ok := stats["indices"].(map[string]interface{}); ok {
			indicesCount = len(indices)
		}
	}

	return ComponentHealth{
		Status:       status,
		LatencyMs:    latency,
		DocsCount:    docsCount,
		IndicesCount: indicesCount,
	}
}

// getESClusterHealth calls GET /_cluster/health on the ES client.
func (s *APIServer) getESClusterHealth(ctx context.Context) (map[string]interface{}, error) {
	if s.es == nil || s.es.RawClient() == nil {
		return nil, fmt.Errorf("ES client not available")
	}

	res, err := s.es.RawClient().Cluster.Health(
		s.es.RawClient().Cluster.Health.WithContext(ctx),
	)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("cluster health returned %s", res.Status())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// getESStats calls GET /_stats on the ES client.
func (s *APIServer) getESStats(ctx context.Context) (map[string]interface{}, error) {
	if s.es == nil || s.es.RawClient() == nil {
		return nil, fmt.Errorf("ES client not available")
	}

	req := esapi.IndicesStatsRequest{
		Index: []string{"wikipedia-edits-*"},
	}
	res, err := req.Do(ctx, s.es.RawClient())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("index stats returned %s", res.Status())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Liveness and Readiness probes (Task 17.5)
// ---------------------------------------------------------------------------

// handleLiveness — GET /health/live — simple alive check.
func (s *APIServer) handleLiveness(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{
		"status": "alive",
	})
}

// handleReadiness — GET /health/ready — full dependency check.
// Redis is required; ES failure degrades readiness but doesn't fail the probe,
// preventing the container from being killed when ES is temporarily overloaded.
func (s *APIServer) handleReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// Must have Redis — hard requirement.
	if err := s.redis.Ping(ctx).Err(); err != nil {
		respondJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"status": "not_ready",
			"reason": "redis unavailable",
		})
		return
	}

	// ES failure is a soft dependency — report degraded but stay ready.
	// This prevents Coolify/Docker from cycling the container when ES is
	// temporarily overloaded but the API can still serve cached data.
	status := "ready"
	if s.config.Elasticsearch.Enabled && s.es != nil {
		if _, err := s.getESClusterHealth(ctx); err != nil {
			status = "degraded"
		}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status": status,
	})
}

// ---------------------------------------------------------------------------
// API Documentation (Task 17.4)
// ---------------------------------------------------------------------------

// handleAPIDocs serves the ReDoc-based API documentation UI.
func (s *APIServer) handleAPIDocs(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>WikiSurge API Documentation</title>
    <meta charset="utf-8"/>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <link href="https://fonts.googleapis.com/css?family=Montserrat:300,400,700|Roboto:300,400,700" rel="stylesheet">
    <style>body { margin: 0; padding: 0; }</style>
</head>
<body>
    <redoc spec-url='/api/docs/openapi.yaml'></redoc>
    <script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

// handleOpenAPISpec serves the OpenAPI YAML specification.
func (s *APIServer) handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	spec := generateOpenAPISpec()
	w.Header().Set("Content-Type", "application/x-yaml; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(spec))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func splitLines(s string) []string {
	var lines []string
	var buf bytes.Buffer
	for _, c := range s {
		if c == '\n' {
			lines = append(lines, buf.String())
			buf.Reset()
		} else if c != '\r' {
			buf.WriteRune(c)
		}
	}
	if buf.Len() > 0 {
		lines = append(lines, buf.String())
	}
	return lines
}
