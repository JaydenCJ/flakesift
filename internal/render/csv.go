package render

import (
	"encoding/csv"
	"fmt"
	"io"

	"github.com/JaydenCJ/flakesift/internal/score"
)

// ScoreCSV writes one row per test, header first — the shape spreadsheet
// pivot tables and BI imports want.
func ScoreCSV(w io.Writer, results []score.Result) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{
		"id", "classname", "name", "score", "class", "runs", "passes",
		"fails", "skips", "flips", "recovered", "fail_rate", "last_outcome", "last_run",
	}); err != nil {
		return err
	}
	for _, r := range results {
		if err := cw.Write([]string{
			r.ID, r.ClassName, r.Name,
			fmt.Sprintf("%.1f", r.Score), string(r.Class),
			fmt.Sprintf("%d", r.Runs), fmt.Sprintf("%d", r.Passes),
			fmt.Sprintf("%d", r.Fails), fmt.Sprintf("%d", r.Skips),
			fmt.Sprintf("%d", r.Flips), fmt.Sprintf("%d", r.Recovered),
			fmt.Sprintf("%.1f", r.FailRate), r.LastOutcome, r.LastRunID,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
