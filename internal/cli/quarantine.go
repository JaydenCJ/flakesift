package cli

import (
	"fmt"
	"io"

	"github.com/JaydenCJ/flakesift/internal/render"
	"github.com/JaydenCJ/flakesift/internal/score"
)

// cmdQuarantine emits the list of tests a team should isolate: everything
// classified flaky, plus broken tests when explicitly requested (hiding a
// 100%-failing test is usually a mistake, so that needs opting in).
func cmdQuarantine(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("quarantine", stderr)
	format := fs.String("format", "lines", "output format: lines, json, gotest, or pytest")
	includeBroken := fs.Bool("include-broken", false, "also quarantine always-failing (broken) tests")
	group := addGroupFlag(fs)
	params := addScoreFlags(fs)
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if err := validateScoreParams(params); err != nil {
		return usageErr(stderr, err)
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
	all := score.EvaluateAll(tests, *params)
	var quarantined []score.Result
	for _, r := range all {
		if r.Class == score.ClassFlaky || (*includeBroken && r.Class == score.ClassBroken) {
			quarantined = append(quarantined, r)
		}
	}

	switch *format {
	case "lines":
		render.QuarantineLines(stdout, quarantined)
	case "json":
		payload := map[string]any{
			"runs":        len(res.Runs),
			"threshold":   params.Threshold,
			"quarantined": jsonSlice(quarantined),
		}
		if err := render.JSON(stdout, "quarantine", payload); err != nil {
			return runtimeErr(stderr, err)
		}
	case "gotest":
		render.QuarantineGotest(stdout, quarantined)
	case "pytest":
		render.QuarantinePytest(stdout, quarantined)
	default:
		return usageErr(stderr, fmt.Errorf("invalid --format %q (want lines, json, gotest, or pytest)", *format))
	}
	return ExitOK
}

// jsonSlice keeps "quarantined": [] instead of null for empty results —
// stable shapes are kinder to downstream jq pipelines.
func jsonSlice(results []score.Result) []score.Result {
	if results == nil {
		return []score.Result{}
	}
	return results
}
