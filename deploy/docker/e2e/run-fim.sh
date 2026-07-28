#!/usr/bin/env bash
# File-integrity monitoring (AIDE): the panel builds a baseline of its
# security-critical files, then a later check must DETECT a tampered file — the
# whole point of FIM. Proven behaviourally against real AIDE: init a baseline,
# modify a watched file, and see the check report the change (and report clean
# before it).
set -u
sec(){ echo; echo "======== $* ========"; }
pass(){ echo "PASS: $*"; }
fail(){ echo "FAIL: $*"; FAILED=1; }
FAILED=0
base=http://127.0.0.1:18455

sec "AIDE present"
[ -x /usr/bin/aide ] && pass "AIDE is installed" || fail "no aide binary"
# Give the watcher a file it owns so a change is unambiguous, and make sure at
# least one watched tree exists.
ssh-keygen -A >/dev/null 2>&1
mkdir -p /etc/heropanel
echo "baseline content v1" > /etc/heropanel/fim-watched.conf

sec "start hp-broker + hpd"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel
export HP_BROKER_TOKEN=tok
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 HP_BROKER_PANEL_USER=root \
  hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker-fim.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18455 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp-fim.db \
  HP_SERVER_WRITE_TIMEOUT=600s \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd-fim.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "auth"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/cf.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/cf.txt)
api(){ curl -s -b /tmp/cf.txt -H "X-CSRF-Token: $CSRF" "$@"; }

sec "no baseline yet"
api $base/api/v1/security/fim | grep -q '"baseline":false' && pass "status reports no baseline yet" || fail "unexpected initial status"
# A check before a baseline must be refused, not silently pass.
code=$(curl -s -o /dev/null -w '%{http_code}' -b /tmp/cf.txt -H "X-CSRF-Token: $CSRF" -X POST $base/api/v1/security/fim/check)
[ "$code" = "409" ] && pass "a check without a baseline is refused (409)" || fail "check without baseline returned $code"

sec "*** BUILD THE BASELINE ***"
init=$(api -X POST $base/api/v1/security/fim/init -H 'Content-Type: application/json')
echo "$init"
echo "$init" | grep -q '"initialised":true' && pass "the FIM baseline was built" || fail "init failed: $init"
api $base/api/v1/security/fim | grep -q '"baseline":true' && pass "status now reports a baseline" || fail "baseline not present after init"

sec "*** CLEAN CHECK: nothing changed yet ***"
chk=$(api -X POST $base/api/v1/security/fim/check -H 'Content-Type: application/json')
echo "$chk"
echo "$chk" | grep -q '"changed":false' && pass "the check is CLEAN immediately after the baseline" \
  || fail "the check reported changes on a clean tree: $chk"

sec "*** TAMPER a watched file, then check MUST DETECT it ***"
echo "tampered content v2 — extra line" >> /etc/heropanel/fim-watched.conf
chk=$(api -X POST $base/api/v1/security/fim/check -H 'Content-Type: application/json')
echo "$chk" | python3 -m json.tool 2>/dev/null | head -8
echo "$chk" | grep -q '"changed":true' \
  && pass "THE FIM CHECK DETECTED THE TAMPERING (changed=true)" || fail "FIM missed a changed file: $chk"
ch=$(echo "$chk" | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; print(d["added"]+d["removed"]+d["changed_count"])' 2>/dev/null)
[ "$ch" -ge 1 ] 2>/dev/null && pass "the report counts at least one added/changed entry ($ch)" || fail "no changed entries counted"

sec "*** HOST-WIDE SCOPE: baseline the wider host, then detect an /etc change ***"
# host scope extends the watch to all of /etc (and the binary/library trees).
echo "host canary v1" > /etc/hp-host-canary.conf
hinit=$(api -X POST $base/api/v1/security/fim/init -H 'Content-Type: application/json' -d '{"scope":"host"}')
echo "$hinit" | grep -q '"scope":"host"' && pass "a host-wide baseline was built (scope=host)" || fail "host init failed: $hinit"
api $base/api/v1/security/fim | grep -q '"scope":"host"' && pass "status reports the host-wide scope" || fail "scope not recorded"
hchk=$(api -X POST $base/api/v1/security/fim/check -H 'Content-Type: application/json')
echo "$hchk" | grep -q '"changed":false' && pass "the host-wide check is CLEAN right after its baseline" \
  || fail "host check dirty on a fresh baseline: $(echo "$hchk" | head -c 200)"
echo "$hchk" | grep -q '"scope":"host"' && pass "the check ran at the host scope it was built with" || fail "check scope mismatch"
echo "host canary v2 — tampered" >> /etc/hp-host-canary.conf
hchk=$(api -X POST $base/api/v1/security/fim/check -H 'Content-Type: application/json')
echo "$hchk" | grep -q '"changed":true' \
  && pass "THE HOST-WIDE CHECK DETECTED A CHANGE UNDER /etc (changed=true)" || fail "host FIM missed an /etc change: $(echo "$hchk" | head -c 200)"

sec "*** HOST AUDIT SCANNER: lynis produces a hardening index ***"
ly=$(api --max-time 300 -X POST $base/api/v1/security/audit/lynis -H 'Content-Type: application/json')
echo "$ly" | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; print("tool=%s warnings=%s suggestions=%s index=%s"%(d["tool"],d["warnings"],d.get("suggestions"),d.get("hardening_index")))' 2>/dev/null
echo "$ly" | grep -q '"tool":"lynis"' && pass "the real lynis audit ran through the panel" || fail "lynis failed: $(echo "$ly" | head -c 200)"
echo "$ly" | grep -q '"hardening_index"' && pass "lynis reported a hardening index (parsed)" || fail "no hardening index parsed"
[ -n "$(echo "$ly" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["report"][:20])' 2>/dev/null)" ] \
  && pass "lynis returned its report" || fail "empty lynis report"

sec "*** HOST AUDIT SCANNER: rkhunter rootkit scan runs ***"
rk=$(api --max-time 300 -X POST $base/api/v1/security/audit/rkhunter -H 'Content-Type: application/json')
echo "$rk" | grep -q '"tool":"rkhunter"' && pass "the real rkhunter rootkit scan ran through the panel" || fail "rkhunter failed: $(echo "$rk" | head -c 200)"
echo "$rk" | python3 -c 'import json,sys; print("rkhunter warnings:",json.load(sys.stdin)["data"]["warnings"])' 2>/dev/null

sec "an unknown audit tool is refused"
code=$(curl -s -o /dev/null -w '%{http_code}' -b /tmp/cf.txt -H "X-CSRF-Token: $CSRF" -X POST $base/api/v1/security/audit/notatool)
[ "$code" = "400" ] && pass "an unknown audit tool is refused (400)" || fail "unknown tool returned $code"

sec "audit chain"
for cap in fim.init fim.check fim.status audit.scan; do
  grep -q "\"capability\":\"$cap\",\"outcome\":\"success\"" /tmp/broker-fim.log \
    && pass "$cap is on the broker's audit chain" || fail "$cap missing from the audit chain"
done

sec "cleanup"
pkill -f 'hpd' 2>/dev/null; pkill -f 'hp-broker' 2>/dev/null; true

if [ "$FAILED" = "0" ]; then echo "run-fim.sh : PASS"; else echo "run-fim.sh : FAIL"; fi
exit "$FAILED"
