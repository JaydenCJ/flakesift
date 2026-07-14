// Tests for output rendering: byte-stable text tables, the JSON envelope
// contract, CSV shape, and every quarantine list format.
package render

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/JaydenCJ/flakesift/internal/ingest"
	"github.com/JaydenCJ/flakesift/internal/score"
	"github.com/JaydenCJ/flakesift/internal/trend"
)

func sampleResults() []score.Result {
	return []score.Result{
		{ID: "pkg.Cart::testCheckout", ClassName: "pkg.Cart", Name: "testCheckout",
			Runs: 10, Passes: 5, Fails: 5, Flips: 9, FailRate: 50, Score: 90, Class: score.ClassFlaky, LastOutcome: "fail", LastRunID: "r10"},
		{ID: "pkg.Auth::testLogin", ClassName: "pkg.Auth", Name: "testLogin",
			Runs: 10, Passes: 10, Recovered: 2, Score: 20, Class: score.ClassSuspect, LastOutcome: "pass", LastRunID: "r10"},
	}
}

func TestScoreTextHeaderAndRows(t *testing.T) {
	var buf bytes.Buffer
	ScoreText(&buf, sampleResults(), 10)
	out := buf.String()
	for _, want := range []string{"flakesift score — 10 runs, 2 tests", "score", "class", "pkg.Cart::testCheckout", "flaky", "90.0", "50.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	var empty bytes.Buffer
	ScoreText(&empty, nil, 0)
	if !strings.Contains(empty.String(), "no tests found") {
		t.Errorf("empty output = %q", empty.String())
	}
}

func TestScoreTextNoTrailingWhitespace(t *testing.T) {
	// Trailing spaces churn diffs when reports are committed; forbid them.
	var buf bytes.Buffer
	ScoreText(&buf, sampleResults(), 10)
	for i, line := range strings.Split(buf.String(), "\n") {
		if line != strings.TrimRight(line, " ") {
			t.Errorf("line %d has trailing whitespace: %q", i+1, line)
		}
	}
}

func TestScoreTextNumericColumnsRightAligned(t *testing.T) {
	var buf bytes.Buffer
	ScoreText(&buf, sampleResults(), 10)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	// Score column: "90.0" and "20.0" must end at the same offset as "score".
	header, first := lines[2], lines[3]
	if strings.Index(header, "score")+len("score") != strings.Index(first, "90.0")+len("90.0") {
		t.Errorf("score column misaligned:\n%s\n%s", header, first)
	}
}

func TestJSONEnvelopeContract(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, "score", map[string]int{"runs": 3}); err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env["tool"] != "flakesift" {
		t.Errorf("tool = %v", env["tool"])
	}
	if env["schema_version"] != float64(1) {
		t.Errorf("schema_version = %v", env["schema_version"])
	}
	if env["kind"] != "score" {
		t.Errorf("kind = %v", env["kind"])
	}
	if env["version"] != "0.1.0" {
		t.Errorf("version = %v", env["version"])
	}
	// Byte stability: identical input, identical bytes, trailing newline.
	var a, b bytes.Buffer
	payload := map[string]any{"tests": sampleResults()}
	if err := JSON(&a, "score", payload); err != nil {
		t.Fatal(err)
	}
	if err := JSON(&b, "score", payload); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("identical input produced different JSON bytes")
	}
	if !bytes.HasSuffix(a.Bytes(), []byte("\n")) {
		t.Error("JSON output must end with a newline")
	}
}

func TestScoreCSVRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := ScoreCSV(&buf, sampleResults()); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("CSV does not re-parse: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want header + 2", len(rows))
	}
	if rows[0][0] != "id" || rows[1][3] != "90.0" {
		t.Errorf("unexpected cells: %v / %v", rows[0], rows[1])
	}
}

func TestScoreCSVEscapesCommasAndQuotes(t *testing.T) {
	// Parameterized test names routinely contain commas and quotes.
	results := []score.Result{{ID: `pkg::case "a,b"`, Name: `case "a,b"`, Class: score.ClassHealthy}}
	var buf bytes.Buffer
	if err := ScoreCSV(&buf, results); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if rows[1][0] != `pkg::case "a,b"` {
		t.Errorf("round-trip = %q", rows[1][0])
	}
}

func TestQuarantineLinesOnePerTest(t *testing.T) {
	var buf bytes.Buffer
	QuarantineLines(&buf, sampleResults())
	want := "pkg.Cart::testCheckout\npkg.Auth::testLogin\n"
	if buf.String() != want {
		t.Errorf("lines = %q, want %q", buf.String(), want)
	}
}

