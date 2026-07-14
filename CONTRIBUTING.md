# Contributing to flakesift

Issues, discussions and pull requests are all welcome.

## Getting started

You need Go ≥1.22; nothing else — the project has zero runtime dependencies.

```bash
git clone https://github.com/JaydenCJ/flakesift && cd flakesift
go build ./...
go test ./...
bash scripts/smoke.sh
```

`scripts/smoke.sh` builds the binary, fabricates a deterministic 10-run
JUnit XML history in a temp dir, and asserts on real CLI output across
every subcommand; it must finish by printing `SMOKE OK`.

## Before you open a pull request

1. `gofmt -l .` reports nothing (formatting is enforced).
2. `go vet ./...` passes with no findings.
3. `go test ./...` passes (90 deterministic tests, no network).
4. `bash scripts/smoke.sh` prints `SMOKE OK`.
5. Add tests for behavior changes; keep logic in pure, unit-testable
   modules (parsing, history, scoring, and rendering never touch the
   filesystem — only `ingest` walks directories).

## Ground rules

- Keep dependencies at zero; adding one needs strong justification in the PR.
- No network calls, ever. flakesift reads local files and writes stdout.
  No telemetry.
- Determinism first: identical input must produce byte-identical output,
  including all orderings. New output paths need a determinism test.
- Scoring changes are model changes: update `docs/scoring.md` in the same
  PR and pin the new expected values with exact-score tests.
- New JUnit dialect support goes into `internal/junit` with a test
  reproducing the real XML shape the tool emits.
- Code comments and doc comments are written in English.

## Reporting bugs

Include the output of `flakesift version`, the full command you ran, the
output of `flakesift runs <your-paths>` (it shows exactly what was ingested
and in which order), and — for parsing bugs — a minimal XML snippet that
reproduces the issue, redacted as needed.

## Security

Please do not open public issues for security problems; use GitHub's
private vulnerability reporting on this repository instead.
