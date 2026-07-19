#!/usr/bin/env bash
# Real end-to-end for the Phase-1 site lifecycle: suspend, resume, clone, logs.
#
# The assertion that matters most here is the one about *another* site. A
# suspended site keeps its vhost on purpose: OpenLiteSpeed answers a Host it does
# not recognize with its first vhost, so dropping the suspended site from the
# config would quietly start serving one customer's content on another customer's
# domain. That is a claim about real OLS routing behaviour, and it is only worth
# anything if a real OLS is the one answering.
set -u
sec(){ echo; echo "======== $* ========"; }
fail=0
check(){ if printf '%s' "$2" | grep -q -- "$3"; then echo "  ok   $1"; else echo "  FAIL $1 (want: $3)"; echo "       got: $2"; fail=1; fi }

sec "start MariaDB + OpenLiteSpeed"
mkdir -p /run/mysqld && chown mysql:mysql /run/mysqld
[ -d /var/lib/mysql/mysql ] || mariadb-install-db --user=mysql --datadir=/var/lib/mysql >/dev/null 2>&1
mariadbd --user=mysql >/tmp/mariadb.log 2>&1 &
for i in $(seq 1 40); do mysqladmin ping >/dev/null 2>&1 && break; sleep 0.5; done
/usr/local/lsws/bin/lswsctrl start 2>&1; sleep 1

