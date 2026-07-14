// Package cli implements the flakesift command line: flag parsing,
// subcommand dispatch, and exit codes. All I/O is injected so integration
// tests can drive the real command in-process.
//
// Exit codes: 0 ok, 1 gate breach, 2 usage error, 3 runtime error.
package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/JaydenCJ/flakesift/internal/history"
	"github.com/JaydenCJ/flakesift/internal/ingest"
	"github.com/JaydenCJ/flakesift/internal/score"
	"github.com/JaydenCJ/flakesift/internal/version"
)

// Exit codes.
const (
	ExitOK      = 0
	ExitBreach  = 1
	ExitUsage   = 2
	ExitRuntime = 3
)

const usage = `flakesift — score test flakiness from plain JUnit XML history

Usage:
  flakesift <command> [flags] <reports-dir|files...>

Commands:
  score       rank every test by flakiness score (default)
  quarantine  emit the list of tests to quarantine
  trend       bucket runs over time and chart fail/flake rates
  gate        exit 1 when flaky/broken counts exceed limits
  runs        list the ingested runs (debug what was read)
  version     print the version

Common flags (each command also has its own; see '<command> -h'):
  --group file|dir   how XML files map to runs (default file)

Exit codes: 0 ok, 1 gate breach, 2 usage error, 3 runtime error.
`

// Main runs the CLI and returns the process exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return ExitUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "flakesift %s\n", version.Version)
		return ExitOK
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return ExitOK
	case "score":
		return cmdScore(rest, stdout, stderr)
	case "quarantine":
		return cmdQuarantine(rest, stdout, stderr)
	case "trend":
		return cmdTrend(rest, stdout, stderr)
	case "gate":
		return cmdGate(rest, stdout, stderr)
	case "runs":
		return cmdRuns(rest, stdout, stderr)
	default:
		// Bare paths mean "score" — the 90% invocation stays short.
		if len(cmd) > 0 && cmd[0] != '-' {
			return cmdScore(args, stdout, stderr)
		}
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", cmd, usage)
		return ExitUsage
	}
}

// newFlagSet builds a silent FlagSet whose errors we render ourselves so
// every misuse consistently exits 2.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// ingestOptions wires the shared --group flag.
func addGroupFlag(fs *flag.FlagSet) *string {
	return fs.String("group", "file", "run grouping: 'file' (one run per XML file) or 'dir' (one run per directory)")
}

func groupOptions(group string) (ingest.Options, error) {
	switch group {
	case "file":
		return ingest.Options{}, nil
	case "dir":
		return ingest.Options{GroupByDir: true}, nil
	default:
		return ingest.Options{}, fmt.Errorf("invalid --group %q (want 'file' or 'dir')", group)
	}
}

// addScoreFlags wires the scoring parameters shared by score, quarantine
// and gate. Defaults live in score.DefaultParams so docs, CLI, and library
// callers agree.
func addScoreFlags(fs *flag.FlagSet) *score.Params {
	p := score.DefaultParams()
	fs.Float64Var(&p.Threshold, "threshold", p.Threshold, "score at or above which a test is flaky (0-100)")
	fs.IntVar(&p.MinRuns, "min-runs", p.MinRuns, "executions required before a test is classified")
	fs.Float64Var(&p.HalfLife, "half-life", p.HalfLife, "recency half-life in runs (0 = uniform weighting)")
	return &p
}

func validateScoreParams(p *score.Params) error {
	if p.Threshold < 0 || p.Threshold > 100 {
		return fmt.Errorf("invalid --threshold %.1f (want 0-100)", p.Threshold)
	}
	if p.MinRuns < 1 {
		return fmt.Errorf("invalid --min-runs %d (want >= 1)", p.MinRuns)
	}
	if p.HalfLife < 0 {
		return fmt.Errorf("invalid --half-life %.1f (want >= 0)", p.HalfLife)
	}
	return nil
}

// usageError marks bad flag values discovered inside load(), so callers
// can exit 2 instead of 3.
type usageError struct{ err error }

func (u *usageError) Error() string { return u.err.Error() }

// load ingests report paths and builds histories — the shared front half
// of every analyzing subcommand.
func load(paths []string, group string) (*ingest.Result, []history.Test, error) {
	opt, err := groupOptions(group)
	if err != nil {
		return nil, nil, &usageError{err}
	}
	res, err := ingest.Collect(paths, opt)
	if err != nil {
		return nil, nil, err
	}
	return res, history.Build(res.Runs), nil
}

func usageErr(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "flakesift: %v\n", err)
	return ExitUsage
}

func runtimeErr(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "flakesift: %v\n", err)
	return ExitRuntime
}

func runIDs(runs []ingest.Run) []string {
	ids := make([]string, len(runs))
	for i, r := range runs {
		ids[i] = r.ID
	}
	return ids
}
