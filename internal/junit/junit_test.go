// Tests for the JUnit XML parser: dialect coverage (testsuites root, bare
// testsuite, nesting, Surefire rerun extensions) and the normalization
// rules that everything downstream relies on.
package junit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func parseString(t *testing.T, xml string) *Report {
	t.Helper()
	rep, err := Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return rep
}

func TestParseTestsuitesRootWithPassingCase(t *testing.T) {
	rep := parseString(t, `<testsuites>
	  <testsuite name="pkg" tests="1">
	    <testcase classname="pkg.Checkout" name="testHappyPath" time="0.42"/>
	  </testsuite>
	</testsuites>`)
	if len(rep.Suites) != 1 {
		t.Fatalf("suites = %d, want 1", len(rep.Suites))
	}
	c := rep.Suites[0].Cases[0]
	if c.Status != StatusPass {
		t.Errorf("status = %v, want pass", c.Status)
	}
	if c.Time != 0.42 {
		t.Errorf("time = %v, want 0.42", c.Time)
	}
}

func TestParseBareTestsuiteRoot(t *testing.T) {
	// Jest and go-junit-report emit <testsuite> as the document root.
	rep := parseString(t, `<testsuite name="solo">
	  <testcase classname="a" name="t1"/>
	  <testcase classname="a" name="t2"/>
	</testsuite>`)
	if got := len(rep.Cases()); got != 2 {
		t.Fatalf("cases = %d, want 2", got)
	}
	if rep.Suites[0].Name != "solo" {
		t.Errorf("suite name = %q, want solo", rep.Suites[0].Name)
	}
}

func TestParseNestedSuitesFlattenWithJoinedNames(t *testing.T) {
	// Gradle nests <testsuite> inside <testsuite>; names must stay traceable.
	rep := parseString(t, `<testsuites>
	  <testsuite name="outer">
	    <testsuite name="inner">
	      <testcase classname="c" name="deep"/>
	    </testsuite>
	  </testsuite>
	</testsuites>`)
	if len(rep.Suites) != 1 {
		t.Fatalf("suites = %d, want 1 (only leaf with cases)", len(rep.Suites))
	}
	if rep.Suites[0].Name != "outer/inner" {
		t.Errorf("name = %q, want outer/inner", rep.Suites[0].Name)
	}
	if rep.Suites[0].Cases[0].Suite != "outer/inner" {
		t.Errorf("case suite = %q, want outer/inner", rep.Suites[0].Cases[0].Suite)
	}
}

func TestParseStatusAndPrecedence(t *testing.T) {
	// Verdict mapping, including the precedence rules for tools that emit
	// several child elements: error > failure > skipped.
	for _, tc := range []struct {
		name     string
		children string
		want     Status
		wantMsg  string
	}{
		{"failure", `<failure message="assertion failed: got 3 want 4" type="AssertionError"/>`, StatusFail, "assertion failed: got 3 want 4"},
		{"skipped", `<skipped message="not on this platform"/>`, StatusSkip, "not on this platform"},
		{"error wins over failure", `<failure message="f"/><error message="boom"/>`, StatusError, "boom"},
		{"failure wins over skip marker", `<skipped/><failure message="f"/>`, StatusFail, "f"},
	} {
		rep := parseString(t, `<testsuite name="s"><testcase classname="c" name="t">`+tc.children+`</testcase></testsuite>`)
		c := rep.Cases()[0]
		if c.Status != tc.want {
			t.Errorf("%s: status = %v, want %v", tc.name, c.Status, tc.want)
		}
		if c.Message != tc.wantMsg {
			t.Errorf("%s: message = %q, want %q", tc.name, c.Message, tc.wantMsg)
		}
	}
}

func TestParseMessageFallsBackToTypeThenBodyFirstLine(t *testing.T) {
	rep := parseString(t, `<testsuite name="s">
	  <testcase classname="c" name="a"><failure type="TimeoutError"/></testcase>
	  <testcase classname="c" name="b"><failure>first line
	stack frame 1
	stack frame 2</failure></testcase>
	</testsuite>`)
	cases := rep.Cases()
	if cases[0].Message != "TimeoutError" {
		t.Errorf("type fallback = %q", cases[0].Message)
	}
	if cases[1].Message != "first line" {
		t.Errorf("body fallback = %q", cases[1].Message)
	}
}

