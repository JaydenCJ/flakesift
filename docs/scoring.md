# The flakiness model

flakesift's score is deliberately simple enough to explain in one sentence:
**the score is the percentage of a test's executed runs that produced direct
evidence of nondeterminism.** This page defines every term in that sentence.

## From XML to run outcomes

1. Every JUnit XML file (or directory, with `--group dir`) is one **run**.
   Runs are ordered by the earliest `<testsuite timestamp="…">` they
   contain, falling back to path order (zero-padded names stay
   chronological).
2. Within one run, all executions of the same test — retry plugins emit one
   `<testcase>` per attempt; Maven Surefire folds attempts into
   `<flakyFailure>` / `<rerunFailure>` children — collapse into a single
   run-level outcome:
   - **pass** — at least one attempt passed. If another attempt failed, the
     run is additionally marked **recovered**.
   - **fail** — every attempt failed (`<failure>` and `<error>` both count).
   - **skip** — only skip markers. Skipped runs carry no stability
     information and are excluded from all math below.

## Flake events

For each test, every executed run `i` contributes a binary flake event:

```
event(i) = recovered(i)  OR  outcome(i) ≠ outcome(i−1)
```

Either a retry rescued the run (the strongest signal a report can carry),
or the verdict flipped versus the test's previous execution.

## Score

```
score = 100 × Σ w(i)·event(i) / Σ w(i)
```

With the default `--half-life 0` all weights are 1, so the score is simply
*flake events per executed run*. With `--half-life N`, a run's weight is
`0.5^(age/N)` where age is measured in runs from the newest execution — a
test cured 200 runs ago stops outranking one that started flaking last week.

Properties worth knowing:

- Always passes → 0. Always fails → 0 (see *broken* below).
- Alternates every run → ~100. Recovered by retry every run → 100.
- One failure inside a long green history → two events (flip in, flip out),
  so a single blip on 20 runs scores 10 — visible, but below the default
  quarantine threshold of 30.
- A sustained regression (green → red and *stays* red) is one event, so it
  scores near 0 and classifies as *suspect* or *broken* — flakesift will
  not tell you to quarantine a genuinely failing test.

## Classes

| Class | Rule | What to do |
|---|---|---|
| `flaky` | score ≥ `--threshold` (default 30) | quarantine, then fix the race |
| `broken` | failed 100% of executed runs, never recovered | fix now — do not quarantine |
| `suspect` | failed or retried at least once, score below threshold | watch; often a real regression |
| `healthy` | never failed, never retried | nothing |
| `new` | fewer than `--min-runs` executions (default 3) | wait for more history |

The `broken`/`flaky` split is the load-bearing design decision: a test that
fails every time is perfectly *deterministic*. Hiding it in a quarantine
list buries a real bug, so `quarantine` excludes broken tests unless you
pass `--include-broken`, and `gate` tracks the two counts separately.

## Trend buckets

`flakesift trend` splits the run sequence into `--buckets` even spans and
reports per-bucket fail rate (failed executions / executions) and flake
rate (flake events / executions), using the same event definition as the
scorer. Flips across a bucket boundary are attributed to the later bucket.
