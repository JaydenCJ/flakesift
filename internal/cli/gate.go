package cli

import (
	"fmt"
	"io"

	"github.com/JaydenCJ/flakesift/internal/score"
)

// cmdGate is the CI hook: it counts flaky and broken tests against limits
// and exits 1 on breach, so a pipeline step can hold the line on suite
// health without any dashboard.
func cmdGate(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("gate", stderr)
	maxFlaky := fs.Int("max-flaky", 0, "maximum number of flaky tests allowed")
	maxBroken := fs.Int("max-broken", -1, "maximum number of broken tests allowed (-1 = ignore)")
	group := addGroupFlag(fs)
	params := addScoreFlags(fs)
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if err := validateScoreParams(params); err != nil {
		return usageErr(stderr, err)
	}
	if *maxFlaky < 0 {
		return usageErr(stderr, fmt.Errorf("invalid --max-flaky %d (want >= 0)", *maxFlaky))
	}
	if *maxBroken < -1 {
		return usageErr(stderr, fmt.Errorf("invalid --max-broken %d (want >= 0, or -1 to ignore)", *maxBroken))
	}
	if fs.NArg() == 0 {
		return usageErr(stderr, fmt.Errorf("no report paths given"))
	}

	_, tests, err := load(fs.Args(), *group)
	if err != nil {
		if _, ok := err.(*usageError); ok {
			return usageErr(stderr, err)
		}
		return runtimeErr(stderr, err)
	}
	results := score.EvaluateAll(tests, *params)

	flaky, broken := 0, 0
	for _, r := range results {
		switch r.Class {
		case score.ClassFlaky:
			flaky++
		case score.ClassBroken:
			broken++
		}
	}

	breach := false
	verdict := func(label string, count, limit int) {
		status := "ok"
		if limit >= 0 && count > limit {
			status = "BREACH"
			breach = true
		}
		limitStr := "ignored"
		if limit >= 0 {
			limitStr = fmt.Sprintf("limit %d", limit)
		}
		fmt.Fprintf(stdout, "%-8s %3d  (%s)  %s\n", label, count, limitStr, status)
	}
	verdict("flaky", flaky, *maxFlaky)
	verdict("broken", broken, *maxBroken)

	if breach {
		fmt.Fprintln(stdout, "gate: FAIL")
		return ExitBreach
	}
	fmt.Fprintln(stdout, "gate: PASS")
	return ExitOK
}