func TestQuarantineGotestAnchoredRegexp(t *testing.T) {
	results := []score.Result{
		{ID: "pkg::TestB", Name: "TestB"},
		{ID: "pkg::TestA", Name: "TestA"},
	}
	var buf bytes.Buffer
	QuarantineGotest(&buf, results)
	got := strings.TrimSpace(buf.String())
	if got != "^(TestA|TestB)$" {
		t.Errorf("regexp = %q, want ^(TestA|TestB)$ (sorted)", got)
	}
	// The emitted pattern must actually work as a Go regexp.
	re := regexp.MustCompile(got)
	if !re.MatchString("TestA") || re.MatchString("TestAX") {
		t.Error("pattern does not match exactly")
	}
	// Duplicate names across classes collapse to one alternative.
	var dup bytes.Buffer
	QuarantineGotest(&dup, []score.Result{
		{ID: "a::TestSame", Name: "TestSame"},
		{ID: "b::TestSame", Name: "TestSame"},
	})
	if got := strings.TrimSpace(dup.String()); got != "^(TestSame)$" {
		t.Errorf("regexp = %q, want single deduplicated name", got)
	}
	// An empty skip pattern would skip *everything*; emit nothing instead.
	var empty bytes.Buffer
	QuarantineGotest(&empty, nil)
	if empty.Len() != 0 {
		t.Errorf("empty output = %q, want nothing", empty.String())
	}
}

func TestQuarantineGotestQuotesMetaCharacters(t *testing.T) {
	// Subtest-style names with regex metacharacters must not widen the match.
	results := []score.Result{{ID: "pkg::TestX/case(1)", Name: "TestX/case(1)"}}
	var buf bytes.Buffer
	QuarantineGotest(&buf, results)
	re := regexp.MustCompile(strings.TrimSpace(buf.String()))
	if !re.MatchString("TestX/case(1)") {
		t.Error("quoted name no longer matches itself")
	}
	if re.MatchString("TestX/case1") {
		t.Error("parentheses were interpreted as a regex group")
	}
}

func TestQuarantinePytestExpression(t *testing.T) {
	results := []score.Result{
		{ID: "tests.test_cart::test_checkout", Name: "test_checkout"},
		{ID: "tests.test_auth::test_login", Name: "test_login"},
	}
	var buf bytes.Buffer
	QuarantinePytest(&buf, results)
	if got := strings.TrimSpace(buf.String()); got != "not test_checkout and not test_login" {
		t.Errorf("expression = %q", got)
	}
}

func TestTrendTextShowsSparklinesAndTable(t *testing.T) {
	buckets := []trend.Bucket{
		{Index: 0, FirstRun: "r1", LastRun: "r5", Runs: 5, Executions: 50, Fails: 5, FlakeEvents: 4, FailRate: 10, FlakeRate: 8},
		{Index: 1, FirstRun: "r6", LastRun: "r9", Runs: 4, Executions: 40, Fails: 2, FlakeEvents: 1, FailRate: 5, FlakeRate: 2.5},
	}
	var buf bytes.Buffer
	TrendText(&buf, buckets, 9)
	out := buf.String()
	for _, want := range []string{"9 runs in 2 buckets", "fail rate", "flake rate", "r1 … r5", "10.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if !strings.ContainsAny(out, "▁▂▃▄▅▆▇█") {
		t.Error("no sparkline characters in trend output")
	}
}

func TestRunsTextListsRunsAndSkipped(t *testing.T) {
	runs := []ingest.Run{
		{ID: "runs/r1.xml", Time: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC), Files: []string{"runs/r1.xml"}, Cases: nil},
		{ID: "runs/r2.xml", Files: []string{"runs/r2.xml"}},
	}
	var buf bytes.Buffer
	RunsText(&buf, runs, []string{"runs/coverage.xml"})
	out := buf.String()
	if !strings.Contains(out, "2026-03-01T10:00:00Z") {
		t.Errorf("timestamp missing:\n%s", out)
	}
	if !strings.Contains(out, "1 non-JUnit XML file skipped") {
		t.Errorf("skip note missing:\n%s", out)
	}
	// Run without timestamp renders a dash, not a zero time.
	if strings.Contains(out, "0001-01-01") {
		t.Error("zero time leaked into output")
	}
	// The note pluralizes properly: "2 files", never "2 file(s)".
	buf.Reset()
	RunsText(&buf, runs, []string{"runs/coverage.xml", "runs/lint.xml"})
	if !strings.Contains(buf.String(), "2 non-JUnit XML files skipped") {
		t.Errorf("plural skip note wrong:\n%s", buf.String())
	}
}
