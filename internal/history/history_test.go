// Tests for run-level outcome condensation: retries, Surefire rerun
// markers, skips, and cross-run ordering.
package history

import (
	"testing"

	"github.com/JaydenCJ/flakesift/internal/ingest"
	"github.com/JaydenCJ/flakesift/internal/junit"
)

func mkCase(name string, status junit.Status) junit.Case {
	return junit.Case{ClassName: "pkg", Name: name, Status: status}
}

func mkRun(id string, cases ...junit.Case) ingest.Run {
	return ingest.Run{ID: id, Cases: cases}
}

func find(t *testing.T, tests []Test, id string) Test {
	t.Helper()
	for _, tt := range tests {
		if tt.ID == id {
			return tt
		}
	}
	t.Fatalf("test %q not in %d histories", id, len(tests))
	return Test{}
}

func TestBuildSingleTestAcrossRunsKeepsRunOrder(t *testing.T) {
	tests := Build([]ingest.Run{
		mkRun("r1", mkCase("t", junit.StatusPass)),
		mkRun("r2", mkCase("t", junit.StatusFail)),
		mkRun("r3", mkCase("t", junit.StatusPass)),
	})
	h := find(t, tests, "pkg::t")
	if len(h.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(h.Entries))
	}
	want := []Outcome{Pass, Fail, Pass}
	for i, e := range h.Entries {
		if e.Outcome != want[i] {
			t.Errorf("entry %d outcome = %v, want %v", i, e.Outcome, want[i])
		}
		if e.RunIndex != i {
			t.Errorf("entry %d run index = %d, want %d", i, e.RunIndex, i)
		}
	}
}

func TestBuildRetryFailThenPassIsRecoveredPass(t *testing.T) {
	// Retry plugins emit one <testcase> per attempt. Fail-then-pass in one
	// run is the canonical masked flake.
	tests := Build([]ingest.Run{
		mkRun("r1", mkCase("t", junit.StatusFail), mkCase("t", junit.StatusPass)),
	})
	e := find(t, tests, "pkg::t").Entries[0]
	if e.Outcome != Pass {
		t.Errorf("outcome = %v, want pass", e.Outcome)
	}
	if !e.Recovered || !e.Retried {
		t.Errorf("recovered/retried = %v/%v, want true/true", e.Recovered, e.Retried)
	}
	if e.Attempts != 2 {
		t.Errorf("attempts = %d, want 2", e.Attempts)
	}
}

func TestBuildAllAttemptsFailIsFail(t *testing.T) {
	tests := Build([]ingest.Run{
		mkRun("r1", mkCase("t", junit.StatusFail), mkCase("t", junit.StatusFail)),
	})
	e := find(t, tests, "pkg::t").Entries[0]
	if e.Outcome != Fail {
		t.Errorf("outcome = %v, want fail", e.Outcome)
	}
	if e.Recovered {
		t.Error("recovered = true, want false (never passed)")
	}
	if !e.Retried {
		t.Error("retried = false, want true (two attempts)")
	}
}

func TestBuildSurefireMarkersMapToRecoveryAndRetry(t *testing.T) {
	// Surefire folds attempts into one <testcase>: <flakyFailure> children
	// hang off a case that finally passed (→ recovered), <rerunFailure>
	// children off one that failed again (→ retried, not recovered).
	flaky := mkCase("t", junit.StatusPass)
	flaky.FlakyAttempts = 2
	e := find(t, Build([]ingest.Run{mkRun("r1", flaky)}), "pkg::t").Entries[0]
	if e.Outcome != Pass || !e.Recovered {
		t.Errorf("flaky entry = %+v, want recovered pass", e)
	}
	if e.Attempts != 3 {
		t.Errorf("attempts = %d, want 3 (1 recorded + 2 flaky)", e.Attempts)
	}

	rerun := mkCase("t", junit.StatusFail)
	rerun.RerunAttempts = 1
	e = find(t, Build([]ingest.Run{mkRun("r1", rerun)}), "pkg::t").Entries[0]
	if e.Outcome != Fail || e.Recovered || !e.Retried {
		t.Errorf("rerun entry = %+v, want retried fail without recovery", e)
	}
}

func TestBuildOutcomeMapping(t *testing.T) {
	// Errors are failures for flake math; a skip-only run is a skip.
	tests := Build([]ingest.Run{mkRun("r1", mkCase("t", junit.StatusError))})
	if got := find(t, tests, "pkg::t").Entries[0].Outcome; got != Fail {
		t.Errorf("error outcome = %v, want fail", got)
	}
	tests = Build([]ingest.Run{mkRun("r1", mkCase("t", junit.StatusSkip))})
	if got := find(t, tests, "pkg::t").Entries[0].Outcome; got != Skip {
		t.Errorf("skip outcome = %v, want skip", got)
	}
}

func TestBuildTestAbsentFromARunLeavesNoEntry(t *testing.T) {
	// Absence (test not selected, shard moved) is not a skip: it must not
	// appear in the history at all.
	tests := Build([]ingest.Run{
		mkRun("r1", mkCase("t", junit.StatusPass)),
		mkRun("r2", mkCase("other", junit.StatusPass)),
		mkRun("r3", mkCase("t", junit.StatusPass)),
	})
	h := find(t, tests, "pkg::t")
	if len(h.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(h.Entries))
	}
	if h.Entries[1].RunIndex != 2 {
		t.Errorf("second entry run index = %d, want 2", h.Entries[1].RunIndex)
	}
}

func TestBuildKeepsFailureMessageFromRun(t *testing.T) {
	c := mkCase("t", junit.StatusFail)
	c.Message = "connection refused to 127.0.0.1:5432"
	tests := Build([]ingest.Run{mkRun("r1", c)})
	if got := find(t, tests, "pkg::t").Entries[0].Message; got != c.Message {
		t.Errorf("message = %q, want %q", got, c.Message)
	}
}

func TestBuildSeparatesClassesAndSortsByID(t *testing.T) {
	// Same test name under different classes are distinct tests, and the
	// result comes back sorted by ID for deterministic output.
	tests := Build([]ingest.Run{mkRun("r1",
		junit.Case{ClassName: "z", Name: "t", Status: junit.StatusPass},
		junit.Case{ClassName: "a", Name: "t", Status: junit.StatusFail},
		junit.Case{ClassName: "m", Name: "t", Status: junit.StatusPass},
	)})
	if len(tests) != 3 {
		t.Fatalf("tests = %d, want 3", len(tests))
	}
	want := []string{"a::t", "m::t", "z::t"}
	for i, tt := range tests {
		if tt.ID != want[i] {
			t.Fatalf("order[%d] = %q, want %q", i, tt.ID, want[i])
		}
	}
}

func TestOutcomeStringLabels(t *testing.T) {
	for _, tc := range []struct {
		o    Outcome
		want string
	}{{Pass, "pass"}, {Fail, "fail"}, {Skip, "skip"}} {
		if got := tc.o.String(); got != tc.want {
			t.Errorf("%d.String() = %q, want %q", tc.o, got, tc.want)
		}
	}
}
