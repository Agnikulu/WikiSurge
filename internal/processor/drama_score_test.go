package processor

// Pure unit tests for computeDramaScore and dramaScoreToSeverity.
// These tests require no Redis, no LLM, and no external services.
//
// Test philosophy:
//   - DRAMA: verify genuinely heated edit wars score ≥ 50.
//   - NO DRAMA: verify normal/collaborative editing stays ≤ 25.
//   - SIGNALS: verify each individual signal works correctly in isolation.
//   - BOUNDARIES: verify score stays in [0, 100] at all extremes.

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ts builds a monotonically-increasing timestamp slice starting at a fixed
// base and stepping by stepSecs between each entry.
func ts(count int, stepSecs int64) []int64 {
	base := int64(1700000000)
	out := make([]int64, count)
	for i := range out {
		out[i] = base + int64(i)*stepSecs
	}
	return out
}

// tsSplit builds timestamps where the first half is spaced by firstStep and
// the second half by secondStep. Useful for escalation/cooling tests.
func tsSplit(count int, firstStep, secondStep int64) []int64 {
	base := int64(1700000000)
	out := make([]int64, count)
	out[0] = base
	mid := count / 2
	for i := 1; i < mid; i++ {
		out[i] = out[i-1] + firstStep
	}
	out[mid] = out[mid-1] + firstStep // bridge gap
	for i := mid + 1; i < count; i++ {
		out[i] = out[i-1] + secondStep
	}
	return out
}

// alternating produces a changes slice alternating between +add and -sub.
func alternating(count int, add, sub int) []int {
	out := make([]int, count)
	for i := range out {
		if i%2 == 0 {
			out[i] = add
		} else {
			out[i] = -sub
		}
	}
	return out
}

// allPositive returns a slice of uniformly positive byte-change values.
func allPositive(count, val int) []int {
	out := make([]int, count)
	for i := range out {
		out[i] = val + i*5 // slight variation to prevent zero std-dev
	}
	return out
}

// ─── Core drama scenarios ────────────────────────────────────────────────────

func TestComputeDramaScore_MaxDrama(t *testing.T) {
	// All five signals are at or near their maximum.
	//
	// Signal breakdown (expected):
	//   revert ratio  30/30  (16/20 = 80% ≥ 75%)
	//   edit velocity 25/25  (≥60 edits/hr — 20 edits in ~12 min)
	//   byte volatility 20/20 (std-dev ≈ 2450 ≥ 2000 — alternating +5000/-100)
	//   editor count  15/15  (5 editors)
	//   escalation    10/10  (second half ~5× faster than first half)
	//
	// Expected total ≈ 100.

	// Timestamps: first 10 edits every 60s, last 10 edits every 12s.
	timestamps := make([]int64, 20)
	timestamps[0] = 1700000000
	for i := 1; i < 10; i++ {
		timestamps[i] = timestamps[i-1] + 60
	}
	timestamps[10] = timestamps[9] + 60
	for i := 11; i < 20; i++ {
		timestamps[i] = timestamps[i-1] + 12
	}

	// Alternating large-add / tiny-remove → maximises byte std-dev.
	changes := alternating(20, 5000, 100)

	score := computeDramaScore(20, 5, 16, changes, timestamps)

	assert.GreaterOrEqual(t, score, 90, "max-drama scenario should score ≥ 90")
	assert.LessOrEqual(t, score, 100, "score must not exceed 100")
	t.Logf("max drama score = %d", score)
}

func TestComputeDramaScore_HeatedEditWar_HighScore(t *testing.T) {
	// Realistic heated edit war: 12 edits over 25 minutes, 3 editors, 9 reverts.
	// 75% revert rate + moderate velocity + multiple editors.
	// Expected: 40–70.

	changes := alternating(12, 800, 780)
	timestamps := ts(12, 120) // every 2 minutes

	score := computeDramaScore(12, 3, 9, changes, timestamps)

	assert.GreaterOrEqual(t, score, 40, "heated edit war should score ≥ 40")
	assert.LessOrEqual(t, score, 75, "heated edit war should score ≤ 75")
	t.Logf("heated edit war score = %d", score)
}

