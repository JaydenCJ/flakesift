package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/JaydenCJ/flakesift/internal/history"
	"github.com/JaydenCJ/flakesift/internal/render"
	"github.com/JaydenCJ/flakesift/internal/trend"
)

// cmdTrend buckets the run sequence and charts fail/flake rates over time.
func cmdTrend(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("trend", stderr)
	format := fs.String("format", "text", "output format: text or json")
	buckets := fs.Int("buckets", 10, "number of chronological buckets")
	testFilter := fs.String("test", "", "only count tests whose ID contains this substring")
	group := addGroupFlag(fs)
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *buckets < 1 {
		return usageErr(stderr, fmt.Errorf("invalid --buckets %d (want >= 1)", *buckets))
	}
	if fs.NArg() == 0 {
		return usageErr(stderr, fmt.Errorf("no report paths given"))
	}

	res, tests, err := load(fs.Args(), *group)
	if err != nil {
		if _, ok := err.(*usageError); ok {
			return usageErr(stderr, err)
		}
		return runtimeErr(stderr, err)
	}
	if *testFilter != "" {
		var kept []history.Test
		for _, t := range tests {
			if strings.Contains(t.ID, *testFilter) {
				kept = append(kept, t)
			}
		}
		tests = kept
	}
	bs := trend.Compute(tests, runIDs(res.Runs), *buckets)

	switch *format {
	case "text":
		render.TrendText(stdout, bs, len(res.Runs))
	case "json":
		payload := map[string]any{
			"runs":    len(res.Runs),
			"buckets": bs,
		}
		if err := render.JSON(stdout, "trend", payload); err != nil {
			return runtimeErr(stderr, err)
		}
	default:
		return usageErr(stderr, fmt.Errorf("invalid --format %q (want text or json)", *format))
	}
	return ExitOK
}
