package render

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/JaydenCJ/flakesift/internal/score"
)

// QuarantineLines writes one test ID per line — the neutral exchange
// format, easy to diff, sort, and commit next to the suite.
func QuarantineLines(w io.Writer, results []score.Result) {
	for _, r := range results {
		fmt.Fprintln(w, r.ID)
	}
}

// QuarantineGotest writes a single anchored regexp suitable for
// `go test -skip`, built from bare test names (Go reports put the package
// in classname). Names are regexp-quoted so Test/weird[chars] can't
// widen the match.
func QuarantineGotest(w io.Writer, results []score.Result) {
	if len(results) == 0 {
		return
	}
	names := uniqueNames(results)
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, regexp.QuoteMeta(n))
	}
	fmt.Fprintf(w, "^(%s)$\n", strings.Join(quoted, "|"))
}

// QuarantinePytest writes a single -k expression that deselects every
// quarantined test by name: `not test_a and not test_b`.
func QuarantinePytest(w io.Writer, results []score.Result) {
	if len(results) == 0 {
		return
	}
	names := uniqueNames(results)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, "not "+n)
	}
	fmt.Fprintln(w, strings.Join(parts, " and "))
}

// uniqueNames extracts sorted, deduplicated bare test names; two flaky
// tests sharing a name across classes still yield one skip pattern.
func uniqueNames(results []score.Result) []string {
	seen := make(map[string]bool)
	var names []string
	for _, r := range results {
		name := r.Name
		if name == "" {
			name = r.ID
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