func TestComputeDramaScore_MildEditWar_MediumScore(t *testing.T) {
	// Minimum meaningful edit war: 6 edits over 90 minutes, 2 editors, 3 reverts.
	// Should register as low–medium drama, not critical.
	// Expected: 10–40.

	changes := alternating(6, 200, 190)
	timestamps := ts(6, 1080) // every 18 minutes

	score := computeDramaScore(6, 2, 3, changes, timestamps)

	assert.GreaterOrEqual(t, score, 10, "mild edit war should score ≥ 10")
	assert.LessOrEqual(t, score, 40, "mild edit war should score ≤ 40")
	t.Logf("mild edit war score = %d", score)
}

// ─── No-drama / false-positive prevention ───────────────────────────────────

func TestComputeDramaScore_CollaborativeEditing_LowScore(t *testing.T) {
	// 8 constructive edits by 3 editors, all adding content, no reverts.
	// This is a normal, healthy article. Should never register as "dramatic".
	// Expected: ≤ 20.

	changes := allPositive(8, 200)
	timestamps := ts(8, 1350) // every 22.5 minutes → 3 hours total

	score := computeDramaScore(8, 3, 0, changes, timestamps)

	assert.LessOrEqual(t, score, 20,
		"healthy collaborative editing should score ≤ 20 (no false drama)")
	t.Logf("collaborative editing score = %d", score)
}

func TestComputeDramaScore_SingleEditor_LowScore(t *testing.T) {
	// One editor making edits at a leisurely pace (e.g., fixing typos across a
	// long article over 30 minutes). No conflict at all. Should score very low.
	// Expected: ≤ 20.

	changes := allPositive(10, 50)
	timestamps := ts(10, 200) // every ~3 minutes — not particularly fast

	score := computeDramaScore(10, 1, 0, changes, timestamps)

	assert.LessOrEqual(t, score, 20,
		"single editor at leisurely pace should score ≤ 20")
	t.Logf("single editor score = %d", score)
}

func TestComputeDramaScore_HighVelocityNoReverts_NotDramatic(t *testing.T) {
	// 20 edits in 5 minutes — very fast — but zero reverts.
	// Velocity alone should not produce a drama score high enough to trigger alerts.
	// Expected: ≤ 35.

	changes := allPositive(20, 100)
	timestamps := ts(20, 15) // every 15 seconds

	score := computeDramaScore(20, 2, 0, changes, timestamps)

	assert.LessOrEqual(t, score, 35,
		"high velocity without reverts should not produce dramatic score")
	t.Logf("high velocity no reverts score = %d", score)
}

func TestComputeDramaScore_ManyEditorsLowConflict_ModerateScore(t *testing.T) {
	// 6 editors each making 1–2 edits, only 1 revert out of 10 edits.
	// Diverse participation but little conflict. Should stay moderate.
	// Expected: ≤ 40.

	changes := []int{100, 300, -50, 200, 400, 150, 250, 80, 350, 200}
	timestamps := ts(10, 360) // every 6 minutes over 1 hour

	score := computeDramaScore(10, 6, 1, changes, timestamps)

	assert.LessOrEqual(t, score, 40,
		"many editors with few reverts should score ≤ 40")
	t.Logf("many editors low conflict score = %d", score)
}

// ─── Escalation signal ───────────────────────────────────────────────────────

