# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-13

### Added

- JUnit XML parser accepting the dialects emitted by common tools without
  configuration: `<testsuites>` roots, nested suites (names flattened with
  `/`), bare `<testsuite>` roots, `<failure>`/`<error>`/`<skipped>` with
  documented precedence, and the Maven Surefire rerun extensions
  (`<flakyFailure>`, `<flakyError>`, `<rerunFailure>`, `<rerunError>`).
- Run ingestion over files and directories with two grouping modes
  (`--group file|dir`), chronological ordering by suite timestamp with
  path-order fallback, case-insensitive `.xml` matching, and silent
  skipping of non-JUnit XML (coverage reports) discovered during walks.
- Per-test history condensation: retry attempts collapse into one run-level
  outcome with recovery ("failed, then passed, inside the same run") and
  retry tracking; skips excluded from stability math.
- Explainable 0–100 flakiness score (flake events per executed run) with
  optional `--half-life` recency weighting, plus a five-way classification:
  healthy / suspect / flaky / broken / new — broken (always-failing) tests
  are deliberately never scored as flaky.
- `score` subcommand with aligned text tables, stable JSON
  (`schema_version: 1`), CSV export, `--top`, and `--min-score`.
- `quarantine` subcommand emitting plain line lists, JSON, an anchored
  `go test -skip` regexp, or a pytest `-k` deselect expression, with
  `--include-broken` opt-in.
- `trend` subcommand bucketing the run sequence with per-bucket fail/flake
  rates and unicode sparklines, and a `--test` substring filter.
- `gate` subcommand enforcing `--max-flaky` / `--max-broken` limits with
  exit code 1 on breach, for CI policy steps.
- `runs` subcommand listing the ingested run order for debugging.
- Runnable examples (`examples/make-history.sh`,
  `examples/quarantine-gate.sh`) and a scoring-model reference
  (`docs/scoring.md`).
- 90 deterministic offline tests (unit + in-process CLI integration against
  fabricated JUnit histories) and `scripts/smoke.sh`.

[0.1.0]: https://github.com/JaydenCJ/flakesift/releases/tag/v0.1.0
