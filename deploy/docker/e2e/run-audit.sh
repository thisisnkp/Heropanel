#!/usr/bin/env bash
# Real end-to-end for the tamper-evident audit chain — and the first suite that
# runs npd's own control-plane store on **real MariaDB** rather than SQLite.
#
# That matters twice over:
#   1. The audit chain hashes each row over the bytes the column stores. SQLite's
#      TEXT hands back what it was given; MariaDB's DATETIME(6) pads a timestamp
#      to six fractional digits and its JSON column has its own round-trip rules.
#      A chain that verifies on SQLite proves nothing about the engine NexPanel
#      actually ships on.
#   2. Every other e2e suite runs npd on sqlite, so the mysql half of the
#      migrations had never once been executed. This runs them.
set -u
sec(){ echo; echo "======== $* ========"; }
fail=0
check(){ # check <label> <haystack> <needle>
  if printf '%s' "$2" | grep -q -- "$3"; then echo "  ok   $1"; else echo "  FAIL $1 (want: $3)"; echo "       got: $2"; fail=1; fi
}

sec "start MariaDB"
mkdir -p /run/mysqld && chown mysql:mysql /run/mysqld
[ -d /var/lib/mysql/mysql ] || mariadb-install-db --user=mysql --datadir=/var/lib/mysql >/dev/null 2>&1
mariadbd --user=mysql >/tmp/mariadb.log 2>&1 &
for i in $(seq 1 40); do mysqladmin ping >/dev/null 2>&1 && break; sleep 0.5; done
echo "mysql: $(mysqladmin ping 2>&1)"
echo "version: $(mysql --protocol=socket -N -B -e 'SELECT VERSION();' 2>&1)"

sec "create npd's own control-plane schema"
mysql --protocol=socket -e "CREATE DATABASE IF NOT EXISTS nexpanel CHARACTER SET utf8mb4;" 2>&1
mysql --protocol=socket -e "CREATE USER IF NOT EXISTS 'npd'@'127.0.0.1' IDENTIFIED BY 'npdpass';" 2>&1
mysql --protocol=socket -e "GRANT ALL ON nexpanel.* TO 'npd'@'127.0.0.1'; FLUSH PRIVILEGES;" 2>&1

sec "start np-broker + npd  (NP_DATABASE_DRIVER=mariadb)"
install -m0755 /np/npd /np/np-broker /usr/local/bin/
mkdir -p /run/nexpanel /srv/nexpanel/sites
export NP_BROKER_TOKEN=tok
NP_LOG_FORMAT=text NP_BROKER_ALLOWED_UID=0 np-broker --serve --socket /run/nexpanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/nexpanel/broker.sock ] && break; sleep 0.2; done

# parseTime stays off: the audit repository reads created_at back as the literal
# text MariaDB renders, which is exactly what the row hash covers.
NP_SERVER_HOST=127.0.0.1 NP_SERVER_PORT=18443 NP_LOG_FORMAT=text \
  NP_DATABASE_DRIVER=mariadb \
  NP_DATABASE_DSN='npd:npdpass@tcp(127.0.0.1:3306)/nexpanel?charset=utf8mb4' \
  NP_BROKER_SOCKET=/run/nexpanel/broker.sock npd >/tmp/npd.log 2>&1 &
for i in $(seq 1 60); do curl -sf http://127.0.0.1:18443/healthz >/dev/null 2>&1 && break; sleep 0.25; done
echo "readyz: $(curl -s http://127.0.0.1:18443/readyz)"

