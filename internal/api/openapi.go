package api

// generateOpenAPISpec returns the complete OpenAPI 3.0 specification for the
// WikiSurge API as a YAML string. It is served at GET /api/docs/openapi.yaml.
func generateOpenAPISpec() string {
	return `openapi: "3.0.3"

info:
  title: WikiSurge API
  version: "1.0.0"
  description: |
    WikiSurge is a real-time Wikipedia edit analytics platform.
    This API exposes trending pages, spike and edit-war alerts,
    full-text search over indexed edits, and live WebSocket feeds.
  contact:
    name: WikiSurge
  license:
    name: MIT

servers:
  - url: http://localhost:8080
    description: Local development
  - url: https://api.wikisurge.example.com
    description: Production

tags:
  - name: Health
    description: Service health and readiness
  - name: Trending
    description: Trending Wikipedia pages
  - name: Stats
    description: Platform statistics
  - name: Alerts
    description: Spike and edit-war alerts
  - name: Edit Wars
    description: Edit war monitoring
  - name: Search
    description: Full-text search over indexed edits
  - name: WebSocket
    description: Real-time data feeds

paths:
  /health:
    get:
      tags: [Health]
      summary: Detailed health check
      description: Returns component-level health for Redis, Elasticsearch, and Kafka.
      responses:
        '200':
          description: All components healthy
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/DetailedHealthResponse'
        '503':
          description: One or more components unhealthy
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/DetailedHealthResponse'

  /health/live:
    get:
      tags: [Health]
      summary: Liveness probe
      description: Simple alive check — returns 200 if the process is running.
      responses:
        '200':
          description: Alive
          content:
            application/json:
              schema:
                type: object
                properties:
                  status:
                    type: string
                    example: alive

  /health/ready:
    get:
      tags: [Health]
      summary: Readiness probe
      description: Full dependency check — returns 200 only when all required backends are reachable.
      responses:
        '200':
          description: Ready
          content:
            application/json:
              schema:
                type: object
                properties:
                  status:
                    type: string
                    example: ready
        '503':
          description: Not ready
          content:
            application/json:
              schema:
                type: object
                properties:
                  status:
                    type: string
                    example: not_ready
                  reason:
                    type: string

  /api/trending:
    get:
      tags: [Trending]
      summary: Get trending pages
      description: Returns top trending Wikipedia pages based on recent edit activity.
      parameters:
        - name: limit
          in: query
          description: Number of results to return
          schema:
            type: integer
            default: 20
            minimum: 1
            maximum: 100
        - name: language
          in: query
          description: Filter by language code (e.g. en, es, fr)
          schema:
            type: string
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: '#/components/schemas/TrendingPage'
              example:
                - title: "2024 Olympics"
                  score: 245.7
                  edits_1h: 89
                  last_edit: "2024-01-15T12:34:56Z"
                  rank: 1
                  language: en
        '400':
          $ref: '#/components/responses/BadRequest'
        '429':
          $ref: '#/components/responses/RateLimited'
        '503':
          $ref: '#/components/responses/ServiceUnavailable'

  /api/stats:
    get:
      tags: [Stats]
      summary: Platform statistics
      description: Returns aggregate statistics about the platform's current state.
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/StatsResponse'

  /api/alerts:
    get:
      tags: [Alerts]
      summary: Get alerts
      description: Returns spike and edit-war alerts from the last 24 hours.
      parameters:
        - name: limit
          in: query
          schema:
            type: integer
            default: 20
            minimum: 1
            maximum: 100
        - name: offset
          in: query
          schema:
            type: integer
            default: 0
            minimum: 0
            maximum: 10000
        - name: since
          in: query
          description: Only show alerts after this time (RFC3339 or Unix timestamp)
          schema:
            type: string
        - name: severity
          in: query
          description: Filter by severity level
          schema:
            type: string
            enum: [low, medium, high, critical]
        - name: type
          in: query
          description: Filter by alert type
          schema:
            type: string
            enum: [spike, edit_war]
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/AlertsResponse'
        '400':
          $ref: '#/components/responses/BadRequest'
        '429':
          $ref: '#/components/responses/RateLimited'

  /api/edit-wars:
    get:
      tags: [Edit Wars]
      summary: Get edit wars
      description: Returns currently active or recent edit wars.
      parameters:
        - name: limit
          in: query
          schema:
            type: integer
            default: 20
            minimum: 1
            maximum: 100
        - name: active
          in: query
          description: If true, only return currently active edit wars
          schema:
            type: boolean
            default: true
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: '#/components/schemas/EditWarEntry'
        '400':
          $ref: '#/components/responses/BadRequest'
        '500':
          $ref: '#/components/responses/InternalError'

  /api/search:
    get:
      tags: [Search]
      summary: Search edits
      description: Full-text search over indexed Wikipedia edits (requires Elasticsearch).
      parameters:
        - name: q
          in: query
          required: true
          description: Search query string. Wrap in quotes for phrase matching.
          schema:
            type: string
        - name: limit
          in: query
          schema:
            type: integer
            default: 50
            minimum: 1
            maximum: 100
        - name: offset
          in: query
          schema:
            type: integer
            default: 0
            minimum: 0
            maximum: 10000
        - name: from
          in: query
          description: Start of time range (RFC3339 or Unix timestamp)
          schema:
            type: string
        - name: to
          in: query
          description: End of time range (RFC3339 or Unix timestamp)
          schema:
            type: string
        - name: language
          in: query
          description: Filter by language code
          schema:
            type: string
        - name: bot
          in: query
          description: Filter by bot edits (true/false)
          schema:
            type: string
            enum: ["true", "false"]
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/SearchResponse'
        '400':
          $ref: '#/components/responses/BadRequest'
        '429':
          $ref: '#/components/responses/RateLimited'
        '503':
          $ref: '#/components/responses/ServiceUnavailable'
        '504':
          description: Search timed out
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Error'

  /ws/feed:
    get:
      tags: [WebSocket]
      summary: Live edit feed
      description: |
        WebSocket endpoint that streams Wikipedia edits in real time.
        Connect via ws:// (or wss:// in production).
      parameters:
        - name: languages
          in: query
          description: Comma-separated language codes to filter (e.g. "en,es,fr")
          schema:
            type: string
        - name: exclude_bots
          in: query
          description: If true, exclude bot edits
          schema:
            type: boolean
            default: false
        - name: page_pattern
          in: query
          description: Regex pattern to filter page titles
          schema:
            type: string
        - name: min_byte_change
          in: query
          description: Minimum absolute byte change to include
          schema:
            type: integer
      responses:
        '101':
          description: WebSocket upgrade successful

  /ws/alerts:
    get:
      tags: [WebSocket]
      summary: Live alert feed
      description: |
        WebSocket endpoint that streams spike and edit-war alerts in real time.
      responses:
        '101':
          description: WebSocket upgrade successful

components:
  schemas:
    Error:
      type: object
      properties:
        error:
          type: object
          properties:
            message:
              type: string
              description: Human-readable error message
            code:
              type: string
              description: Machine-readable error code
              enum:
                - INVALID_PARAMETER
                - RATE_LIMIT_EXCEEDED
                - INTERNAL_ERROR
                - SERVICE_UNAVAILABLE
                - NOT_FOUND
                - UNAUTHORIZED
                - TIMEOUT
            details:
              type: string
              description: Additional context
            request_id:
              type: string
              description: Unique request identifier
      example:
        error:
          message: "Invalid 'limit' parameter"
          code: "INVALID_PARAMETER"
          details: "field: limit"
          request_id: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"

    TrendingPage:
      type: object
      properties:
        title:
          type: string
        score:
          type: number
          format: double
        edits_1h:
          type: integer
        last_edit:
          type: string
          format: date-time
        rank:
          type: integer
        language:
          type: string

    StatsResponse:
      type: object
      properties:
        edits_per_second:
          type: number
        hot_pages_count:
          type: integer
        trending_count:
          type: integer
        active_alerts:
          type: integer
        uptime:
          type: integer
          description: Server uptime in seconds
        top_languages:
          type: array
          items:
            $ref: '#/components/schemas/LanguageStat'

    LanguageStat:
      type: object
      properties:
        language:
          type: string
        count:
          type: integer

    AlertsResponse:
      type: object
      properties:
        alerts:
          type: array
          items:
            $ref: '#/components/schemas/AlertEntry'
        total:
          type: integer
        pagination:
          $ref: '#/components/schemas/Pagination'

    AlertEntry:
      type: object
      properties:
        type:
          type: string
          enum: [spike, edit_war]
        page_title:
          type: string
        spike_ratio:
          type: number
        severity:
          type: string
          enum: [low, medium, high, critical]
        timestamp:
          type: string
          format: date-time
        edits_5min:
          type: integer
        editor_count:
          type: integer
        edit_count:
          type: integer
        revert_count:
          type: integer
        editors:
          type: array
          items:
            type: string
        wiki:
          type: string

    EditWarEntry:
      type: object
      properties:
        page_title:
          type: string
        editor_count:
          type: integer
        edit_count:
          type: integer
        revert_count:
          type: integer
        severity:
          type: string
        start_time:
          type: string
          format: date-time
        editors:
          type: array
          items:
            type: string
        active:
          type: boolean

    SearchResponse:
      type: object
      properties:
        hits:
          type: array
          items:
            $ref: '#/components/schemas/SearchHit'
        total:
          type: integer
        query:
          type: string
        pagination:
          $ref: '#/components/schemas/Pagination'

    SearchHit:
      type: object
      properties:
        title:
          type: string
        user:
          type: string
        timestamp:
          type: string
          format: date-time
        comment:
          type: string
        byte_change:
          type: integer
        wiki:
          type: string
        score:
          type: number
        language:
          type: string

    Pagination:
      type: object
      properties:
        total:
          type: integer
        limit:
          type: integer
        offset:
          type: integer
        has_more:
          type: boolean

    DetailedHealthResponse:
      type: object
      properties:
        status:
          type: string
          enum: [ok, degraded, error]
        timestamp:
          type: string
          format: date-time
        uptime:
          type: integer
          description: Uptime in seconds
        version:
          type: string
        components:
          type: object
          additionalProperties:
            $ref: '#/components/schemas/ComponentHealth'

    ComponentHealth:
      type: object
      properties:
        status:
          type: string
          enum: [healthy, degraded, unhealthy, disabled]
        latency_ms:
          type: number
        memory_mb:
          type: number
        docs_count:
          type: integer
        indices_count:
          type: integer
        lag:
          type: integer
        details:
          type: string

    WSMessage:
      type: object
      properties:
        type:
          type: string
          enum: [edit, spike, edit_war]
        data:
          type: object
          description: Edit or alert payload

  responses:
    BadRequest:
      description: Invalid request parameters
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Error'

    RateLimited:
      description: Rate limit exceeded
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Error'

    InternalError:
      description: Internal server error
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Error'

    ServiceUnavailable:
      description: Required service is unavailable
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/Error'
`
}
