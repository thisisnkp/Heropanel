#!/usr/bin/env bash
# Performance budgets, measured rather than asserted in a doc.
#
# docs/10 has carried "idle RAM (npd+broker < 80 MB) and cold-start (< 1.5 s) as
# CI budgets" as a cross-cutting workstream since Phase 0, and nothing ever
# measured either one. A budget nobody measures is a wish. This script measures
# both against real processes and fails the build when they regress.
#
# What it deliberately does NOT do is measure under systemd. There is no systemd
# in this container (docs/10 records the same gap for cgroup limits and the
# self-update transient unit), so these are the numbers for the processes
# themselves — which is where the regressions actually come from.
set -u
sec(){ echo; echo "======== $* ========"; }
fail=0
pass(){ echo "  ok   $1"; }
bad(){ echo "  FAIL $1"; fail=1; }

# Budgets. Both come from docs/10; keep them in step with it.
RSS_BUDGET_KB=$((80 * 1024))
COLD_START_BUDGET_MS=1500

base=http://127.0.0.1:18450
KEY=$(head -c 32 /dev/urandom | base64)
TOKEN=$(head -c 32 /dev/urandom | base64)

install -m0755 /np/npd /usr/local/bin/
install -m0755 /np/np-broker /usr/local/bin/
rm -f /tmp/budget.db

sec "cold start: process launch to /readyz"
# Milliseconds since the epoch, before anything is spawned. date +%s%3N is the
# only clock available here that is finer than a second.
t0=$(date +%s%3N)
NP_SERVER_HOST=127.0.0.1 NP_SERVER_PORT=18450 NP_LOG_FORMAT=text \
  NP_DATABASE_DRIVER=sqlite NP_DATABASE_DSN=/tmp/budget.db \
  NP_SECRET_KEY="$KEY" npd >/tmp/npd-budget.log 2>&1 &
NPD_PID=$!

ready=0
for _ in $(seq 1 200); do
  if curl -sf $base/readyz >/dev/null 2>&1; then ready=1; break; fi
  sleep 0.05
done
t1=$(date +%s%3N)
cold=$((t1 - t0))

if [ "$ready" != 1 ]; then
  bad "npd never became ready"
  tail -20 /tmp/npd-budget.log
  echo "run-budget.sh : FAIL"; exit 1
fi
echo "  cold start: ${cold} ms (budget ${COLD_START_BUDGET_MS} ms)"
if [ "$cold" -le "$COLD_START_BUDGET_MS" ]; then
  pass "cold start within budget"
else
  bad "cold start ${cold} ms exceeds ${COLD_START_BUDGET_MS} ms"
fi

sec "idle RSS: npd + np-broker"
# Let the runtime settle: the first GC cycle and the initial heap growth after
# serving a request are not what "idle" means.
curl -sf $base/healthz >/dev/null 2>&1 || true
sleep 3

NP_BROKER_TOKEN="$TOKEN" np-broker --serve --socket /tmp/budget-broker.sock >/tmp/broker-budget.log 2>&1 &
BROKER_PID=$!
sleep 2

rss_of(){ awk '/^VmRSS:/{print $2}' "/proc/$1/status" 2>/dev/null || echo 0; }
npd_rss=$(rss_of "$NPD_PID")
broker_rss=$(rss_of "$BROKER_PID")
: "${npd_rss:=0}" "${broker_rss:=0}"
total=$((npd_rss + broker_rss))

echo "  npd:       ${npd_rss} kB"
echo "  np-broker: ${broker_rss} kB"
echo "  total:     ${total} kB (budget ${RSS_BUDGET_KB} kB)"

if [ "$total" -eq 0 ]; then
  bad "could not read RSS for either process"
elif [ "$total" -le "$RSS_BUDGET_KB" ]; then
  pass "idle RSS within budget"
else
  bad "idle RSS ${total} kB exceeds ${RSS_BUDGET_KB} kB"
fi

kill "$NPD_PID" "$BROKER_PID" 2>/dev/null
wait "$NPD_PID" 2>/dev/null
wait "$BROKER_PID" 2>/dev/null

echo
if [ "$fail" = 0 ]; then echo "run-budget.sh : PASS"; else echo "run-budget.sh : FAIL"; fi
exit "$fail"
