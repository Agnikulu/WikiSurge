// cmd/test-llm: manual test harness for comparing LLM prompt / hyperparameter configs.
//
// Usage:
//
//	go run ./cmd/test-llm/ \
//	  --page "2024 United States presidential election" \
//	  --temperature 0.5 \
//	  --max-tokens 900 \
//	  --runs 2
//
// Environment variables:
//
//	OPENAI_API_KEY   OpenAI secret key
//	REDIS_ADDR       Redis address (default: localhost:6379)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/Agnikulu/WikiSurge/internal/llm"
)

func main() {
	page := flag.String("page", "2024 United States presidential election", "Wikipedia page title to analyze")
	temperature := flag.Float64("temperature", 0.5, "LLM temperature (0.0–1.0)")
	maxTokens := flag.Int("max-tokens", 900, "LLM max_tokens")
	runs := flag.Int("runs", 1, "How many times to run the analysis (LLM is non-deterministic)")
	model := flag.String("model", "gpt-4o-mini", "LLM model name")
	redisAddr := flag.String("redis", "", "Redis address (overrides REDIS_ADDR env)")
	selfEval := flag.Bool("self-eval", false, "Run a second LLM call to self-evaluate each output (costs extra tokens)")
	flag.Parse()

	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "error: OPENAI_API_KEY is not set")
		os.Exit(1)
	}

	addr := *redisAddr
	if addr == "" {
		addr = os.Getenv("REDIS_ADDR")
	}
	if addr == "" {
		addr = "localhost:6379"
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot connect to Redis at %s: %v\n", addr, err)
		os.Exit(1)
	}

	cfg := llm.Config{
		Provider:    llm.ProviderOpenAI,
		APIKey:      apiKey,
		Model:       *model,
		MaxTokens:   *maxTokens,
		Temperature: *temperature,
		Timeout:     60 * time.Second,
	}

	client := llm.NewClient(cfg, logger)
	svc := llm.NewAnalysisService(client, rdb, 0, logger)

	fmt.Fprintf(os.Stderr, "Config: model=%s temperature=%.2f max_tokens=%d runs=%d\n",
		*model, *temperature, *maxTokens, *runs)
	fmt.Fprintf(os.Stderr, "Page:   %q\n\n", *page)

	for i := 1; i <= *runs; i++ {
		fmt.Fprintf(os.Stderr, "--- Run %d/%d ---\n", i, *runs)

		// Clear cache so each run calls the LLM fresh.
		cacheKey := fmt.Sprintf("editwar:analysis:%s", *page)
		rdb.Del(ctx, cacheKey)

		start := time.Now()
		analysis, err := svc.Analyze(ctx, *page)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Fprintf(os.Stderr, "error on run %d: %v\n", i, err)
			continue
		}

		out, _ := json.MarshalIndent(analysis, "", "  ")
		fmt.Printf("=== Run %d (%.1fs) ===\n%s\n\n", i, elapsed.Seconds(), string(out))

		if *selfEval {
			score, evalErr := selfEvaluate(ctx, client, analysis)
			if evalErr != nil {
				fmt.Fprintf(os.Stderr, "self-eval error on run %d: %v\n", i, evalErr)
			} else {
				fmt.Printf("=== Self-Eval Run %d ===\n%s\n\n", i, score)
			}
		}
	}
}

// selfEvaluate sends the analysis back to the LLM and asks it to score itself.
func selfEvaluate(ctx context.Context, client *llm.Client, analysis *llm.Analysis) (string, error) {
	data, _ := json.Marshal(analysis)

	prompt := fmt.Sprintf(`You are evaluating a Wikipedia edit-war analysis summary.
Rate the following summary on these 3 dimensions, each 1–5:
  1. clarity      – would a general reader (not a Wikipedia editor) understand this?
  2. drama        – does it convey why this dispute is genuinely interesting?
  3. roles        – are the editor role descriptions factual and specific (not vague labels)?

Return ONLY valid JSON: {"clarity": N, "drama": N, "roles": N, "notes": "one sentence"}

Summary to evaluate:
%s`, string(data))

	evalCfg := llm.Config{
		Provider:    llm.ProviderOpenAI,
		APIKey:      client.APIKey(),
		Model:       "gpt-4o-mini",
		MaxTokens:   120,
		Temperature: 0.0,
		Timeout:     30 * time.Second,
	}
	evalClient := llm.NewClient(evalCfg, zerolog.Nop())
	return evalClient.Complete(ctx, "You are a concise JSON evaluator.", prompt)
}
