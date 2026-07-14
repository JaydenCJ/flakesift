// Tests for the flakiness model: the score must reward instability, not
// mere failure, and the classes must route each pathology to the right
// team action (quarantine vs fix vs wait).
package score

import (
	"testing"

	"github.com/JaydenCJ/flakesift/internal/history"
)

// hist builds a Test from a compact outcome string: 'p' pass, 'f' fail,
// 's' skip, 'r' recovered pass (retry rescued the run).
func hist(pattern string) history.Test {
	t := history.Test{ID: "pkg::t", ClassName: "pkg", Name: "t"}
	for i, ch := range pattern {
		e := history.Entry{RunID: "r", RunIndex: i, Attempts: 1}
		switch ch {
		case 'p':
			e.Outcome = history.Pass
		case 'f':
			e.Outcome = history.Fail
		case 's':
			e.Outcome = history.Skip
		case 'r':
			e.Outcome = history.Pass
			e.Recovered = true
			e.Retried = true
			e.Attempts = 2
		}
		t.Entries = append(t.Entries, e)
	}
	return t
}

func eval(pattern string) Result {
	return Evaluate(hist(pattern), DefaultParams())
}

func TestStablePassScoresZeroHealthy(t *testing.T) {
	r := eval("pppppppppp")
	if r.Score != 0 {
		t.Errorf("score = %v, want 0", r.Score)
	}
	if r.Class != ClassHealthy {
		t.Errorf("class = %v, want healthy", r.Class)
	}
}

func TestAlwaysFailingIsBrokenNotFlaky(t *testing.T) {
	// The whole point of the broken class: quarantining a 100%-failing
	// test hides a real bug, so it must never rank as flaky.
	r := eval("ffffffffff")
	if r.Score != 0 {
		t.Errorf("score = %v, want 0 (perfectly consistent)", r.Score)
	}
	if r.Class != ClassBroken {
		t.Errorf("class = %v, want broken", r.Class)
	}
	if r.FailRate != 100 {
		t.Errorf("fail rate = %v, want 100", r.FailRate)
	}
}

func TestAlternatingOutcomesScoreNearMaximum(t *testing.T) {
	r := eval("pfpfpfpfpf")
	if r.Score < 85 {
		t.Errorf("score = %v, want near 100 for a coin-flip test", r.Score)
	}
	if r.Class != ClassFlaky {
		t.Errorf("class = %v, want flaky", r.Class)
	}
	if r.Flips != 9 {
		t.Errorf("flips = %d, want 9", r.Flips)
	}
}

func TestAlwaysRecoveringViaRetryScoresMaximum(t *testing.T) {
	// Every run "passes" — but only because a retry rescued it each time.
	// Run-level verdicts alone would call this healthy; the recovery
	// signal is what catches retry-masked flakes.
	r := eval("rrrrrrrrrr")
	if r.Score != 100 {
		t.Errorf("score = %v, want 100", r.Score)
	}
	if r.Class != ClassFlaky {
		t.Errorf("class = %v, want flaky", r.Class)
	}
	if r.Fails != 0 {
		t.Errorf("fails = %d, want 0 (all runs ended green)", r.Fails)
	}
	if r.Recovered != 10 {
		t.Errorf("recovered = %d, want 10", r.Recovered)
	}
}

func TestSingleBlipInLongHistoryStaysBelowThreshold(t *testing.T) {
	// One failure in 20 runs produces two flips (into and out of the
	// failure) — real but small; it must not trigger quarantine.
	r := eval("pppppppppfpppppppppp")
	if r.Score >= DefaultParams().Threshold {
		t.Errorf("score = %v, want < %v", r.Score, DefaultParams().Threshold)
	}
	if r.Class != ClassSuspect {
		t.Errorf("class = %v, want suspect (failed once, below threshold)", r.Class)
	}
	if r.Flips != 2 {
		t.Errorf("flips = %d, want 2", r.Flips)
	}
}

func TestScoreValueExamples(t *testing.T) {
	// Exact expected values pin the formula: events / executed runs × 100,
	// rounded to one decimal.
	for _, tc := range []struct {
		pattern string
		want    float64
		why     string
	}{
		{"pppppfffff", 10, "one flip in ten runs — a regression, not churn"},
		{"pfr", 66.7, "flip + flip-with-recovery in three runs, rounded"},
		{"ppppr", 20, "one recovery in five runs"},
	} {
		if r := eval(tc.pattern); r.Score != tc.want {
			t.Errorf("%q score = %v, want %v (%s)", tc.pattern, r.Score, tc.want, tc.why)
		}
	}
}

func TestSkipsAreExcludedFromEventMath(t *testing.T) {
	// p s f: the skip must not double the flip count or add a run.
	r := eval("psf")
	if r.Runs != 2 {
		t.Errorf("runs = %d, want 2 executed", r.Runs)
	}
	if r.Skips != 1 {
		t.Errorf("skips = %d, want 1", r.Skips)
	}
	if r.Flips != 1 {
		t.Errorf("flips = %d, want 1 (pass→fail across the skip)", r.Flips)
	}
	// All-skip history must not divide by zero.
	r = eval("sss")
	if r.Runs != 0 || r.Skips != 3 || r.Score != 0 {
		t.Errorf("all-skip result = %+v, want zeroed", r)
	}
	if r.Class != ClassNew || r.LastOutcome != "skip" {
		t.Errorf("all-skip class/last = %v/%q, want new/skip", r.Class, r.LastOutcome)
	}
}

