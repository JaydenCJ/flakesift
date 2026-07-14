// Tests for time bucketing and sparkline rendering.
package trend

import (
	"testing"

	"github.com/JaydenCJ/flakesift/internal/history"
)

// mkTest builds a history from an outcome pattern over sequential runs:
// 'p' pass, 'f' fail, 's' skip, 'r' recovered pass.
func mkTest(id, pattern string) history.Test {
	t := history.Test{ID: id, Name: id}
	for i, ch := range pattern {
		e := history.Entry{RunID: runID(i), RunIndex: i, Attempts: 1}
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
		}
		t.Entries = append(t.Entries, e)
	}
	return t
}

func runID(i int) string {
	return string(rune('a' + i))
}

func ids(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = runID(i)
	}
	return out
}

func TestComputeSplitsRunsEvenly(t *testing.T) {
	bs := Compute(nil, ids(10), 5)
	if len(bs) != 5 {
		t.Fatalf("buckets = %d, want 5", len(bs))
	}
	for i, b := range bs {
		if b.Runs != 2 {
			t.Errorf("bucket %d runs = %d, want 2", i, b.Runs)
		}
	}
	// Uneven split: every run lands somewhere, sizes differ by at most one.
	total := 0
	for _, b := range Compute(nil, ids(7), 3) {
		if b.Runs < 2 || b.Runs > 3 {
			t.Errorf("bucket %d runs = %d, want 2 or 3", b.Index, b.Runs)
		}
		total += b.Runs
	}
	if total != 7 {
		t.Errorf("total runs = %d, want 7", total)
	}
}

func TestComputeClampsBucketsToRunCount(t *testing.T) {
	bs := Compute(nil, ids(3), 10)
	if len(bs) != 3 {
		t.Fatalf("buckets = %d, want 3 (one per run)", len(bs))
	}
	if bs := Compute(nil, nil, 5); bs != nil {
		t.Errorf("buckets for zero runs = %v, want nil", bs)
	}
}

func TestComputeCountsFailsPerBucket(t *testing.T) {
	// 4 runs, 2 buckets: fails land in [pf][fp] → 1 fail each.
	bs := Compute([]history.Test{mkTest("t", "pffp")}, ids(4), 2)
	if bs[0].Fails != 1 || bs[1].Fails != 1 {
		t.Errorf("fails = %d/%d, want 1/1", bs[0].Fails, bs[1].Fails)
	}
	if bs[0].Executions != 2 || bs[1].Executions != 2 {
		t.Errorf("executions = %d/%d, want 2/2", bs[0].Executions, bs[1].Executions)
	}
}

func TestComputeFlakeEventsMatchScorerDefinition(t *testing.T) {
	// p f p r: events at index 1 (flip), 2 (flip), 3 (recovery) = 3 total.
	bs := Compute([]history.Test{mkTest("t", "pfpr")}, ids(4), 1)
	if bs[0].FlakeEvents != 3 {
		t.Errorf("flake events = %d, want 3", bs[0].FlakeEvents)
	}
}

func TestComputeFlipAcrossBucketBoundaryCountsInLaterBucket(t *testing.T) {
	// pp|fp: the pass→fail flip happens at run 2 → bucket 1.
	bs := Compute([]history.Test{mkTest("t", "ppfp")}, ids(4), 2)
	if bs[0].FlakeEvents != 0 {
		t.Errorf("bucket 0 events = %d, want 0", bs[0].FlakeEvents)
	}
	if bs[1].FlakeEvents != 2 {
		t.Errorf("bucket 1 events = %d, want 2 (flip in, flip out)", bs[1].FlakeEvents)
	}
}

func TestComputeSkipsDoNotCountAsExecutions(t *testing.T) {
	bs := Compute([]history.Test{mkTest("t", "psfp")}, ids(4), 1)
	if bs[0].Executions != 3 {
		t.Errorf("executions = %d, want 3", bs[0].Executions)
	}
}

func TestComputeRatesArePercentages(t *testing.T) {
	// 4 executions, 2 fails → 50%; events: flip at 1... pattern pffp:
	// flip at index 1 (p→f) and index 3 (f→p) → 2 events → 50%.
	bs := Compute([]history.Test{mkTest("t", "pffp")}, ids(4), 1)
	if bs[0].FailRate != 50 {
		t.Errorf("fail rate = %v, want 50", bs[0].FailRate)
	}
	if bs[0].FlakeRate != 50 {
		t.Errorf("flake rate = %v, want 50", bs[0].FlakeRate)
	}
}

func TestComputeBucketSpanNamesFirstAndLastRun(t *testing.T) {
	bs := Compute(nil, ids(4), 2)
	if bs[0].FirstRun != "a" || bs[0].LastRun != "b" {
		t.Errorf("bucket 0 span = %s…%s, want a…b", bs[0].FirstRun, bs[0].LastRun)
	}
	if bs[1].FirstRun != "c" || bs[1].LastRun != "d" {
		t.Errorf("bucket 1 span = %s…%s, want c…d", bs[1].FirstRun, bs[1].LastRun)
	}
}

func TestSparklineRendering(t *testing.T) {
	if got := Sparkline([]float64{0, 50, 100}); got != "▁▅█" {
		t.Errorf("scaled sparkline = %q, want ▁▅█", got)
	}
	// All-zero keeps its width (a flat healthy trend is still a trend).
	if got := Sparkline([]float64{0, 0, 0, 0}); got != "▁▁▁▁" {
		t.Errorf("all-zero sparkline = %q, want four minimum bars", got)
	}
	if got := Sparkline(nil); got != "" {
		t.Errorf("empty sparkline = %q, want empty string", got)
	}
}
