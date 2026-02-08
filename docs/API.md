# WikiSurge API Documentation

WikiSurge is a real-time Wikipedia edit analytics platform. This document describes
all REST and WebSocket endpoints, error handling conventions, and usage patterns.

## Base URL

| Environment | URL |
|---|---|
| Development | `http://localhost:8080` |
| Production  | `https://api.wikisurge.example.com` |

## Authentication

Authentication is not required for the current version. All endpoints are publicly
accessible. Future versions will introduce API key or OAuth-based authentication.

## Rate Limiting

All endpoints are rate-limited using a sliding-window algorithm backed by Redis.

| Endpoint | Limit |
|---|---|
| `/api/search` | 100 req/min |
| `/api/trending` | 500 req/min |
| `/api/alerts` | 500 req/min |
| `/api/edit-wars` | 500 req/min |
| `/api/stats` | 1000 req/min |

When rate-limited, the API returns `429 Too Many Requests`:
```json
{
  "error": {
    "message": "Rate limit exceeded",
    "code": "RATE_LIMIT_EXCEEDED",
    "request_id": "abc123"
  }
}
```

## Error Codes

All errors use a consistent envelope:

```json
{
  "error": {
    "message": "Human-readable message",
    "code": "ERROR_CODE",
    "details": "Additional context",
    "request_id": "unique-id"
  }
}
```

| HTTP Status | Error Code | Meaning |
|---|---|---|
| 400 | `INVALID_PARAMETER` | Invalid query parameter or request body |
| 401 | `UNAUTHORIZED` | Missing or invalid credentials (future) |
| 404 | `NOT_FOUND` | Resource not found |
| 429 | `RATE_LIMIT_EXCEEDED` | Too many requests |
| 500 | `INTERNAL_ERROR` | Unexpected server error |
| 503 | `SERVICE_UNAVAILABLE` | Required backend (ES, Redis) unavailable |
| 504 | `TIMEOUT` | Upstream request timed out |

---

## Endpoints

### GET /health

Returns detailed health status for all components.

#### Example Request
```bash
curl http://localhost:8080/health
```

#### Example Response
```json
{
  "status": "ok",
  "timestamp": "2024-01-15T12:00:00Z",
  "uptime": 3600,
  "version": "1.0.0",
  "components": {
    "redis": {
      "status": "healthy",
      "latency_ms": 2.3,
      "memory_mb": 156.7
    },
    "elasticsearch": {
      "status": "healthy",
      "latency_ms": 45.2,
      "docs_count": 123456,
      "indices_count": 7
    },
    "kafka": {
      "status": "healthy"
    }
  }
}
```

### GET /health/live

Simple liveness probe. Returns 200 if the process is alive.

```bash
curl http://localhost:8080/health/live
```

### GET /health/ready

Full readiness probe. Returns 200 only when Redis (and Elasticsearch, if enabled) are reachable.

```bash
curl http://localhost:8080/health/ready
```

---

### GET /api/trending

Returns a list of currently trending Wikipedia pages.

#### Parameters
| Name | In | Type | Default | Description |
|---|---|---|---|---|
| `limit` | query | integer | 20 | Number of results (1–100) |
| `language` | query | string | — | Filter by language code (e.g. `en`) |

#### Example Request
```bash
curl "http://localhost:8080/api/trending?limit=10&language=en"
```

#### Example Response
```json
[
  {
    "title": "2024 Olympics",
    "score": 245.7,
    "edits_1h": 89,
    "last_edit": "2024-01-15T12:34:56Z",
    "rank": 1,
    "language": "en"
  }
]
```

#### Error Codes
- `400 INVALID_PARAMETER` — Invalid limit or language
- `429 RATE_LIMIT_EXCEEDED` — Too many requests
- `503 SERVICE_UNAVAILABLE` — Trending service not available

---

### GET /api/stats

Returns aggregate platform statistics.

#### Example Request
```bash
curl http://localhost:8080/api/stats
```

#### Example Response
```json
{
  "edits_per_second": 12.5,
  "hot_pages_count": 234,
  "trending_count": 150,
  "active_alerts": 3,
  "uptime": 3600,
  "top_languages": [
    {"language": "en", "count": 456},
    {"language": "de", "count": 123}
  ]
}
```

---

### GET /api/alerts

Returns spike and edit-war alerts.

#### Parameters
| Name | In | Type | Default | Description |
|---|---|---|---|---|
| `limit` | query | integer | 20 | Number of results (1–100) |
| `offset` | query | integer | 0 | Pagination offset |
| `since` | query | string | 24h ago | RFC3339 or Unix timestamp |
| `severity` | query | string | — | Filter: `low`, `medium`, `high`, `critical` |
| `type` | query | string | — | Filter: `spike`, `edit_war` |

#### Example Request
```bash
curl "http://localhost:8080/api/alerts?type=spike&severity=high&limit=5"
```

#### Example Response
```json
{
  "alerts": [
    {
      "type": "spike",
      "page_title": "Breaking_News",
      "spike_ratio": 15.2,
      "severity": "high",
      "timestamp": "2024-01-15T12:00:00Z",
      "edits_5min": 45
    }
  ],
  "total": 1,
  "pagination": {
    "total": 1,
    "limit": 5,
    "offset": 0,
    "has_more": false
  }
}
```

---

### GET /api/edit-wars

Returns currently active or historical edit wars.

#### Parameters
| Name | In | Type | Default | Description |
|---|---|---|---|---|
| `limit` | query | integer | 20 | Number of results (1–100) |
| `active` | query | boolean | true | Only active wars |

#### Example Request
```bash
curl "http://localhost:8080/api/edit-wars?active=true&limit=10"
```

