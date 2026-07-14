#!/usr/bin/env bash
# End-to-end smoke test for flakesift: builds the binary, fabricates a
# deterministic 10-run JUnit XML history, and asserts on the real CLI
# output of every subcommand. No network, idempotent, finishes in seconds.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

fail() {
  echo "SMOKE FAIL: $*" >&2
  exit 1
}

BIN="$WORKDIR/flakesift"
REPORTS="$WORKDIR/reports"
mkdir -p "$REPORTS"

echo "1. build"
(cd "$ROOT" && go build -o "$BIN" ./cmd/flakesift) || fail "go build failed"

echo "2. version matches manifest"
"$BIN" version | grep -qx "flakesift 0.1.0" || fail "version mismatch"

echo "3. fabricate a 10-run history (stable / coin-flip / retry-masked / broken)"
for i in $(seq 1 10); do
  if [ $((i % 2)) -eq 0 ]; then
    FLIP='<testcase classname="pkg.Cart" name="testCheckoutRace"><failure message="cart total mismatch"/></testcase>'
  else
    FLIP='<testcase classname="pkg.Cart" name="testCheckoutRace"/>'
  fi
  if [ $((i % 3)) -eq 0 ]; then
    RETRY='<testcase classname="pkg.Auth" name="testLoginRetry"><failure message="token race"/></testcase>
    <testcase classname="pkg.Auth" name="testLoginRetry"/>'
  else
    RETRY='<testcase classname="pkg.Auth" name="testLoginRetry"/>'
  fi
  DAY="$(printf '%02d' "$i")"
  cat > "$REPORTS/run-0$DAY.xml" <<EOF
<testsuites>
  <testsuite name="unit" timestamp="2026-03-${DAY}T10:00:00">
    <testcase classname="pkg.Util" name="testParse" time="0.01"/>
    $FLIP
    $RETRY
    <testcase classname="pkg.DB" name="testMigration"><error message="schema drift"/></testcase>
  </testsuite>
</testsuites>
EOF
done
# One foreign XML file that must be skipped, not fatal.
echo '<coverage line-rate="0.9"></coverage>' > "$REPORTS/coverage.xml"

echo "4. score ranks the coin-flip test first and separates classes"
OUT="$("$BIN" score "$REPORTS")"
echo "$OUT" | grep -q "flakesift score — 10 runs, 4 tests" || fail "score header wrong"
echo "$OUT" | sed -n '4p' | grep -q "testCheckoutRace" || fail "coin-flip test not ranked first"
echo "$OUT" | grep -q "flaky" || fail "flaky class missing"
echo "$OUT" | grep -q "broken" || fail "broken class missing"
echo "$OUT" | grep -q "healthy" || fail "healthy class missing"

echo "5. JSON output carries the stable envelope"
JSON="$("$BIN" score --format json "$REPORTS")"
echo "$JSON" | grep -q '"tool": "flakesift"' || fail "json envelope missing"
echo "$JSON" | grep -q '"schema_version": 1' || fail "schema_version missing"
echo "$JSON" | grep -q '"class": "broken"' || fail "broken class missing in json"

echo "6. quarantine lists flaky tests only (broken needs opt-in)"
Q="$("$BIN" quarantine "$REPORTS")"
echo "$Q" | grep -q "pkg.Cart::testCheckoutRace" || fail "flaky test not quarantined"
echo "$Q" | grep -q "testMigration" && fail "broken test quarantined without opt-in"
"$BIN" quarantine --include-broken "$REPORTS" | grep -q "testMigration" \
  || fail "--include-broken did not add the broken test"

echo "7. quarantine gotest format is an anchored skip regexp"
"$BIN" quarantine --format gotest "$REPORTS" \
  | grep -qx '^\^(testCheckoutRace|testLoginRetry)\$$' || fail "gotest regexp wrong"

echo "8. trend renders sparkline buckets"
TREND="$("$BIN" trend --buckets 5 "$REPORTS")"
echo "$TREND" | grep -q "10 runs in 5 buckets" || fail "trend header wrong"
echo "$TREND" | grep -q "█" || fail "sparkline missing"

echo "9. gate enforces limits with exit codes"
"$BIN" gate --max-flaky 2 "$REPORTS" >/dev/null || fail "gate should pass at limit 2"
if "$BIN" gate --max-flaky 1 "$REPORTS" >/dev/null; then
  fail "gate should breach at limit 1 (two flaky tests)"
fi

echo "10. runs shows ingestion and the skipped foreign file"
RUNS="$("$BIN" runs "$REPORTS")"
echo "$RUNS" | grep -q "10 runs ingested" || fail "runs count wrong"
echo "$RUNS" | grep -q "1 non-JUnit XML file skipped" || fail "skip note missing"

echo "11. usage errors exit 2"
set +e
"$BIN" score --format yaml "$REPORTS" >/dev/null 2>&1
[ $? -eq 2 ] || fail "bad --format should exit 2"
set -e

echo "SMOKE OK"
