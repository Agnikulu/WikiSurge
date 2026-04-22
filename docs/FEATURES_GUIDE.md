# WikiSurge Features Guide

> **Companion to [ARCHITECTURE_GUIDE.md](ARCHITECTURE_GUIDE.md)**
>
> The architecture guide covers the core data pipeline — SSE ingestion, Kafka,
> Redis, Elasticsearch, and WebSockets. This document covers the four remaining
> pillars of WikiSurge:
>
> 1. **LLM-Powered Edit War Analysis** — how AI explains what editors are fighting about
> 2. **Deployment Infrastructure** — Docker, GitHub Actions, Hetzner, Coolify, Cloudflare
> 3. **User Authentication** — JWT tokens, password hashing, middleware
> 4. **Email Digest System** — Resend API, digest scheduling, personalized emails
>
> Same format as the architecture guide: every concept is explained from scratch
> with analogies, code references, and diagrams.

---

## Table of Contents

- [WikiSurge Features Guide](#wikisurge-features-guide)
  - [Table of Contents](#table-of-contents)
  - [1. LLM-Powered Edit War Analysis](#1-llm-powered-edit-war-analysis)
    - [1.1 Background: What Is an LLM?](#11-background-what-is-an-llm)
    - [1.2 The Problem: Edit Wars Are Confusing](#12-the-problem-edit-wars-are-confusing)
    - [1.3 Multi-Provider LLM Client](#13-multi-provider-llm-client)
    - [1.4 Fetching Diffs from Wikipedia](#14-fetching-diffs-from-wikipedia)
    - [1.5 The Analysis Pipeline](#15-the-analysis-pipeline)
    - [1.6 Prompt Engineering](#16-prompt-engineering)
    - [1.7 Parsing LLM Responses](#17-parsing-llm-responses)
    - [1.8 Heuristic Fallback (No LLM Mode)](#18-heuristic-fallback-no-llm-mode)
    - [1.9 Caching and Re-Analysis](#19-caching-and-re-analysis)
  - [2. Deployment Infrastructure](#2-deployment-infrastructure)
    - [2.1 Background: Containers and Docker](#21-background-containers-and-docker)
    - [2.2 Multi-Stage Docker Builds](#22-multi-stage-docker-builds)
    - [2.3 GitHub Actions CI/CD](#23-github-actions-cicd)
    - [2.4 Hetzner VPS](#24-hetzner-vps)
    - [2.5 Coolify — Self-Hosted PaaS](#25-coolify--self-hosted-paas)
    - [2.6 Cloudflare — DNS and CDN](#26-cloudflare--dns-and-cdn)
    - [2.7 Docker Compose in Production](#27-docker-compose-in-production)
    - [2.8 Memory Budgeting](#28-memory-budgeting)
    - [2.9 The Full Deployment Flow](#29-the-full-deployment-flow)
  - [3. User Authentication](#3-user-authentication)
    - [3.1 Background: Passwords, Hashing, and Tokens](#31-background-passwords-hashing-and-tokens)
    - [3.2 Password Hashing with bcrypt](#32-password-hashing-with-bcrypt)
    - [3.3 JWT Tokens](#33-jwt-tokens)
    - [3.4 The User Model](#34-the-user-model)
    - [3.5 Registration Flow](#35-registration-flow)
    - [3.6 Login Flow](#36-login-flow)
    - [3.7 Auth Middleware](#37-auth-middleware)
    - [3.8 Admin System](#38-admin-system)
  - [4. Email Digest System](#4-email-digest-system)
    - [4.1 Background: Transactional Email](#41-background-transactional-email)
    - [4.2 The Resend API](#42-the-resend-api)
    - [4.3 Sender Implementations](#43-sender-implementations)
    - [4.4 Digest Data Collection](#44-digest-data-collection)
    - [4.5 Personalization and Filtering](#45-personalization-and-filtering)
    - [4.6 HTML Email Rendering](#46-html-email-rendering)
    - [4.7 The Digest Scheduler](#47-the-digest-scheduler)
    - [4.8 Unsubscribe Flow](#48-unsubscribe-flow)
    - [4.9 How LLM Analysis Feeds Into Emails](#49-how-llm-analysis-feeds-into-emails)
  - [5. API Resilience \& Performance](#5-api-resilience--performance)
    - [5.1 Background: Why APIs Need Protection](#51-background-why-apis-need-protection)
    - [5.2 Circuit Breaker — Fail Fast, Recover Automatically](#52-circuit-breaker--fail-fast-recover-automatically)
    - [5.3 Graceful Degradation — Keep Core Features Alive](#53-graceful-degradation--keep-core-features-alive)
    - [5.4 Redis-Backed Sliding Window Rate Limiting](#54-redis-backed-sliding-window-rate-limiting)
    - [5.5 In-Memory Response Cache with TTL](#55-in-memory-response-cache-with-ttl)
    - [5.6 Object Pool Reuse (GC Optimization)](#56-object-pool-reuse-gc-optimization)
    - [5.7 The Full Request Lifecycle](#57-the-full-request-lifecycle)
  - [6. How Everything Connects](#6-how-everything-connects)
  - [7. Glossary](#7-glossary)

---

## 1. LLM-Powered Edit War Analysis

### 1.1 Background: What Is an LLM?

An **LLM (Large Language Model)** is a type of AI that has been trained on
enormous amounts of text. You give it a question or instruction (called a
**prompt**), and it generates a human-like text response. Examples include
OpenAI's GPT-4, Anthropic's Claude, and open-source models you can run locally
with Ollama.

**Analogy:** Think of an LLM as a very well-read assistant. It has "read"
billions of documents, so when you ask "Summarize what these Wikipedia editors
are arguing about," it can produce a coherent explanation — even though it has
never seen this specific edit war before.

Key terms:
- **Provider** — the company or tool hosting the LLM (OpenAI, Anthropic, Ollama)
- **Model** — the specific AI model to use (e.g. `gpt-4o-mini`, `claude-sonnet-4-20250514`)
- **Prompt** — the instruction you send to the LLM
- **Tokens** — the unit LLMs use to measure text length (roughly ¾ of a word)
- **Temperature** — controls randomness: 0 = deterministic, 1 = creative

### 1.2 The Problem: Edit Wars Are Confusing

WikiSurge's edit war detector (covered in the architecture guide) identifies
*that* an edit war is happening — editors rapidly reverting each other's changes.
But it doesn't explain *what they're fighting about*.

Looking at raw edit data, you'd see something like:

```
User:Alice   edited "Climate Change"  (comment: "rv vandalism")
User:Bob     edited "Climate Change"  (comment: "restored sourced content")
User:Alice   edited "Climate Change"  (comment: "removing pov")
User:Carol   edited "Climate Change"  (comment: "undo")
```

Who's right? What's the actual dispute about? That's where the LLM comes in.

### 1.3 Multi-Provider LLM Client

WikiSurge doesn't lock you into one AI provider. The LLM client supports three:

```
┌──────────────────────────────────────────────────┐
│               LLM Client (client.go)             │
│                                                  │
│  provider = "openai"  ──► OpenAI API (GPT-4o)    │
│  provider = "anthropic" ► Anthropic API (Claude)  │
│  provider = "ollama"  ──► Local Ollama (Llama 3)  │
└──────────────────────────────────────────────────┘
```

**Code:** `internal/llm/client.go`

The client is configured via YAML:

```yaml
llm:
  provider: "openai"          # Which AI provider
  api_key: "sk-..."           # API key (not needed for Ollama)
  model: "gpt-4o-mini"        # Which model to use
  max_tokens: 512             # Maximum response length
  temperature: 0.3            # Low = focused, high = creative
  timeout: 30s                # Give up after 30 seconds
```

**How provider selection works:** The `Complete()` method checks which provider
is configured and dispatches to the correct API:

```go
func (c *Client) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
    switch c.config.Provider {
    case "openai":
        return c.completeOpenAI(ctx, systemPrompt, userPrompt)
    case "anthropic":
        return c.completeAnthropic(ctx, systemPrompt, userPrompt)
    case "ollama":
        return c.completeOllama(ctx, systemPrompt, userPrompt)
    default:
        return c.completeOpenAI(ctx, systemPrompt, userPrompt)  // default fallback
    }
}
```

**Why `gpt-4o-mini` as default?** It's cheap (~$0.15 per million input tokens),
fast (~1-2 seconds), and smart enough for summarization tasks. We're not writing
novels — we're summarizing edit conflicts, so a smaller model works great.

**The `Enabled()` check:** Before calling the LLM, the system checks if it's
actually configured. Cloud providers need an API key; Ollama needs a base URL:

```go
func (c *Client) Enabled() bool {
    switch c.config.Provider {
    case "ollama":
        return c.config.BaseURL != ""
    default:
        return c.config.APIKey != ""
    }
}
```

This means WikiSurge works fine **without any LLM** — it falls back to
heuristic analysis (Section 1.8).

### 1.4 Fetching Diffs from Wikipedia

Before asking the LLM "what's the fight about?", we need to show it the actual
changes. WikiSurge fetches the text diffs that editors made.

**Code:** `internal/llm/diff_fetcher.go`

**What's a diff?** It's the difference between two versions of a page — what
was added, changed, or removed. Wikipedia's API provides this.

```
┌─────────────────┐        HTTP GET         ┌──────────────────────┐
│  WikiSurge      │ ─────────────────────► │  Wikipedia API        │
│  DiffFetcher    │                         │  (MediaWiki)          │
│                 │ ◄───────────────────── │                       │
│  Plain text     │     HTML diff response  │  action=query         │
│  diffs          │                         │  prop=revisions       │
└─────────────────┘                         │  rvdiffto=prev        │
                                            └──────────────────────┘
```

**The process:**

1. **Select revisions** — from the edit war timeline stored in Redis, pick up
   to `MaxDiffsToFetch = 8` revision IDs
2. **Batch API call** — send all revision IDs in one request using pipe-separated
   IDs: `revids=123|456|789`
3. **Parse response** — extract the diff HTML from each revision
4. **Strip HTML** — convert `<ins>added text</ins>` and `<del>removed text</del>`
   to plain text the LLM can read
5. **Truncate** — cap each diff at `MaxDiffChars = 800` characters

**Why limit to 8 diffs and 800 chars each?** LLMs have input limits (and cost
money per token). 8 diffs × 800 chars = ~6,400 characters max, which fits
comfortably in any model's context window while keeping costs low.

**Why strip HTML?** The Wikipedia API returns diffs as HTML tables with
`<ins>` and `<del>` tags. LLMs understand plain text better than HTML, and
plain text uses fewer tokens. The fetcher strips all HTML tags and cleans
up whitespace.

### 1.5 The Analysis Pipeline

**Code:** `internal/llm/analysis.go`

Here's the full flow when someone requests an analysis of an edit war:

```
┌─────────────┐     ┌────────────┐     ┌────────────┐     ┌────────────┐
│  API Request │────►│ Check      │────►│ Get Edit   │────►│ Fetch Diffs│
│  /api/edit-  │     │ Redis      │     │ War        │     │ from       │
│  wars/:title │     │ Cache      │     │ Timeline   │     │ Wikipedia  │
│  /analysis   │     │            │     │ from Redis │     │ API        │
└─────────────┘     └─────┬──────┘     └────────────┘     └─────┬──────┘
                      HIT │                                      │
                          ▼                                      ▼
                    Return cached              ┌────────────────────────┐
                    result                     │ Build Prompt           │
                    (instant)                  │ (system + user prompt) │
                                               └───────────┬────────────┘
                                                           │
                                                           ▼
                                               ┌────────────────────────┐
                                               │ Call LLM Provider      │
                                               │ (OpenAI/Anthropic/     │
                                               │  Ollama)               │
                                               └───────────┬────────────┘
                                                           │
                                                           ▼
                                               ┌────────────────────────┐
                                               │ Parse JSON Response    │
                                               │ Cache in Redis (1 hr)  │
                                               │ Return Analysis        │
                                               └────────────────────────┘
```

**The Analysis struct** — what the LLM produces:

```go
type Analysis struct {
    PageTitle      string    // "Climate Change"
    Summary        string    // "Editors are disputing the attribution of..."
    Sides          []Side    // Groups of editors and their positions
    ContentArea    string    // "Scientific consensus section"
    Severity       string    // "high" / "medium" / "low"
    Recommendation string    // "Semi-protection recommended"
    EditCount      int       // Number of edits in the war
    GeneratedAt    time.Time // When this analysis was created
    CacheHit       bool      // Was this served from cache?
}

type Side struct {
    Position string   // "Wants to include industry-funded studies"
    Editors  []Editor // Who is on this side
}

type Editor struct {
    User      string // "User:Alice"
    EditCount int    // 12
    Role      string // "primary aggressor" / "reverter" / "mediator"
}
```

### 1.6 Prompt Engineering

**What is prompt engineering?** It's the art of writing instructions that get
the best possible output from an LLM. Think of it like writing a very precise
job description.

WikiSurge uses a **two-part prompt**:

**System Prompt** (sets the LLM's role):
> You are a Wikipedia edit war analyst. Given a timeline of edits and text
> diffs from a Wikipedia page, analyze the edit war and provide a structured
> analysis.

The system prompt then specifies the exact JSON format the response must follow,
including all fields (summary, sides, content_area, severity, recommendation).

**User Prompt** (provides the specific data):
The user prompt is built dynamically from the edit war data:

```
Page: Climate Change
Period: 2024-01-15 to 2024-01-15
Total edits in conflict: 15

Timeline of edits:
  [14:23] User:Alice (comment: "rv vandalism") [+240 bytes]
  [14:25] User:Bob   (comment: "restored sourced content") [-240 bytes]
  ...

Text diffs (showing actual content changes):
  --- Edit by User:Alice at 14:23 ---
  Removed: "Studies suggest that industrial emissions account for..."
  Added: "The scientific consensus is that..."
  ...
```

**Why two prompts?** The system prompt is like a permanent instruction manual —
it never changes between analyses. The user prompt is the specific case the LLM
needs to analyze. This separation helps the LLM stay consistent across
different edit wars.

**Why JSON output?** We need structured data, not free-form text. By asking
for JSON in the system prompt and providing an example schema, the LLM returns
parseable output that WikiSurge can display in the UI and store in Redis.

### 1.7 Parsing LLM Responses

LLMs don't always return perfect JSON. They might:
- Wrap it in ```json ... ``` markdown blocks
- Include explanatory text before/after the JSON
- Truncate the response if it hits the token limit

**Code:** `internal/llm/analysis.go` — `parseAnalysisResponse()`

WikiSurge handles all of these:

```go
// 1. Try to find JSON inside markdown code blocks
if idx := strings.Index(response, "```json"); idx >= 0 {
    // Extract content between ```json and ```
}

// 2. Try to find raw JSON object
if idx := strings.Index(response, "{"); idx >= 0 {
    // Extract from first { to last }
}

// 3. If JSON is truncated (missing closing braces), try to repair it
// Count open/close braces and add missing ones
```

**Truncation repair** is particularly clever: if the LLM runs out of tokens
mid-response, the JSON might look like `{"summary": "...", "sides": [{"position": "..."`
— it's missing closing brackets. The parser counts open `{` and `[` characters,
then appends the needed `]` and `}` to make it valid JSON.

### 1.8 Heuristic Fallback (No LLM Mode)

If no LLM is configured (no API key, no Ollama), WikiSurge still provides
analysis — just simpler, rule-based analysis instead of AI-generated.

**Code:** `internal/llm/analysis.go` — `heuristicAnalysis()`

The heuristic approach:

1. **Byte pattern analysis** — looks at `+bytes` and `-bytes` in the timeline.
   Large additions followed by removals suggest content disputes.

2. **Comment scanning** — searches edit comments for keywords:
   - "revert", "rv", "undo" → revert activity
   - "vandal" → possible vandalism response
   - "pov", "bias", "neutral" → neutrality dispute
   - "source", "cite", "ref" → sourcing dispute

3. **Editor grouping** — editors who revert each other are placed on opposing
   sides. The one with more reverts is labeled the "primary reverter."

4. **Severity calculation**:
   - High: 15+ edits or 4+ editors
   - Medium: 8+ edits or 3+ editors
   - Low: everything else

The result mirrors the same `Analysis` struct, so downstream code (UI, emails,
cache) doesn't need to know whether an LLM or heuristics produced the analysis.

### 1.9 Caching and Re-Analysis

LLM calls are slow (~2-5 seconds) and cost money. WikiSurge caches analyses
in Redis:

```
┌──────────────────────────────────────────────────────┐
│                Redis Cache Keys                      │
│                                                      │
│  editwar:analysis:{page_title}                       │
│  ├── TTL: 1 hour (active edit war)                   │
│  └── TTL: 7 days (finalized edit war)                │
│                                                      │
│  editwar:timeline:{page_title}                       │
│  └── List of edits in the war (from detector)        │
└──────────────────────────────────────────────────────┘
```

**Active wars** get a 1-hour cache TTL. Why? Because new edits are still
coming in — the analysis might be outdated. After the hour, a new request
triggers a fresh analysis with the latest data.

**Finalized wars** (when the edit war detector marks a war as over — no new
conflicting edits for a while) get a 7-day cache TTL. The war is over, so
the analysis won't change.

**Re-analysis:** When a cached analysis exists but the edit count has grown
significantly (new edits since last analysis), `Reanalyze()` is called. It
fetches fresh diffs and asks the LLM again, ensuring the summary stays current
as the conflict evolves.

```
Time ──────────────────────────────────────────────────►

Edit war starts     Analysis cached     New edits arrive
      │                   │                   │
      ▼                   ▼                   ▼
  [detected]  ──►  [LLM analysis]  ──►  [re-analyze]
                   cache 1 hour          cache 1 hour
                                                │
                   No new edits for a while      │
                          │                      │
                          ▼                      │
                   [finalize]                    │
                   cache 7 days                  │
```

---

## 2. Deployment Infrastructure

### 2.1 Background: Containers and Docker

**What is a container?** Imagine shipping physical goods. Before containers
existed, every port had to figure out how to load and unload different shaped
cargo. Then someone invented the **shipping container** — a standard-sized box
that any crane, truck, or ship can handle. It doesn't matter what's inside.

**Docker containers** are the software equivalent. You package your application
with everything it needs (code, dependencies, config) into a standardized
"box." That box runs the same way on your laptop, on a test server, or in
production.

Key terms:
- **Image** — a snapshot/template of your container (like a class in OOP)
- **Container** — a running instance of an image (like an object)
- **Dockerfile** — the recipe for building an image
- **Registry** — where images are stored (like npm for packages, but for containers)
- **GHCR** — GitHub Container Registry, where WikiSurge stores its images

### 2.2 Multi-Stage Docker Builds

A naive Docker image for a Go app would include the Go compiler, source code,
all dependencies — easily 1+ GB. WikiSurge uses **multi-stage builds** to
produce tiny ~15 MB images.

**Code:** `deployments/Dockerfile.api`

```dockerfile
# STAGE 1: Build — has Go compiler, source code, everything
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache gcc musl-dev   # C compiler (needed for SQLite)
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download                     # Cache dependencies
COPY . .
RUN CGO_ENABLED=1 go build -o api ./cmd/api  # Compile

# STAGE 2: Run — just Alpine Linux + the compiled binary
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -g '' wikisurge          # Non-root user
RUN mkdir -p /data && chown wikisurge:wikisurge /data

COPY --from=builder /app/api /usr/local/bin/api  # Copy ONLY the binary

USER wikisurge
EXPOSE 8081
CMD ["api"]
```

**Why two stages?**

```
Stage 1 (builder):  ~800 MB           Stage 2 (final):  ~15 MB
┌──────────────────────┐               ┌──────────────────────┐
│ Go compiler          │               │ Alpine Linux (5 MB)  │
│ Source code           │               │ api binary (10 MB)   │
│ All dependencies      │    COPY       │ ca-certificates      │
│ gcc, musl-dev         │ ──────────►  │ tzdata               │
│ Compiled binary ◄─────│               │                      │
└──────────────────────┘               └──────────────────────┘
   (thrown away)                          (shipped to production)
```

The first stage exists only to compile the Go code. The second stage starts
from a clean Alpine Linux image and copies only the compiled binary. Everything
else — source code, compiler, build tools — is thrown away.

**CGO_ENABLED=1:** Most Go apps can compile as pure Go (`CGO_ENABLED=0`). But
WikiSurge uses `go-sqlite3`, which is a C library wrapped in Go. The C compiler
(`gcc`, `musl-dev`) is needed to compile it. This is why the API Dockerfile is
special — the other services (ingestor, processor) use simpler builds without CGO.

**Non-root user:** The `RUN adduser -D wikisurge` line creates a user with no
privileges. The container runs as this user instead of root. If an attacker ever
broke into the container, they'd have limited permissions.

### 2.3 GitHub Actions CI/CD

**What is CI/CD?**
- **CI (Continuous Integration)** — automatically build and test code when pushed
- **CD (Continuous Deployment)** — automatically deploy the built code to production

**Why build on GitHub, not on the server?**
WikiSurge runs on a small Hetzner VPS with limited RAM. Building Docker images
compiles Go code, which is CPU and memory intensive. Running `docker build` on
the VPS would:
- Eat up all available RAM (the VPS needs it for Kafka, Redis, etc.)
- Take 10+ minutes (GitHub's runners are faster)
- Potentially crash the server during builds

So WikiSurge builds images on GitHub's free CI runners, pushes the images to
GHCR, and the server just *downloads* and *runs* them.

**Code:** `.github/workflows/build-images.yml`

The workflow has three phases:

```
Phase 1: Detect Changes       Phase 2: Build Images        Phase 3: Deploy
┌─────────────────────┐       ┌─────────────────────┐      ┌──────────────┐
│ dorny/paths-filter   │       │ Build only what      │      │ curl Coolify │
│                      │       │ changed:             │      │ webhook      │
│ api changed? ────────│──────►│  ✓ api image         │─────►│              │
│ processor changed? ──│──────►│  ✓ processor image   │      │ Coolify      │
│ ingestor changed? ───│──────►│  ✗ (no changes)      │      │ pulls new    │
│ frontend changed? ───│──────►│  ✗ (no changes)      │      │ images and   │
└─────────────────────┘       └─────────────────────┘      │ redeploys    │
                                                            └──────────────┘
```

**Phase 1 — Smart change detection:**
Not every push touches every service. The `dorny/paths-filter` action checks
which files changed:

```yaml
filters:
  api:
    - 'internal/api/**'
    - 'internal/auth/**'
    - 'internal/storage/**'
    - 'cmd/api/**'
    - 'go.mod'
  processor:
    - 'internal/processor/**'
    - 'internal/kafka/**'
    - 'cmd/processor/**'
```

If you only changed a frontend file, only the frontend image gets rebuilt.
This saves CI minutes and deployment time.

**Phase 2 — Build and push:**
Each service that changed gets its own build job:

```yaml
- uses: docker/build-push-action@v5
  with:
    context: .
    file: deployments/Dockerfile.api
    push: true
    tags: ghcr.io/agnikulu/wikisurge-api:latest
    platforms: linux/amd64        # API is amd64-only (CGO/SQLite)
    cache-from: type=gha          # GitHub Actions cache
    cache-to: type=gha,mode=max
```

The `cache-from: type=gha` line is important — it uses GitHub Actions' built-in
cache to store Docker layers. If only one Go file changed, Docker reuses cached
layers for "download dependencies" and "copy source" and only re-runs the
"compile" step. This cuts build time from ~5 minutes to ~1 minute.

**Platform note:** The API service builds only for `linux/amd64` because CGO
(C code for SQLite) makes cross-compilation complex. The other services
(processor, ingestor, frontend) build for both `linux/amd64` and `linux/arm64`.

**Phase 3 — Trigger Coolify:**
After all builds complete, a single `curl` command hits Coolify's webhook:

```yaml
- run: |
    curl -X GET "${{ secrets.COOLIFY_WEBHOOK_URL }}" \
      -H "Authorization: Bearer ${{ secrets.COOLIFY_TOKEN }}"
```

### 2.4 Hetzner VPS

**Hetzner** is a German cloud provider known for excellent price-to-performance
ratio. WikiSurge runs on a single VPS (Virtual Private Server) — essentially a
rented computer in a data center.

**Why Hetzner over AWS/GCP/Azure?**
- A comparable AWS EC2 instance costs 3-5x more
- For a side project, you don't need AWS's vast ecosystem
- Simple: one server, one bill, no complex IAM or networking

The VPS runs all WikiSurge containers via Docker Compose. It's like having one
powerful computer that runs 7+ programs simultaneously — Kafka, Redis,
Elasticsearch, the API, the processor, the ingestor, and the frontend.

### 2.5 Coolify — Self-Hosted PaaS

**What is a PaaS?** Platform as a Service — a system that handles deployment,
scaling, SSL certificates, and monitoring so you don't have to SSH into a
server and run commands manually. Heroku is the famous example, but it's
expensive.

**Coolify** is a free, self-hosted alternative to Heroku. It runs on your own
server (the Hetzner VPS) and provides:

- **Automatic Docker deployment** — pull images and restart containers
- **SSL certificates** — automatic HTTPS via Let's Encrypt
- **Webhook triggers** — a URL that, when hit, triggers a redeploy
- **Environment variables** — manage secrets without editing files on the server
- **Reverse proxy** — routes `yourdomain.com` to the right container via Traefik

**How WikiSurge uses Coolify:**
Coolify manages the Docker Compose stack. When GitHub Actions hits the webhook,
Coolify:
1. Pulls the latest images from GHCR
2. Stops the old containers
3. Starts new containers with the updated images
4. Runs health checks to verify the new containers are working

If a new container fails to start, Coolify keeps the old one running.

### 2.6 Cloudflare — DNS and CDN

**DNS** (Domain Name System) translates `wikisurge.com` to an IP address like
`65.109.x.x`. **Cloudflare** provides DNS plus additional features:

```
User in Tokyo                    Cloudflare Edge           Hetzner (Germany)
┌──────────┐     HTTPS      ┌──────────────────┐    HTTPS    ┌────────────┐
│ Browser   │ ─────────────►│ Cloudflare CDN    │ ──────────►│ WikiSurge  │
│           │               │                    │            │ Server     │
│           │ ◄─────────────│ • DDoS protection  │ ◄──────────│            │
│ (fast!)   │  Cached resp  │ • SSL termination  │  Response  │            │
└──────────┘               │ • Cache static     │            └────────────┘
                            │ • Compress (gzip)  │
                            └──────────────────┘
```

**What Cloudflare provides for WikiSurge:**
- **CDN** — caches static files (JS, CSS, images) at edge servers worldwide, so
  a user in Tokyo doesn't wait for a response from Germany
- **DDoS protection** — filters malicious traffic before it reaches the server
- **SSL termination** — handles HTTPS encryption/decryption, reducing server load
- **Proxy mode** — hides the real server IP address for security

### 2.7 Docker Compose in Production

**Code:** `deployments/docker-compose.prod.yml`

All 7 services are defined in a single Docker Compose file:

```yaml
services:
  kafka:        # Redpanda (Kafka-compatible), 400 MB limit
  redis:        # Redis with 256 MB maxmemory, 150 MB container limit
  elasticsearch: # ES 8.12.0, 2 GB limit
  ingestor:     # SSE → Kafka, 100 MB limit
  processor:    # Kafka → Redis/ES, 150 MB limit
  api:          # HTTP/WebSocket server, 100 MB limit
  frontend:     # Nginx serving React app, 50 MB limit
```

**Key production settings:**

1. **Pre-built images** — each service pulls from GHCR instead of building locally:
   ```yaml
   api:
     image: ghcr.io/agnikulu/wikisurge-api:latest
   ```

2. **Memory limits** — every container has a hard memory cap:
   ```yaml
   deploy:
     resources:
       limits:
         memory: 100M
   ```

3. **Environment files** — secrets are loaded from `.env` files, not baked into
   images:
   ```yaml
   env_file:
     - .env.api
   ```

4. **Volumes** — the API's SQLite database persists across container restarts:
   ```yaml
   volumes:
     - api-data:/data
   ```

5. **Internal network** — containers communicate on a private Docker network.
   Only the API (port 8081) and frontend (port 3000) are exposed externally.

6. **Traefik labels** — Coolify uses these to route incoming HTTP requests to
   the right container.

### 2.8 Memory Budgeting

The Hetzner VPS has limited RAM. Every megabyte matters. WikiSurge uses Go-
specific memory tuning:

```yaml
environment:
  - GOGC=50              # Run garbage collection more often
  - GOMEMLIMIT=80MiB     # Hard limit Go's memory usage
```

**GOGC=50** — Go's garbage collector normally runs when heap grows to 100% of
the previous size. Setting GOGC=50 means it runs at 50% growth — more frequent
GC, less peak memory.

**GOMEMLIMIT=80MiB** — hard cap on Go's memory. If the program approaches this
limit, Go aggressively garbage-collects. This prevents a Go process from slowly
eating all available RAM.

**Total memory budget:**

```
Elasticsearch:   2,048 MB  (the hungriest service — indexing is memory-intensive)
Kafka/Redpanda:    400 MB  (message broker — needs memory for log segments)
Redis:             256 MB  (in-memory data store — configured with maxmemory)
Processor:         150 MB  (Go service — most complex processing)
API:               100 MB  (Go service — handles HTTP + WebSocket)
Ingestor:          100 MB  (Go service — SSE parsing + Kafka production)
Frontend:           50 MB  (Nginx — just serving static files)
─────────────────────────
Total:          ~3,104 MB  + OS overhead ≈ needs ~4 GB VPS
```

### 2.9 The Full Deployment Flow

Putting it all together — what happens when you `git push` to main:

```
Developer                GitHub Actions              GHCR                 Hetzner VPS
┌──────┐  git push   ┌───────────────┐  push    ┌──────────┐  pull   ┌──────────────┐
│ Code │ ──────────►│ 1. Detect     │ images  │ Container│ images │ 4. Coolify   │
│ change│            │    changes    │ ───────►│ Registry │ ──────►│    pulls new │
└──────┘            │ 2. Build only │          │ (GHCR)   │        │    images    │
                     │    affected   │          └──────────┘        │ 5. Stop old  │
                     │    services   │                              │    containers│
                     │ 3. Curl       │─── webhook ────────────────►│ 6. Start new │
                     │    Coolify    │                              │    containers│
                     └───────────────┘                              │ 7. Health    │
                                                                    │    checks    │
                                                                    └──────────────┘
```

1. **You push code** to the `main` branch on GitHub
2. **GitHub Actions** detects which services were affected by your changes
3. **Only affected services** get rebuilt as Docker images (~1-2 min each)
4. **Images are pushed** to GitHub Container Registry (GHCR)
5. **Coolify webhook is triggered** — the VPS knows to update
6. **Coolify pulls** the new images from GHCR (fast download, ~15 MB each)
7. **Old containers stop, new containers start** — zero-downtime if using
   rolling updates
8. **Health checks** verify the new containers are working

The entire process takes about 3-5 minutes from push to live deployment.

---

## 3. User Authentication

### 3.1 Background: Passwords, Hashing, and Tokens

**The problem:** Users need accounts to save watchlists and receive digest
emails. But storing passwords is dangerous — if the database leaks, every
user's password is exposed.

**Three core concepts:**

1. **Password hashing** — a one-way mathematical function that turns a password
   into gibberish. You can convert "mypassword123" → "$2a$10$xJ3k..." but you
   can NEVER reverse it back. To check a login, you hash the attempt and compare
   hashes.

2. **JWT (JSON Web Token)** — a signed "ticket" the server gives you after login.
   You include this ticket in every request to prove you're authenticated,
   without sending your password again.

3. **Middleware** — code that runs before your request reaches the actual handler.
   Auth middleware checks your JWT ticket before letting the request through.

**Analogy:** Think of a concert:
- **Registration** = buying a ticket (you prove your identity once)
- **Password hash** = the ticket office shreds your ID after verifying it
- **JWT token** = the wristband they give you
- **Middleware** = the bouncer checking wristbands at every door

### 3.2 Password Hashing with bcrypt

**Code:** `internal/auth/auth.go`

WikiSurge uses **bcrypt**, which is specifically designed for password hashing.
Unlike SHA-256 (which is fast), bcrypt is intentionally *slow* — it takes ~100ms
to hash a password. This means an attacker can only try ~10 passwords per second
instead of billions.

```go
func HashPassword(password string) (string, error) {
    bytes, err := bcrypt.GenerateFromPassword(
        []byte(password),
        bcrypt.DefaultCost,  // Cost factor 10 = 2^10 = 1024 iterations
    )
    return string(bytes), err
}

func CheckPassword(password, hash string) bool {
    err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
    return err == nil
}
```

**What `bcrypt.DefaultCost` means:** bcrypt has a "cost" parameter. Cost 10
means 2^10 = 1,024 iterations of the internal hash function. Each increment
doubles the time:

```
Cost 10:  ~100 ms   (default — good balance)
Cost 12:  ~400 ms   (more secure, but slower logins)
Cost 14:  ~1,600 ms (too slow for interactive use)
```

**The bcrypt output** looks like: `$2a$10$xJ3kQ...` where:
- `$2a$` = bcrypt version
- `10$` = cost factor
- Rest = salt + hash (the salt is embedded in the output, so you don't store it separately)

### 3.3 JWT Tokens

**Code:** `internal/auth/auth.go`

**What's inside a JWT?** Three parts separated by dots: `header.payload.signature`

```
eyJhbGciOiJIUzI1NiJ9.eyJ1c2VyX2lkIjoiYWJjMTIzIn0.HMAC_signature
│                     │                                │
│  Header:            │  Payload (claims):             │  Signature:
│  {"alg":"HS256"}    │  {"user_id": "abc123",         │  HMAC-SHA256(
│                     │   "email": "user@test.com",    │    header + payload,
│                     │   "is_admin": false,           │    secret_key
│                     │   "exp": 1706140800}           │  )
```

**How WikiSurge creates JWTs:**

```go
type JWTService struct {
    secretKey []byte        // The secret used to sign tokens
    expiry    time.Duration // How long tokens last (default: 24 hours)
}

func (s *JWTService) GenerateToken(userID, email string, isAdmin bool) (string, error) {
    claims := Claims{
        UserID:  userID,
        Email:   email,
        IsAdmin: isAdmin,
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.expiry)),
            IssuedAt:  jwt.NewNumericDate(time.Now()),
        },
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString(s.secretKey)
}
```

**HMAC-SHA256 signing:** The signature is created using a secret key that only
the server knows. When the server receives a JWT back, it re-computes the
signature. If the token was tampered with (e.g., someone changed `is_admin`
from `false` to `true`), the signature won't match, and the request is rejected.

**Token extraction:** Tokens are sent in the HTTP `Authorization` header:
```
Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJ1c2...
```

The `ExtractTokenFromRequest()` function strips the "Bearer " prefix and returns
the raw token string for validation.

### 3.4 The User Model

**Code:** `internal/models/user.go`

```go
type User struct {
    ID             string          `json:"id"`
    Email          string          `json:"email"`
    PasswordHash   string          `json:"-"`   // NEVER exposed in JSON responses
    Watchlist      []string        `json:"watchlist"`
    DigestFreq     DigestFrequency `json:"digest_frequency"`    // none/daily/weekly/both
    DigestContent  DigestContent   `json:"digest_content"`      // both/watchlist/global
    SpikeThreshold float64         `json:"spike_threshold"`     // only email if activity > Nx
    UnsubToken     string          `json:"-"`                   // one-click unsubscribe
    Verified       bool            `json:"verified"`
    IsAdmin        bool            `json:"is_admin"`
    CreatedAt      time.Time       `json:"created_at"`
    LastDigestAt   time.Time       `json:"last_digest_at"`
}
```

**The `json:"-"` tag:** The `PasswordHash` and `UnsubToken` fields have
`json:"-"`, which means Go's JSON encoder **skips them entirely**. Even if you
accidentally return a User object in an API response, the password hash is never
sent to the client. It's a safety net.

**Where are users stored?** SQLite — a file-based database that lives at
`/data/wikisurge.db` inside the API container. It's stored on a Docker volume
(`api-data:/data`), so it persists across container restarts and redeployments.

**Why SQLite instead of PostgreSQL?**
- Zero setup — no separate database server to manage
- Perfect for hundreds or even thousands of users
- Single file for the whole database — easy to backup
- The Docker volume ensures data survives redeployments

### 3.5 Registration Flow

**Code:** `internal/api/user_handlers.go` — `handleRegister()`

```
Client                         API Server                    SQLite
┌──────┐   POST /api/register  ┌──────────────────┐         ┌───────┐
│      │  {email, password}    │                  │         │       │
│      │ ─────────────────────►│ 1. Validate      │         │       │
│      │                       │    - email format │         │       │
│      │                       │    - password ≥8  │         │       │
│      │                       │                   │         │       │
│      │                       │ 2. Check if email │ SELECT  │       │
│      │                       │    already exists │ ───────►│       │
│      │                       │                   │ ◄───────│       │
│      │                       │                   │         │       │
│      │                       │ 3. bcrypt hash    │         │       │
│      │                       │    the password   │         │       │
│      │                       │                   │         │       │
│      │                       │ 4. Generate UUID  │         │       │
│      │                       │    + unsub token  │         │       │
│      │                       │                   │         │       │
│      │                       │ 5. Create user    │ INSERT  │       │
│      │                       │                   │ ───────►│       │
│      │                       │                   │         │       │
│      │                       │ 6. Auto-verify    │         │       │
│      │                       │    (no email      │         │       │
│      │                       │     verification) │         │       │
│      │                       │                   │         │       │
│      │                       │ 7. Check admin    │         │       │
│      │                       │    email match    │         │       │
│      │                       │                   │         │       │
│      │                       │ 8. Generate JWT   │         │       │
│      │   {token, user}       │                   │         │       │
│      │ ◄─────────────────────│                   │         │       │
└──────┘                       └──────────────────┘         └───────┘
```

**Step by step:**

1. **Validate input** — email must be valid format, password must be ≥ 8 characters
2. **Check duplicates** — look up the email in SQLite; reject if already registered
3. **Hash password** — `bcrypt.GenerateFromPassword(password, cost=10)`
4. **Generate IDs** — UUID v4 for user ID, another UUID for the unsubscribe token
5. **Create user** — insert into SQLite with default preferences:
   - `DigestFreq = "daily"` — send daily digests by default
   - `DigestContent = "both"` — include watchlist + global content
   - `SpikeThreshold = 2.0` — only email about 2x-normal activity
6. **Auto-verify** — WikiSurge skips email verification (sets `Verified = true`
   immediately). This simplifies onboarding.
7. **Admin auto-promotion** — if the registering email matches the `admin_email`
   in the config file, the user is automatically promoted to admin
8. **Return JWT** — the client immediately has a token to use for authenticated
   requests

### 3.6 Login Flow

**Code:** `internal/api/user_handlers.go` — `handleLogin()`

```go
// Simplified flow:
user, err := s.userStore.GetByEmail(ctx, req.Email)   // 1. Find user
if err != nil {
    return 401  // "Invalid credentials" (don't reveal if email exists)
}

if !auth.CheckPassword(req.Password, user.PasswordHash) {  // 2. Compare hashes
    return 401  // Same generic error
}

token, _ := s.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)  // 3. Issue JWT
```

**Security note:** Both "email not found" and "wrong password" return the same
generic "Invalid credentials" error. This prevents **user enumeration** — an
attacker can't figure out which emails are registered by trying different emails
and seeing different error messages.

### 3.7 Auth Middleware

**Code:** `internal/auth/middleware.go`

Middleware sits between the HTTP router and your handler. It intercepts every
request to protected routes:

```
Incoming request
       │
       ▼
┌──────────────────┐  No token   ┌──────────────┐
│ Auth Middleware   │ ──────────►│ 401 Unauth.  │
│                  │             └──────────────┘
│ 1. Extract JWT   │
│    from header   │  Invalid    ┌──────────────┐
│ 2. Validate      │ ──────────►│ 401 Unauth.  │
│    signature     │             └──────────────┘
│ 3. Check expiry  │
│ 4. Inject user   │  Expired    ┌──────────────┐
│    info into ctx │ ──────────►│ 401 Unauth.  │
│                  │             └──────────────┘
│                  │
│       OK ────────│─────────────────────────────►  Handler runs
└──────────────────┘                                 (has user info
                                                      in context)
```

If all checks pass, the middleware injects user info into Go's `context.Context`:

```go
ctx = context.WithValue(ctx, userIDKey, claims.UserID)
ctx = context.WithValue(ctx, emailKey, claims.Email)
ctx = context.WithValue(ctx, isAdminKey, claims.IsAdmin)
```

Any downstream handler can then retrieve this info:

```go
userID := auth.UserIDFromContext(r.Context())  // "abc-123-..."
email  := auth.EmailFromContext(r.Context())   // "user@test.com"
admin  := auth.IsAdminFromContext(r.Context()) // false
```

**Admin middleware** adds one more check — after validating the JWT, it checks
`claims.IsAdmin`. If false, it returns HTTP 403 (Forbidden) instead of 401
(Unauthorized). The distinction:
- **401** = "I don't know who you are" (missing/invalid token)
- **403** = "I know who you are, but you can't do this" (not an admin)

### 3.8 Admin System

WikiSurge has a simple admin system:

**Auto-promotion:** When a user registers with the email specified in the config
file's `admin_email` field, they're automatically flagged as admin:

```yaml
# configs/config.prod.yaml
api:
  admin_email: "agnik@example.com"
```

**Admin-only endpoints:**
- `GET /api/admin/users` — list all registered users
- `DELETE /api/admin/users/:id` — delete a user (can't delete yourself)

These routes use the `AdminMiddleware`, so non-admin users get a 403 response.

---

## 4. Email Digest System

### 4.1 Background: Transactional Email

**What is transactional email?** It's email sent by an application in response
to a user action or event — password resets, order confirmations, digest reports.
It's different from marketing email (newsletters, promotions).

**Why can't you just use Gmail's SMTP?** You technically can (and WikiSurge
supports it), but:
- Gmail limits you to ~500 emails/day
- Emails often land in spam because Gmail's servers aren't "authorized" to send
  from your domain
- No delivery tracking — you don't know if emails were received

**Dedicated email APIs** like Resend, SendGrid, or Mailgun solve these:
- They're authorized to send email (proper SPF/DKIM records)
- High deliverability — emails land in inbox, not spam
- Tracking — you can see opens, bounces, complaints
- Generous free tiers (Resend: 3,000 emails/month free)

### 4.2 The Resend API

**Code:** `internal/email/sender.go`

WikiSurge uses **Resend** as its primary email provider. Resend is a modern email
API — you send a single HTTP POST request, and they handle the actual email
delivery.

```
WikiSurge API                     Resend                      User's Inbox
┌──────────────┐   POST /emails   ┌──────────────┐   SMTP     ┌───────────┐
│ ResendSender │ ────────────────►│ Resend API   │ ─────────►│ Gmail/    │
│              │                   │              │           │ Outlook/  │
│  {from, to,  │   200 OK +       │ • SPF/DKIM   │           │ etc.      │
│   subject,   │   message_id     │ • Delivery   │           │           │
│   html}      │ ◄────────────────│ • Tracking   │           │           │
└──────────────┘                   └──────────────┘           └───────────┘
```

**The actual API call:**

```go
func (s *ResendSender) Send(ctx context.Context, to, subject, htmlBody string) error {
    payload := map[string]string{
        "from":    s.from,      // "WikiSurge <digest@yourdomain.com>"
        "to":      to,          // "user@gmail.com"
        "subject": subject,     // "🔥 WikiSurge Daily Digest — Jan 15"
        "html":    htmlBody,    // Full HTML email
    }

    body, _ := json.Marshal(payload)
    req, _ := http.NewRequestWithContext(ctx, "POST",
        "https://api.resend.com/emails", bytes.NewReader(body))

    req.Header.Set("Authorization", "Bearer "+s.apiKey)  // Resend API key
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    // ... check for errors, non-200 status codes
}
```

That's it. One HTTP POST with a JSON body containing `from`, `to`, `subject`,
and `html`. Resend handles everything else — connecting to the recipient's mail
server, retrying on temporary failures, signing the email with DKIM.

**Configuration:**

```yaml
email:
  provider: "resend"                          # or "smtp" or "log"
  from: "WikiSurge <digest@yourdomain.com>"
  resend_api_key: "re_xxxxxxxxxxxxx"          # from resend.com dashboard
```

### 4.3 Sender Implementations

WikiSurge has three email sender implementations behind the same `Sender`
interface. This is Go's version of polymorphism — any code that needs to send
email just calls `sender.Send()` without knowing which implementation is active.

```go
type Sender interface {
    Send(ctx context.Context, to, subject, htmlBody string) error
}
```

**1. ResendSender** — production email via Resend API (explained above)

**2. SMTPSender** — traditional SMTP for self-hosted email or Gmail:

```go
type SMTPSender struct {
    host     string  // "smtp.gmail.com"
    port     string  // "587"
    username string  // "you@gmail.com"
    password string  // App password (not your real password)
    from     string  // "WikiSurge <you@gmail.com>"
}
```

SMTP is the original email protocol from 1982. It still works everywhere. The
SMTPSender connects to an SMTP server, authenticates, and sends the email.
Useful if you want to use Gmail, Mailgun, or your company's mail server.

**3. LogSender** — development only, sends nothing:

```go
func (s *LogSender) Send(ctx context.Context, to, subject, htmlBody string) error {
    s.logger.Info().
        Str("to", to).
        Str("subject", subject).
        Int("html_len", len(htmlBody)).
        Msg("would send email (log mode)")
    return nil
}
```

During development, you don't want to actually send emails. LogSender logs what
*would* be sent — the recipient, subject, and HTML length — then returns
success. You see the output in your terminal logs.

**The factory pattern:** The correct sender is created based on config:

```
config.email.provider = "resend"  →  NewResendSender(apiKey, from)
config.email.provider = "smtp"    →  NewSMTPSender(host, port, user, pass, from)
config.email.provider = "log"     →  NewLogSender(logger)         (default)
```

### 4.4 Digest Data Collection

**Code:** `internal/digest/collector.go`

Before an email can be sent, we need to gather what happened. The `Collector`
pulls data from multiple Redis sources:

```
┌──────────────────────────────────────────────────────────────────┐
│                    Collector.CollectGlobal()                      │
│                                                                  │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐         │
│  │ RedisAlerts  │    │ Trending    │    │ StatsTracker│         │
│  │ (edit wars)  │    │ Scorer      │    │ (counters)  │         │
│  │              │    │ (top pages) │    │             │         │
│  └──────┬──────┘    └──────┬──────┘    └──────┬──────┘         │
│         │                  │                   │                │
│         ▼                  ▼                   ▼                │
│  ┌─────────────────────────────────────────────────────┐       │
│  │                    DigestData                        │       │
│  │                                                      │       │
│  │  GlobalHighlights:  top 10 events (wars + trending)  │       │
│  │  EditWarHighlights: edit wars with LLM analysis      │       │
│  │  TrendingHighlights: trending pages                  │       │
│  │  Stats: {TotalEdits, TopLanguages, EditWars}         │       │
│  └─────────────────────────────────────────────────────┘       │
│                              │                                  │
│                              ▼                                  │
│                    enrichEditWars()                              │
│                    ├── Fetch cached LLM analysis from Redis     │
│                    ├── Extract editors from analysis             │
│                    ├── Count reverts from change lists           │
│                    └── Update summaries with LLM text            │
└──────────────────────────────────────────────────────────────────┘
```

**Global collection (once per digest run):**

1. **Edit war alerts** — fetch from `editwars` Redis Stream, deduplicate by title
2. **Trending pages** — top 20 from TrendingScorer, filtered to the digest period
3. **Merge and rank** — edit wars first (more important), then trending by edit count
4. **Cap at 10** — nobody wants 50 items in a digest email
5. **Enrich edit wars** — pull cached LLM analyses from Redis keys like
   `editwar:analysis:Climate_Change`, extracting the AI-generated summary,
   severity, content area, and involved editors
6. **Fun stats** — total edits in the period, top 5 languages, number of edit wars

### 4.5 Personalization and Filtering

**Code:** `internal/digest/collector.go`

Each user gets a personalized version of the digest based on their preferences:

**PersonalizeForUser()** — adds watchlist events specific to this user:

```go
func (c *Collector) PersonalizeForUser(ctx context.Context, global *DigestData, user *User) *DigestData {
    personalized := *global  // copy the global data
    personalized.WatchlistEvents = c.collectWatchlistEvents(ctx, user, global)
    return &personalized
}
```

For each page on the user's watchlist, the collector checks:
- Did it appear in global highlights? (Notable — edit war or trending)
- How many edits did it receive in the period? (From per-page counters)
- Is it "notable"? (>10 edits = active, in global highlights = definitely notable)

**ShouldSendToUser()** — decides if a digest is worth sending:

```go
func (c *Collector) ShouldSendToUser(data *DigestData, user *User) bool {
    // User wants global content → send if there are any highlights
    if user.DigestContent == "global" || user.DigestContent == "both" {
        if len(data.GlobalHighlights) > 0 { return true }
    }
    // User wants watchlist content → send if any watchlist event exceeds threshold
    if user.DigestContent == "watchlist" || user.DigestContent == "both" {
        for _, ev := range data.WatchlistEvents {
            if ev.IsNotable && ev.SpikeRatio >= user.SpikeThreshold { return true }
        }
    }
    return false  // Nothing interesting — don't bother the user
}
```

This respects user preferences:
- `DigestContent = "global"` — only global highlights, no watchlist
- `DigestContent = "watchlist"` — only their watched pages
- `DigestContent = "both"` — everything
- `SpikeThreshold = 2.0` — only notify if activity is 2x normal (filters noise)

### 4.6 HTML Email Rendering

**Code:** `internal/digest/render.go`

Email clients (Gmail, Outlook, Apple Mail) have terrible CSS support. They're
stuck in 2005 — no flexbox, no grid, limited color support. WikiSurge uses
Go's `html/template` package with inline styles:

```go
func RenderDigestEmail(data *DigestData, user *User, dashboardURL, unsubToken string) (subject, htmlBody string, err error) {
    // 1. Build template data with display settings
    td := DigestEmailData{
        UserEmail:         user.Email,
        Period:            "Daily",           // or "Weekly"
        ShowWatchlist:     true,              // based on user.DigestContent
        ShowGlobal:        true,
        ShowEditWars:      len(editWarHighlights) > 0,
        ShowTrending:      len(trendingHighlights) > 0,
        DashboardURL:      dashboardURL,
        UnsubscribeURL:    dashboardURL + "/api/digest/unsubscribe?token=" + unsubToken,
        Year:              time.Now().Year(), // for the copyright footer
    }

    // 2. Parse and execute Go HTML template
    tmpl, _ := template.New("digest").Funcs(TemplateFuncs()).Parse(digestTemplate)
    var buf bytes.Buffer
    tmpl.Execute(&buf, td)

    return subject, buf.String(), nil
}
```

**Subject line** is dynamically generated with highlights:

```
"🔥 WikiSurge Daily Digest — 2 edit wars, 5 trending pages"
"📊 WikiSurge Weekly Digest — Jan 8 – Jan 15, 2024"
```

**The email structure:**

```
┌──────────────────────────────────────────────┐
│  WikiSurge Header + Logo                     │
├──────────────────────────────────────────────┤
│  📊 Fun Stats                                │
│  "12,847 edits across 45 languages"          │
│  Top languages: English (34%), German (12%)  │
├──────────────────────────────────────────────┤
│  ⚔️ Edit Wars (if any)                       │
│  1. Climate Change — "Dispute over..."       │
│     Severity: HIGH | 4 editors | 23 edits    │
│  2. ...                                      │
├──────────────────────────────────────────────┤
│  🔥 Trending Pages                           │
│  1. Taylor Swift — 89 edits (trending)       │
│  2. ...                                      │
├──────────────────────────────────────────────┤
│  👀 Your Watchlist (personalized)            │
│  ★ JavaScript — edit war detected!           │
│  • React — 5 edits (quiet)                   │
│  • Go — no recent activity                   │
├──────────────────────────────────────────────┤
│  Footer: Unsubscribe link | Dashboard link   │
└──────────────────────────────────────────────┘
```

### 4.7 The Digest Scheduler

**Code:** `internal/digest/scheduler.go`

The scheduler runs as a background goroutine inside the API server. It checks
every minute whether it's time to send digests.

```
┌──────────────────────────────────────────────────────────────┐
│                    Scheduler Loop                             │
│                                                              │
│  Every minute:                                               │
│   ├── Is it daily send time (default: 09:00 UTC)?            │
│   │   └── Yes → runDigest("daily")                           │
│   └── Is it weekly send time (default: Monday 09:00 UTC)?    │
│       └── Yes → runDigest("weekly")                          │
│                                                              │
│  runDigest(period):                                          │
│   1. collector.CollectGlobal(period)                         │
│   │   └── Gathers highlights, edit wars, trending, stats     │
│   │                                                          │
│   2. userStore.GetUsersForDigest(period)                     │
│   │   └── Find users with digest_frequency = period or both  │
│   │                                                          │
│   3. Worker pool (10 concurrent workers)                     │
│      ├── Worker 1: processUser(user1, globalData)            │
│      ├── Worker 2: processUser(user2, globalData)            │
│      ├── ...                                                 │
│      └── Worker 10: processUser(user10, globalData)          │
│                                                              │
│  processUser(user, globalData):                              │
│   1. PersonalizeForUser(globalData, user)                    │
│   2. ShouldSendToUser(personalizedData, user) → skip if no   │
│   3. RenderDigestEmail(data, user, dashURL, unsubToken)      │
│   4. sender.Send(user.Email, subject, htmlBody)              │
│   5. MarkDigestSent(user) → update LastDigestAt              │
└──────────────────────────────────────────────────────────────┘
```

**Why a worker pool?** If there are 200 users, sending emails one at a time
would take a while (each Resend API call takes ~200ms). With 10 concurrent
workers, it's 20x faster through parallelism:

```go
// Worker pool pattern
sem := make(chan struct{}, s.maxWorkers)  // Semaphore with 10 slots

for _, user := range users {
    sem <- struct{}{}       // Acquire a slot (blocks if all 10 are busy)
    go func(u *models.User) {
        defer func() { <-sem }()  // Release the slot when done
        s.processUser(ctx, u, globalData)
    }(user)
}
```

**Manual trigger:** `RunNow()` allows sending a digest immediately, useful for
testing or one-off sends from the admin panel.

### 4.8 Unsubscribe Flow

**Code:** `internal/api/user_handlers.go` — `handleUnsubscribe()`

Email laws (CAN-SPAM, GDPR) require a way to unsubscribe. WikiSurge uses
token-based unsubscription:

```
Email footer:
  "Don't want these? Unsubscribe: https://wikisurge.com/api/digest/unsubscribe?token=abc123"
                                                                                    │
                                                                                    │
                              ┌─────────────────────────────────────────────────────┘
                              │
                              ▼
                    GET /api/digest/unsubscribe?token=abc123
                              │
                    1. Look up user by unsub token
                    2. Set digest_frequency = "none"
                    3. Return HTML page: "You've been unsubscribed ✓"
```

**Why a token instead of requiring login?** Email clients don't have your JWT.
The unsubscribe link must work with a single click — no login required. Each
user has a unique, random `UnsubToken` (UUID) generated at registration. It's
like a secret password that only appears in their emails.

**No authentication needed** — the unsubscribe endpoint is public. The random
UUID token provides enough security because:
- UUIDs are 128 bits of randomness — practically impossible to guess
- Even if guessed, unsubscribing someone is annoying but not harmful
- It's better to let people unsubscribe easily than to risk spam complaints

### 4.9 How LLM Analysis Feeds Into Emails

The LLM analysis system (Section 1) and the email system are connected through
Redis. Here's how they work together:

```
Edit War Detector              LLM Analysis              Digest Collector
┌───────────────┐         ┌───────────────────┐        ┌───────────────────┐
│ Detects edit   │────────►│ Analyzes conflict │────────►│ Reads cached     │
│ war on page    │         │ via LLM prompt    │        │ analysis from    │
│                │         │                   │        │ Redis            │
│ Stores in      │         │ Caches result in  │        │                  │
│ Redis Stream   │         │ Redis:            │        │ Enriches email:  │
│ editwars       │         │ editwar:analysis: │        │ • LLM summary    │
│                │         │ {title}           │        │ • Severity       │
└───────────────┘         └───────────────────┘        │ • Editor sides   │
                                                        │ • Content area   │
                                                        └───────────────────┘
                                                                  │
                                                                  ▼
                                                        Digest email shows:
                                                        "⚔️ Climate Change
                                                         Editors are disputing
                                                         the inclusion of
                                                         industry-funded studies
                                                         in the scientific
                                                         consensus section.
                                                         Severity: HIGH"
```

Without LLM analysis, the email would just say "Edit war detected on Climate
Change (15 edits, 4 editors)." With it, users get a human-readable explanation
of what the fight is actually about.

---

## 5. API Resilience & Performance

> The [Architecture Guide](ARCHITECTURE_GUIDE.md) covers the data pipeline
> (SSE → Kafka → Redis → WebSocket). This section covers the five patterns that
> keep the **API layer** fast and reliable under load, failures, and abuse.

### 5.1 Background: Why APIs Need Protection

WikiSurge's API sits between the data pipeline and every browser dashboard in
the world. It faces three categories of problems:

| Problem | What happens without protection |
|---------|-------------------------------|
| **Dependency failure** | Elasticsearch goes down → every search request hangs for 30s → all goroutines blocked → entire API unresponsive |
| **Traffic spikes** | News event → 10× more users → database overwhelmed → everyone gets slow responses |
| **Abuse / bugs** | A script hammers `/api/search` 10,000 times/sec → Elasticsearch melts |

The five patterns below work together as layered defenses. Each one addresses
a specific failure mode, and they compose into a pipeline that every request
passes through.

### 5.2 Circuit Breaker — Fail Fast, Recover Automatically

**Code:** `internal/resilience/circuit_breaker.go`

**The pattern:** When calls to an external service (Elasticsearch, Redis) start
failing repeatedly, stop trying and fail immediately. This prevents the failure
from cascading to your own system.

**Three states:**

```
CLOSED (normal)                  OPEN (tripped)               HALF-OPEN (testing)
─────────────────               ────────────────             ──────────────────
Requests flow through.          All requests rejected         Allow 1 probe request.
Count consecutive failures.     instantly (no waiting).       Success? → CLOSED
5 failures → trip to OPEN.      After 30s → HALF-OPEN.       Failure? → back to OPEN
Any success resets count.
```

**WikiSurge implementation details:**

The `CircuitBreaker` struct wraps any fallible call with `Call()`:

```go
err := esBreaker.Call(func() error {
    return elasticsearch.Search(query)
})

if err == resilience.ErrCircuitOpen {
    // Service is known-bad — return cached/fallback data instead
    return fallbackResponse()
}
```

Key design decisions in the code:
- **Thread-safe:** All state is protected by a `sync.Mutex` — safe for concurrent goroutines
- **State transition callbacks:** `OnStateChange()` lets the degradation manager react
  when a breaker trips (e.g., disable a feature automatically)
- **Registry pattern:** `CircuitBreakerRegistry` manages named breakers so you
  can have one for Elasticsearch, one for external APIs, etc.
- **Metrics:** Each breaker exports Prometheus gauges/counters:
  - `circuit_breaker_state{breaker="es"}` — 0=closed, 1=open, 2=half-open
  - `circuit_breaker_failures_total` — tracks failure streak
  - `circuit_breaker_rejections_total` — how many fast-fails saved

**Configuration:**

| Setting | Default | Purpose |
|---------|---------|---------|
| `FailureThreshold` | 5 | Consecutive failures before tripping |
| `ResetTimeout` | 30s | How long to wait before probing |
| `HalfOpenMaxCalls` | 1 | Probe requests allowed in half-open |

### 5.3 Graceful Degradation — Keep Core Features Alive

**Code:** `internal/resilience/degradation.go`

**The idea:** When an infrastructure component fails, disable the features that
depend on it while keeping everything else running. The system automatically
adjusts — no human intervention needed at 3 AM.

**Three levels:**

| Level | Name | Meaning | Health endpoint returns |
|-------|------|---------|----------------------|
| 0 | None | All components healthy | `"status": "healthy"` |
| 1 | Partial | 1 component unhealthy | `"status": "degraded"` |
| 2 | Severe | 2+ components unhealthy | `"status": "critical"` |

**Three concrete scenarios with code-level detail:**

**Scenario 1 — Elasticsearch goes down:**
```go
dm.HandleElasticsearchUnavailable("connection refused")
// What happens internally:
// 1. Mark ES component as unhealthy
// 2. features.DisableFeature(FeatureElasticsearchIndexing)
//    → Processor stops sending docs to ES
//    → /api/search returns "service temporarily unavailable"
// 3. Recalculate level: 1 unhealthy → DegradationPartial
// ALL OTHER FEATURES CONTINUE: trending, alerts, live feed, edit wars
```

**Scenario 2 — Redis memory pressure:**
```go
// Stage A: Memory > 80%
dm.HandleRedisMemoryLimit(100)  // reduce hot pages from 1000 → 100

// Stage B: Memory still climbing > 95%
dm.HandleRedisMemoryCritical()  // disable trending tracking entirely
// Core edit forwarding + alerts still work
```

**Scenario 3 — High Kafka consumer lag:**
```go
dm.HandleHighKafkaLag()
// Pauses ES indexing to let processor catch up on real-time alerts
// When lag recovers:
dm.HandleKafkaLagRecovered()
// Re-enables ES indexing automatically
```

**The audit trail** — every action is recorded:

```go
type DegradationAction struct {
    Timestamp time.Time  // When it happened
    Component string     // "elasticsearch", "redis", "kafka"
    Action    string     // "disabled indexing", "reduced hot page limit"
    Reason    string     // "connection refused", "memory pressure"
}
```

The last 50 actions are kept in memory and returned by the `/health` endpoint.
This is invaluable for debugging: "At 03:14 AM, the system reduced hot page
limits because Redis was at 85% memory. At 03:22 AM, it recovered."

### 5.4 Redis-Backed Sliding Window Rate Limiting

**Code:** `internal/api/rate_limiter.go`

**The algorithm:** For each client IP + endpoint combination, maintain a Redis
Sorted Set of timestamped request entries. Before allowing a new request, count
how many entries fall within the last 60 seconds.

**Why Redis Sorted Sets?** They support efficient range queries by score
(timestamp). `ZREMRANGEBYSCORE` removes old entries in O(log N + M), and `ZCARD`
counts remaining entries in O(1).

**Per-endpoint limits (hardcoded in code):**

```go
rl.limits = map[string]int{
    "/api/search":    100,   // Elasticsearch queries are expensive
    "/api/trending":  500,   // Redis reads, moderate cost
    "/api/stats":     1000,  // Lightweight computation
    "/api/alerts":    500,   // Redis reads
    "/api/edit-wars": 500,   // Redis reads
}
```

**The sliding window operation (Redis pipeline):**

```
Step 1: ZREMRANGEBYSCORE ratelimit:/api/search:1.2.3.4  -inf  {60s ago}
        → Removes expired entries

Step 2: ZCARD ratelimit:/api/search:1.2.3.4
        → Returns count of requests in current window

Step 3: If count < limit:
          ZADD ratelimit:/api/search:1.2.3.4  {now_ns}  {uuid}
          EXPIRE ratelimit:/api/search:1.2.3.4  70s
```

The `EXPIRE 70s` (slightly longer than the 60s window) is a safety net — if
the cleanup in Step 1 doesn't run for some reason, Redis will eventually
delete the entire key automatically.

**Client IP detection (security-aware):**

```go
func getClientIP(r *http.Request) string {
    // 1. X-Forwarded-For header (from reverse proxy)
    // 2. X-Real-IP header (Nginx convention)
    // 3. r.RemoteAddr (direct connection)
}
```

**Whitelist support:** Trusted IPs bypass rate limiting entirely. Useful for
health checkers, internal monitoring systems, and load balancers:

```go
func (rl *RateLimiter) isWhitelisted(ipStr string) bool {
    // Check exact IP matches
    // Check CIDR ranges (e.g., "10.0.0.0/8" for internal network)
}
```

**Fail-open design:** If Redis is unreachable, the rate limiter **allows the
request** and logs a warning. This is a deliberate choice — it's better to
temporarily lose rate limiting than to lock out all users because of a Redis
blip.

**429 response format:**

```json
{
    "error": "Rate limit exceeded",
    "code": "RATE_LIMIT",
    "limit": 100,
    "remaining": 0,
    "reset_at": "2026-04-12T10:31:00Z"
}
```

Plus headers: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`,
and `Retry-After`.

### 5.5 In-Memory Response Cache with TTL

**Code:** `internal/api/cache.go`

**The idea:** Store serialized API responses in a Go map. If the same request
arrives again before the TTL expires, return the cached bytes directly —
skipping all handler logic, database queries, and JSON serialization.

**Why not just use Redis caching?** Two reasons:
1. **Speed:** In-memory access is ~100 nanoseconds. Redis round-trip is ~500 microseconds.
   For endpoints called hundreds of times per second, this 5000× speedup matters.
2. **Reduced Redis load:** The trending endpoint alone could generate thousands
   of Redis queries per second. Caching at the API layer shields Redis.

**Implementation:**

```go
type responseCache struct {
    mu      sync.RWMutex           // Reader-writer lock (many readers, one writer)
    entries map[string]*cacheEntry // key → {data []byte, expiresAt time.Time}
    stopCh  chan struct{}           // Signals cleanup goroutine to exit
}
```

**TTLs per response type:**

| Type | TTL | Rationale |
|------|-----|-----------|
| Alerts | 5s | Near-real-time expectation |
| Search results | 10s | Expensive to compute, slight delay acceptable |
| Geo activity | 30s | Geographic aggregation changes slowly |

**The 10,000 entry cap:**

```go
const responseCacheMaxEntries = 10_000

func (c *responseCache) Set(key string, data []byte, ttl time.Duration) {
    if len(c.entries) >= responseCacheMaxEntries {
        return  // Don't cache — existing entries expire naturally
    }
    c.entries[key] = &cacheEntry{data: data, expiresAt: time.Now().Add(ttl)}
}
```

This is a simpler strategy than LRU eviction — when full, stop caching until
entries expire. With short TTLs (5-30s) and a 30-second cleanup interval,
the cache rarely stays full for long.

**Cache key generation:** SHA-256 hash of request parameters, truncated to 32
hex characters. Identical requests always produce the same key:

```go
func cacheKey(parts ...string) string {
    h := sha256.New()
    for _, p := range parts {
        h.Write([]byte(p))
        h.Write([]byte("|"))  // Separator prevents "ab"+"c" == "a"+"bc"
    }
    return fmt.Sprintf("%x", h.Sum(nil))[:32]
}
```

**Cache hit/miss visibility:** Responses include `X-Cache: HIT` or
`X-Cache: MISS` headers so developers can verify caching is working.

### 5.6 Object Pool Reuse (GC Optimization)

**Code:** `internal/api/optimizations.go`

**Background: Go's garbage collector (GC):**

Go automatically frees memory that's no longer in use. But this has a cost:
the GC must periodically scan all live objects to identify garbage. More
allocations → more scanning → higher tail latency (those occasional slow
requests that show up in p99 metrics).

**The solution:** Reuse objects instead of creating new ones. Go provides
`sync.Pool` — a thread-safe object pool that integrates with the GC.

**Pool 1: Buffer Pool (JSON serialization)**

```go
var bufferPool = sync.Pool{
    New: func() interface{} { return new(bytes.Buffer) },
}

func getBuffer() *bytes.Buffer {
    buf := bufferPool.Get().(*bytes.Buffer)
    buf.Reset()  // Clear previous content
    return buf
}

func putBuffer(buf *bytes.Buffer) {
    if buf.Cap() > 1<<20 { return }  // Don't pool >1MB buffers
    bufferPool.Put(buf)
}
```

Every API response requires JSON serialization into a byte buffer. Without
pooling, each of the 1000+ requests/second allocates and discards a buffer.
With pooling, roughly 50-100 buffers are reused indefinitely.

The 1 MB cap prevents memory waste: if an unusually large response grows a
buffer to 10 MB, returning it to the pool would waste 10 MB of idle RAM.
Better to let the GC reclaim it and allocate a fresh small buffer next time.

**Pool 2: Trending Slice Pool**

```go
var trendingResponsePool = sync.Pool{
    New: func() interface{} {
        s := make([]TrendingPageResponse, 0, 100)
        return &s
    },
}
```

The `/api/trending` endpoint is the most frequently called. Each call returns
a slice of up to 100 trending pages. Pre-allocating capacity-100 slices
avoids the repeated grow-copy cycle that happens when `append()` outgrows
the backing array.

**Pool 3: Language Cache**

```go
var languageCache sync.Map
const languageCacheMaxSize = 100_000
```

A `sync.Map` (lock-free concurrent map) caches extracted language codes from
page titles. Since the same pages appear repeatedly (hot/trending pages), this
eliminates redundant string parsing. When the cache exceeds 100K entries, all
entries are cleared atomically — language codes are trivial to recompute.

**Optimized JSON encoding:**

```go
func respondJSONPooled(...) {
    buf := getBuffer()            // 1. Borrow buffer from pool
    defer putBuffer(buf)          // 4. Return buffer when done

    enc := json.NewEncoder(buf)
    enc.SetEscapeHTML(false)      // 2. Skip HTML escaping (5-10% faster)
    enc.Encode(data)              // 3. Write JSON directly to buffer

    w.Write(buf.Bytes())          // 5. Send to client
}
```

`SetEscapeHTML(false)` disables the default escaping of `<`, `>`, `&`
characters. Since API responses are consumed as JSON (not rendered as HTML),
this escaping is unnecessary overhead.

### 5.7 The Full Request Lifecycle

Here's every protection layer a single API request passes through:

```
Browser: GET /api/trending?limit=20
                │
                ▼
        ┌───────────────────────────────────────┐
   1.   │  Rate Limiter Middleware               │
        │  ZREMRANGEBYSCORE + ZCARD on Redis     │
        │  /api/trending limit = 500/min         │
        │  → Remaining: 347, allowed             │
        └───────────────────┬───────────────────┘
                            │
                            ▼
        ┌───────────────────────────────────────┐
   2.   │  Response Cache Lookup                 │
        │  cacheKey("trending", "limit=20")      │
        │  → SHA-256 → check in-memory map       │
        │  → MISS (expired 3s ago)               │
        └───────────────────┬───────────────────┘
                            │
                            ▼
        ┌───────────────────────────────────────┐
   3.   │  Handler: handleGetTrending()          │
        │  trendingSlice := getTrendingSlice()   │ ← Borrow from pool
        │  defer putTrendingSlice(trendingSlice)  │ ← Return when done
        └───────────────────┬───────────────────┘
                            │
                            ▼
        ┌───────────────────────────────────────┐
   4.   │  Storage Layer (Redis read)            │
        │  circuitBreaker.Call(func() {          │
        │      trending.GetTopTrending(20)       │
        │  })                                    │
        │  → Breaker CLOSED, call proceeds       │
        └───────────────────┬───────────────────┘
                            │
                            ▼
        ┌───────────────────────────────────────┐
   5.   │  JSON Serialization (pooled)           │
        │  buf := getBuffer()                    │ ← Borrow from pool
        │  json.NewEncoder(buf).Encode(results)  │
        │  cache.Set(key, buf.Bytes(), 10s)      │ ← Cache for next time
        │  w.Write(buf.Bytes())                  │
        │  putBuffer(buf)                        │ ← Return to pool
        └───────────────────────────────────────┘
                            │
                            ▼
        200 OK + X-Cache: MISS
        X-RateLimit-Remaining: 346
```

**Next request (within 10s):** Hits the cache at step 2, returns in
~100 nanoseconds. Steps 3-5 are skipped entirely.

---

## 6. How Everything Connects

Here's the full system with all four features integrated:

```
Wikipedia SSE Stream
       │
       ▼
┌──────────────┐     ┌───────┐     ┌──────────────────────────────────────┐
│   Ingestor    │────►│ Kafka │────►│              Processor               │
└──────────────┘     └───────┘     │                                      │
                                    │  Spike Detector ──► Redis Alerts     │
                                    │  Trending Scorer ─► Redis Trending   │
                                    │  Edit War Detector ► Redis Timeline  │
                                    │  ES Indexer ──────► Elasticsearch    │
                                    │  WS Forwarder ───► Redis Pub/Sub    │
                                    └───────────────┬──────────────────────┘
                                                    │
                        ┌───────────────────────────┼───────────────────┐
                        │                           │                   │
                        ▼                           ▼                   ▼
              ┌──────────────────┐      ┌────────────────┐    ┌─────────────┐
              │   LLM Analysis    │      │  API Server     │    │ WebSocket   │
              │   Service         │      │                 │    │ Server      │
              │                   │      │ Auth Middleware  │    │             │
              │ OpenAI/Anthropic/ │      │ User CRUD       │    │ /ws/feed    │
              │ Ollama            │      │ Preferences     │    │ /ws/alerts  │
              │                   │      │ Admin panel     │    │             │
              │ Analyzes edit     │      │                 │    │ Live to     │
              │ wars via prompts  │      │ SQLite (users)  │    │ browsers    │
              └────────┬─────────┘      └────────┬────────┘    └─────────────┘
                       │                         │
                       │    Redis Cache           │
                       └──────────┬──────────────┘
                                  │
                                  ▼
                        ┌──────────────────┐
                        │  Digest Scheduler │
                        │                   │
                        │  Daily/Weekly     │
                        │  Collects data    │─────► Resend API ────► User inbox
                        │  Personalizes     │
                        │  Renders HTML     │
                        └──────────────────┘
```

**Data flows:**
1. **Real-time path:** Wikipedia → Ingestor → Kafka → Processor → Redis → WebSocket → Browser
2. **Analysis path:** Edit war detected → LLM analyzes → Cache in Redis → Shown in UI + emails
3. **Auth path:** Register/Login → bcrypt + JWT → Middleware validates → Protected endpoints
4. **Email path:** Scheduler triggers → Collect from Redis → Personalize per user → Resend → Inbox

---

## 7. Glossary

| Term | Definition |
|------|-----------|
| **bcrypt** | A password hashing algorithm that is intentionally slow, making brute-force attacks impractical. Uses a salt (random data) to ensure identical passwords produce different hashes. |
| **CDN** | Content Delivery Network — a global network of servers that cache and serve static files close to users, reducing latency. |
| **CGO** | Go's mechanism for calling C code. Required for `go-sqlite3` because SQLite is a C library. |
| **CI/CD** | Continuous Integration / Continuous Deployment — automatically building, testing, and deploying code when changes are pushed. |
| **Cloudflare** | A service providing DNS, CDN, DDoS protection, and SSL termination. Acts as a proxy between users and your server. |
| **Coolify** | A self-hosted Platform-as-a-Service (like Heroku but free). Manages Docker deployments, SSL certificates, and reverse proxying on your own server. |
| **Diff** | The difference between two versions of a document — what was added, changed, or removed. |
| **Docker** | A tool for packaging applications into containers — standardized, isolated environments that run the same way everywhere. |
| **Docker Compose** | A tool for defining and running multi-container Docker applications using a YAML file. |
| **GHCR** | GitHub Container Registry — a Docker image registry hosted by GitHub, where WikiSurge stores its pre-built images. |
| **GOGC** | A Go environment variable controlling garbage collection frequency. Lower values = more frequent GC = lower memory usage. |
| **GOMEMLIMIT** | A Go environment variable setting a soft memory limit. Go aggressively garbage-collects when approaching this limit. |
| **Hetzner** | A German cloud provider offering affordable VPS (Virtual Private Server) hosting with excellent performance. |
| **Heuristic** | A rule-based approach (if-then logic) as opposed to AI/ML. WikiSurge uses heuristics as a fallback when no LLM is configured. |
| **HMAC-SHA256** | A cryptographic method for creating a digital signature using a secret key. Used to sign JWT tokens so they can't be forged. |
| **JWT** | JSON Web Token — a signed, base64-encoded token containing user identity claims. Sent in HTTP headers to authenticate requests. |
| **LLM** | Large Language Model — an AI trained on text data that can generate human-like responses to prompts. |
| **MediaWiki API** | Wikipedia's API for querying page data, revision history, and diffs. Used to fetch actual text changes for LLM analysis. |
| **Middleware** | Code that runs between receiving an HTTP request and handling it. Auth middleware checks tokens; logging middleware records requests. |
| **Multi-stage build** | A Docker technique using multiple `FROM` statements. Build tools exist only in early stages; the final image contains only the compiled binary. |
| **Prompt engineering** | The practice of crafting instructions for an LLM to get optimal results. Includes system prompts (role/rules) and user prompts (specific data). |
| **Resend** | A modern transactional email API. Send emails via HTTP POST with JSON. Free tier: 3,000 emails/month. |
| **SMTP** | Simple Mail Transfer Protocol — the original email protocol (1982). Still used for sending email between servers. |
| **SQLite** | A file-based relational database. The entire database is a single file — no server process needed. Perfect for small-to-medium applications. |
| **Temperature** | An LLM parameter controlling randomness. 0.0 = always pick the most likely word. 1.0 = more creative/random. WikiSurge uses 0.3 (focused). |
| **Token (LLM)** | The unit LLMs use to measure text. Roughly ¾ of an English word. Costs are measured per token. |
| **Token (JWT)** | A signed string proving user identity. Not related to LLM tokens despite sharing the name. |
| **Traefik** | A reverse proxy and load balancer that Coolify uses to route incoming HTTP requests to the correct Docker container. |
| **Transactional email** | Email sent by an application in response to user actions or events (password reset, digest reports), as opposed to marketing email. |
| **Unsubscribe token** | A random UUID embedded in email links. Allows one-click unsubscription without requiring login. |
| **UUID** | Universally Unique Identifier — a 128-bit random string (e.g., `550e8400-e29b-41d4-a716-446655440000`). Practically impossible to guess or collide. |
| **Worker pool** | A pattern where a fixed number of goroutines process tasks from a queue. Limits concurrency to prevent overwhelming external services. |
| **Circuit breaker** | A resilience pattern that stops calling a failing service after repeated errors, preventing cascade failures. Three states: closed (normal), open (rejecting), half-open (probing). |
| **Graceful degradation** | Automatically disabling non-essential features when components fail, keeping core functionality alive without human intervention. |
| **Sliding window** | A rate limiting technique that counts requests in a rolling time period (e.g., "the last 60 seconds") rather than fixed calendar intervals. Prevents burst abuse at window boundaries. |
| **sync.Pool** | Go's built-in thread-safe object pool. Objects are borrowed with `Get()` and returned with `Put()`. The GC may reclaim idle pool entries. |
| **Fail-open** | A design choice where, if a safety mechanism fails (e.g., Redis unreachable), requests are allowed through rather than blocked. Prioritizes availability over strictness. |
| **TTL (cache)** | Time To Live for cached responses. After the TTL expires, the next request triggers a fresh computation. Short TTLs (5-30s) balance freshness and performance. |
| **GC pressure** | The load placed on Go's garbage collector by frequent object allocation and deallocation. Reduced by reusing objects via pools. |

