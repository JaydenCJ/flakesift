package cli

import (
	"fmt"
	"io"

	"github.com/JaydenCJ/flakesift/internal/ingest"
	"github.com/JaydenCJ/flakesift/internal/render"
)

// cmdRuns lists what was ingested — the first thing to check when a score
// looks wrong is whether the runs and their order match expectations.
func cmdRuns(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("runs", stderr)
	format := fs.String("format", "text", "output format: text or json")
	group := addGroupFlag(fs)
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() == 0 {
		return usageErr(stderr, fmt.Errorf("no report paths given"))
	}

	opt, err := groupOptions(*group)
	if err != nil {
		return usageErr(stderr, err)
	}
	res, err := ingest.Collect(fs.Args(), opt)
	if err != nil {
		return runtimeErr(stderr, err)
	}

	switch *format {
	case "text":
		render.RunsText(stdout, res.Runs, res.Skipped)
	case "json":
		type runInfo struct {
			ID    string   `json:"id"`
			Time  string   `json:"time,omitempty"`
			Files []string `json:"files"`
			Cases int      `json:"cases"`
		}
		infos := make([]runInfo, 0, len(res.Runs))
		for _, r := range res.Runs {
			info := runInfo{ID: r.ID, Files: r.Files, Cases: len(r.Cases)}
			if !r.Time.IsZero() {
				info.Time = r.Time.UTC().Format("2006-01-02T15:04:05Z")
			}
			infos = append(infos, info)
		}
		payload := map[string]any{
			"runs":    infos,
			"skipped": res.Skipped,
		}
		if err := render.JSON(stdout, "runs", payload); err != nil {
			return runtimeErr(stderr, err)
		}
	default:
		return usageErr(stderr, fmt.Errorf("invalid --format %q (want text or json)", *format))
	}
	return ExitOK
}