func TestComputeDramaScore_EscalatingWar_HigherThanSteady(t *testing.T) {
	// Same edit war (6/8 reverts) compared with steady vs escalating timelines.
	// Escalating (second half 10× faster) should score noticeably higher.

	changes := alternating(8, 500, 490)
	// Steady: every 4 minutes
	steadyTS := ts(8, 240)
	// Escalating: first 4 edits every 10 min, last 4 every 1 min
	escalatingTS := tsSplit(8, 600, 60)

	steadyScore := computeDramaScore(8, 2, 6, changes, steadyTS)
	escalatingScore := computeDramaScore(8, 2, 6, changes, escalatingTS)

	assert.Greater(t, escalatingScore, steadyScore,
		"escalating war should score higher than same war with steady pace")
	t.Logf("steady score=%d  escalating score=%d", steadyScore, escalatingScore)
}

func TestComputeDramaScore_CoolingWar_LowerThanSteady(t *testing.T) {
	// Same edit war but second half is much slower (cooling down).
	// Should score lower than steady pace (no escalation bonus).

	changes := alternating(8, 500, 490)
	steadyTS := ts(8, 240)
	// Cooling: first 4 edits every 1 min, last 4 every 10 min
	coolingTS := tsSplit(8, 60, 600)

	steadyScore := computeDramaScore(8, 2, 6, changes, steadyTS)
	coolingScore := computeDramaScore(8, 2, 6, changes, coolingTS)

	assert.LessOrEqual(t, coolingScore, steadyScore,
		"cooling war should not score higher than steady")
	t.Logf("steady score=%d  cooling score=%d", steadyScore, coolingScore)
}

// ─── Edge cases ──────────────────────────────────────────────────────────────

func TestComputeDramaScore_EmptyInputs_Zero(t *testing.T) {
	score := computeDramaScore(0, 0, 0, nil, nil)
	assert.Equal(t, 0, score, "empty inputs should produce score of 0")
}

func TestComputeDramaScore_SingleEdit_Low(t *testing.T) {
	score := computeDramaScore(1, 1, 0, []int{500}, []int64{1700000000})
	assert.LessOrEqual(t, score, 20, "single edit should score ≤ 20")
	t.Logf("single edit score = %d", score)
}

func TestComputeDramaScore_AlwaysCappedAt100(t *testing.T) {
	// Feed absurdly extreme values to verify the score never exceeds 100.
	changes := alternating(1000, 100000, 1)
	timestamps := ts(1000, 1) // 1 second apart — unrealistically fast

	score := computeDramaScore(1000, 100, 999, changes, timestamps)
	assert.LessOrEqual(t, score, 100, "score must never exceed 100")
	assert.GreaterOrEqual(t, score, 0, "score must never go below 0")
	t.Logf("extreme inputs score = %d", score)
}

func TestComputeDramaScore_NoTimestamps_PartialScore(t *testing.T) {
	// No timestamp data — velocity and escalation signals are unavailable,
	// but the function should not panic and should still score the other signals.

	changes := alternating(10, 500, 490)
	score := computeDramaScore(10, 3, 8, changes, nil)

	assert.GreaterOrEqual(t, score, 0, "should not produce negative score")
	assert.LessOrEqual(t, score, 100, "should not exceed 100")
	t.Logf("no timestamps score = %d (velocity/escalation unavailable)", score)
}

// ─── Signal isolation ────────────────────────────────────────────────────────

func TestComputeDramaScore_RevertRatioSignal(t *testing.T) {
	// Hold all other signals at their minimum and vary only the revert ratio.
	// 2 editors, minimal velocity, uniform small byte changes, no escalation.
	changes := alternating(20, 100, 100) // zero std-dev
	timestamps := ts(20, 3600)           // one edit per hour — minimal velocity

	zeroReverts := computeDramaScore(20, 2, 0, changes, timestamps)
	halfReverts := computeDramaScore(20, 2, 10, changes, timestamps)
	fullReverts := computeDramaScore(20, 2, 15, changes, timestamps) // 75%

	assert.Less(t, zeroReverts, halfReverts,
		"more reverts should produce higher score")
	assert.LessOrEqual(t, halfReverts, fullReverts,
		"75%+ revert rate should max the revert signal")
	t.Logf("0 reverts=%d  10/20=%d  15/20=%d", zeroReverts, halfReverts, fullReverts)
}

