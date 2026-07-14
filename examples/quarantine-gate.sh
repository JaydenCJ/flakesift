#!/usr/bin/env bash
# A CI-shaped pipeline step: check suite health against limits, refresh the
# skip pattern for go test, dump a JSON snapshot — and only then fail the
# step if the gate breached, so a red gate still leaves you the artifacts.
#
# Usage: bash examples/quarantine-gate.sh <reports-dir>
set -euo pipefail

REPORTS="${1:?usage: quarantine-gate.sh <reports-dir>}"
BIN="${FLAKESIFT:-flakesift}"

echo "== suite health gate =="
# Breach when more than 2 flaky tests exist or any test is broken.
GATE=0
"$BIN" gate --max-flaky 2 --max-broken 0 "$REPORTS" || GATE=$?

echo
echo "== refresh quarantine list =="
# Anchored regexp ready for: go test -skip "$(cat quarantine.gotest)" ./...
"$BIN" quarantine --format gotest "$REPORTS" > quarantine.gotest
echo "wrote quarantine.gotest:"
cat quarantine.gotest

echo
echo "== machine-readable snapshot for dashboards =="
"$BIN" score --format json "$REPORTS" | head -20
echo "…"

exit "$GATE"