func TestParseSurefireFlakyFailureCountsAttempts(t *testing.T) {
	// flakyFailure/flakyError children mean earlier attempts failed but the
	// case finally passed — the canonical retry-flake shape.
	rep := parseString(t, `<testsuite name="s">
	  <testcase classname="c" name="t">
	    <flakyFailure message="first try failed"/>
	    <flakyError message="second try oomed"/>
	  </testcase>
	</testsuite>`)
	c := rep.Cases()[0]
	if c.Status != StatusPass {
		t.Errorf("status = %v, want pass (flaky case ultimately passed)", c.Status)
	}
	if c.FlakyAttempts != 2 {
		t.Errorf("flaky attempts = %d, want 2", c.FlakyAttempts)
	}
}

func TestParseSurefireRerunFailureCountsAttempts(t *testing.T) {
	rep := parseString(t, `<testsuite name="s">
	  <testcase classname="c" name="t">
	    <failure message="failed"/>
	    <rerunFailure message="failed again"/>
	    <rerunError message="and again"/>
	  </testcase>
	</testsuite>`)
	c := rep.Cases()[0]
	if c.Status != StatusFail {
		t.Errorf("status = %v, want fail", c.Status)
	}
	if c.RerunAttempts != 2 {
		t.Errorf("rerun attempts = %d, want 2", c.RerunAttempts)
	}
}

func TestParseTimestampVariants(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want time.Time
	}{
		{"2026-03-01T10:00:00", time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)},
		{"2026-03-01T10:00:00Z", time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)},
		{"2026-03-01T10:00:00+02:00", time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)},
		{"2026-03-01 10:00:00", time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)},
		{"2026-03-01T10:00:00.123456", time.Date(2026, 3, 1, 10, 0, 0, 123456000, time.UTC)},
	} {
		got := parseTimestamp(tc.raw)
		if !got.UTC().Equal(tc.want) {
			t.Errorf("parseTimestamp(%q) = %v, want %v", tc.raw, got.UTC(), tc.want)
		}
	}
}

func TestParseAttributeGarbageIsHarmless(t *testing.T) {
	// Bad timestamps become the zero time (run falls back to path order);
	// bad or locale-formatted durations become 0, never an error.
	for _, raw := range []string{"", "yesterday", "13:00", "2026-99-99T99:99:99"} {
		if got := parseTimestamp(raw); !got.IsZero() {
			t.Errorf("parseTimestamp(%q) = %v, want zero", raw, got)
		}
	}
	if got := parseSeconds("1,234.5"); got != 1234.5 {
		t.Errorf("comma-grouped = %v, want 1234.5", got)
	}
	if got := parseSeconds("n/a"); got != 0 {
		t.Errorf("garbage = %v, want 0", got)
	}
	if got := parseSeconds("-3"); got != 0 {
		t.Errorf("negative = %v, want 0", got)
	}
}

func TestCaseIDIncludesClassname(t *testing.T) {
	c := Case{ClassName: "pkg.Suite", Name: "testX"}
	if c.ID() != "pkg.Suite::testX" {
		t.Errorf("id = %q", c.ID())
	}
	bare := Case{Name: "TestOnly"}
	if bare.ID() != "TestOnly" {
		t.Errorf("bare id = %q", bare.ID())
	}
}

func TestParseRejectsBadDocuments(t *testing.T) {
	// Foreign roots get the sentinel error (directory walks skip on it);
	// truncated and empty documents fail with ordinary parse errors.
	_, err := Parse(strings.NewReader(`<coverage line-rate="0.9"></coverage>`))
	if !errors.Is(err, ErrNotJUnit) {
		t.Fatalf("foreign root err = %v, want ErrNotJUnit", err)
	}
	if _, err := Parse(strings.NewReader(`<testsuite name="s"><testcase`)); err == nil {
		t.Fatal("want error for truncated XML")
	}
	if _, err := Parse(strings.NewReader("")); err == nil {
		t.Fatal("want error for empty document")
	}
	// ParseFile prefixes the offending path, so multi-file ingestion
	// errors say which artifact is corrupt.
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.xml")
	if err := os.WriteFile(path, []byte("<oops>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFile(path); err == nil || !strings.Contains(err.Error(), "bad.xml") {
		t.Fatalf("err = %v, want path in message", err)
	}
}

func TestParseIgnoresSystemOutAndProperties(t *testing.T) {
	rep := parseString(t, `<testsuite name="s">
	  <properties><property name="os" value="linux"/></properties>
	  <testcase classname="c" name="t"><system-out>lots of log noise</system-out></testcase>
	</testsuite>`)
	c := rep.Cases()[0]
	if c.Status != StatusPass || c.Message != "" {
		t.Errorf("case = %+v, want clean pass", c)
	}
}

func TestStatusStringLabels(t *testing.T) {
	for _, tc := range []struct {
		s    Status
		want string
	}{{StatusPass, "pass"}, {StatusFail, "fail"}, {StatusError, "error"}, {StatusSkip, "skip"}} {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("%d.String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}
