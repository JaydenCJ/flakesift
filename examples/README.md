# Examples

Two runnable scripts, no setup beyond a built `flakesift` binary.

## make-history.sh

Fabricates a deterministic 20-run JUnit XML history (one file per run) with
five tests, one per pathology flakesift distinguishes: a coin-flip flake, a
retry-masked flake, a fresh regression, a broken test, and a healthy test.

```bash
bash examples/make-history.sh /tmp/ci-history
flakesift score /tmp/ci-history
flakesift trend --buckets 5 /tmp/ci-history
```

Because the generator is deterministic, the scores you see match the ones
quoted in the top-level README.

## quarantine-gate.sh

A CI-shaped pipeline step: check suite-health limits with `gate`, refresh a
`go test -skip` pattern with `quarantine --format gotest`, dump a JSON
snapshot for dashboards — then exit with the gate's status (1 on breach), so
a red gate still leaves you every artifact. The sample history contains a
broken test, so against it this script prints everything and exits 1.

```bash
bash examples/make-history.sh /tmp/ci-history
FLAKESIFT=./flakesift bash examples/quarantine-gate.sh /tmp/ci-history
```

Set `FLAKESIFT` to the path of your binary if it is not on `PATH`.
