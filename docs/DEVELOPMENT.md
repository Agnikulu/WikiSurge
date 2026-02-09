# WikiSurge Development Guide

## Table of Contents
- [Getting Started](#getting-started)
- [Code Structure](#code-structure)
- [Adding Features](#adding-features)
- [Testing](#testing)
- [Contributing](#contributing)

---

## Getting Started

### Clone and Setup

```bash
# Clone repository
git clone https://github.com/yourusername/WikiSurge.git
cd WikiSurge

# Install Go dependencies
go mod download

# Install frontend dependencies
cd web
npm install
cd ..

# Start infrastructure
docker-compose up -d kafka redis elasticsearch

# Verify setup
./scripts/test-infrastructure.sh
```

### Development Environment

**Required:**
- Go 1.23+
- Node.js 20+
- Docker & Docker Compose
- Git

**Recommended:**
- VS Code with Go extension
- Delve debugger
- Air (hot reload)
- Postman/Insomnia (API testing)

**VS Code Extensions:**
- Go (golang.go)
- ESLint (dbaeumer.vscode-eslint)
- Prettier (esbenp.prettier-vscode)
- Docker (ms-azuretools.vscode-docker)

### Install Development Tools

```bash
# Go tools
go install github.com/cosmtrek/air@latest
go install golang.org/x/tools/cmd/goimports@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install github.com/go-delve/delve/cmd/dlv@latest

# Frontend tools
npm install -g typescript
npm install -g eslint
```

---

## Code Structure

### Directory Layout

```
WikiSurge/
├── cmd/                    # Main applications
│   ├── api/               # API server entry point
│   ├── ingestor/          # Ingestor entry point
│   ├── processor/         # Processor entry point
│   └── demo/              # Demo/testing tool
├── internal/              # Private application code
│   ├── api/              # API server implementation
│   ├── ingestor/         # Ingestor implementation
│   ├── processor/        # Processing pipelines
│   ├── storage/          # Data layer (Redis, ES)
│   ├── kafka/            # Kafka client wrappers
│   ├── models/           # Data models
│   ├── config/           # Configuration management
│   ├── metrics/          # Prometheus metrics
│   ├── monitoring/       # Health checks, alerts
│   └── resilience/       # Circuit breakers, retries
├── web/                   # Frontend application
│   ├── src/              # React source code
│   │   ├── components/  # React components
│   │   ├── hooks/       # Custom React hooks
│   │   ├── store/       # Zustand state management
│   │   ├── types/       # TypeScript types
│   │   └── utils/       # Utility functions
│   └── public/          # Static assets
├── configs/              # Configuration files
├── deployments/          # Deployment configs (Docker, K8s)
├── docs/                 # Documentation
├── monitoring/           # Grafana dashboards, alerts
├── scripts/              # Automation scripts
├── test/                 # Integration & load tests
│   ├── integration/     # Integration tests
│   ├── load/            # Load test scenarios
│   └── chaos/           # Chaos engineering tests
├── go.mod                # Go dependencies
├── Makefile              # Build automation
└── docker-compose.yml    # Local development stack
```

### Package Organization

**cmd/** - Entry points only, minimal logic:
```go
package main

func main() {
    cfg := loadConfig()
    svc := service.New(cfg)
    svc.Run()
}
```

**internal/** - Business logic, well-organized:
- Each package has single responsibility
- Interfaces define contracts
- Implementation hidden from external packages

**Example structure:**
```
internal/processor/
├── spike_detector.go       # Spike detection logic
├── spike_detector_test.go  # Unit tests
├── edit_war_detector.go    # Edit war detection
├── trending.go             # Trending calculation
├── processor.go            # Main processor orchestrator
└── interfaces.go           # Interface definitions
```

---

### Naming Conventions

**Files:**
- Lowercase, underscore-separated: `spike_detector.go`
- Tests: `*_test.go`
- Interfaces: `interfaces.go` or embedded in main file

**Packages:**
- Short, lowercase, single word if possible
- No underscores: `processor`, not `spike_processor`

**Functions/Methods:**
- PascalCase for exported: `ProcessEdit`
- camelCase for private: `validateEdit`
- Test functions: `TestProcessEdit`, `BenchmarkProcessEdit`

**Variables:**
- camelCase: `editCount`, `hotPageTracker`
- Constants: PascalCase or UPPER_CASE: `MaxHotPages` or `MAX_HOT_PAGES`
- Acronyms: `apiServer` (not `APIServer` for variables)

**Types:**
- PascalCase: `SpikeDetector`, `EditWarAlert`
- Interfaces: often suffix with `-er`: `Consumer`, `Detector`

---

### Code Style Guide

**Go:**

Follow [Effective Go](https://golang.org/doc/effective_go.html) and [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments).

```go
// Good: Clear, documented, idiomatic
// ProcessEdit handles a single Wikipedia edit through the spike detector.
// It returns an error if processing fails.
func (sd *SpikeDetector) ProcessEdit(ctx context.Context, edit *models.WikipediaEdit) error {
    if edit == nil {
        return errors.New("edit cannot be nil")
    }
    
    // Check if page is hot
    isHot, err := sd.hotPages.IsHot(ctx, edit.Title)
    if err != nil {
        return fmt.Errorf("checking hot status: %w", err)
    }
    
    if !isHot {
        return nil // Only process hot pages
    }
    
    // ... rest of logic
}

// Bad: No docs, unclear, nested ifs
func (sd *SpikeDetector) ProcessEdit(ctx context.Context, edit *models.WikipediaEdit) error {
    if edit != nil {
        isHot, err := sd.hotPages.IsHot(ctx, edit.Title)
        if err == nil {
            if isHot {
                // ... logic
            }
        } else {
            return err
        }
    }
    return nil
}
```

**TypeScript/React:**

Follow [Airbnb JavaScript Style Guide](https://github.com/airbnb/javascript).

```typescript
// Good: Type-safe, clear, functional
interface AlertsPanelProps {
  maxAlerts?: number;
  onAlertDismiss?: (alert: Alert) => void;
}

export const AlertsPanel: React.FC<AlertsPanelProps> = ({
  maxAlerts = 100,
  onAlertDismiss,
}) => {
  const [alerts, setAlerts] = useState<Alert[]>([]);
  
  const handleDismiss = useCallback((alert: Alert) => {
    setAlerts(prev => prev.filter(a => a.id !== alert.id));
    onAlertDismiss?.(alert);
  }, [onAlertDismiss]);
  
  // ... render
};

// Bad: Any types, unclear, imperative
export function AlertsPanel(props: any) {
  let alerts = [];
  
  function dismiss(alert) {
    for (let i = 0; i < alerts.length; i++) {
      if (alerts[i].id === alert.id) {
        alerts.splice(i, 1);
        break;
      }
    }
  }
}
```

---

## Adding Features

### Creating a New Consumer

**Use case:** Add new detection algorithm (e.g., "vandalism detector").

**Steps:**

#### 1. Create consumer implementation

**`internal/processor/vandalism_detector.go`:**
```go
package processor

import (
    "context"
    "github.com/Agnikulu/WikiSurge/internal/models"
    "github.com/Agnikulu/WikiSurge/internal/storage"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/rs/zerolog"
)

type VandalismDetector struct {
    redis       *redis.Client
    alerts      *storage.RedisAlerts
    config      *config.Config
    logger      zerolog.Logger
    metrics     *VandalismMetrics
}

type VandalismMetrics struct {
    DetectionsTotal *prometheus.CounterVec
    ProcessingTime  prometheus.Histogram
}

func NewVandalismDetector(
    redis *redis.Client,
    alerts *storage.RedisAlerts,
    cfg *config.Config,
    logger zerolog.Logger,
) *VandalismDetector {
    metrics := &VandalismMetrics{
        DetectionsTotal: prometheus.NewCounterVec(
            prometheus.CounterOpts{
                Name: "vandalism_detections_total",
                Help: "Total vandalism events detected",
            },
            []string{"severity"},
        ),
        ProcessingTime: prometheus.NewHistogram(
            prometheus.HistogramOpts{
                Name: "vandalism_processing_seconds",
                Help: "Time to process edit for vandalism",
            },
        ),
    }
    
    prometheus.MustRegister(
        metrics.DetectionsTotal,
        metrics.ProcessingTime,
    )
    
    return &VandalismDetector{
        redis:   redis,
        alerts:  alerts,
        config:  cfg,
        logger:  logger.With().Str("component", "vandalism_detector").Logger(),
        metrics: metrics,
    }
}

// ProcessEdit implements the EditConsumer interface
func (vd *VandalismDetector) ProcessEdit(ctx context.Context, edit *models.WikipediaEdit) error {
    start := time.Now()
    defer func() {
        vd.metrics.ProcessingTime.Observe(time.Since(start).Seconds())
    }()
    
    // Detection logic here
    if vd.isVandalism(edit) {
        alert := &VandalismAlert{
            PageTitle: edit.Title,
            User:      edit.User,
            // ... fields
        }
        
        if err := vd.publishAlert(ctx, alert); err != nil {
            return err
        }
        
        vd.metrics.DetectionsTotal.WithLabelValues(alert.Severity).Inc()
        vd.logger.Info().
            Str("page", edit.Title).
            Str("user", edit.User).
            Msg("Vandalism detected")
    }
    
    return nil
}

func (vd *VandalismDetector) isVandalism(edit *models.WikipediaEdit) bool {
    // Heuristics:
    // - Large content removal
    // - Profanity patterns
    // - Repeated reverts
    // - Anonymous user
    // - etc.
    return false // TODO: implement
}
```

#### 2. Add unit tests

**`internal/processor/vandalism_detector_test.go`:**
```go
package processor

import (
    "context"
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestVandalismDetector_ProcessEdit(t *testing.T) {
    detector := setupVandalismDetector(t)
    ctx := context.Background()
    
    t.Run("detects large deletion", func(t *testing.T) {
        edit := &models.WikipediaEdit{
            Title: "Test Page",
            Length: models.Length{
                Old: 10000,
                New: 100, // 99% deleted
            },
            Comment: "blanking",
        }
        
        err := detector.ProcessEdit(ctx, edit)
        assert.NoError(t, err)
        
        // Verify alert was published
        alerts := getPublishedAlerts(t, detector)
        assert.Len(t, alerts, 1)
        assert.Equal(t, "high", alerts[0].Severity)
    })
    
    t.Run("ignores normal edits", func(t *testing.T) {
        edit := &models.WikipediaEdit{
            Title: "Test Page",
            Length: models.Length{
                Old: 1000,
                New: 1100, // Normal addition
            },
            Comment: "added citation",
        }
        
        err := detector.ProcessEdit(ctx, edit)
        assert.NoError(t, err)
        
        // Verify no alert
        alerts := getPublishedAlerts(t, detector)
        assert.Len(t, alerts, 0)
    })
}
```

#### 3. Register consumer in orchestrator

**`cmd/processor/main.go`:**
```go
func (o *processorOrchestrator) initProcessors() {
    // ... existing detectors
    
    // Initialize vandalism detector
    o.vandalismDetector = processor.NewVandalismDetector(
        o.redisClient,
        o.alerts,
        o.cfg,
        o.logger,
    )
    o.registerComponent("vandalism-detector")
    o.logger.Info().Msg("Initialized VandalismDetector")
}

func (o *processorOrchestrator) createConsumers() error {
    // ... existing consumers
    
    // Vandalism detector consumer
    if o.cfg.Processor.Features.Vandalism {
        var err error
        o.vandalismConsumer, err = kafka.NewConsumer(
            o.cfg,
            baseConsumerCfg("vandalism-detector"),
            o.vandalismDetector,
            o.logger,
        )
        if err != nil {
            return fmt.Errorf("failed to create vandalism consumer: %w", err)
        }
        o.logger.Info().Msg("Created vandalism consumer")
    }
    
    return nil
}
```

#### 4. Add configuration

**`configs/config.dev.yaml`:**
```yaml
processor:
  features:
    spike_detection: true
    edit_wars: true
    trending: true
    vandalism: true  # New feature
    elasticsearch: true
    websocket: true
```

#### 5. Add metrics dashboard

Create `monitoring/vandalism-dashboard.json` for Grafana.

---

### Adding a New API Endpoint

**Use case:** Add endpoint to get recent vandalism events.

**Steps:**

#### 1. Add handler

**`internal/api/handlers.go`:**
```go
// GET /api/vandalism?limit=20&severity=high
func (s *APIServer) handleGetVandalism(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    
    // Parse query parameters
    limit := parseQueryInt(r, "limit", 20)
    severity := r.URL.Query().Get("severity") // optional filter
    
    // Validate
    if limit < 1 || limit > 100 {
        respondError(w, http.StatusBadRequest, "limit must be 1-100", "", "")
        return
    }
    
    // Fetch from Redis
    alerts, err := s.alerts.GetVandalismAlertsSince(ctx, time.Now().Add(-24*time.Hour), int64(limit))
    if err != nil {
        s.logger.Error().Err(err).Msg("Failed to get vandalism alerts")
        respondError(w, http.StatusInternalServerError, "Failed to fetch alerts", ErrCodeInternalError, "")
        return
    }
    
    // Filter by severity if specified
    if severity != "" {
        alerts = filterBySeverity(alerts, severity)
    }
    
    respondJSON(w, http.StatusOK, alerts)
}

func filterBySeverity(alerts []VandalismAlert, severity string) []VandalismAlert {
    filtered := make([]VandalismAlert, 0, len(alerts))
    for _, a := range alerts {
        if a.Severity == severity {
            filtered = append(filtered, a)
        }
    }
    return filtered
}
```

#### 2. Register route

**`internal/api/server.go`:**
```go
func (s *APIServer) setupRoutes() {
    // ... existing routes
    
    s.router.HandleFunc("GET /api/vandalism", s.handleGetVandalism)
}
```

#### 3. Add integration test

**`internal/api/integration_test.go`:**
```go
func TestGetVandalism(t *testing.T) {
    srv := setupTestServer(t)
    defer srv.Shutdown()
    
    // Seed test data
    seedVandalismAlerts(t, srv, 5)
    
    // Test default limit
    resp := testRequest(t, "GET", "/api/vandalism", nil)
    assert.Equal(t, 200, resp.StatusCode)
    
    var alerts []VandalismAlert
    json.NewDecoder(resp.Body).Decode(&alerts)
    assert.Len(t, alerts, 5)
    
    // Test severity filter
    resp = testRequest(t, "GET", "/api/vandalism?severity=high", nil)
    json.NewDecoder(resp.Body).Decode(&alerts)
    for _, a := range alerts {
        assert.Equal(t, "high", a.Severity)
    }
}
```

#### 4. Update OpenAPI spec

**`docs/openapi.yaml`:**
```yaml
paths:
  /api/vandalism:
    get:
      summary: Get recent vandalism events
      parameters:
        - name: limit
          in: query
          schema:
            type: integer
            minimum: 1
            maximum: 100
            default: 20
        - name: severity
          in: query
          schema:
            type: string
            enum: [low, medium, high, critical]
      responses:
        '200':
          description: List of vandalism events
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: '#/components/schemas/VandalismAlert'
```

#### 5. Add frontend integration

**`web/src/utils/api.ts`:**
```typescript
export const getVandalism = async (
  limit = 20,
  severity?: string
): Promise<VandalismAlert[]> => {
  const params: Record<string, unknown> = { limit };
  if (severity) params.severity = severity;
  
  const response = await api.get('/api/vandalism', { params });
  return response.data;
};
```

**`web/src/components/Vandalism/VandalismList.tsx`:**
```typescript
export function VandalismList() {
  const [alerts, setAlerts] = useState<VandalismAlert[]>([]);
  
  useEffect(() => {
    getVandalism(20).then(setAlerts);
  }, []);
  
  return (
    <div>
      {alerts.map(alert => (
        <VandalismCard key={alert.id} alert={alert} />
      ))}
    </div>
  );
}
```

---

### Adding a New Metric

**Use case:** Track API cache hit rate.

**Steps:**

#### 1. Define metric

**`internal/metrics/api_metrics.go`:**
```go
package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
    CacheHitsTotal = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "cache_hits_total",
            Help: "Total number of cache hits",
        },
    )
    
    CacheMissesTotal = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "cache_misses_total",
            Help: "Total number of cache misses",
        },
    )
)

func init() {
    prometheus.MustRegister(CacheHitsTotal, CacheMissesTotal)
}
```

#### 2. Instrument code

**`internal/api/cache.go`:**
```go
func (c *responseCache) Get(key string) ([]byte, bool) {
    c.mu.RLock()
    entry, ok := c.data[key]
    c.mu.RUnlock()
    
    if !ok || time.Now().After(entry.expiresAt) {
        metrics.CacheMissesTotal.Inc()
        return nil, false
    }
    
    metrics.CacheHitsTotal.Inc()
    return entry.data, true
}
```

#### 3. Add to dashboard

**Prometheus query:**
```promql
# Cache hit rate
rate(cache_hits_total[5m]) / (rate(cache_hits_total[5m]) + rate(cache_misses_total[5m]))
```

**Grafana panel:**
```json
{
  "title": "Cache Hit Rate",
  "targets": [{
    "expr": "rate(cache_hits_total[5m]) / (rate(cache_hits_total[5m]) + rate(cache_misses_total[5m]))"
  }],
  "type": "graph"
}
```

---

### Adding Dashboard Component

**Use case:** Add component to show vandalism events.

**Steps:**

#### 1. Create component file

**`web/src/components/Vandalism/VandalismList.tsx`:**
```typescript
import { useEffect, useState, memo } from 'react';
import { getVandalism } from '../../utils/api';
import type { VandalismAlert } from '../../types';

export const VandalismList = memo(function VandalismList() {
  const [alerts, setAlerts] = useState<VandalismAlert[]>([]);
  const [loading, setLoading] = useState(true);
  
  useEffect(() => {
    getVandalism(20)
      .then(setAlerts)
      .finally(() => setLoading(false));
  }, []);
  
  if (loading) {
    return <div className="loading">Loading vandalism events...</div>;
  }
  
  return (
    <div className="vandalism-list">
      <h2>Recent Vandalism</h2>
      {alerts.map(alert => (
        <VandalismCard key={alert.id} alert={alert} />
      ))}
    </div>
  );
});
```

#### 2. Add types

**`web/src/types/index.ts`:**
```typescript
export interface VandalismAlert {
  id: string;
  page_title: string;
  user: string;
  severity: 'low' | 'medium' | 'high' | 'critical';
  timestamp: string;
  reason: string;
}
```

#### 3. Add to main app

**`web/src/App.tsx`:**
```typescript
import { VandalismList } from './components/Vandalism/VandalismList';

function App() {
  return (
    <div>
      {/* ... other components */}
      <VandalismList />
    </div>
  );
}
```

#### 4. Add tests

**`web/src/components/Vandalism/VandalismList.test.tsx`:**
```typescript
import { render, screen, waitFor } from '@testing-library/react';
import { VandalismList } from './VandalismList';
import * as api from '../../utils/api';

jest.mock('../../utils/api');

test('renders vandalism alerts', async () => {
  const mockAlerts = [
    {
      id: '1',
      page_title: 'Test Page',
      user: 'Vandal',
      severity: 'high',
      timestamp: '2026-02-09T15:00:00Z',
      reason: 'Large deletion',
    },
  ];
  
  (api.getVandalism as jest.Mock).mockResolvedValue(mockAlerts);
  
  render(<VandalismList />);
  
  await waitFor(() => {
    expect(screen.getByText('Test Page')).toBeInTheDocument();
    expect(screen.getByText('Vandal')).toBeInTheDocument();
  });
});
```

---

## Testing

### Running Tests

**Go unit tests:**
```bash
# All tests
go test ./...

# Specific package
go test ./internal/processor

# With coverage
go test -cover ./...

# Coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Verbose
go test -v ./...

# Run specific test
go test -run TestSpikeDetector_ProcessEdit ./internal/processor
```

**Frontend tests:**
```bash
cd web

# All tests
npm test

# Watch mode
npm test -- --watch

# Coverage
npm test -- --coverage

# Specific file
npm test AlertsPanel.test.tsx
```

**Integration tests:**
```bash
# Requires running infrastructure
docker-compose up -d

# Run integration tests
go test -tags=integration ./test/integration/...

# Or use Makefile
make test-integration
```

**Load tests:**
```bash
# Using k6
k6 run test/load/api_load_test.js

# Using custom script
./scripts/load-test.sh
```

---

### Writing Tests

**Go unit tests:**

```go
func TestSpikeDetector_ProcessEdit(t *testing.T) {
    // Setup
    detector, mock := setupTestDetector(t)
    ctx := context.Background()
    
    tests := []struct {
        name    string
        edit    *models.WikipediaEdit
        want    bool
        wantErr bool
    }{
        {
            name: "detects spike",
            edit: &models.WikipediaEdit{
                Title: "Hot Page",
                // ... fields
            },
            want:    true,
            wantErr: false,
        },
        {
            name: "ignores cold page",
            edit: &models.WikipediaEdit{
                Title: "Cold Page",
            },
            want:    false,
            wantErr: false,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := detector.ProcessEdit(ctx, tt.edit)
            
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
            
            // Verify behavior
            if tt.want {
                assert.True(t, mock.AlertPublished())
            }
        })
    }
}
```

**React component tests:**

```typescript
describe('AlertsPanel', () => {
  it('renders alerts', () => {
    const alerts = [
      { id: '1', page_title: 'Test', severity: 'high', timestamp: '2026-02-09T15:00:00Z' },
    ];
    
    render(<AlertsPanel alerts={alerts} />);
    
    expect(screen.getByText('Test')).toBeInTheDocument();
    expect(screen.getByText('high')).toBeInTheDocument();
  });
  
  it('calls onDismiss when clicked', () => {
    const onDismiss = jest.fn();
    const alerts = [/* ... */];
    
    render(<AlertsPanel alerts={alerts} onDismiss={onDismiss} />);
    
    fireEvent.click(screen.getByText('Dismiss'));
    
    expect(onDismiss).toHaveBeenCalledWith(alerts[0]);
  });
});
```

### Test Coverage

**Target coverage:**
- Overall: >80%
- Critical paths: >90%
- New code: >85%

**Check coverage:**
```bash
# Go
go test -cover ./...

# Frontend
npm test -- --coverage

# View report
open coverage/lcov-report/index.html
```

---

## Contributing

### Branch Strategy

```
main (protected)
├── develop (integration branch)
│   ├── feature/vandalism-detection
│   ├── feature/enhanced-caching
│   ├── bugfix/memory-leak
│   └── hotfix/critical-crash
```

**Branch naming:**
- `feature/` - New features
- `bugfix/` - Bug fixes
- `hotfix/` - Critical production fixes
- `refactor/` - Code improvements
- `docs/` - Documentation updates

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <subject>

<body>

<footer>
```

**Types:**
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation
- `style`: Formatting, no code change
- `refactor`: Code restructuring
- `perf`: Performance improvement
- `test`: Adding tests
- `chore`: Build/tooling changes

**Examples:**
```
feat(processor): add vandalism detection

Implements vandalism detection based on edit patterns:
- Large content deletions
- Profanity detection
- Repeated reverts

Closes #123

---

fix(api): prevent websocket memory leak

Previously, disconnected clients were not properly cleaned up.
Now using cleanup goroutine with 5-minute timeout.

Fixes #456

---

docs: update deployment guide

Added Kubernetes deployment section with manifest examples.
```

### Pull Request Process

1. **Create branch from develop:**
```bash
git checkout develop
git pull origin develop
git checkout -b feature/my-feature
```

2. **Make changes and commit:**
```bash
git add .
git commit -m "feat(api): add new endpoint"
```

3. **Run tests locally:**
```bash
make test
make lint
```

4. **Push and create PR:**
```bash
git push origin feature/my-feature
```

5. **PR template:**
```markdown
## Description
Brief description of changes.

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation update

## Testing
- [ ] Unit tests pass
- [ ] Integration tests pass
- [ ] Manual testing completed

## Checklist
- [ ] Code follows style guide
- [ ] Comments added for complex logic
- [ ] Documentation updated
- [ ] No new warnings introduced
```

6. **Code review:**
- At least one approval required
- All tests must pass
- No merge conflicts

7. **Merge:**
```bash
git checkout develop
git merge --no-ff feature/my-feature
git push origin develop
```

### Code Review Guidelines

**Reviewers should check:**
- Correctness and logic
- Test coverage
- Performance implications
- Security concerns
- Documentation completeness
- Code style adherence

**Feedback should be:**
- Constructive
- Specific
- Actionable
- Respectful

**Example comments:**
```
✅ Good: "Consider using sync.Map here for better concurrent access performance"
❌ Bad: "This is wrong"

✅ Good: "Add error handling for this Redis call (line 45)"
❌ Bad: "Missing error handling"
```

---

For API details, see [API.md](API.md).
For deployment, see [DEPLOYMENT.md](DEPLOYMENT.md).
For operations, see [OPERATIONS.md](OPERATIONS.md).
