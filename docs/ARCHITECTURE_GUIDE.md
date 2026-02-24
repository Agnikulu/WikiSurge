# WikiSurge — Architecture Guide (Beginner-Friendly)

> **Who is this for?** Anyone learning about real-time data pipelines, message queues, in-memory databases, WebSockets, or search engines for the first time. Every technology is explained from scratch before showing how WikiSurge uses it.

---

## Table of Contents

1. [What Does WikiSurge Do?](#1-what-does-wikisurge-do)
2. [The Big Picture — Data Flow](#2-the-big-picture--data-flow)
3. [Background Concepts You Need First](#3-background-concepts-you-need-first)
4. [Step 1 — Wikipedia SSE → Ingestor](#4-step-1--wikipedia-sse--ingestor)
5. [Step 2 — Kafka: The Message Queue](#5-step-2--kafka-the-message-queue)
6. [Step 3 — The Processor: Five Parallel Consumer Groups](#6-step-3--the-processor-five-parallel-consumer-groups)
7. [Step 4 — Elasticsearch: Search & History](#7-step-4--elasticsearch-search--history)
8. [Step 5 — Redis: Live State & Alerts](#8-step-5--redis-live-state--alerts)
9. [Step 6 — WebSocket `/ws/feed`: Live Edit Stream](#9-step-6--websocket-wsfeed-live-edit-stream)
10. [Step 7 — WebSocket `/ws/alerts`: Spike & Edit War Alerts](#10-step-7--websocket-wsalerts-spike--edit-war-alerts)
11. [End-to-End: Tracing One Edit Through the Entire System](#11-end-to-end-tracing-one-edit-through-the-entire-system)
12. [Glossary](#12-glossary)

---

## 1. What Does WikiSurge Do?

Wikipedia is one of the busiest websites on the planet. Thousands of people edit articles every minute — fixing typos, adding citations, sometimes even getting into heated arguments (called "edit wars") over controversial topics.

**WikiSurge watches all of those edits in real-time** and answers three questions:

| Question | Feature |
|----------|---------|
| "Which pages are suddenly getting a LOT of edits right now?" | **Spike Detection** — e.g., a celebrity just died, so their Wikipedia page is getting 20× more edits than usual |
| "Which pages are the most interesting today?" | **Trending Pages** — pages with the highest recent activity, weighted by edit size and recency |
| "Are people fighting over an article?" | **Edit War Detection** — multiple editors repeatedly undoing each other's changes |

The system streams these insights to a **browser dashboard** in real-time using WebSockets — you see edits and alerts appear the instant they happen, no page refreshing needed.

---

## 2. The Big Picture — Data Flow

Here's the full pipeline, end to end:

```
 ┌──────────────────────────┐
 │   Wikipedia SSE Stream   │  ← Wikipedia broadcasts every edit as it happens
 └────────────┬─────────────┘
              │
              ▼
 ┌──────────────────────────┐
 │        INGESTOR          │  ← Our Go service connects, validates, filters
 └────────────┬─────────────┘
              │  Produces JSON messages
              ▼
 ┌──────────────────────────┐
 │     KAFKA (Redpanda)     │  ← Durable message queue — holds edits safely
 │   topic: wikipedia.edits │
 └────────────┬─────────────┘
              │  Five consumer groups read independently
              ├──────────────────┬──────────────────┬──────────────────┬──────────────────┐
              ▼                  ▼                  ▼                  ▼                  ▼
     ┌────────────┐    ┌────────────────┐  ┌────────────────┐  ┌────────────┐   ┌────────────┐
     │   Spike    │    │   Trending     │  │   Edit War     │  │ Elastic-   │   │ WebSocket  │
     │  Detector  │    │  Aggregator    │  │   Detector     │  │ search     │   │ Forwarder  │
     └─────┬──────┘    └───────┬────────┘  └───────┬────────┘  │ Indexer    │   └─────┬──────┘
           │                   │                   │           └─────┬──────┘         │
           │                   │                   │                 │                 │
           ▼                   ▼                   ▼                 ▼                 ▼
 ┌──────────────────────────────────────────────────────────────────────────────────────────┐
 │                                    REDIS (in-memory)                                     │
 │  • Spike alerts → Redis Streams    • Trending scores → Sorted Sets                      │
 │  • Edit war alerts → Redis Streams • Live edits → Pub/Sub channel                       │
 │  • Hot page tracking → Sorted Sets + Hashes                                             │
 └───────────────────────────────────────┬──────────────────────────────────────────────────┘
                                         │                            │
                   ┌─────────────────────┘                            │
                   ▼                                                  ▼
          ┌─────────────────┐                               ┌─────────────────┐
          │   API SERVER    │ ← Reads from Redis + ES       │ ELASTICSEARCH   │
          │   (Go, HTTP)    │                               │  (search index) │
          └────────┬────────┘                               └─────────────────┘
                   │
        ┌──────────┼──────────┐
        ▼                     ▼
 ┌─────────────┐      ┌─────────────┐
 │  /ws/feed   │      │ /ws/alerts  │  ← Browser connects via WebSocket
 │ (live edits)│      │(spike/wars) │
 └─────────────┘      └─────────────┘
        │                     │
        ▼                     ▼
 ┌──────────────────────────────────┐
 │         BROWSER DASHBOARD        │
 │   (React + Tailwind frontend)    │
 └──────────────────────────────────┘
```

**The key idea**: data flows in one direction — from Wikipedia, through validation, into a queue, out to multiple processors in parallel, into fast storage, and finally to your browser. Each step is explained in detail below.

---

## 3. Background Concepts You Need First

Before we dive into each step, here's a crash course on the technologies WikiSurge uses. If you already know these, skip ahead.

### 3.1 SSE (Server-Sent Events)

**What it is:** A way for a server to *push* data to a client over a long-lived HTTP connection. The client opens the connection once, and the server keeps sending lines of text (events) forever — like a never-ending HTTP response.

**Analogy:** Imagine subscribing to a news ticker. You call the hotline once, and the voice on the other end just keeps reading headlines to you indefinitely. You don't need to keep asking "any news?" — it just flows.

**How Wikipedia uses it:** Wikipedia runs a public SSE endpoint at `https://stream.wikimedia.org/v2/stream/recentchange`. Every time anyone on Earth edits any Wikipedia article in any language, an event appears on this stream within seconds. WikiSurge connects to this stream and listens.

### 3.2 Message Queues (Kafka)

**What it is:** Think of a conveyor belt in a factory. The Ingestor (producer) puts items on the belt, and various workers (consumers) pick items off at their own pace. The belt remembers what's on it — if a worker goes on break and comes back, the items are still there waiting.

**Why not just call functions directly?** If the Ingestor called the Processor directly:
- If the Processor crashes → edits are **lost forever**
- If the Processor is slow → the Ingestor gets backed up and might lose its SSE connection
- If you want 5 different processors → the Ingestor needs to know about all of them

A message queue solves all three problems. The producer and consumers don't even need to know each other exists.

**Key Kafka concepts:**
| Concept | What it means |
|---------|---------------|
| **Topic** | A named channel/category. WikiSurge has one: `wikipedia.edits` |
| **Partition** | A topic is split into partitions for parallelism. Messages with the same *key* always go to the same partition (guaranteeing order for that key) |
| **Producer** | Anything that writes messages to a topic |
| **Consumer** | Anything that reads messages from a topic |
| **Consumer Group** | A *team* of consumers that split work. Each message goes to exactly one member of the group. But different groups each get a *copy* of every message |
| **Offset** | A number tracking "how far have I read?" — like a bookmark |
| **Dead Letter Queue (DLQ)** | A separate topic where broken/un-processable messages get sent so you can investigate later |

### 3.3 Redis (In-Memory Key-Value Store)

**What it is:** A giant dictionary that lives entirely in RAM. You give it a key (like a word in a dictionary), and it gives you back a value (like the definition) — but in *microseconds* instead of the milliseconds a disk database takes.

**Why not just use a regular database?** Speed. WikiSurge processes thousands of edits per second. For each edit, it needs to:
- Check "how many edits has this page gotten in the last 5 minutes?" 
- Update trending scores
- Track which editors are editing which pages

All of this needs to happen in under a millisecond. A disk-based database like PostgreSQL would buckle under this load. Redis handles it effortlessly because everything is in RAM.

**The tradeoff:** RAM is expensive and limited. You can't store everything forever. WikiSurge carefully manages what lives in Redis (only active/hot pages) and lets quiet pages expire automatically.

**Redis data structures WikiSurge uses:**

| Structure | What it is | WikiSurge use case |
|-----------|-----------|-------------------|
| **String / Counter** | A simple value, often a number | `activity:{title}` — counts edits to a page (10-min TTL) |
| **Hash** | A mini dictionary inside a key (field → value pairs) | `hot:meta:{title}` — stores edit count, last editor, byte change for a hot page |
| **Sorted Set** | A set where each member has a numeric score, kept in order | `trending:global` — all pages ranked by trending score; `hot:window:{title}` — timestamped edits for rate calculation |
| **List** | An ordered sequence of values | `editwar:changes:{title}` — sequence of byte changes to detect reverts |
| **Pub/Sub** | Fire-and-forget message broadcasting | `wikisurge:edits:live` — every processed edit is published here for the API to relay to browsers |
| **Streams** | Like Pub/Sub but the messages *persist* and readers can catch up | `alerts:spikes`, `alerts:editwars` — alerts are stored here so the API can replay missed ones |

### 3.4 Elasticsearch

**What it is:** A search engine for your own data — think of it as "Google, but for your database." You feed it documents (JSON objects), and it builds an *inverted index* so you can search any field instantly.

**Inverted index, simply explained:** Imagine you have 1 million Wikipedia edit records. You want to find all edits to pages containing "Obama". Without an index, you'd scan all 1 million records (slow). An inverted index pre-builds a lookup table: "Obama" → [doc #42, doc #8891, doc #99201, ...]. Now the lookup is instant.

**Why WikiSurge needs it:** Redis handles "what's happening right now" brilliantly. But "search across the last 7 days of interesting edits" is a text-search problem — that's what Elasticsearch was built for.

### 3.5 WebSockets

**What it is:** A persistent, two-way connection between a browser and a server. Unlike regular HTTP (you ask, server answers, connection closes), a WebSocket stays open so the server can push data to the browser the instant something happens — no polling, no delay.

**Analogy:** 
- Regular HTTP = sending letters back and forth (each letter is a separate trip)
- WebSocket = a phone call (once connected, both sides can talk anytime)

**How the upgrade works:**
1. Browser sends a normal HTTP request with a special header: "Hey, can we upgrade to WebSocket?"
2. Server says "Yes" (HTTP 101 Switching Protocols)
3. From that point on, both sides can send messages freely over the same connection

WikiSurge uses two WebSocket endpoints:
- **`/ws/feed`** — streams every live edit to the dashboard (filterable by language, bot status, etc.)
- **`/ws/alerts`** — streams spike and edit-war alerts

---

## 4. Step 1 — Wikipedia SSE → Ingestor

**Code:** `internal/ingestor/client.go`  
**Entry point:** `cmd/ingestor/main.go`

### What Happens

The Ingestor is a long-running Go service that does one job: **connect to Wikipedia's live edit stream, validate each edit, and drop valid edits onto Kafka.**

Here's the flow:

```
Wikipedia SSE Stream
   (https://stream.wikimedia.org/v2/stream/recentchange)
                │
                ▼
    ┌───────────────────────┐
    │  WikiStreamClient     │
    │  ┌─────────────────┐  │
    │  │ 1. Connect()    │  │  ← Opens HTTP connection with Accept: text/event-stream
    │  │    (SSE client)  │  │
    │  └────────┬────────┘  │
    │           ▼           │
    │  ┌─────────────────┐  │
    │  │ 2. eventLoop()  │  │  ← Runs forever; auto-reconnects on failure
    │  │    goroutine     │  │     with exponential backoff (1s → 2s → 4s → ... → 60s max)
    │  └────────┬────────┘  │
    │           ▼           │
    │  ┌─────────────────┐  │
    │  │ 3. processEvent │  │  ← For each SSE event:
    │  │    (per event)   │  │
    │  │   ┌────────────┐ │  │
    │  │   │Rate limit  │ │  │  ← Wait if above 100 events/sec (configurable)
    │  │   │   check    │ │  │
    │  │   └─────┬──────┘ │  │
    │  │         ▼        │  │
    │  │   ┌────────────┐ │  │
    │  │   │ JSON parse │ │  │  ← Unmarshal SSE data → WikipediaEdit struct
    │  │   └─────┬──────┘ │  │
    │  │         ▼        │  │
    │  │   ┌────────────┐ │  │
    │  │   │ Validate   │ │  │  ← Must have title, user, valid wiki URL
    │  │   └─────┬──────┘ │  │
    │  │         ▼        │  │
    │  │   ┌────────────┐ │  │
    │  │   │ Filter     │ │  │  ← Drop bots?  Allowed languages?  Main namespace only?
    │  │   │ (see below)│ │  │     Non-Wikipedia projects (Wiktionary etc.) excluded
    │  │   └─────┬──────┘ │  │
    │  │         ▼        │  │
    │  │   ┌────────────┐ │  │
    │  │   │ → Kafka    │ │  │  ← producer.Produce(&edit) — non-blocking buffer
    │  │   └────────────┘ │  │
    │  └─────────────────┘  │
    └───────────────────────┘
```

### Filters Applied

The Ingestor doesn't send everything to Kafka — that would be a firehose of irrelevant data. It filters:

| Filter | What it drops | Why |
|--------|--------------|-----|
| **Bot edits** | Edits by automated bots (configurable) | Bots make thousands of mechanical edits; they're noise for spike detection |
| **Non-Wikipedia** | Wikidata, Wiktionary, Commons, Meta | We only care about Wikipedia article edits |
| **Language** | Languages not in `allowed_languages` config | Default: `["en", "es", "fr", "de"]` — reduces volume |
| **Namespace** | Non-article pages (User pages, Talk pages, Template pages, etc.) | Namespace `0` = actual articles; namespace `1` = Talk pages, etc. |
| **Edit type** | Anything that's not `"edit"` or `"new"` (page creation) | Log entries, categorization changes, etc. aren't interesting |

### Resilience Features

- **Rate limiter**: Prevents overwhelming downstream systems (default 100 events/sec, burst 200)
- **Auto-reconnect**: If the SSE connection drops (network blip, Wikipedia restart), the client waits with exponential backoff and reconnects
- **Idle timeout**: If no events arrive for 2 minutes, forces a reconnect (the stream might be silently dead)
- **Non-blocking produce**: If Kafka is slow, messages are dropped rather than blocking the SSE reader (you'd lose your stream position)

---

## 5. Step 2 — Kafka: The Message Queue

**Code:** `internal/kafka/producer.go`, `internal/kafka/consumer.go`, `internal/kafka/dead_letter.go`

### Why Kafka Exists in This System

Without Kafka, the Ingestor would call the Processor directly. This creates tight coupling:

```
 WITHOUT KAFKA (fragile):                  WITH KAFKA (resilient):

 Ingestor ──→ Processor                   Ingestor ──→ [Kafka] ──→ Processor
     │                                         │           │           │
     └─ If Processor crashes,                  │      Messages sit     │
        edits are LOST                         │      safely here      │
                                               │      until consumed   │
     └─ If Processor is slow,                  │                       │
        Ingestor backs up                      └─ Producer doesn't     └─ Consumer reads
        and may lose SSE connection               care who reads          at its own pace
```

Kafka decouples the producer from the consumer. They don't need to be running at the same time, at the same speed, or even know about each other.

### How WikiSurge Configures Kafka

**Topic:** `wikipedia.edits` — one topic for all edits.

**Partitioning by page title:** Each message is keyed by the page title (e.g., `"Barack Obama"`). Kafka hashes this key to decide which partition the message goes to. This means:
- All edits to "Barack Obama" land in the **same partition**, in order
- This is essential for spike detection (you need to count edits *per page*)
- Different pages can be processed in parallel across partitions

**Compression:** Snappy — a fast compression algorithm. Reduces network bandwidth by ~50% with negligible CPU cost.

### The Producer (Ingestor side)

The producer uses an **internal buffer + batching** pattern for efficiency:

```
 edit arrives from SSE
       │
       ▼
 ┌─────────────────────┐
 │  1000-slot buffer    │  ← Non-blocking: if full, message is DROPPED (backpressure)
 │  (Go channel)        │
 └──────────┬───────────┘
            │
   ┌────────▼────────┐
   │  Batching Loop   │  ← Background goroutine
   │  (goroutine)     │
   │                  │
   │  Collects up to  │
   │  100 messages    │  ← OR waits 100ms, whichever comes first
   │  per batch       │
   │                  │
   │  Writes batch    │
   │  to Kafka        │  ← One network call for 100 messages (efficient!)
   └──────────────────┘
```

**Why batch?** Sending 1 message at a time = 1 network round trip per message = slow. Sending 100 messages in one batch = 1 round trip for 100 messages = 100× fewer network calls.

**Message format:**
```json
{
  "Key": "Barack Obama",             // ← determines partition
  "Value": "{...edit JSON...}",       // ← the full edit data
  "Headers": [
    {"Key": "wiki",      "Value": "enwiki"},
    {"Key": "language",  "Value": "en"},
    {"Key": "timestamp", "Value": "1708800000"},
    {"Key": "bot",       "Value": "false"}
  ]
}
```

### The Consumer (Processor side)

Each consumer group runs a **consume loop**:

```go
for {
    message := reader.FetchMessage(ctx)  // blocks until a message is available
    handler.ProcessEdit(ctx, &edit)       // call the specific handler (spike, trending, etc.)
    reader.CommitMessages(ctx, message)   // tell Kafka "I'm done with this one"
}
```

**Consumer Groups** are the key concept here. WikiSurge runs **5 consumer groups** simultaneously:

| Group ID | Handler | What it does |
|----------|---------|-------------|
| `spike-detector` | SpikeDetector | Checks if page edit rate is abnormally high |
| `trending-aggregator` | TrendingAggregator | Updates trending scores in Redis |
| `edit-war-detector` | EditWarDetector | Tracks editor conflicts per page |
| `elasticsearch-indexer` | SelectiveIndexer | Decides if edit is worth saving to Elasticsearch |
| `websocket-forwarder` | WebSocketForwarder | Publishes edit to Redis Pub/Sub for live dashboard |

**Each group gets a copy of every message.** They don't interfere with each other. If the spike detector is slow, the trending aggregator still processes at full speed.

### Dead Letter Queue (DLQ)

If a message can't be processed (corrupted JSON, unexpected format), it goes to topic `wikipedia.edits.dlq` instead of being silently dropped:

```json
{
  "original_topic": "wikipedia.edits",
  "original_key": "Barack Obama",
  "original_value": "<the raw message>",
  "error": "failed to unmarshal: invalid character...",
  "timestamp": "2026-02-24T12:00:00Z",
  "consumer_group": "spike-detector"
}
```

This lets you investigate failures without losing data.

---

## 6. Step 3 — The Processor: Five Parallel Consumer Groups

**Code:** `internal/processor/` (detector.go, aggregator.go, edit_war_detector.go, indexer.go, ws_forwarder.go)  
**Entry point:** `cmd/processor/main.go`

One Wikipedia edit entering Kafka gets processed by **five independent consumer groups simultaneously**. No group waits for another. Think of it like five different people each reading the same newspaper — they each get the full paper and can read at their own speed.

```
                       Kafka topic: wikipedia.edits
                                │
        ┌───────────┬───────────┼───────────┬───────────┐
        ▼           ▼           ▼           ▼           ▼
  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐
  │  Spike   │ │ Trending │ │ Edit War │ │    ES    │ │    WS    │
  │ Detector │ │Aggregator│ │ Detector │ │ Indexer  │ │Forwarder │
  └────┬─────┘ └────┬─────┘ └────┬─────┘ └────┬─────┘ └────┬─────┘
       │             │            │             │            │
       ▼             ▼            ▼             ▼            ▼
  XADD alerts   ZADD trending  XADD alerts  Bulk index   PUBLISH
  :spikes       :global        :editwars    to ES        to Redis
  + SET spike:  (sorted set)   + SET        (selective)  pub/sub
    {title}                    editwar:
                               {title}
```

### 3a. Spike Detector

**Goal:** Detect when a page is getting edited at an unusually high rate.

**How it works — the sliding window approach:**

1. **Activity counter** (Stage 1 — lightweight gate):
   - For every edit, do `INCR activity:{title}` in Redis (costs almost nothing)
   - This key has a 10-minute TTL — auto-expires if the page goes quiet
   - If the counter is below threshold (default: 2), **stop here**. No further processing.

2. **Hot page promotion** (Stage 2 — detailed tracking):
   - Once the counter hits the threshold, the page is "promoted" to hot tracking
   - A Redis sorted set `hot:window:{title}` is created with timestamped edit entries  
   - A metadata hash `hot:meta:{title}` stores edit count, editors, byte changes

3. **Spike check** (only for hot pages):
   - Count edits in last 5 minutes vs. last 1 hour
   - Calculate ratio: `rate_5min / rate_1hour`
   - Thresholds: **5× = medium**, **10× = high**, **20× = critical**

4. **Alert published** to Redis Stream:
   ```
   XADD alerts:spikes MAXLEN ~1000 * data=<SpikeAlert JSON>
   ```
   Also sets `spike:{title}` key (1-hour TTL) so the ES indexer knows this page is spiking.

**Why the two-stage gate?** Memory efficiency. Wikipedia has millions of articles. If you created a sorted set for every single page that gets one edit, you'd exhaust Redis memory immediately. The activity counter costs a few bytes per page, and only pages with genuine repeated activity get the expensive sorted set.

**Cooldown:** After alerting on a page, suppress duplicate alerts for that page for 10 minutes to avoid spamming.

### 3b. Trending Aggregator

**Goal:** Maintain a ranked list of the most interesting pages right now.

**Scoring formula:**
```
base_score = 1.0
  × 1.5 if large edit (significant byte change)
  × 0.5 if bot edit (bots are less interesting)
  × 2.0 if new page creation
```

**Decay:** Scores decay over time using a **30-minute half-life**. This means:
- A score of 10.0 becomes 5.0 after 30 minutes of no new edits
- Becomes 2.5 after 60 minutes, 1.25 after 90 minutes, etc.
- Pages that stop receiving edits gradually fall off the trending list

**Lazy decay:** WikiSurge doesn't run a timer to decay all scores. Instead, it calculates the decayed score on-the-fly whenever you read a page's score:
```
elapsed_minutes = (now - last_updated) / 60
decay_factor = 0.5 ^ (elapsed_minutes / 30)
current_score = raw_score × decay_factor
```

**Redis structure:**
- `trending:{title}` hash — stores `raw_score`, `last_updated`, `server_url`
- `trending:global` sorted set — all pages ranked by score (used by API for "top trending" endpoint)

**Stats tracking:** Also records per-language daily edit counts, human vs. bot ratios, and per-minute edit timeline for the dashboard's statistics panel.

### 3c. Edit War Detector

**Goal:** Detect when multiple editors are repeatedly reverting each other's changes on the same page.

**What is an edit war?** Imagine two people arguing about whether a politician's Wikipedia bio should say "controversial" or "acclaimed." Person A changes it to "controversial." Person B changes it back to "acclaimed." Person A reverts. Person B reverts again. This back-and-forth is an edit war.

**Detection algorithm:**

1. **Track editors per page:**
   ```
   HINCRBY editwar:editors:{title} "User:Alice" 1
   HINCRBY editwar:editors:{title} "User:Bob" 1
   ```
   These keys auto-expire after 10 minutes (the detection window).

2. **Track byte changes** in a list:
   ```
   RPUSH editwar:changes:{title} +500
   RPUSH editwar:changes:{title} -480
   RPUSH editwar:changes:{title} +490
   ```
   Alternating positive/negative changes of similar magnitude = likely reverts.

3. **Revert detection:** Look for consecutive changes where:
   - Signs are opposite (+500 then -480)
   - Magnitudes are within 30% of each other (480 is within 30% of 500)
   - This pattern counts as a "revert"

4. **Trigger conditions** (ALL must be met in a 10-minute window):
   - ≥ 5 total edits
   - ≥ 2 unique editors  
   - ≥ 2 detected reverts

5. **Severity calculation:**
   - More editors, more edits, more reverts = higher severity
   - Results: `low`, `medium`, `high`, `critical`

6. **Alert published** to `alerts:editwars` Redis Stream + sets `editwar:{title}` key (12-hour TTL) for ES indexer.

### 3d. Elasticsearch Indexer (Selective)

**Goal:** Save "interesting" edits to Elasticsearch for search/history — but NOT every edit (that would be millions of documents per day).

**The Priority Waterfall — `ShouldIndex()` cascade:**

Each edit runs through these checks in order. First match wins:

| Priority | Check | How | Action |
|----------|-------|-----|--------|
| 1 | **Watchlist** | `SISMEMBER indexing:watchlist` in Redis | Always index |
| 2 | **Sample** | Random sampling (e.g., 50% in dev) | Index |
| 3 | **Trending** | Page in top N of `trending:global` sorted set | Index |
| 4 | **Spiking** | `spike:{title}` key exists in Redis (1h TTL) | Index |
| 5 | **Edit war** | `editwar:{title}` key exists (12h TTL) | Index |
| 6 | **Hot page** | Page promoted to hot tracking | Index |
| 7 | **Recent activity** | Page has ≥2 recent edits | Index |
| — | **Default** | None of the above matched | **Skip** |

**Buffer & Bulk flush:**
```
Approved edits  →  1000-capacity channel  →  Background goroutine drains
                   (buffer)                  in batches of 500 docs
                                             OR every 5 seconds
                                             using ES _bulk API
```

**Daily indices:** One index per day, e.g., `wikipedia-edits-2026-02-24`. Old indices auto-deleted after 7 days.

> **⚠️ Race window note:** The indexer runs as an independent consumer group, so it may process an edit *before* the spike/edit-war detectors have flagged that page. This is acceptable — the next edit to that page will catch the flag.

### 3e. WebSocket Forwarder

**Goal:** Get every edit to the browser dashboard in real-time.

This is the simplest consumer group. For every edit:
1. Serialize to JSON
2. `PUBLISH wikisurge:edits:live <JSON>` on Redis Pub/Sub

That's it. The API server (a separate process) subscribes to this channel and relays to browser WebSocket clients.

**Why not send directly to WebSockets?** The Processor and API server are separate processes (possibly on different machines). Redis Pub/Sub bridges them.

---

## 7. Step 4 — Elasticsearch: Search & History

**Code:** `internal/storage/elasticsearch.go`, `internal/processor/indexer.go`

### What Lives in Elasticsearch

Only **"interesting" edits** — ones that passed the priority waterfall in Step 3d above. A typical day might have 500,000+ edits flow through the pipeline, but only 10,000–50,000 get indexed in Elasticsearch.

**Document structure** (simplified):
```json
{
  "id": "enwiki-12345678",
  "title": "Barack Obama",
  "user": "HistoryBuff42",
  "language": "en",
  "wiki": "enwiki",
  "timestamp": "2026-02-24T10:30:00Z",
  "byte_change": 1250,
  "is_bot": false,
  "is_new_page": false,
  "comment": "Added reference for 2024 election section",
  "index_reason": "trending_top_5",
  "server_url": "https://en.wikipedia.org"
}
```

### Index Management

- **Daily indices:** `wikipedia-edits-2026-02-24`, `wikipedia-edits-2026-02-25`, etc.
- **Retention:** 7 days. Indices older than 7 days are automatically deleted.
- **Why daily?** Makes deletion easy (drop the whole index instead of deleting individual documents). Also improves search performance — recent searches only hit today's index.

### Bulk Indexing

Elasticsearch is most efficient when you send many documents at once:

```
NOT this (slow):                    THIS (fast):
───────────────                     ─────────────
POST doc1                           POST _bulk
POST doc2                              doc1
POST doc3                              doc2
POST doc4                              doc3
POST doc5                              doc4
(5 network round trips)                doc5
                                    (1 network round trip)
```

WikiSurge's indexer accumulates documents in a buffer (capacity 1000) and flushes them in batches of 500 using the `_bulk` API, or every 5 seconds if the batch isn't full yet.

---

## 8. Step 5 — Redis: Live State & Alerts

**Code:** `internal/storage/redis_hot_pages.go`, `internal/storage/redis_trending.go`, `internal/storage/redis_alerts.go`, `internal/storage/redis_stats.go`

Redis serves two fundamentally different purposes in WikiSurge, using two different mechanisms:

### Pub/Sub — Ephemeral Live Edits

**Channel:** `wikisurge:edits:live`

```
Processor (WebSocket Forwarder)
    │
    │  PUBLISH wikisurge:edits:live <JSON>
    ▼
  Redis Pub/Sub
    │
    │  If someone is listening → they get it
    │  If nobody is listening → message is GONE (that's fine!)
    ▼
API Server (StartEditRelay goroutine)
    │
    │  Deserializes → BroadcastEditFiltered()
    ▼
Browser WebSocket clients
```

**Why ephemeral is OK:** Live edits are like a live TV broadcast. If you weren't watching, you missed it — and that's fine. You'll see the next one.

### Streams — Persistent Alerts

**Streams:** `alerts:spikes`, `alerts:editwars`

```
Processor (Spike Detector / Edit War Detector)
    │
    │  XADD alerts:spikes MAXLEN ~1000 * data=<JSON>
    ▼
  Redis Stream (stores up to ~1000 entries)
    │
    │  XREAD BLOCK 1000 COUNT 10 STREAMS alerts:spikes alerts:editwars $ $
    ▼
API Server (AlertHub — single shared subscription loop)
    │
    │  Fans out to all subscribed WebSocket clients
    ▼
Browser /ws/alerts clients
```

**Why persistent?** If the API server restarts, it can replay alerts it missed. Each reader tracks its own position in the stream (the stream ID).

**MAXLEN ~1000:** Each stream is capped at approximately 1000 entries. The `~` means Redis uses approximate trimming for better performance (might keep 1020 instead of exactly 1000).

### Full Redis Key Map

Here's every key pattern WikiSurge uses in Redis:

| Key Pattern | Type | TTL | Purpose |
|-------------|------|-----|---------|
| `activity:{title}` | String (counter) | 10 min | Stage 1: lightweight edit counter for hot-page promotion |
| `hot:window:{title}` | Sorted Set | ~70 min | Timestamped edit entries for sliding-window rate calculation |
| `hot:meta:{title}` | Hash | ~70 min | Metadata: edit count, last editor, byte change, server URL |
| `trending:{title}` | Hash | 8 days | Per-page: raw score + last updated timestamp |
| `trending:global` | Sorted Set | — | Global ranking of all pages by trending score |
| `editwar:editors:{title}` | Hash | 10 min | Per-editor edit counts for a page |
| `editwar:changes:{title}` | List | 10 min | Sequence of byte changes for revert detection |
| `editwar:timeline:{title}` | List | 12 hours | Detailed edit timeline (user, comment, byte change) for LLM analysis |
| `editwar:start:{title}` | String | 12 hours | Persisted timestamp of when edit war was first detected |
| `spike:{wiki}:{title}` | String | 1 hour | Flag: "this page is currently spiking" (read by ES indexer) |
| `editwar:{title}` | String | 12 hours | Flag: "this page has an active edit war" (read by ES indexer) |
| `indexing:watchlist` | Set | — | Pages that should always be indexed in ES |
| `alerts:spikes` | Stream | capped ~1000 | Spike alert log |
| `alerts:editwars` | Stream | capped ~1000 | Edit war alert log |
| `wikisurge:edits:live` | Pub/Sub channel | — | Live edit broadcast (ephemeral) |
| `stats:edits:{lang}:{date}` | Hash | 48 hours | Per-language daily edit counts |
| `stats:timeline:{date}` | Hash | 48 hours | Per-minute edit timeline |
| `stats:pages:{date}` | Sorted Set | 48 hours | Per-page daily edit counts |

### Memory Efficiency — Hot Page Promotion

This is one of the most important design decisions in WikiSurge. Let's walk through why:

**The problem:** Wikipedia has ~60 million articles across all languages. If you created Redis tracking structures for every page that gets a single edit, you'd need gigabytes of RAM.

**The solution:** A two-stage gate:

```
 Edit arrives for "Barack Obama"
       │
       ▼
 Stage 1: INCR activity:Barack Obama     ← Costs: ~50 bytes
       │
       │  Counter = 1?  → Done. Wait for more edits.
       │  Counter = 2?  → PROMOTE! ↓
       ▼
 Stage 2: Create full tracking:
   • hot:window:Barack Obama (sorted set)  ← Costs: ~500 bytes per edit
   • hot:meta:Barack Obama (hash)          ← Costs: ~200 bytes
```

Pages that receive only 1 edit and then go quiet → the `activity:` counter expires after 10 minutes → negligible memory cost.

Pages with genuine activity get promoted to full tracking — and even then, the sorted sets are capped at 100 members, and there's a circuit breaker at 1000 total hot pages.

---

## 9. Step 6 — WebSocket `/ws/feed`: Live Edit Stream

**Code:** `internal/api/websocket.go`, `internal/api/server.go`

### How it works

The API server runs as a **separate process** from the Processor. It doesn't read from Kafka directly — instead, it subscribes to the Redis Pub/Sub channel where the WebSocket Forwarder (Step 3e) publishes edits.

```
 Redis pub/sub "wikisurge:edits:live"
       │
       ▼
 APIServer.StartEditRelay()          ← goroutine subscribes to Redis channel
       │
       │  json.Unmarshal → WikipediaEdit
       ▼
 wsHub.BroadcastEditFiltered(&edit)  ← sends to all matching clients
       │
       ├─→ Client A (filter: languages=en, exclude_bots=true)  ✓ matches → send
       ├─→ Client B (filter: languages=fr)                      ✗ doesn't match → skip
       └─→ Client C (no filter)                                  ✓ matches → send
```

### The Hub — Managing Connections

The `WebSocketHub` is a single goroutine that manages **all** WebSocket connections through three channels:

```go
type WebSocketHub struct {
    clients    map[*Client]bool  // all connected clients
    register   chan *Client       // new clients arrive here
    unregister chan *Client       // disconnected clients cleaned up here
    broadcast  chan []byte        // messages to send to all clients
}
```

**Why a single goroutine?** Thread safety without locks. The hub's `Run()` method is the only goroutine that reads/writes the `clients` map, so there are no race conditions.

### Per-Client Filtering

When a browser connects, it can pass query parameters to filter what it receives:

```
ws://localhost:8080/ws/feed?languages=en,fr&exclude_bots=true&min_byte_change=100
```

These become an `EditFilter` struct. On every broadcast, the hub checks `client.filter.Matches(edit)` — non-matching clients are skipped, saving bandwidth.

| Parameter | Type | Example | Effect |
|-----------|------|---------|--------|
| `languages` | comma-separated | `en,fr,de` | Only receive edits in these languages |
| `exclude_bots` | boolean | `true` | Hide automated bot edits |
| `page_pattern` | regex | `.*Obama.*` | Only pages matching this pattern |
| `min_byte_change` | integer | `100` | Only edits with ≥100 bytes changed |

### Connection Lifecycle

```
1. Browser sends:    GET /ws/feed?languages=en HTTP/1.1
                     Upgrade: websocket
                     Connection: Upgrade
       │
       ▼
2. Server upgrades:  HTTP/1.1 101 Switching Protocols
                     (connection is now a WebSocket)
       │
       ▼
3. Server creates Client:
   • 512-capacity send channel (buffer for outgoing messages)
   • Parsed EditFilter from query params
   • Unique UUID identifier
       │
       ▼
4. Client registers with hub:
   • Rejected if > 100 total clients (global limit)
   • Rejected if > 50 clients from same IP (per-IP limit)
       │
       ▼
5. Two goroutines start:
   ┌──────────────┐    ┌──────────────┐
   │  writePump   │    │  readPump    │
   │              │    │              │
   │ Reads from   │    │ Reads from   │
   │ send channel │    │ WebSocket    │
   │ → writes to  │    │ (only to     │
   │   WebSocket  │    │  detect      │
   │              │    │  disconnect) │
   │ Pings every  │    │              │
   │ 30 seconds   │    │ Pong timeout │
   │              │    │ = 60 seconds │
   └──────────────┘    └──────────────┘
       │
       ▼
6. On disconnect / error:
   • Client unregisters from hub
   • Send channel closed
   • WebSocket connection closed

7. Every 60 seconds — stale sweep:
   • Any client whose 512-slot send buffer is 100% full = "stuck"
   • Evicted from the hub (slow clients shouldn't block everyone)
```

---

## 10. Step 7 — WebSocket `/ws/alerts`: Spike & Edit War Alerts

**Code:** `internal/api/alert_hub.go`, `internal/api/websocket_alerts.go`

### Why a Different Pattern?

`/ws/feed` uses Redis **Pub/Sub** (ephemeral — if you weren't listening, the message is gone).

`/ws/alerts` uses Redis **Streams** (persistent — messages are saved, readers can catch up).

This means alerts need a different reading mechanism: **XREAD** instead of **SUBSCRIBE**.

### The AlertHub — One Reader, Many Clients

A naive approach would be: each WebSocket client runs its own `XREAD` loop against Redis. But that wastes Redis connections (100 clients = 100 Redis connections doing the same work).

WikiSurge's `AlertHub` solves this:

```
                    Redis Streams
              alerts:spikes    alerts:editwars
                    │                │
                    └───────┬────────┘
                            ▼
                 ┌──────────────────────┐
                 │      AlertHub        │  ← ONE goroutine
                 │                      │
                 │  XREAD BLOCK 1000    │  ← Blocks up to 1 second waiting
                 │  COUNT 10            │     for new entries
                 │  STREAMS             │
                 │    alerts:spikes     │
                 │    alerts:editwars   │
                 │    $ $               │  ← "$" = only new messages
                 │                      │
                 │  On new alert:       │
                 │  1. Parse → Alert    │
                 │  2. Advance stream   │
                 │     ID pointer       │
                 │  3. Fan out to all   │
                 │     subscribers      │
                 └──────────┬───────────┘
                            │
              ┌─────────────┼─────────────┐
              ▼             ▼             ▼
        ┌──────────┐  ┌──────────┐  ┌──────────┐
        │ Client A │  │ Client B │  │ Client C │
        │ (ch:128) │  │ (ch:128) │  │ (ch:128) │
        └──────────┘  └──────────┘  └──────────┘
         Browser        Browser        Browser
```

**Each subscriber gets a buffered channel (capacity 128).** The AlertHub does a non-blocking send to each channel — if a channel is full (slow client), that alert is dropped for that client rather than blocking the entire hub.

**Auto-reconnect:** If Redis disconnects, the `Run()` loop catches the error and immediately re-subscribes — no manual intervention needed.

### Alert Message Format

Alerts arrive at the browser as JSON:

```json
{
  "type": "spike",
  "data": {
    "id": "spike-1708800000000",
    "type": "spike",
    "timestamp": "2026-02-24T10:30:00Z",
    "data": {
      "page_title": "Breaking News Event",
      "spike_ratio": 12.5,
      "edits_5min": 25,
      "edits_1hour": 12,
      "severity": "high",
      "unique_editors": 8,
      "server_url": "https://en.wikipedia.org"
    }
  }
}
```

```json
{
  "type": "editwar",
  "data": {
    "id": "editwar-1708800000000",
    "type": "edit_war",
    "timestamp": "2026-02-24T10:30:00Z",
    "data": {
      "page_title": "Controversial Topic",
      "editor_count": 4,
      "edit_count": 12,
      "revert_count": 5,
      "severity": "high",
      "editors": ["User:Alice", "User:Bob", "User:Charlie", "User:Diana"],
      "server_url": "https://en.wikipedia.org"
    }
  }
}
```

### Alert WebSocket Connection Lifecycle

```
1. Browser:  GET /ws/alerts → WebSocket upgrade
2. Server:   alertHub.Subscribe() → gets buffered channel (capacity 128)
3. Read goroutine:  only exists to detect client disconnect
4. Write goroutine:
     select {
       case alert := <-alertCh:   → marshal + write to WebSocket
       case <-pingTicker.C:       → send ping (keep-alive)
       case <-done:               → client disconnected, exit
     }
5. On disconnect:  alertHub.Unsubscribe(ch) → removes channel from fan-out set
```

---

## 11. End-to-End: Tracing One Edit Through the Entire System

Let's follow a single edit from the moment someone hits "Publish changes" on Wikipedia to the moment it appears on your browser dashboard.

> **Scenario:** User "HistoryBuff42" adds 1,250 bytes to the English Wikipedia article "Barack Obama." This page has been getting a lot of edits in the last few minutes because of breaking news.

### Step by step:

```
STEP 1 — Wikipedia publishes the edit via SSE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Wikipedia's EventStreams emits a Server-Sent Event:
   event: message
   data: {"title":"Barack Obama","user":"HistoryBuff42","bot":false,
          "wiki":"enwiki","server_url":"https://en.wikipedia.org",
          "length":{"old":95000,"new":96250},...}

                              │
                              ▼

STEP 2 — Ingestor receives, validates, and produces to Kafka
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
WikiStreamClient.processEvent():
  ✓ Rate limiter: under 100/sec, passes through
  ✓ JSON parse: valid WikipediaEdit struct
  ✓ Validation: has title, user, valid wiki URL
  ✓ Filters: not a bot, language=en (allowed), namespace=0, type="edit"
  → producer.Produce(&edit) — drops into 1000-slot buffer channel

Producer batching loop:
  Collects this edit + 99 others → writes batch to Kafka
  Key = "Barack Obama" → hashed to partition 7

                              │
                              ▼

STEP 3 — Kafka holds the message; five consumer groups begin reading
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Message sits in topic "wikipedia.edits", partition 7, waiting.
All 5 consumer groups fetch it independently.

  ┌─ spike-detector ────────────────────────────────────────────┐
  │  INCR activity:Barack Obama → returns 15 (already hot)      │
  │  ZADD hot:window:Barack Obama (add this edit's timestamp)   │
  │  Count: 25 edits in last 5 min, 12 edits in last hour      │
  │  Ratio: (25/5) / (12/60) = 5.0 / 0.2 = 25.0×              │
  │  → CRITICAL spike! (≥20×)                                   │
  │  XADD alerts:spikes * data=<SpikeAlert JSON>                │
  │  SET spike:enwiki:Barack Obama (1-hour TTL)                  │
  └─────────────────────────────────────────────────────────────┘

  ┌─ trending-aggregator ───────────────────────────────────────┐
  │  Score increment: 1.0 × 1.5 (large edit) = 1.5             │
  │  HSET trending:Barack Obama raw_score=47.3 last_updated=now │
  │  ZADD trending:global 47.3 "Barack Obama"                   │
  │  RecordEdit(ctx, "en", false) → update daily stats          │
  └─────────────────────────────────────────────────────────────┘

  ┌─ edit-war-detector ─────────────────────────────────────────┐
  │  Page is hot → proceed                                      │
  │  HINCRBY editwar:editors:Barack Obama "HistoryBuff42" 1     │
  │  RPUSH editwar:changes:Barack Obama 1250                    │
  │  Check: 3 unique editors, 8 total edits, 1 revert          │
  │  → Not enough reverts (need ≥2). No alert.                  │
  └─────────────────────────────────────────────────────────────┘

  ┌─ elasticsearch-indexer ─────────────────────────────────────┐
  │  ShouldIndex("Barack Obama")?                               │
  │    1. Watchlist? No                                         │
  │    2. Sample? (random) No                                   │
  │    3. Trending? Yes! Rank #2 in trending:global             │
  │  → Index! Reason: "trending_top_2"                          │
  │  Convert to EditDocument → push to 1000-slot buffer         │
  │  (will be flushed with next batch of 500 to ES _bulk API)   │
  └─────────────────────────────────────────────────────────────┘

  ┌─ websocket-forwarder ───────────────────────────────────────┐
  │  json.Marshal(edit)                                         │
  │  PUBLISH wikisurge:edits:live <JSON>                        │
  └─────────────────────────────────────────────────────────────┘

                              │
                              ▼

STEP 4 — API Server relays to browser
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
StartEditRelay() goroutine receives the pub/sub message:
  → json.Unmarshal → WikipediaEdit
  → wsHub.BroadcastEditFiltered(&edit)
  → Client A has filter {languages: ["en"], exclude_bots: true}
  → edit.Language()="en", edit.Bot=false → MATCH ✓
  → Sends JSON to Client A's 512-slot send channel
  → writePump goroutine writes to WebSocket

AlertHub XREAD loop picks up the spike alert:
  → Parse stream entry → Alert struct
  → Fan out to all /ws/alerts subscribers
  → Client receives: {"type":"spike","data":{...}}

                              │
                              ▼

STEP 5 — Browser dashboard updates
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
React frontend:
  • /ws/feed listener: new edit appears in the live feed table
  • /ws/alerts listener: spike notification banner appears!
    "🔥 CRITICAL: Barack Obama — 25× normal edit rate"
```

**Total time from Wikipedia edit to browser display: typically under 1 second.**

---

## 12. Glossary

| Term | Definition |
|------|-----------|
| **SSE** | Server-Sent Events — a protocol for servers to push data to clients over HTTP |
| **Kafka** | A distributed message queue (WikiSurge uses Redpanda, a Kafka-compatible alternative) |
| **Topic** | A named channel in Kafka where messages are published |
| **Partition** | A subdivision of a topic for parallel processing |
| **Consumer Group** | A team of consumers that share work; different groups each get all messages |
| **Offset** | A sequential ID tracking a consumer's position in a partition |
| **DLQ** | Dead Letter Queue — where failed messages go for investigation |
| **Redis** | An in-memory key-value store with microsecond response times |
| **Sorted Set** | A Redis data structure: a set where each member has a score, kept in order |
| **Hash** | A Redis data structure: a key containing field-value pairs (like a mini dictionary) |
| **Stream** | A Redis data structure: an append-only log with consumer tracking |
| **Pub/Sub** | Publish/Subscribe — a messaging pattern where senders broadcast and receivers listen |
| **XADD** | Redis command to append an entry to a Stream |
| **XREAD** | Redis command to read from one or more Streams (can block waiting for new data) |
| **Elasticsearch** | A full-text search engine that builds inverted indices for fast lookups |
| **Bulk API** | Elasticsearch endpoint for indexing many documents in a single request |
| **ILM** | Index Lifecycle Management — Elasticsearch feature for auto-managing index retention |
| **WebSocket** | A protocol for persistent, bidirectional browser-server communication |
| **Hub** | WikiSurge's central goroutine that manages all WebSocket connections |
| **Goroutine** | Go's lightweight thread — WikiSurge uses many of these for concurrent processing |
| **Backpressure** | When a fast producer drops messages because a slow consumer can't keep up |
| **Half-life** | The time it takes for a value to decay to half its original amount (trending scores use 30 min) |
| **Hot page** | A Wikipedia page that has been promoted to detailed tracking due to repeated edits |
| **Sliding window** | A technique that only considers data within a moving time range (e.g., "last 5 minutes") |
| **TTL** | Time To Live — how long a Redis key exists before auto-deletion |
| **Snappy** | A fast compression algorithm used for Kafka messages |

---

> **Architecture document generated from WikiSurge source code.**  
> **Files referenced:** `internal/ingestor/client.go`, `internal/kafka/producer.go`, `internal/kafka/consumer.go`, `internal/kafka/dead_letter.go`, `internal/processor/detector.go`, `internal/processor/aggregator.go`, `internal/processor/edit_war_detector.go`, `internal/processor/indexer.go`, `internal/processor/ws_forwarder.go`, `internal/storage/redis_hot_pages.go`, `internal/storage/redis_trending.go`, `internal/storage/redis_alerts.go`, `internal/storage/storage_strategy.go`, `internal/api/websocket.go`, `internal/api/alert_hub.go`, `internal/api/websocket_alerts.go`, `internal/api/server.go`, `configs/config.dev.yaml`