func TestFewerThanMinRunsClassifiesNew(t *testing.T) {
	r := eval("pf")
	if r.Class != ClassNew {
		t.Errorf("class = %v, want new (2 runs < min 3)", r.Class)
	}
	if r.Score == 0 {
		t.Error("score should still be computed for new tests")
	}
	// Skips are not evidence: two executions + five skips is still "new".
	if r := eval("psssssf"); r.Class != ClassNew {
		t.Errorf("class = %v, want new despite 7 entries", r.Class)
	}
}

func TestBrokenRequiresNoRecoveredRuns(t *testing.T) {
	// fail, fail, recovered-pass … every *verdict* below is fail-heavy but
	// the recovery proves nondeterminism → flaky territory, not broken.
	tst := hist("ffr")
	r := Evaluate(tst, Params{Threshold: 30, MinRuns: 3, HalfLife: 0})
	if r.Class == ClassBroken {
		t.Errorf("class = broken, but a recovered run proves flakiness")
	}
}

func TestHalfLifeWeightsRecentEventsHigher(t *testing.T) {
	// Same events, opposite ends of history: flaky-then-cured vs
	// recently-flaky. With recency weighting the recent flake must score
	// strictly higher.
	p := Params{Threshold: 30, MinRuns: 3, HalfLife: 5}
	cured := Evaluate(hist("pfpfpfpppppppppppppp"), p)
	recent := Evaluate(hist("ppppppppppppppfpfpfp"), p)
	if recent.Score <= cured.Score {
		t.Errorf("recent = %v, cured = %v; want recent > cured", recent.Score, cured.Score)
	}
	// Without weighting the two mirror histories score identically.
	u := DefaultParams()
	a, b := Evaluate(hist("pfpfpfpppppppppppppp"), u), Evaluate(hist("ppppppppppppppfpfpfp"), u)
	if a.Score != b.Score {
		t.Errorf("unweighted mirror scores differ: %v vs %v", a.Score, b.Score)
	}
	// Weighting must not manufacture a score for a stable test.
	if r := Evaluate(hist("pppppppppp"), p); r.Score != 0 {
		t.Errorf("weighted stable score = %v, want 0", r.Score)
	}
}

func TestThresholdBoundaryIsInclusive(t *testing.T) {
	// Exactly 1 event in 5 runs = 20.0; threshold 20 must classify flaky.
	r := Evaluate(hist("ppppr"), Params{Threshold: 20, MinRuns: 3, HalfLife: 0})
	if r.Score != 20 {
		t.Fatalf("score = %v, want 20", r.Score)
	}
	if r.Class != ClassFlaky {
		t.Errorf("class = %v, want flaky at inclusive threshold", r.Class)
	}
}

func TestLastOutcomeRunAndMessageReported(t *testing.T) {
	// The report shows where to start digging: the most recent run, its
	// verdict, and the most recent failure message.
	tst := hist("pff")
	tst.Entries[1].Message = "old"
	tst.Entries[2].RunID = "run-003"
	tst.Entries[2].Message = "connection reset"
	r := Evaluate(tst, DefaultParams())
	if r.LastOutcome != "fail" || r.LastRunID != "run-003" {
		t.Errorf("last = %s@%s, want fail@run-003", r.LastOutcome, r.LastRunID)
	}
	if r.LastMessage != "connection reset" {
		t.Errorf("last message = %q", r.LastMessage)
	}
}

func TestEvaluateAllSortsByScoreThenFailRateThenID(t *testing.T) {
	mk := func(id, pattern string) history.Test {
		h := hist(pattern)
		h.ID = id
		return h
	}
	results := EvaluateAll([]history.Test{
		mk("b::steady", "pppppppppp"),
		mk("a::coin", "pfpfpfpfpf"),
		mk("c::blip", "pppppfpppp"),
		mk("a::steady", "pppppppppp"),
	}, DefaultParams())
	wantOrder := []string{"a::coin", "c::blip", "a::steady", "b::steady"}
	for i, w := range wantOrder {
		if results[i].ID != w {
			t.Fatalf("order[%d] = %q, want %q", i, results[i].ID, w)
		}
	}
}

func TestEvaluateAllTieBreaksOnFailRate(t *testing.T) {
	// Same score (one event in 10 runs), different fail rates.
	flip := hist("pppppfffff") // 1 flip, 5 fails
	flip.ID = "z::flip"
	rec := hist("pppppppppr") // 1 recovery, 0 fails
	rec.ID = "a::rec"
	results := EvaluateAll([]history.Test{rec, flip}, DefaultParams())
	if results[0].ID != "z::flip" {
		t.Errorf("order[0] = %q, want z::flip (higher fail rate wins the tie)", results[0].ID)
	}
}

func TestFailRateIsPercentageOfExecutedRuns(t *testing.T) {
	r := eval("pfps")
	if r.FailRate != 33.3 {
		t.Errorf("fail rate = %v, want 33.3 (1 fail / 3 executed)", r.FailRate)
	}
}
