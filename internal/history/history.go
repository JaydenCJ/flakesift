// Package history condenses raw JUnit cases into one per-run outcome per
// test, across the whole ordered run sequence.
//
// A test can appear several times inside one run — classic retry plugins
// emit one <testcase> per attempt, Surefire folds attempts into
// <flakyFailure>/<rerunFailure> children. Both shapes collapse into the
// same Entry: the run-level outcome plus whether the test *recovered*
// (failed, then passed, inside the same run). Recovery is the strongest
// per-run flake signal and is scored separately from run-to-run flips.
package history

import (
	"sort"

	"github.com/JaydenCJ/flakesift/internal/ingest"
	"github.com/JaydenCJ/flakesift/internal/junit"
)

// Outcome is the run-level result of a test after retries are folded in.
type Outcome int

const (
	Pass Outcome = iota
	Fail
	Skip
)

// String returns the lowercase human label for an outcome.
func (o Outcome) String() string {
	switch o {
	case Pass:
		return "pass"
	case Fail:
		return "fail"
	case Skip:
		return "skip"
	}
	return "unknown"
}

// Entry is what happened to one test in one run.
type Entry struct {
	RunID    string
	RunIndex int // position in the chronological run order
	Outcome  Outcome
	Attempts int // executions inside the run, retries included
	// Recovered means the run contains both a failed and a passing attempt
	// of this test — a retry masked a failure.
	Recovered bool
	// Retried means the test ran more than once in this run, whatever the
	// final outcome (covers rerunFailure: retried and failed again).
	Retried bool
	Message string // last failure message seen in the run, if any
}

// Test is the full cross-run history of one test.
type Test struct {
	ID        string
	ClassName string
	Name      string
	Entries   []Entry // chronological run order
}

// Build turns ordered runs into per-test histories, sorted by test ID so
// downstream output is deterministic.
func Build(runs []ingest.Run) []Test {
	byID := make(map[string]*Test)
	var order []string

	for runIndex, run := range runs {
		grouped := make(map[string][]junit.Case)
		var caseOrder []string
		for _, c := range run.Cases {
			id := c.ID()
			if _, ok := grouped[id]; !ok {
				caseOrder = append(caseOrder, id)
			}
			grouped[id] = append(grouped[id], c)
		}
		for _, id := range caseOrder {
			cases := grouped[id]
			t, ok := byID[id]
			if !ok {
				t = &Test{ID: id, ClassName: cases[0].ClassName, Name: cases[0].Name}
				byID[id] = t
				order = append(order, id)
			}
			t.Entries = append(t.Entries, condense(run.ID, runIndex, cases))
		}
	}

	sort.Strings(order)
	out := make([]Test, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out
}

// condense folds every attempt of one test inside one run into an Entry.
func condense(runID string, runIndex int, cases []junit.Case) Entry {
	e := Entry{RunID: runID, RunIndex: runIndex}
	passes, fails := 0, 0
	for _, c := range cases {
		e.Attempts++ // the recorded case itself
		e.Attempts += c.FlakyAttempts + c.RerunAttempts
		switch c.Status {
		case junit.StatusPass:
			passes++
		case junit.StatusFail, junit.StatusError:
			fails++
			e.Message = c.Message
		}
		// Surefire semantics: flakyFailure children hang off a case that
		// ultimately passed; rerunFailure children off one that failed again.
		if c.FlakyAttempts > 0 {
			fails++ // at least one earlier attempt failed
		}
		if c.FlakyAttempts+c.RerunAttempts > 0 {
			e.Retried = true
		}
	}
	if len(cases) > 1 {
		e.Retried = true
	}
	switch {
	case passes > 0 && fails > 0:
		e.Outcome = Pass // a retry rescued the run
		e.Recovered = true
		e.Retried = true
	case fails > 0:
		e.Outcome = Fail
	case passes > 0:
		e.Outcome = Pass
	default:
		e.Outcome = Skip
	}
	return e
}
