// Package junit parses JUnit-style XML test reports into a neutral model.
//
// It accepts the dialects emitted by common tools without configuration:
// a <testsuites> root (including nested <testsuite> elements, as written by
// Gradle and pytest), a bare <testsuite> root (Jest, go-junit-report), and
// the Maven Surefire rerun extensions (<flakyFailure>, <flakyError>,
// <rerunFailure>, <rerunError>) that record in-run retries — the strongest
// direct evidence of flakiness a report can carry.
package junit

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// ErrNotJUnit marks XML documents whose root element is neither
// <testsuites> nor <testsuite>. Directory ingestion skips such files
// silently; explicit file arguments surface the error.
var ErrNotJUnit = errors.New("root element is not <testsuites> or <testsuite>")

// Status is the outcome of a single recorded test case execution.
type Status int

const (
	StatusPass Status = iota
	StatusFail
	StatusError
	StatusSkip
)

// String returns the lowercase human label for a status.
func (s Status) String() string {
	switch s {
	case StatusPass:
		return "pass"
	case StatusFail:
		return "fail"
	case StatusError:
		return "error"
	case StatusSkip:
		return "skip"
	}
	return "unknown"
}

// Case is one <testcase> element, normalized.
type Case struct {
	ClassName string
	Name      string
	Suite     string  // fully-qualified suite name ("outer/inner")
	Time      float64 // seconds; 0 when absent or unparseable
	Status    Status
	Message   string // first failure/error message, if any
	// FlakyAttempts counts <flakyFailure>/<flakyError> children: failed
	// attempts that a retry later turned into a pass.
	FlakyAttempts int
	// RerunAttempts counts <rerunFailure>/<rerunError> children: retries
	// that failed again.
	RerunAttempts int
}

// ID returns the stable identifier used to correlate a test across runs:
// "classname::name", or just the name when no classname was recorded.
func (c Case) ID() string {
	if c.ClassName == "" {
		return c.Name
	}
	return c.ClassName + "::" + c.Name
}

// Suite is one <testsuite> element (leaf suites only; nesting is flattened
// into the Name with "/" separators).
type Suite struct {
	Name      string
	Timestamp time.Time // zero when absent or unparseable
	Cases     []Case
}

// Report is a fully parsed JUnit XML document.
type Report struct {
	Suites []Suite
}

// Cases returns every case in the report, in document order.
func (r *Report) Cases() []Case {
	var out []Case
	for _, s := range r.Suites {
		out = append(out, s.Cases...)
	}
	return out
}

type xmlResult struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Body    string `xml:",chardata"`
}

type xmlCase struct {
	Name          string      `xml:"name,attr"`
	ClassName     string      `xml:"classname,attr"`
	Time          string      `xml:"time,attr"`
	Failures      []xmlResult `xml:"failure"`
	Errors        []xmlResult `xml:"error"`
	Skipped       []xmlResult `xml:"skipped"`
	FlakyFailures []xmlResult `xml:"flakyFailure"`
	FlakyErrors   []xmlResult `xml:"flakyError"`
	RerunFailures []xmlResult `xml:"rerunFailure"`
	RerunErrors   []xmlResult `xml:"rerunError"`
}

type xmlSuite struct {
	Name      string     `xml:"name,attr"`
	Timestamp string     `xml:"timestamp,attr"`
	Suites    []xmlSuite `xml:"testsuite"`
	Cases     []xmlCase  `xml:"testcase"`
}

// Parse reads one JUnit XML document from r.
func Parse(r io.Reader) (*Report, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	root, err := rootName(data)
	if err != nil {
		return nil, err
	}
	rep := &Report{}
	switch root {
	case "testsuites":
		var doc struct {
			Suites []xmlSuite `xml:"testsuite"`
		}
		if err := xml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("malformed JUnit XML: %w", err)
		}
		for i := range doc.Suites {
			flatten(&doc.Suites[i], "", rep)
		}
	case "testsuite":
		var s xmlSuite
		if err := xml.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("malformed JUnit XML: %w", err)
		}
		flatten(&s, "", rep)
	default:
		return nil, fmt.Errorf("%w (found <%s>)", ErrNotJUnit, root)
	}
	return rep, nil
}

// ParseFile reads one JUnit XML document from disk.
func ParseFile(path string) (*Report, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rep, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return rep, nil
}

// rootName scans for the first start element without decoding the body,
// so non-JUnit XML can be rejected cheaply and by name.
func rootName(data []byte) (string, error) {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return "", errors.New("empty XML document")
		}
		if err != nil {
			return "", fmt.Errorf("malformed XML: %w", err)
		}
		if start, ok := tok.(xml.StartElement); ok {
			return start.Name.Local, nil
		}
	}
}

// flatten converts an xmlSuite (and its nested suites) into leaf Suites,
// joining nested names with "/" so "outer/inner" stays traceable.
func flatten(s *xmlSuite, parent string, rep *Report) {
	name := s.Name
	if parent != "" {
		if name == "" {
			name = parent
		} else {
			name = parent + "/" + name
		}
	}
	if len(s.Cases) > 0 {
		suite := Suite{
			Name:      name,
			Timestamp: parseTimestamp(s.Timestamp),
			Cases:     make([]Case, 0, len(s.Cases)),
		}
		for _, c := range s.Cases {
			suite.Cases = append(suite.Cases, convertCase(c, name))
		}
		rep.Suites = append(rep.Suites, suite)
	}
	for i := range s.Suites {
		flatten(&s.Suites[i], name, rep)
	}
}

func convertCase(c xmlCase, suite string) Case {
	out := Case{
		ClassName:     c.ClassName,
		Name:          c.Name,
		Suite:         suite,
		Time:          parseSeconds(c.Time),
		FlakyAttempts: len(c.FlakyFailures) + len(c.FlakyErrors),
		RerunAttempts: len(c.RerunFailures) + len(c.RerunErrors),
	}
	// Precedence mirrors how CI viewers read the spec: an error or failure
	// wins over a skip marker if a tool emits both.
	switch {
	case len(c.Errors) > 0:
		out.Status = StatusError
		out.Message = resultMessage(c.Errors[0])
	case len(c.Failures) > 0:
		out.Status = StatusFail
		out.Message = resultMessage(c.Failures[0])
	case len(c.Skipped) > 0:
		out.Status = StatusSkip
		out.Message = resultMessage(c.Skipped[0])
	default:
		out.Status = StatusPass
	}
	return out
}

func resultMessage(r xmlResult) string {
	if m := strings.TrimSpace(r.Message); m != "" {
		return m
	}
	if t := strings.TrimSpace(r.Type); t != "" {
		return t
	}
	body := strings.TrimSpace(r.Body)
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		body = body[:i]
	}
	return body
}

func parseSeconds(s string) float64 {
	// Some tools localize with comma group separators ("1,234.5").
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// timestampFormats covers the layouts seen in the wild: strict ISO 8601
// (Surefire, Gradle), RFC 3339 with zone (pytest ≥7), space-separated,
// and fractional seconds.
var timestampFormats = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
}

func parseTimestamp(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range timestampFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
