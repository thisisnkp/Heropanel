#!/usr/bin/env bash
# Rotating data-key envelope, end to end against a real hpd: the panel starts in
# legacy (generation 0) mode, rotates to a wrapped data key (generation 1) which
# is persisted, and — after a full restart with the same master key — reloads the
# keyring from the database and still reports generation 1. That restart is the
# real proof: the wrapped key round-trips through the DB and unwraps under the
# master on boot. No broker needed — this is pure hpd + its datastore.
set -u
sec(){ echo; echo "======== $* ========"; }
fail=0
check(){ if printf '%s' "$2" | grep -q -- "$3"; then echo "  ok   $1"; else echo "  FAIL $1 (want: $3)"; echo "       got: $(printf '%s' "$2" | head -c 200)"; fail=1; fi }
base=http://127.0.0.1:18443
KEY=$(head -c 32 /dev/urandom | base64)

start_hpd(){
  HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18443 HP_LOG_FORMAT=text \
    HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp.db \
    HP_SECRET_KEY="$KEY" hpd >/tmp/hpd.log 2>&1 &
  HPD_PID=$!
  for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done
}

sec "start hpd (sqlite, master key set)"
install -m0755 /hp/hpd /usr/local/bin/
start_hpd

sec "auth"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
login(){ curl -s -c /tmp/c.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null; CSRF=$(awk '/hp_csrf/{print $7}' /tmp/c.txt); }
login
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }

sec "status starts in legacy mode (generation 0)"
S=$(api $base/api/v1/system/keyring); echo "$S"
check "available"        "$S" '"available":true'
check "legacy in use"    "$S" '"legacy_key_in_use":true'
check "generation 0"     "$S" '"active_generation":0'

sec "ROTATE the data key -> generation 1"
R=$(api -X POST $base/api/v1/system/keyring/rotate); echo "$R"
check "rotated to gen 1"     "$R" '"active_generation":1'
check "one key on the ring"  "$R" '"key_count":1'
check "no longer legacy"     "$R" '"legacy_key_in_use":false'

sec "the wrapped data key is persisted in the datastore"
if command -v sqlite3 >/dev/null 2>&1; then
  CNT=$(sqlite3 /tmp/hp.db 'SELECT count(*) FROM data_keys;' 2>/dev/null || echo "?")
  check "data_keys row present" "$CNT" '1'
else
  echo "  (sqlite3 CLI absent — persistence is proven by the restart+reload below)"
fi

sec "RESTART hpd with the same master key — keyring reloads from the DB"
kill "$HPD_PID" 2>/dev/null; wait "$HPD_PID" 2>/dev/null
start_hpd
check "keyring reloaded on boot" "$(cat /tmp/hpd.log)" 'data-key ring loaded'
login
S=$(api $base/api/v1/system/keyring); echo "$S"
check "still generation 1 after restart" "$S" '"active_generation":1'

sec "the rotation is on the audit log"
check "rotate audited" "$(api $base/api/v1/audit)" 'POST /api/v1/system/keyring/rotate'

sec "RESULT"
if [ "$fail" -eq 0 ]; then echo "run-keyring.sh : PASS"; else echo "run-keyring.sh : FAIL"; fi
exit "$fail"
