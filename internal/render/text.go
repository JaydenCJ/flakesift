// Package render turns scored results, trends, and run listings into
// terminal text, stable JSON, CSV, and quarantine lists. All output is
// deterministic: identical input produces byte-identical output.
package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/JaydenCJ/flakesift/internal/ingest"
	"github.com/JaydenCJ/flakesift/internal/score"
	"github.com/JaydenCJ/flakesift/internal/trend"
)

// Column alignment markers for table.
const (
	alignLeft  = false
	alignRight = true
)

// table writes rows with padded columns; align[i] == alignRight pads the
// column on the left. The final column is never padded, so trailing
// whitespace never appears in output.
func table(w io.Writer, headers []string, rows [][]string, align []bool) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len([]rune(h))
	}
	for _, row := range rows {
		for i, cell := range row {
			if n := len([]rune(cell)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	writeRow := func(cells []string) {
		var b strings.Builder
		for i, cell := range cells {
			if i > 0 {
				b.WriteString("  ")
			}
			pad := widths[i] - len([]rune(cell))
			last := i == len(cells)-1
			if align[i] == alignRight {
				b.WriteString(strings.Repeat(" ", pad))
				b.WriteString(cell)
			} else {
				b.WriteString(cell)
				if !last {
					b.WriteString(strings.Repeat(" ", pad))
				}
			}
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
	writeRow(headers)
	for _, row := range rows {
		writeRow(row)
	}
}

// ScoreText renders the score table for humans.
func ScoreText(w io.Writer, results []score.Result, runCount int) {
	fmt.Fprintf(w, "flakesift score — %d runs, %d tests\n\n", runCount, len(results))
	if len(results) == 0 {
		fmt.Fprintln(w, "no tests found")
		return
	}
	headers := []string{"score", "class", "runs", "fail%", "flips", "retries", "last", "test"}
	align := []bool{alignRight, alignLeft, alignRight, alignRight, alignRight, alignRight, alignLeft, alignLeft}
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		rows = append(rows, []string{
			fmt.Sprintf("%.1f", r.Score),
			string(r.Class),
			fmt.Sprintf("%d", r.Runs),
			fmt.Sprintf("%.1f", r.FailRate),
			fmt.Sprintf("%d", r.Flips),
			fmt.Sprintf("%d", r.Recovered),
			r.LastOutcome,
			r.ID,
		})
	}
	table(w, headers, rows, align)
}

// TrendText renders buckets plus sparklines for humans.
func TrendText(w io.Writer, buckets []trend.Bucket, runCount int) {
	fmt.Fprintf(w, "flakesift trend — %d runs in %d buckets\n\n", runCount, len(buckets))
	if len(buckets) == 0 {
		fmt.Fprintln(w, "no runs found")
		return
	}
	failRates := make([]float64, len(buckets))
	flakeRates := make([]float64, len(buckets))
	for i, b := range buckets {
		failRates[i] = b.FailRate
		flakeRates[i] = b.FlakeRate
	}
	fmt.Fprintf(w, "fail rate   %s  (max %.1f%%)\n", trend.Sparkline(failRates), maxOf(failRates))
	fmt.Fprintf(w, "flake rate  %s  (max %.1f%%)\n\n", trend.Sparkline(flakeRates), maxOf(flakeRates))

	headers := []string{"bucket", "runs", "execs", "fails", "flakes", "fail%", "flake%", "span"}
	align := []bool{alignRight, alignRight, alignRight, alignRight, alignRight, alignRight, alignRight, alignLeft}
	rows := make([][]string, 0, len(buckets))
	for _, b := range buckets {
		span := b.FirstRun
		if b.LastRun != b.FirstRun {
			span += " … " + b.LastRun
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", b.Index+1),
			fmt.Sprintf("%d", b.Runs),
			fmt.Sprintf("%d", b.Executions),
			fmt.Sprintf("%d", b.Fails),
			fmt.Sprintf("%d", b.FlakeEvents),
			fmt.Sprintf("%.1f", b.FailRate),
			fmt.Sprintf("%.1f", b.FlakeRate),
			span,
		})
	}
	table(w, headers, rows, align)
}

// RunsText renders the ingested run listing for humans.
func RunsText(w io.Writer, runs []ingest.Run, skipped []string) {
	fmt.Fprintf(w, "flakesift runs — %d runs ingested\n\n", len(runs))
	headers := []string{"#", "run", "time", "files", "cases"}
	align := []bool{alignRight, alignLeft, alignLeft, alignRight, alignRight}
	rows := make([][]string, 0, len(runs))
	for i, r := range runs {
		ts := "-"
		if !r.Time.IsZero() {
			ts = r.Time.UTC().Format("2006-01-02T15:04:05Z")
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", i+1),
			r.ID,
			ts,
			fmt.Sprintf("%d", len(r.Files)),
			fmt.Sprintf("%d", len(r.Cases)),
		})
	}
	table(w, headers, rows, align)
	if len(skipped) > 0 {
		noun := "files"
		if len(skipped) == 1 {
			noun = "file"
		}
		fmt.Fprintf(w, "\n%d non-JUnit XML %s skipped\n", len(skipped), noun)
	}
}

func maxOf(vals []float64) float64 {
	m := 0.0
	for _, v := range vals {
		if v > m {
			m = v
		}
	}
	return m
}
