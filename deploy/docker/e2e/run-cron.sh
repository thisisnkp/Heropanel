#!/usr/bin/env bash
# Phase 6, Scheduler: site-scoped cron jobs as real systemd timer+service pairs.
#
# What this proves that a unit test cannot: the whole path API -> broker ->
# unit files on disk -> the command actually EXECUTING as the site's
# unprivileged user (never root), its output captured and readable back through
# the API. Timer *firing on schedule* is systemd's own behaviour, pinned by the
# unit-rendering tests; the shim runs the oneshot on demand, which is what
# `run now` uses.
set -u
sec(){ echo; echo "======== $* ========"; }
pass(){ echo "PASS: $*"; }
fail(){ echo "FAIL: $*"; FAILED=1; }
FAILED=0
base=http://127.0.0.1:18466

sec "start OpenLiteSpeed (site provisioning applies a vhost)"
/usr/local/lsws/bin/lswsctrl start 2>&1
sleep 1

sec "start hp-broker (root) + hpd (sqlite)"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel /srv/heropanel/sites
export HP_BROKER_TOKEN=tok
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 HP_BROKER_PANEL_USER=root \
  hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker-cron.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18466 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp-cron.db \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd-cron.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "auth (bootstrap + login + CSRF)"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/cc.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/cc.txt)
api(){ curl -s -b /tmp/cc.txt -H "X-CSRF-Token: $CSRF" "$@"; }
code(){ curl -s -o /dev/null -w '%{http_code}' -b /tmp/cc.txt -H "X-CSRF-Token: $CSRF" "$@"; }

sec "create a site to schedule jobs for"
site=$(api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"CronSite","primary_domain":"cron.test","type":"static"}')
uid=$(echo "$site" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["uid"])')
[ -n "$uid" ] && pass "site created ($uid)" || fail "site create failed: $site"

sec "*** SCHEDULE A JOB (real timer + oneshot units on disk) ***"
job=$(api -X POST "$base/api/v1/sites/$uid/cron" -H 'Content-Type: application/json' \
  -d '{"name":"who-runs-me","command":"id -un","schedule":"*-*-* 03:00:00"}')
echo "$job"
jid=$(echo "$job" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["uid"])')
[ -n "$jid" ] && pass "job scheduled ($jid)" || fail "job create failed"

svc_unit="/etc/systemd/system/heropanel-cron-$jid.service"
timer_unit="/etc/systemd/system/heropanel-cron-$jid.timer"
[ -f "$svc_unit" ] && [ -f "$timer_unit" ] \
  && pass "the timer and service units exist on disk" \
  || fail "unit files missing"
grep -q 'User=hps1' "$svc_unit" && grep -q 'Type=oneshot' "$svc_unit" \
  && pass "the service runs as the site user, oneshot" \
  || fail "service unit not hardened as expected: $(cat "$svc_unit")"
grep -q 'OnCalendar=\*-\*-\* 03:00:00' "$timer_unit" && grep -q 'Persistent=true' "$timer_unit" \
  && pass "the timer carries the schedule and Persistent=true" \
  || fail "timer unit wrong: $(cat "$timer_unit")"
grep -q '"capability":"cron.apply","outcome":"success"' /tmp/broker-cron.log \
  && pass "cron.apply is on the broker's audit chain" \
  || fail "cron.apply missing from the broker log"

sec "*** RUN NOW: THE COMMAND EXECUTES AS THE SITE USER ***"
c=$(code -X POST "$base/api/v1/sites/$uid/cron/$jid/run")
[ "$c" = "200" ] && pass "run-now returned 200" || fail "run-now returned $c"
sleep 1
logs=$(api "$base/api/v1/sites/$uid/cron/$jid/logs")
echo "$logs"
# The job was `id -un` — its output IS the user it ran as. It must be the site
# user, never root: this is the module's whole safety claim, observed live.
echo "$logs" | grep -q 'hps1' \
  && pass "THE JOB RAN AS THE SITE USER (hps1), NOT ROOT" \
  || fail "the job did not run as the site user: $logs"

sec "A BAD SCHEDULE IS REFUSED"
c=$(code -X POST "$base/api/v1/sites/$uid/cron" -H 'Content-Type: application/json' \
  -d '{"name":"evil","command":"true","schedule":"daily; rm -rf /"}')
[ "$c" = "400" ] && pass "a shell-shaped schedule is refused (400)" \
                 || fail "a bad schedule returned $c"

sec "DISABLE REMOVES THE TIMER, THE DEFINITION SURVIVES"
c=$(code -X PUT "$base/api/v1/sites/$uid/cron/$jid" -H 'Content-Type: application/json' -d '{"enabled":false}')
[ "$c" = "200" ] || fail "disable returned $c"
[ ! -f "$timer_unit" ] && pass "the timer unit is gone after disable" \
                       || fail "the timer survived a disable"
api "$base/api/v1/sites/$uid/cron" | grep -q 'who-runs-me' \
  && pass "the job definition is still listed" || fail "disable deleted the definition"

sec "DELETE REMOVES EVERYTHING"
c=$(code -X DELETE "$base/api/v1/sites/$uid/cron/$jid")
[ "$c" = "200" ] || fail "delete returned $c"
api "$base/api/v1/sites/$uid/cron" | grep -q 'who-runs-me' \
  && fail "the job survived deletion" || pass "the job is gone"

sec "cleanup"
pkill -f 'hpd' 2>/dev/null; pkill -f 'hp-broker' 2>/dev/null; true

if [ "$FAILED" = "0" ]; then echo "run-cron.sh : PASS"; else echo "run-cron.sh : FAIL"; fi
