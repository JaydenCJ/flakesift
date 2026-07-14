// Tests for run discovery, grouping, and chronological ordering — the
// scorer is only as good as the run sequence this package hands it.
package ingest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeReport drops a minimal one-case JUnit file. timestamp may be empty.
func writeReport(t *testing.T, path, timestamp, caseName, verdict string) {
	t.Helper()
	body := ""
	if verdict == "fail" {
		body = `<failure message="boom"/>`
	}
	ts := ""
	if timestamp != "" {
		ts = fmt.Sprintf(` timestamp=%q`, timestamp)
	}
	xml := fmt.Sprintf(`<testsuite name="s"%s>
  <testcase classname="pkg" name=%q>%s</testcase>
</testsuite>`, ts, caseName, body)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCollectOneRunPerFileByDefault(t *testing.T) {
	dir := t.TempDir()
	writeReport(t, filepath.Join(dir, "a.xml"), "", "t", "pass")
	writeReport(t, filepath.Join(dir, "b.xml"), "", "t", "fail")
	res, err := Collect([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Runs) != 2 {
		t.Fatalf("runs = %d, want 2", len(res.Runs))
	}
}

func TestCollectGroupByDirMergesFilesIntoOneRun(t *testing.T) {
	// One artifact folder per pipeline, one file per shard.
	dir := t.TempDir()
	writeReport(t, filepath.Join(dir, "run-001", "shard1.xml"), "", "t1", "pass")
	writeReport(t, filepath.Join(dir, "run-001", "shard2.xml"), "", "t2", "pass")
	writeReport(t, filepath.Join(dir, "run-002", "shard1.xml"), "", "t1", "fail")
	res, err := Collect([]string{dir}, Options{GroupByDir: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Runs) != 2 {
		t.Fatalf("runs = %d, want 2", len(res.Runs))
	}
	if len(res.Runs[0].Cases) != 2 {
		t.Errorf("first run cases = %d, want 2 (shards merged)", len(res.Runs[0].Cases))
	}
	if len(res.Runs[0].Files) != 2 {
		t.Errorf("first run files = %d, want 2", len(res.Runs[0].Files))
	}
}

func TestCollectOrdersRunsByTimestamp(t *testing.T) {
	// File names deliberately sort *against* the timestamps to prove the
	// timestamp wins.
	dir := t.TempDir()
	writeReport(t, filepath.Join(dir, "a.xml"), "2026-03-02T10:00:00", "t", "pass")
	writeReport(t, filepath.Join(dir, "b.xml"), "2026-03-01T10:00:00", "t", "pass")
	res, err := Collect([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(res.Runs[0].ID, "b.xml") {
		t.Errorf("first run = %q, want the earlier-timestamped b.xml", res.Runs[0].ID)
	}
}

func TestCollectFallsBackToPathOrderWithoutTimestamps(t *testing.T) {
	// Zero-padded run folders must stay chronological without timestamps.
	dir := t.TempDir()
	for _, name := range []string{"run-010.xml", "run-002.xml", "run-001.xml"} {
		writeReport(t, filepath.Join(dir, name), "", "t", "pass")
	}
	res, err := Collect([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range res.Runs {
		got = append(got, filepath.Base(r.ID))
	}
	want := []string{"run-001.xml", "run-002.xml", "run-010.xml"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestCollectRunTimeIsEarliestSuiteTimestamp(t *testing.T) {
	dir := t.TempDir()
	xml := `<testsuites>
  <testsuite name="a" timestamp="2026-03-01T12:00:00"><testcase name="t1"/></testsuite>
  <testsuite name="b" timestamp="2026-03-01T09:00:00"><testcase name="t2"/></testsuite>
</testsuites>`
	path := filepath.Join(dir, "r.xml")
	if err := os.WriteFile(path, []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Collect([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Runs[0].Time.Format("15:04:05"); got != "09:00:00" {
		t.Errorf("run time = %s, want the earliest suite's 09:00:00", got)
	}
}

func TestCollectSkipsForeignXMLDiscoveredInDirectories(t *testing.T) {
	// Artifact folders routinely hold coverage.xml next to the reports.
	dir := t.TempDir()
	writeReport(t, filepath.Join(dir, "junit.xml"), "", "t", "pass")
	if err := os.WriteFile(filepath.Join(dir, "coverage.xml"),
		[]byte(`<coverage line-rate="0.9"></coverage>`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Collect([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(res.Runs))
	}
	if len(res.Skipped) != 1 || !strings.HasSuffix(res.Skipped[0], "coverage.xml") {
		t.Errorf("skipped = %v, want coverage.xml", res.Skipped)
	}
}

func TestCollectErrorsOnExplicitForeignFile(t *testing.T) {
	// A file the user named must parse; silently ignoring it hides typos.
	dir := t.TempDir()
	path := filepath.Join(dir, "coverage.xml")
	if err := os.WriteFile(path, []byte(`<coverage></coverage>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Collect([]string{path}, Options{}); err == nil {
		t.Fatal("want error for explicit non-JUnit file")
	}
}

func TestCollectErrorsOnMalformedDiscoveredFile(t *testing.T) {
	// Truncated JUnit XML in a directory is corruption, not foreign data —
	// fail loudly instead of scoring a partial history.
	dir := t.TempDir()
	writeReport(t, filepath.Join(dir, "ok.xml"), "", "t", "pass")
	if err := os.WriteFile(filepath.Join(dir, "broken.xml"),
		[]byte(`<testsuite name="s"><testcase`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Collect([]string{dir}, Options{}); err == nil {
		t.Fatal("want error for malformed XML")
	}
}

func TestCollectMatchesXMLExtensionCaseInsensitively(t *testing.T) {
	// notes.txt is never touched; REPORT.XML (Windows CI) is picked up.
	dir := t.TempDir()
	writeReport(t, filepath.Join(dir, "REPORT.XML"), "", "t", "pass")
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Collect([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Runs) != 1 || len(res.Skipped) != 0 {
		t.Fatalf("runs = %d skipped = %d, want 1/0", len(res.Runs), len(res.Skipped))
	}
}

func TestCollectDeduplicatesOverlappingArgs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.xml")
	writeReport(t, path, "", "t", "pass")
	res, err := Collect([]string{dir, path}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Runs) != 1 {
		t.Fatalf("runs = %d, want 1 (dir + explicit file overlap)", len(res.Runs))
	}
}

func TestCollectErrorsWhenNoRunSurvives(t *testing.T) {
	// Missing path, empty directory, and only-foreign-XML directory must
	// all fail loudly — scoring an empty history would silently report a
	// perfectly healthy suite.
	if _, err := Collect([]string{"/does/not/exist-flakesift"}, Options{}); err == nil {
		t.Fatal("want error for missing path")
	}
	empty := t.TempDir()
	_, err := Collect([]string{empty}, Options{})
	if err == nil || !strings.Contains(err.Error(), "no JUnit XML files found") {
		t.Fatalf("err = %v, want 'no JUnit XML files found'", err)
	}
	foreign := t.TempDir()
	if err := os.WriteFile(filepath.Join(foreign, "coverage.xml"),
		[]byte(`<coverage></coverage>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Collect([]string{foreign}, Options{}); err == nil {
		t.Fatal("want error when every file was skipped")
	}
}
