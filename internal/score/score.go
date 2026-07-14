// Package score turns per-test run histories into a 0–100 flakiness score
// and a classification.
//
// The model is deliberately explainable. For each test, every executed run
// (skips excluded) contributes a binary "flake event":
//
//	event(i) = recovered-in-run(i)  OR  outcome(i) != outcome(i-1)
//
// i.e. either a retry rescued the run, or the verdict flipped versus the
// previous execution. The score is 100 × the (optionally recency-weighted)
// mean of those events. A test that passes every time scores 0; a test
// that fails every time also scores 0 — that test is *broken*, not flaky,
// and is classified separately so quarantining it doesn't hide a real bug.
// A test that alternates or needs retries approaches 100.
//
// Recency weighting (--half-life N) halves a run's influence every N runs
// of age, so a test that was cured 200 runs ago stops ranking above one
// that started flaking last week.
package score

import (
	"math"
	"sort"

	"github.com/JaydenCJ/flakesift/internal/history"
)

// Params tunes scoring and classification.
type Params struct {
	Threshold float64 // score at or above which a test is "flaky"
	MinRuns   int     // executions required before classifying at all
	HalfLife  float64 // in runs; 0 disables recency weighting
}

// DefaultParams mirrors the CLI defaults.
func DefaultParams() Params {
	return Params{Threshold: 30, MinRuns: 3, HalfLife: 0}
}

// Class buckets a test by what a team should do with it.
type Class string

const (
	ClassHealthy Class = "healthy" // never failed, never needed a retry
	ClassSuspect Class = "suspect" // failed or retried, but below threshold
	ClassFlaky   Class = "flaky"   // score at or above threshold: quarantine
	ClassBroken  Class = "broken"  // failed every execution: fix, don't hide
	ClassNew     Class = "new"     // too few executions to judge
)

// Result is the scored summary of one test.
type Result struct {
	ID          string  `json:"id"`
	ClassName   string  `json:"classname,omitempty"`
	Name        string  `json:"name"`
	Runs        int     `json:"runs"` // executed runs (skips excluded)
	Passes      int     `json:"passes"`
	Fails       int     `json:"fails"`
	Skips       int     `json:"skips"`
	Flips       int     `json:"flips"`     // pass/fail verdict changes across runs
	Recovered   int     `json:"recovered"` // runs rescued by a retry
	FailRate    float64 `json:"fail_rate"` // fails / runs
	Score       float64 `json:"score"`     // 0–100, one decimal
	Class       Class   `json:"class"`
	LastOutcome string  `json:"last_outcome"`
	LastRunID   string  `json:"last_run"`
	LastMessage string  `json:"last_message,omitempty"`
}

// Evaluate scores a single test history.
func Evaluate(t history.Test, p Params) Result {
	r := Result{ID: t.ID, ClassName: t.ClassName, Name: t.Name}

	// Skips are excluded from the event sequence: a skipped run says
	// nothing about stability and must not manufacture flips around it.
	var executed []history.Entry
	for _, e := range t.Entries {
		if e.Outcome == history.Skip {
			r.Skips++
			continue
		}
		executed = append(executed, e)
	}
	r.Runs = len(executed)

	var weightSum, eventSum float64
	n := len(executed)
	for i, e := range executed {
		switch e.Outcome {
		case history.Pass:
			r.Passes++
		case history.Fail:
			r.Fails++
		}
		if e.Recovered {
			r.Recovered++
		}
		flipped := i > 0 && executed[i].Outcome != executed[i-1].Outcome
		if flipped {
			r.Flips++
		}

		w := 1.0
		if p.HalfLife > 0 {
			// Newest run has age 0 → weight 1; each HalfLife of age halves it.
			w = math.Pow(0.5, float64(n-1-i)/p.HalfLife)
		}
		weightSum += w
		if flipped || e.Recovered {
			eventSum += w
		}
		if e.Message != "" {
			r.LastMessage = e.Message
		}
	}

	if n > 0 {
		r.FailRate = round1(100 * float64(r.Fails) / float64(n))
		r.Score = round1(100 * eventSum / weightSum)
		last := executed[n-1]
		r.LastOutcome = last.Outcome.String()
		r.LastRunID = last.RunID
	} else if len(t.Entries) > 0 {
		last := t.Entries[len(t.Entries)-1]
		r.LastOutcome = last.Outcome.String()
		r.LastRunID = last.RunID
	}

	r.Class = classify(r, p)
	return r
}

// EvaluateAll scores every history and orders results by descending score,
// then descending fail rate, then ID — a stable, deterministic ranking.
func EvaluateAll(tests []history.Test, p Params) []Result {
	out := make([]Result, 0, len(tests))
	for _, t := range tests {
		out = append(out, Evaluate(t, p))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].FailRate != out[j].FailRate {
			return out[i].FailRate > out[j].FailRate
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func classify(r Result, p Params) Class {
	switch {
	case r.Runs < p.MinRuns:
		return ClassNew
	case r.Fails == r.Runs && r.Recovered == 0:
		return ClassBroken
	case r.Score >= p.Threshold:
		return ClassFlaky
	case r.Fails > 0 || r.Recovered > 0:
		return ClassSuspect
	default:
		return ClassHealthy
	}
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