func TestComputeDramaScore_ByteVolatilitySignal(t *testing.T) {
	// Hold all other signals fixed and vary only the size uniformity.
	// Uniform changes (low std-dev) vs highly variable changes (high std-dev).
	timestamps := ts(10, 60)

	uniformChanges := alternating(10, 500, 499) // abs-stddev ≈ 0.5 bytes
	variableChanges := []int{5000, -100, 5000, -100, 5000, -100, 5000, -100, 5000, -100}

	uniformScore := computeDramaScore(10, 2, 7, uniformChanges, timestamps)
	variableScore := computeDramaScore(10, 2, 7, variableChanges, timestamps)

	assert.Greater(t, variableScore, uniformScore,
		"highly variable byte changes should produce higher drama score")
	t.Logf("uniform changes score=%d  variable changes score=%d", uniformScore, variableScore)
}

func TestComputeDramaScore_EditorCountSignal(t *testing.T) {
	// Same edit war with different numbers of editors.
	changes := alternating(12, 400, 390)
	timestamps := ts(12, 120)

	twoEditors := computeDramaScore(12, 2, 9, changes, timestamps)
	fiveEditors := computeDramaScore(12, 5, 9, changes, timestamps)

	assert.Greater(t, fiveEditors, twoEditors,
		"more editors should increase the drama score")
	t.Logf("2 editors=%d  5 editors=%d", twoEditors, fiveEditors)
}

// ─── Severity mapping ────────────────────────────────────────────────────────

func TestDramaScoreToSeverity(t *testing.T) {
	tests := []struct {
		score    int
		expected string
	}{
		{0, "low"},
		{20, "low"},
		{39, "low"},
		{40, "medium"},
		{55, "medium"},
		{59, "medium"},
		{60, "high"},
		{75, "high"},
		{79, "high"},
		{80, "critical"},
		{95, "critical"},
		{100, "critical"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := dramaScoreToSeverity(tt.score)
			assert.Equal(t, tt.expected, got,
				"score %d should map to severity %q", tt.score, tt.expected)
		})
	}
}

func TestDramaScoreToSeverity_ThresholdBoundaries(t *testing.T) {
	// Verify the exact boundaries: one below, at, one above each threshold.
	cases := []struct {
		score    int
		expected string
	}{
		{38, "low"}, {39, "low"}, {40, "medium"}, {41, "medium"},
		{58, "medium"}, {59, "medium"}, {60, "high"}, {61, "high"},
		{78, "high"}, {79, "high"}, {80, "critical"}, {81, "critical"},
	}
	for _, c := range cases {
		got := dramaScoreToSeverity(c.score)
		assert.Equal(t, c.expected, got, "score=%d", c.score)
	}
}

// ─── Headline drama ordering ─────────────────────────────────────────────────

func TestComputeDramaScore_MoreDramaticWar_AlwaysScoresHigher(t *testing.T) {
	// A small quiet edit war should always score lower than a raging one.
	// This is the core false-positive/false-negative prevention test.

	// Small, slow, few editors, few reverts
	smallWar := computeDramaScore(
		5, 2, 2,
		alternating(5, 200, 190),
		ts(5, 1800), // every 30 minutes
	)

	// Large, fast, many editors, high revert rate
	largeWar := computeDramaScore(
		20, 5, 16,
		alternating(20, 1000, 980),
		ts(20, 60), // every 1 minute
	)

	assert.Less(t, smallWar, largeWar,
		"small quiet war (score=%d) must score lower than large heated war (score=%d)",
		smallWar, largeWar)
	assert.LessOrEqual(t, smallWar, 35,
		"small war should be low-medium, not shown as alarming (score=%d)", smallWar)
	assert.GreaterOrEqual(t, largeWar, 55,
		"large war should be medium-high (score=%d)", largeWar)

	t.Logf("small quiet war=%d  large heated war=%d", smallWar, largeWar)
}