sec "start hp-broker + hpd"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel /srv/heropanel/sites
export HP_BROKER_TOKEN=tok
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18443 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp.db \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd.log 2>&1 &
for i in $(seq 1 60); do curl -sf http://127.0.0.1:18443/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "auth"
curl -s -X POST http://127.0.0.1:18443/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST http://127.0.0.1:18443/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }
uidof(){ printf '%s' "$1" | grep -o '"uid":"[^"]*"' | head -1 | cut -d'"' -f4; }
get(){ curl -s -H "Host: $1" -o /tmp/body -w '%{http_code}' http://127.0.0.1/; }

# The site tree is 0750 owned by the site's user, and OpenLiteSpeed serves as
# `nobody`. In production OLS is given access to the tree by the installer; in
# this container we widen it by hand, exactly as run.sh does. Not part of what
# this suite is testing — just the price of a real web server reading real files.
serve_perms(){
  chmod o+x  "/srv/heropanel/sites/$1"
  chmod o+rx "/srv/heropanel/sites/$1/public"
  chmod o+rwx "/srv/heropanel/sites/$1/logs"
  [ -f "/srv/heropanel/sites/$1/public/index.html" ] && chmod o+r "/srv/heropanel/sites/$1/public/index.html"
  return 0
}

sec "create two sites (the second one proves the routing claim)"
S1=$(api -X POST http://127.0.0.1:18443/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"Acme","primary_domain":"acme.test","type":"static"}')
U1=$(uidof "$S1"); echo "site 1: $U1"
S2=$(api -X POST http://127.0.0.1:18443/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"Other","primary_domain":"other.test","type":"static"}')
U2=$(uidof "$S2"); echo "site 2: $U2"

# Distinct content, so "which site answered" is unambiguous.
echo '<h1>ACME CONTENT</h1>'  > /srv/heropanel/sites/1/public/index.html
echo '<h1>OTHER CONTENT</h1>' > /srv/heropanel/sites/2/public/index.html
serve_perms 1; serve_perms 2
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1

get acme.test  >/dev/null; check "acme.test serves acme"   "$(cat /tmp/body)" 'ACME CONTENT'
get other.test >/dev/null; check "other.test serves other" "$(cat /tmp/body)" 'OTHER CONTENT'

sec "SUSPEND"
R=$(api -X POST "http://127.0.0.1:18443/api/v1/sites/$U1/suspend"); echo "$R" | head -c 200; echo
check "status is suspended" "$R" '"status":"suspended"'

CODE=$(get acme.test); BODY=$(cat /tmp/body)
echo "acme.test -> $CODE"
check "suspended site returns 503"       "$CODE" '^503$'
check "suspended site serves no content" "$(printf '%s' "$BODY" | grep -c 'ACME CONTENT')" '^0$'
# The one that would be a real incident: acme.test must NOT be answered by the
# other customer's site just because it was suspended.
check "suspended domain did NOT fall through to the other site" "$(printf '%s' "$BODY" | grep -c 'OTHER CONTENT')" '^0$'
get other.test >/dev/null; check "the other site is untouched" "$(cat /tmp/body)" 'OTHER CONTENT'

sec "RESUME"
R=$(api -X POST "http://127.0.0.1:18443/api/v1/sites/$U1/resume")
check "status is active" "$R" '"status":"active"'
CODE=$(get acme.test); echo "acme.test -> $CODE"
check "resumed site serves again" "$(cat /tmp/body)" 'ACME CONTENT'
check "resumed site returns 200"  "$CODE" '^200$'

sec "suspend is idempotent and guarded"
api -X POST "http://127.0.0.1:18443/api/v1/sites/$U1/suspend" >/dev/null
R=$(api -X POST "http://127.0.0.1:18443/api/v1/sites/$U1/suspend")
check "second suspend is a no-op, not an error" "$R" '"status":"suspended"'
CODE=$(curl -s -o /dev/null -w '%{http_code}' -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" \
  -X POST "http://127.0.0.1:18443/api/v1/sites/$U1/resume")
check "resume answers 200" "$CODE" '^200$'

sec "LOGS  (0750, owned by the site user — hpd cannot read them itself)"
echo "site log dir: $(stat -c '%a %U:%G' /srv/heropanel/sites/1/logs)"
curl -s -o /dev/null -H 'Host: acme.test' http://127.0.0.1/    # generate a hit
curl -s -o /dev/null -H 'Host: acme.test' http://127.0.0.1/nope
sleep 1
L=$(api "http://127.0.0.1:18443/api/v1/sites/$U1/logs?kind=access&lines=50")
echo "$L" | head -c 300; echo
check "access log is readable through the broker" "$L" '"exists":true'
check "the log has the request in it"             "$L" 'GET /'
L=$(api "http://127.0.0.1:18443/api/v1/sites/$U1/logs?kind=error&lines=10")
check "error log kind works" "$L" '"kind":"error"'
check "an unknown log kind is refused" \
  "$(api "http://127.0.0.1:18443/api/v1/sites/$U1/logs?kind=../../../etc/passwd")" 'bad_log_kind'

sec "CLONE"
C=$(api -X POST "http://127.0.0.1:18443/api/v1/sites/$U2/clone" -H 'Content-Type: application/json' \
  -d '{"name":"Other Staging","primary_domain":"staging.test"}')
echo "$C" | head -c 250; echo
U3=$(uidof "$C"); echo "clone: $U3"
check "clone is active" "$C" '"status":"active"'

check "clone got its own Linux user"      "$(printf '%s' "$C" | grep -o '"system_user":"[^"]*"')" 'hps3'
check "clone got its own document root"   "$(printf '%s' "$C" | grep -o '"document_root":"[^"]*"')" '/srv/heropanel/sites/3/public'
serve_perms 3; /usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1
get staging.test >/dev/null; check "clone content was copied" "$(cat /tmp/body)" 'OTHER CONTENT'
check "clone serves 200"                  "$(get staging.test)" '^200$'
get other.test >/dev/null; check "source site still serves" "$(cat /tmp/body)" 'OTHER CONTENT'

# The isolation assertion: a clone whose files are still owned by the source's
# user means two sites can read and write each other's data.
OWNER=$(stat -c '%U:%G' /srv/heropanel/sites/3/public/index.html)
echo "cloned file owner: $OWNER"
check "cloned content is owned by the clone's user, not the source's" "$OWNER" '^hps3:hps3$'

sec "clone refuses a site onto itself / bad input"
check "invalid domain refused" \
  "$(api -X POST "http://127.0.0.1:18443/api/v1/sites/$U2/clone" -H 'Content-Type: application/json' -d '{"name":"x","primary_domain":"not a domain"}')" \
  'error'

sec "the whole lifecycle is in the audit chain"
A=$(api 'http://127.0.0.1:18443/api/v1/audit')
check "suspend audited" "$A" '"action":"POST /api/v1/sites/{uid}/suspend"'
check "resume audited"  "$A" '"action":"POST /api/v1/sites/{uid}/resume"'
check "clone audited"   "$A" '"action":"POST /api/v1/sites/{uid}/clone"'
check "chain intact"    "$(api http://127.0.0.1:18443/api/v1/audit/verify)" '"intact":true'

sec "broker audit shows the privileged steps"
grep -oE '"capability":"site\.(read_log|copy_tree)","outcome":"[a-z]+"' /tmp/broker.log | sort -u
check "site.read_log succeeded" "$(cat /tmp/broker.log)" '"capability":"site.read_log","outcome":"success"'
check "site.copy_tree succeeded" "$(cat /tmp/broker.log)" '"capability":"site.copy_tree","outcome":"success"'

sec "RESULT"
if [ "$fail" -eq 0 ]; then echo "run-lifecycle.sh : PASS"; else echo "run-lifecycle.sh : FAIL"; fi
exit "$fail"
