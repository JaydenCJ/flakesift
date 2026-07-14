#!/usr/bin/env bash
# Fabricates a deterministic 20-run JUnit XML history so you can try
# flakesift without hunting for real CI artifacts. One XML file per run,
# five tests covering every pathology flakesift distinguishes:
#
#   orders.CartTest::testCheckoutRace   coin-flip: alternates pass/fail
#   auth.SessionTest::testLoginRetry    retry-masked: fails then passes in-run
#   billing.InvoiceTest::testRounding   regression: passes, then fails from run 15
#   search.IndexTest::testMigration     broken: fails every run
#   search.IndexTest::testTokenize      healthy: passes every run
#
# Usage: bash examples/make-history.sh [target-dir]   (default: ./ci-history)
set -euo pipefail

TARGET="${1:-ci-history}"
mkdir -p "$TARGET"

for i in $(seq 1 20); do
  DAY="$(printf '%02d' "$i")"

  if [ $((i % 2)) -eq 0 ]; then
    CART='<testcase classname="orders.CartTest" name="testCheckoutRace" time="1.32"><failure message="cart total mismatch: expected 3 items, got 2"/></testcase>'
  else
    CART='<testcase classname="orders.CartTest" name="testCheckoutRace" time="1.29"/>'
  fi

  if [ $((i % 3)) -eq 0 ]; then
    # Retry-plugin shape: one <testcase> per attempt, failure then pass.
    AUTH='<testcase classname="auth.SessionTest" name="testLoginRetry" time="0.88"><failure message="token refresh race: 401 from stub server"/></testcase>
    <testcase classname="auth.SessionTest" name="testLoginRetry" time="0.91"/>'
  else
    AUTH='<testcase classname="auth.SessionTest" name="testLoginRetry" time="0.87"/>'
  fi

  if [ "$i" -ge 15 ]; then
    BILLING='<testcase classname="billing.InvoiceTest" name="testRounding" time="0.05"><failure message="rounding drift: 10.005 rendered as 10.00"/></testcase>'
  else
    BILLING='<testcase classname="billing.InvoiceTest" name="testRounding" time="0.05"/>'
  fi

  cat > "$TARGET/run-0$DAY.xml" <<EOF
<testsuites>
  <testsuite name="unit" timestamp="2026-06-${DAY}T04:30:00" tests="6">
    $CART
    $AUTH
    $BILLING
    <testcase classname="search.IndexTest" name="testMigration" time="2.10"><error message="schema drift: column missing"/></testcase>
    <testcase classname="search.IndexTest" name="testTokenize" time="0.02"/>
  </testsuite>
</testsuites>
EOF
done

echo "wrote 20 runs to $TARGET/"