sec "the mysql migrations actually ran"
APPLIED=$(mysql --protocol=socket -N -B -e "SELECT COUNT(*) FROM nexpanel.schema_migrations;" 2>&1)
echo "schema_migrations rows: $APPLIED"
check "all 40 mysql migrations applied" "$APPLIED" "40"
TABLES=$(mysql --protocol=socket -N -B -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='nexpanel';" 2>&1)
echo "tables in nexpanel: $TABLES"

sec "auth (bootstrap + login + CSRF)"
curl -s -X POST http://127.0.0.1:18443/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST http://127.0.0.1:18443/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/np_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }

sec "the bootstrap + login are themselves in the chain"
LOG=$(api http://127.0.0.1:18443/api/v1/audit)
echo "$LOG" | head -c 600; echo
check "bootstrap recorded"      "$LOG" '"action":"POST /api/v1/auth/bootstrap"'
check "login recorded"          "$LOG" '"action":"POST /api/v1/auth/login"'
check "login names its actor"   "$LOG" '"actor_kind":"user"'
check "email kept as detail"    "$LOG" 'a@h.io'
check "password never recorded" "$(printf '%s' "$LOG" | grep -c supersecret1)" '^0$'

sec "a real mutation lands in the chain (create a database)"
api -X POST http://127.0.0.1:18443/api/v1/databases -H 'Content-Type: application/json' \
  -d '{"name":"auditdemo"}' >/dev/null
LOG=$(api 'http://127.0.0.1:18443/api/v1/audit?resource_type=databases')
check "database create recorded" "$LOG" '"action":"POST /api/v1/databases"'
check "resource uid attached"    "$LOG" '"resource_type":"databases"'
check "name kept as detail"      "$LOG" 'auditdemo'

sec "a refused request is recorded as denied"
# An unauthenticated attempt to create a database. This is the entry that has to
# exist: someone tried to reach a privileged endpoint and was turned away, and
# a log that only holds successes would never show it.
#
# (Not a CSRF rejection: CSRF is opt-in and off here, so a session cookie with no
# X-CSRF-Token is simply allowed — as SameSite=Strict already covers it.)
CODE=$(curl -s -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:18443/api/v1/databases \
  -H 'Content-Type: application/json' -d '{"name":"nope"}')
echo "unauthenticated POST /databases -> $CODE"
check "refused with 401"        "$CODE" '401'
LOG=$(api http://127.0.0.1:18443/api/v1/audit)
check "denied attempt recorded" "$LOG" '"outcome":"denied"'
check "denied attempt is anonymous" "$LOG" '"actor_kind":"anonymous"'

sec "a read is NOT recorded, but an export IS"
BEFORE=$(mysql --protocol=socket -N -B -e "SELECT COUNT(*) FROM nexpanel.audit_log;")
api http://127.0.0.1:18443/api/v1/databases >/dev/null
AFTER_GET=$(mysql --protocol=socket -N -B -e "SELECT COUNT(*) FROM nexpanel.audit_log;")
UID_=$(api http://127.0.0.1:18443/api/v1/databases | grep -o '"uid":"[^"]*"' | head -1 | cut -d'"' -f4)
api "http://127.0.0.1:18443/api/v1/databases/$UID_/export" >/dev/null
AFTER_EXPORT=$(mysql --protocol=socket -N -B -e "SELECT COUNT(*) FROM nexpanel.audit_log;")
echo "audit rows: before=$BEFORE afterGET=$AFTER_GET afterEXPORT=$AFTER_EXPORT"
check "plain GET added no row" "$((AFTER_GET - BEFORE))" '^0$'
check "export forced a row"    "$((AFTER_EXPORT - AFTER_GET))" '^1$'
check "export recorded"        "$(api 'http://127.0.0.1:18443/api/v1/audit?action=GET%20/api/v1/databases/%7Buid%7D/export')" '"action":"GET /api/v1/databases/{uid}/export"'

sec "the chain verifies against real MariaDB"
# This is the assertion the whole suite exists for. On SQLite it would pass even
# if the timestamp/JSON round-trip were wrong.
V=$(api http://127.0.0.1:18443/api/v1/audit/verify); echo "$V"
check "chain intact on MariaDB" "$V" '"intact":true'

sec "DATETIME(6) round-trips byte-for-byte"
TS=$(mysql --protocol=socket -N -B -e "SELECT created_at FROM nexpanel.audit_log ORDER BY id LIMIT 1;")
echo "created_at as MariaDB renders it: $TS"
check "six fractional digits" "$TS" '\.[0-9]\{6\}$'

sec "tamper a row directly in SQL -> the chain reports it"
mysql --protocol=socket -e \
  "UPDATE nexpanel.audit_log SET action='GET /api/v1/databases' WHERE action='POST /api/v1/databases';" 2>&1
V=$(api http://127.0.0.1:18443/api/v1/audit/verify); echo "$V"
check "tampering detected" "$V" '"intact":false'
check "verify still answers 200 (not a 500)" "$(curl -s -o /dev/null -w '%{http_code}' -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" http://127.0.0.1:18443/api/v1/audit/verify)" '200'

sec "audit.read is enforced"
# A principal without the scope must not read the log. Bootstrap made an admin
# (superuser), so assert the negative with no session at all.
CODE=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:18443/api/v1/audit)
check "anonymous cannot read the audit log" "$CODE" '401'

sec "RESULT"
if [ "$fail" -eq 0 ]; then echo "run-audit.sh : PASS"; else echo "run-audit.sh : FAIL"; fi
exit "$fail"
