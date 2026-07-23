#!/usr/bin/env bash
# Phase 6, Monitor module (slice M1): node metrics, read one-shot AND pushed live
# over the realtime hub — the "live dashboards, no idle polling" exit criterion.
#
# The claim this proves that a unit test cannot: a browser subscribes to
# `monitor:node` and the server, which was doing no metric work at all, starts
# sampling and pushing. wsprobe stands in for the browser (a plain curl can
# complete the upgrade but not speak the hub's subscribe/event frames).
set -u
sec(){ echo; echo "======== $* ========"; }
pass(){ echo "PASS: $*"; }
fail(){ echo "FAIL: $*"; FAILED=1; }
FAILED=0
base=http://127.0.0.1:18455

sec "start hp-broker (root) + hpd (sqlite)"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel
export HP_BROKER_TOKEN=tok
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 HP_BROKER_PANEL_USER=root \
  hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker-monitor.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
# HP_SECRET_KEY seals alert notification targets; HP_MONITOR_PERSIST_SEC shortens
# the persist/evaluate cadence so a firing can be proven without a real minute.
export HP_SECRET_KEY=$(head -c32 /dev/urandom | base64 -w0)
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18455 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp-monitor.db \
  HP_MONITOR_PERSIST_SEC=2 \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd-monitor.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done

# The realtime hub must come up even without Redis — its local push needs none.
grep -q 'realtime hub enabled' /tmp/hpd-monitor.log \
  && pass "the realtime hub is up without Redis" \
  || fail "the realtime hub did not start on a Redis-less install"

sec "auth (bootstrap + login + CSRF)"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/cm.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/cm.txt)
api(){ curl -s -b /tmp/cm.txt -H "X-CSRF-Token: $CSRF" "$@"; }
code(){ curl -s -o /dev/null -w '%{http_code}' -b /tmp/cm.txt -H "X-CSRF-Token: $CSRF" "$@"; }

sec "ONE-SHOT NODE SAMPLE (initial paint)"
node=$(api "$base/api/v1/monitor/node")
echo "$node"
echo "$node" | grep -q '"cpu_percent"' \
  && pass "the node sample reports CPU" || fail "no cpu_percent in the node sample"
echo "$node" | grep -q '"mem_total_kb"' && python3 - "$node" <<'PY'
import json,sys
d=json.loads(sys.argv[1])["data"]
assert d["mem_total_kb"]>0, "mem_total_kb must be positive on a real host"
assert d["load1"]>=0
paths=[x["path"] for x in (d.get("disks") or [])]
assert "/" in paths, f"root filesystem not reported, got {paths}"
root=next(x for x in d["disks"] if x["path"]=="/")
assert root["total_bytes"]>0 and 0<=root["used_percent"]<=100
print("  mem_total_kb=%d load1=%s root_used=%.0f%%" % (d["mem_total_kb"], d["load1"], root["used_percent"]))
PY
[ $? -eq 0 ] && pass "memory, load and the root filesystem are reported with sane values" \
             || fail "the node sample had missing or nonsensical values"

sec "PERMISSION IS ENFORCED"
c=$(curl -s -o /dev/null -w '%{http_code}' "$base/api/v1/monitor/node")
[ "$c" = "401" ] && pass "an unauthenticated node read is refused (401)" \
                 || fail "an unauthenticated node read returned $c"

sec "*** LIVE PUSH: SUBSCRIPTION-GATED SAMPLING ***"
# wsprobe subscribes to monitor:node. The server was sampling NOTHING until this
# subscription; receiving an event proves the gate opened and the push works.
if probe=$(/hp/wsprobe "$base" a@h.io supersecret1 monitor:node 15 2>/tmp/wsprobe.err); then
  echo "  $probe"
  echo "$probe" | grep -q 'EVENT monitor:node' && echo "$probe" | grep -q 'cpu_percent' \
    && pass "a live node sample was pushed to a subscriber over the hub" \
    || fail "the pushed event did not carry a node sample"
else
  echo "  wsprobe stderr: $(cat /tmp/wsprobe.err)"
  fail "no live node sample arrived over the hub"
fi

sec "SERVICE HEALTH (via the broker's service.status)"
# Proves the pipeline hpd -> broker -> systemctl is-active -> parsed state -> API.
svc=$(api "$base/api/v1/monitor/services")
echo "$svc"
echo "$svc" | grep -q 'openlitespeed' && echo "$svc" | grep -q '"state"' \
  && pass "service health reports a state for each service" \
  || fail "service health did not report per-service states"
