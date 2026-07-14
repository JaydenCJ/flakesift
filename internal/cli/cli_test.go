// In-process integration tests: they drive cli.Main exactly as main()
// does, against a fabricated 12-run JUnit history covering every test
// pathology (stable, coin-flip, retry-masked, broken, new).
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeHistory fabricates 12 runs in dir, one XML file per run:
//
//	pkg.Util::testParse         — passes every run           → healthy
//	pkg.Cart::testCheckoutRace  — alternates pass/fail       → flaky
//	pkg.Auth::testLoginRetry    — fails then passes in-run   → flaky (retry-masked)
//	pkg.DB::testMigration       — fails every run            → broken
//	pkg.New::testFresh          — only in the last two runs  → new
func writeHistory(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for i := 1; i <= 12; i++ {
		var cases strings.Builder
		cases.WriteString(`    <testcase classname="pkg.Util" name="testParse" time="0.01"/>` + "\n")
		if i%2 == 0 {
			cases.WriteString(`    <testcase classname="pkg.Cart" name="testCheckoutRace"><failure message="cart total mismatch"/></testcase>` + "\n")
		} else {
			cases.WriteString(`    <testcase classname="pkg.Cart" name="testCheckoutRace"/>` + "\n")
		}
		if i%3 == 0 {
			// Retry plugin shape: failed attempt then passing attempt.
			cases.WriteString(`    <testcase classname="pkg.Auth" name="testLoginRetry"><failure message="token race"/></testcase>` + "\n")
			cases.WriteString(`    <testcase classname="pkg.Auth" name="testLoginRetry"/>` + "\n")
		} else {
			cases.WriteString(`    <testcase classname="pkg.Auth" name="testLoginRetry"/>` + "\n")
		}
		cases.WriteString(`    <testcase classname="pkg.DB" name="testMigration"><error message="schema drift"/></testcase>` + "\n")
		if i >= 11 {
			cases.WriteString(`    <testcase classname="pkg.New" name="testFresh"/>` + "\n")
		}
		xml := fmt.Sprintf(`<testsuites>
  <testsuite name="unit" timestamp="2026-03-%02dT10:00:00">
%s  </testsuite>
</testsuites>`, i, cases.String())
		path := filepath.Join(dir, fmt.Sprintf("run-%03d.xml", i))
		if err := os.WriteFile(path, []byte(xml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// run drives the CLI in-process and captures both streams.
func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Main(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestVersionCommandAndFlagAlias(t *testing.T) {
	code, out, _ := run(t, "version")
	if code != ExitOK || out != "flakesift 0.1.0\n" {
		t.Fatalf("code = %d, out = %q", code, out)
	}
	code, out, _ = run(t, "--version")
	if code != ExitOK || !strings.Contains(out, "0.1.0") {
		t.Fatalf("alias code = %d, out = %q", code, out)
	}
}

func TestTopLevelUsageHandling(t *testing.T) {
	// help exits 0 with usage on stdout; no args and unknown flags exit 2
	// with usage on stderr.
	code, out, _ := run(t, "help")
	if code != ExitOK || !strings.Contains(out, "quarantine") {
		t.Fatalf("help code = %d, out = %q", code, out)
	}
	code, _, errOut := run(t)
	if code != ExitUsage || !strings.Contains(errOut, "Usage") {
		t.Fatalf("no-args code = %d, stderr = %q", code, errOut)
	}
	if code, _, _ := run(t, "--bogus"); code != ExitUsage {
		t.Fatalf("unknown flag code = %d, want %d", code, ExitUsage)
	}
}

func TestScoreTextRanksPathologies(t *testing.T) {
	dir := writeHistory(t)
	code, out, _ := run(t, "score", dir)
	if code != ExitOK {
		t.Fatalf("code = %d", code)
	}
	// The coin-flip test must outrank everything; broken must not be flaky.
	idxCart := strings.Index(out, "pkg.Cart::testCheckoutRace")
	idxUtil := strings.Index(out, "pkg.Util::testParse")
	if idxCart < 0 || idxUtil < 0 || idxCart > idxUtil {
		t.Errorf("ranking wrong:\n%s", out)
	}
	for _, want := range []string{"12 runs, 5 tests", "flaky", "broken", "healthy", "new"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// A bare path with no subcommand defaults to score.
	code, out, _ = run(t, dir)
	if code != ExitOK || !strings.Contains(out, "flakesift score") {
		t.Fatalf("bare-path code = %d, out = %q", code, out)
	}
}

func TestScoreJSONEnvelopeAndClasses(t *testing.T) {
	dir := writeHistory(t)
	code, out, _ := run(t, "score", "--format", "json", dir)
	if code != ExitOK {
		t.Fatalf("code = %d", code)
	}
	var env struct {
		Tool string `json:"tool"`
		Kind string `json:"kind"`
		Data struct {
			Runs  int `json:"runs"`
			Tests []struct {
				ID    string  `json:"id"`
				Class string  `json:"class"`
				Score float64 `json:"score"`
			} `json:"tests"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env.Data.Runs != 12 || len(env.Data.Tests) != 5 {
		t.Fatalf("runs = %d, tests = %d", env.Data.Runs, len(env.Data.Tests))
	}
	// Determinism: a second identical invocation is byte-identical.
	_, again, _ := run(t, "score", "--format", "json", dir)
	if again != out {
		t.Error("two identical invocations produced different output")
	}
	classes := map[string]string{}
	for _, tt := range env.Data.Tests {
		classes[tt.ID] = tt.Class
	}
	for id, want := range map[string]string{
		"pkg.Cart::testCheckoutRace": "flaky",
		"pkg.Auth::testLoginRetry":   "flaky",
		"pkg.DB::testMigration":      "broken",
		"pkg.Util::testParse":        "healthy",
		"pkg.New::testFresh":         "new",
	} {
		if classes[id] != want {
			t.Errorf("%s class = %q, want %q", id, classes[id], want)
		}
	}
}

func TestScoreCSVFormat(t *testing.T) {
	dir := writeHistory(t)
	code, out, _ := run(t, "score", "--format", "csv", dir)
	if code != ExitOK {
		t.Fatalf("code = %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 6 {
		t.Fatalf("lines = %d, want header + 5", len(lines))
	}
	if !strings.HasPrefix(lines[0], "id,classname,name,score") {
		t.Errorf("header = %q", lines[0])
	}
}

func TestScoreTopAndMinScoreFilters(t *testing.T) {
	dir := writeHistory(t)
	_, out, _ := run(t, "score", "--top", "2", dir)
	if strings.Contains(out, "pkg.Util::testParse") {
		t.Errorf("--top 2 should drop the healthy test:\n%s", out)
	}
	if !strings.Contains(out, "pkg.Cart::testCheckoutRace") {
		t.Errorf("--top 2 should keep the top flake:\n%s", out)
	}
	_, out, _ = run(t, "score", "--min-score", "50", dir)
	if strings.Contains(out, "healthy") {
		t.Errorf("--min-score 50 should hide healthy tests:\n%s", out)
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	// Every flag misuse must land on exit 2 so CI can tell "you called it
	// wrong" apart from "your suite breached the gate" (1) and "flakesift
	// itself failed" (3).
	dir := writeHistory(t)
	for _, tc := range [][]string{
		{"score", "--format", "yaml", dir},
		{"score", "--threshold", "150", dir},
		{"score", "--min-runs", "0", dir},
		{"score", "--top", "-1", dir},
		{"score", "--group", "week", dir},
		{"score"}, // no paths
		{"quarantine", "--format", "xunit", dir},
		{"gate", "--max-flaky", "-1", dir},
		{"gate", "--max-broken", "-2", dir},
		{"trend", "--buckets", "0", dir},
		{"trend", "--format", "csv", dir},
		{"runs", "--format", "csv", dir},
	} {
		if code, _, _ := run(t, tc...); code != ExitUsage {
			t.Errorf("%v: code = %d, want %d", tc, code, ExitUsage)
		}
	}
}

func TestScoreMissingPathExitsRuntime(t *testing.T) {
	code, _, _ := run(t, "score", "/does/not/exist-flakesift")
	if code != ExitRuntime {
		t.Fatalf("code = %d, want %d", code, ExitRuntime)
	}
}

func TestQuarantineLinesListsFlakyOnly(t *testing.T) {
	dir := writeHistory(t)
	code, out, _ := run(t, "quarantine", dir)
	if code != ExitOK {
		t.Fatalf("code = %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("quarantined = %v, want the two flaky tests", lines)
	}
	if strings.Contains(out, "testMigration") {
		t.Error("broken test quarantined without --include-broken")
	}
}

func TestQuarantineIncludeBroken(t *testing.T) {
	dir := writeHistory(t)
	_, out, _ := run(t, "quarantine", "--include-broken", dir)
	if !strings.Contains(out, "pkg.DB::testMigration") {
		t.Errorf("broken test missing with --include-broken:\n%s", out)
	}
}

func TestQuarantineToolFormats(t *testing.T) {
	dir := writeHistory(t)
	_, out, _ := run(t, "quarantine", "--format", "gotest", dir)
	want := "^(testCheckoutRace|testLoginRetry)$\n"
	if out != want {
		t.Errorf("gotest out = %q, want %q", out, want)
	}
	_, out, _ = run(t, "quarantine", "--format", "pytest", dir)
	if !strings.Contains(out, "not testCheckoutRace and not testLoginRetry") {
		t.Errorf("pytest out = %q", out)
	}
}

func TestQuarantineJSONKeepsEmptyArray(t *testing.T) {
	// A clean suite must yield [] not null — jq pipelines depend on it.
	dir := t.TempDir()
	xml := `<testsuite name="s"><testcase classname="a" name="t"/></testsuite>`
	for i := 1; i <= 3; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("r%d.xml", i)), []byte(xml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, out, _ := run(t, "quarantine", "--format", "json", dir)
	if code != ExitOK {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(out, `"quarantined": []`) {
		t.Errorf("empty quarantine not []:\n%s", out)
	}
}

func TestQuarantineHigherThresholdShrinksList(t *testing.T) {
	dir := writeHistory(t)
	// Cart scores 91.7 (11 flips / 12 runs), Auth 33.3 (4 recoveries / 12).
	_, out, _ := run(t, "quarantine", "--threshold", "90", dir)
	if strings.TrimSpace(out) != "pkg.Cart::testCheckoutRace" {
		t.Errorf("out = %q, want only the coin-flip test at threshold 90", out)
	}
}

func TestTrendTextBucketsAndSparkline(t *testing.T) {
	dir := writeHistory(t)
	code, out, _ := run(t, "trend", "--buckets", "4", dir)
	if code != ExitOK {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(out, "12 runs in 4 buckets") {
		t.Errorf("header wrong:\n%s", out)
	}
	if !strings.ContainsAny(out, "▁▂▃▄▅▆▇█") {
		t.Errorf("no sparkline:\n%s", out)
	}
}

func TestTrendJSONBucketShape(t *testing.T) {
	dir := writeHistory(t)
	_, out, _ := run(t, "trend", "--format", "json", "--buckets", "3", dir)
	var env struct {
		Data struct {
			Buckets []struct {
				Runs      int     `json:"runs"`
				FailRate  float64 `json:"fail_rate"`
				FlakeRate float64 `json:"flake_rate"`
			} `json:"buckets"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(env.Data.Buckets) != 3 {
		t.Fatalf("buckets = %d, want 3", len(env.Data.Buckets))
	}
	for i, b := range env.Data.Buckets {
		if b.Runs != 4 {
			t.Errorf("bucket %d runs = %d, want 4", i, b.Runs)
		}
	}
}

func TestTrendTestFilterNarrowsCounts(t *testing.T) {
	dir := writeHistory(t)
	_, all, _ := run(t, "trend", "--format", "json", "--buckets", "1", dir)
	_, only, _ := run(t, "trend", "--format", "json", "--buckets", "1", "--test", "testMigration", dir)
	var a, b struct {
		Data struct {
			Buckets []struct {
				Executions int `json:"executions"`
				Fails      int `json:"fails"`
			} `json:"buckets"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(all), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(only), &b); err != nil {
		t.Fatal(err)
	}
	if b.Data.Buckets[0].Executions != 12 || b.Data.Buckets[0].Fails != 12 {
		t.Errorf("filtered = %+v, want 12 executions / 12 fails", b.Data.Buckets[0])
	}
	if a.Data.Buckets[0].Executions <= b.Data.Buckets[0].Executions {
		t.Error("filter did not narrow executions")
	}
}

func TestGatePassesUnderLimitsAndIgnoresBrokenByDefault(t *testing.T) {
	dir := writeHistory(t)
	code, out, _ := run(t, "gate", "--max-flaky", "2", dir)
	if code != ExitOK || !strings.Contains(out, "gate: PASS") {
		t.Fatalf("code = %d, out = %q", code, out)
	}
	// The broken test exists but --max-broken defaults to ignore.
	if !strings.Contains(out, "ignored") {
		t.Errorf("broken limit not marked ignored:\n%s", out)
	}
}

func TestGateBreachesOnLimits(t *testing.T) {
	dir := writeHistory(t)
	// Two flaky tests > limit 1.
	code, out, _ := run(t, "gate", "--max-flaky", "1", dir)
	if code != ExitBreach {
		t.Fatalf("code = %d, want %d", code, ExitBreach)
	}
	if !strings.Contains(out, "BREACH") || !strings.Contains(out, "gate: FAIL") {
		t.Errorf("out = %q", out)
	}
	// One broken test > limit 0 when the broken gate is armed.
	code, _, _ = run(t, "gate", "--max-flaky", "2", "--max-broken", "0", dir)
	if code != ExitBreach {
		t.Fatalf("broken gate code = %d, want %d", code, ExitBreach)
	}
	// Raising the threshold to 90 leaves a single flaky test → PASS.
	code, _, _ = run(t, "gate", "--max-flaky", "1", "--threshold", "90", dir)
	if code != ExitOK {
		t.Fatalf("threshold-90 code = %d, want 0", code)
	}
}

func TestRunsTextListsAllRuns(t *testing.T) {
	dir := writeHistory(t)
	code, out, _ := run(t, "runs", dir)
	if code != ExitOK {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(out, "12 runs ingested") {
		t.Errorf("header wrong:\n%s", out)
	}
	if !strings.Contains(out, "2026-03-01T10:00:00Z") {
		t.Errorf("timestamp missing:\n%s", out)
	}
}

func TestRunsJSONShape(t *testing.T) {
	dir := writeHistory(t)
	_, out, _ := run(t, "runs", "--format", "json", dir)
	var env struct {
		Data struct {
			Runs []struct {
				ID    string   `json:"id"`
				Files []string `json:"files"`
				Cases int      `json:"cases"`
			} `json:"runs"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(env.Data.Runs) != 12 {
		t.Fatalf("runs = %d, want 12", len(env.Data.Runs))
	}
	if env.Data.Runs[0].Cases == 0 {
		t.Error("case counts missing")
	}
}

func TestGroupDirMergesShards(t *testing.T) {
	// Same history, but split so each run directory holds two shard files.
	dir := t.TempDir()
	for i := 1; i <= 4; i++ {
		runDir := filepath.Join(dir, fmt.Sprintf("run-%03d", i))
		if err := os.MkdirAll(runDir, 0o755); err != nil {
			t.Fatal(err)
		}
		verdict := ""
		if i%2 == 0 {
			verdict = `<failure message="x"/>`
		}
		shard1 := fmt.Sprintf(`<testsuite name="s1"><testcase classname="a" name="t1">%s</testcase></testsuite>`, verdict)
		shard2 := `<testsuite name="s2"><testcase classname="a" name="t2"/></testsuite>`
		if err := os.WriteFile(filepath.Join(runDir, "shard1.xml"), []byte(shard1), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(runDir, "shard2.xml"), []byte(shard2), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, out, _ := run(t, "runs", "--group", "dir", dir)
	if !strings.Contains(out, "4 runs ingested") {
		t.Errorf("dir grouping failed:\n%s", out)
	}
	_, scoreOut, _ := run(t, "score", "--group", "dir", dir)
	if !strings.Contains(scoreOut, "4 runs, 2 tests") {
		t.Errorf("score over grouped runs wrong:\n%s", scoreOut)
	}
}
