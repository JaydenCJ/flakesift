package cli

import (
	"fmt"
	"io"

	"github.com/JaydenCJ/flakesift/internal/render"
	"github.com/JaydenCJ/flakesift/internal/score"
)

// cmdScore ranks every test by flakiness.
func cmdScore(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("score", stderr)
	format := fs.String("format", "text", "output format: text, json, or csv")
	top := fs.Int("top", 0, "show only the N highest-scoring tests (0 = all)")
	minScore := fs.Float64("min-score", 0, "hide tests scoring below this value")
	group := addGroupFlag(fs)
	params := addScoreFlags(fs)
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if err := validateScoreParams(params); err != nil {
		return usageErr(stderr, err)
	}
	if *top < 0 {
		return usageErr(stderr, fmt.Errorf("invalid --top %d (want >= 0)", *top))
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
	results := score.EvaluateAll(tests, *params)
	results = filterResults(results, *minScore, *top)

	switch *format {
	case "text":
		render.ScoreText(stdout, results, len(res.Runs))
	case "json":
		payload := map[string]any{
			"runs":  len(res.Runs),
			"tests": results,
		}
		if err := render.JSON(stdout, "score", payload); err != nil {
			return runtimeErr(stderr, err)
		}
	case "csv":
		if err := render.ScoreCSV(stdout, results); err != nil {
			return runtimeErr(stderr, err)
		}
	default:
		return usageErr(stderr, fmt.Errorf("invalid --format %q (want text, json, or csv)", *format))
	}
	return ExitOK
}

func filterResults(results []score.Result, minScore float64, top int) []score.Result {
	if minScore > 0 {
		kept := results[:0]
		for _, r := range results {
			if r.Score >= minScore {
				kept = append(kept, r)
			}
		}
		results = kept
	}
	if top > 0 && top < len(results) {
		results = results[:top]
	}
	return results
}
