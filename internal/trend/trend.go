// Package trend buckets the chronological run sequence and measures how
// failure and flake-event rates move over time — is the suite getting
// better or worse, and did last month's quarantine actually help?
package trend

import (
	"math"
	"strings"

	"github.com/JaydenCJ/flakesift/internal/history"
)

// Bucket aggregates a contiguous slice of the run sequence.
type Bucket struct {
	Index       int     `json:"index"`
	FirstRun    string  `json:"first_run"`
	LastRun     string  `json:"last_run"`
	Runs        int     `json:"runs"`
	Executions  int     `json:"executions"` // non-skip test executions
	Fails       int     `json:"fails"`
	FlakeEvents int     `json:"flake_events"`
	FailRate    float64 `json:"fail_rate"`  // percent
	FlakeRate   float64 `json:"flake_rate"` // percent
}

// Compute splits runCount chronological runs into at most want buckets
// (evenly, earlier buckets never smaller than later ones by more than one
// run) and attributes every execution, failure, and flake event of the
// given histories to its bucket. runIDs must be the ordered run IDs.
func Compute(tests []history.Test, runIDs []string, want int) []Bucket {
	runCount := len(runIDs)
	if runCount == 0 {
		return nil
	}
	if want < 1 {
		want = 1
	}
	if want > runCount {
		want = runCount
	}

	buckets := make([]Bucket, want)
	// bucketOf maps a run index to its bucket with even split semantics:
	// run i of n lands in bucket i*want/n.
	bucketOf := func(runIndex int) int {
		return runIndex * want / runCount
	}
	for i := range buckets {
		buckets[i].Index = i
	}
	for i, id := range runIDs {
		b := &buckets[bucketOf(i)]
		if b.Runs == 0 {
			b.FirstRun = id
		}
		b.LastRun = id
		b.Runs++
	}

	for _, t := range tests {
		executedBefore := false // a first execution has no previous verdict to flip against
		var prevOutcome history.Outcome
		for _, e := range t.Entries {
			if e.Outcome == history.Skip {
				continue
			}
			b := &buckets[bucketOf(e.RunIndex)]
			b.Executions++
			if e.Outcome == history.Fail {
				b.Fails++
			}
			// Same flake-event definition as the scorer: recovery inside
			// the run, or a verdict flip versus the previous execution.
			if e.Recovered || (executedBefore && e.Outcome != prevOutcome) {
				b.FlakeEvents++
			}
			executedBefore = true
			prevOutcome = e.Outcome
		}
	}

	for i := range buckets {
		if buckets[i].Executions > 0 {
			n := float64(buckets[i].Executions)
			buckets[i].FailRate = round1(100 * float64(buckets[i].Fails) / n)
			buckets[i].FlakeRate = round1(100 * float64(buckets[i].FlakeEvents) / n)
		}
	}
	return buckets
}

var sparkTicks = []rune("▁▂▃▄▅▆▇█")

// Sparkline renders values as a unicode bar strip, scaled to the maximum.
// All-zero input renders as all-minimum bars rather than an empty string,
// so trend rows keep their width.
func Sparkline(vals []float64) string {
	if len(vals) == 0 {
		return ""
	}
	max := 0.0
	for _, v := range vals {
		if v > max {
			max = v
		}
	}
	var b strings.Builder
	for _, v := range vals {
		idx := 0
		if max > 0 {
			idx = int(math.Round(v / max * float64(len(sparkTicks)-1)))
		}
		b.WriteRune(sparkTicks[idx])
	}
	return b.String()
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