grep -q 'service.status' /tmp/broker-monitor.log \
  && pass "service.status ran through the broker's audit chain" \
  || fail "service status did not go through the broker"

sec "PER-SITE METRICS ENDPOINT"
# No sites are provisioned in this minimal harness, so the list is empty — but the
# endpoint must respond with the sites envelope. (Live per-site cgroup values need
# real systemd slices, which the shim-based container has not; the cgroup parsing
# is unit-tested.)
sites=$(api "$base/api/v1/monitor/sites")
echo "$sites"
echo "$sites" | grep -q '"sites"' \
  && pass "the per-site metrics endpoint responds with the sites envelope" \
  || fail "the per-site metrics endpoint did not respond"

sec "*** LIVE PUSH: SERVICE HEALTH IS ALSO GATED + PUSHED ***"
if probe=$(/hp/wsprobe "$base" a@h.io supersecret1 monitor:services 15 2>/tmp/wsprobe2.err); then
  echo "  $probe"
  echo "$probe" | grep -q 'EVENT monitor:services' && echo "$probe" | grep -q '"service"' \
    && pass "live service health was pushed to a subscriber" \
    || fail "the pushed services event did not carry service states"
else
  echo "  wsprobe stderr: $(cat /tmp/wsprobe2.err)"
  fail "no live service-health event arrived over the hub"
fi

sec "HISTORY ENDPOINT (persisted + rolled-up node metrics)"
# The persister writes once a minute and the rollup runs hourly, so a short run
# has no points yet — but the endpoint must respond with the points envelope for
# each allowed range. (Persist averaging, hourly rollup and pruning are covered
# by the repository unit tests against real SQLite.)
hist=$(api "$base/api/v1/monitor/history?range=24h")
echo "$hist"
echo "$hist" | grep -q '"points"' \
  && pass "the history endpoint responds with the points envelope" \
  || fail "the history endpoint did not respond"
# An out-of-range value falls back to the default rather than erroring.
c=$(code "$base/api/v1/monitor/history?range=nonsense")
[ "$c" = "200" ] && pass "an unknown range falls back to the default (200)" \
                 || fail "an unknown range returned $c"

sec "*** ALERTS: A BREACH FIRES A WEBHOOK AND RECORDS AN EVENT ***"
# Stand up a local receiver, create a rule that always breaches (memory used > 0),
# and prove the evaluator fires: the webhook is POSTed and an event is recorded.
python3 - <<'PY' &
import http.server
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        n=int(self.headers.get('Content-Length',0)); body=self.rfile.read(n)
        open('/tmp/webhook.log','ab').write(body+b'\n')
        self.send_response(200); self.end_headers()
    def log_message(self,*a): pass
http.server.HTTPServer(('127.0.0.1',18777),H).serve_forever()
PY
recv=$!
sleep 0.5

rule=$(api -X POST "$base/api/v1/monitor/alerts/rules" -H 'Content-Type: application/json' \
  -d '{"name":"mem-always","metric":"mem","op":"gt","threshold":0,"for_sec":0,"notify_kind":"webhook","notify_target":{"webhook_url":"http://127.0.0.1:18777/hook"}}')
echo "$rule"
echo "$rule" | grep -q '"uid"' \
  && pass "a webhook alert rule was created (target sealed)" \
  || fail "the alert rule was not created"

# The rule list must NOT leak the webhook URL — targets are write-only.
api "$base/api/v1/monitor/alerts/rules" | grep -q '18777' \
  && fail "the notification target leaked in the rules list" \
  || pass "the notification target is write-only (not returned)"

# Wait for a few persist/evaluate ticks (2s cadence).
for i in $(seq 1 8); do [ -f /tmp/webhook.log ] && grep -q firing /tmp/webhook.log && break; sleep 1; done

grep -q '"state":"firing"' /tmp/webhook.log 2>/dev/null && grep -q 'mem-always' /tmp/webhook.log 2>/dev/null \
  && pass "the breach fired a webhook to the receiver" \
  || fail "no webhook firing arrived ($(cat /tmp/webhook.log 2>/dev/null | head -c200))"

api "$base/api/v1/monitor/alerts/events" | grep -q '"state":"firing"' \
  && pass "the firing was recorded as an alert event" \
  || fail "no alert event was recorded"

kill "$recv" 2>/dev/null || true

sec "cleanup"
pkill -f 'hpd' 2>/dev/null; pkill -f 'hp-broker' 2>/dev/null; true

if [ "$FAILED" = "0" ]; then echo "run-monitor.sh : PASS"; else echo "run-monitor.sh : FAIL"; fi