#### Example Response
```json
[
  {
    "page_title": "Controversial_Topic",
    "editor_count": 5,
    "edit_count": 24,
    "revert_count": 8,
    "severity": "high",
    "editors": ["User1", "User2", "User3"],
    "active": true
  }
]
```

---

### GET /api/search

Full-text search over indexed Wikipedia edits. Requires Elasticsearch.

#### Parameters
| Name | In | Type | Default | Description |
|---|---|---|---|---|
| `q` | query | string | **required** | Search query. Wrap in quotes for phrase matching. |
| `limit` | query | integer | 50 | Number of results (1–100) |
| `offset` | query | integer | 0 | Pagination offset |
| `from` | query | string | 7 days ago | Start of time range |
| `to` | query | string | now | End of time range |
| `language` | query | string | — | Filter by language |
| `bot` | query | string | — | `true`/`false` to filter bot edits |

#### Example Request
```bash
curl "http://localhost:8080/api/search?q=climate+change&limit=10&language=en"
```

#### Example Response
```json
{
  "hits": [
    {
      "title": "Climate change",
      "user": "EcoEditor",
      "timestamp": "2024-01-15T11:30:00Z",
      "comment": "Updated references",
      "byte_change": 1250,
      "wiki": "enwiki",
      "score": 12.5,
      "language": "en"
    }
  ],
  "total": 42,
  "query": "climate change",
  "pagination": {
    "total": 42,
    "limit": 10,
    "offset": 0,
    "has_more": true
  }
}
```

#### Error Codes
- `400 INVALID_PARAMETER` — Missing `q`, invalid limit/offset, `from` > `to`
- `503 SERVICE_UNAVAILABLE` — Elasticsearch disabled
- `504 TIMEOUT` — Search timed out

---

### GET /api/docs

Serves the interactive API documentation (ReDoc UI). Open in a browser.

```bash
open http://localhost:8080/api/docs
```

### GET /api/docs/openapi.yaml

Returns the raw OpenAPI 3.0 specification in YAML format.

---

## WebSocket Endpoints

### WS /ws/feed

Streams Wikipedia edits in real time.

#### Connection
```javascript
const ws = new WebSocket('ws://localhost:8080/ws/feed?languages=en&exclude_bots=true');

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  console.log(msg.type, msg.data);
};
```

#### Query Parameters
| Name | Type | Description |
|---|---|---|
| `languages` | string | Comma-separated language codes |
| `exclude_bots` | boolean | Exclude bot edits |
| `page_pattern` | string | Regex for page title filtering |
| `min_byte_change` | integer | Minimum absolute byte change |

#### Message Format
```json
{
  "type": "edit",
  "data": {
    "title": "Example Page",
    "user": "Editor123",
    "wiki": "enwiki",
    "timestamp": "2024-01-15T12:00:00Z",
    "comment": "Fixed typo",
    "bot": false
  }
}
```

### WS /ws/alerts

Streams spike and edit-war alerts in real time.

#### Connection
```javascript
const ws = new WebSocket('ws://localhost:8080/ws/alerts');

ws.onmessage = (event) => {
  const alert = JSON.parse(event.data);
  console.log(`Alert: ${alert.type}`, alert.data);
};
```

#### Message Format
```json
{
  "type": "spike",
  "data": {
    "type": "spike",
    "timestamp": "2024-01-15T12:00:00Z",
    "data": {
      "title": "Breaking News",
      "spike_ratio": 15.2,
      "edit_count": 45
    }
  }
}
```

---

## Best Practices

### Pagination
Use `limit` and `offset` parameters. The response includes `has_more` to
indicate whether additional pages exist.

```bash
# Page 1
curl "http://localhost:8080/api/alerts?limit=20&offset=0"

# Page 2
curl "http://localhost:8080/api/alerts?limit=20&offset=20"
```

### Caching
- Responses include `Cache-Control` and `ETag` headers
- Use `If-None-Match` with the `ETag` value to avoid re-downloading unchanged data
- Responses include `X-Cache: HIT` or `X-Cache: MISS` to indicate cache status

```bash
# Get initial response with ETag
ETAG=$(curl -sI http://localhost:8080/api/trending | grep ETag | awk '{print $2}')

# Conditional GET
curl -H "If-None-Match: $ETAG" http://localhost:8080/api/trending
# Returns 304 Not Modified if unchanged
```

### Error Handling
Always check the HTTP status code first. Parse the error body for the `code`
field when handling specific error scenarios programmatically.

```python
import requests

resp = requests.get("http://localhost:8080/api/trending?limit=999")
if resp.status_code != 200:
    error = resp.json()["error"]
    print(f"Error {error['code']}: {error['message']}")
```

### Request ID
Every response includes an `X-Request-ID` header. Include this when reporting
issues or debugging request flows.

---

## Metrics

Prometheus metrics are exposed at `:2112/metrics`. Key API metrics:

| Metric | Type | Description |
|---|---|---|
| `api_requests_total` | counter | Total requests by endpoint and method |
| `api_request_duration_seconds` | histogram | Request latency by endpoint |
| `api_response_size_bytes` | histogram | Response size by endpoint |
| `api_errors_total` | counter | Errors by error code |
| `api_cache_hits_total` | counter | Response cache hits |
| `api_cache_misses_total` | counter | Response cache misses |
| `api_requests_in_flight` | gauge | Concurrent requests |
| `websocket_connections_active` | gauge | Active WebSocket connections |
| `websocket_messages_sent_total` | counter | Messages sent to WS clients |
| `rate_limit_hits_total` | counter | Rate limiter rejections |
