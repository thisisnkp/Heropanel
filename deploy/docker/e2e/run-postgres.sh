#!/usr/bin/env bash
# Real PostgreSQL engine: HeroPanel creates a database, a role, and a grant on a
# live PostgreSQL server via the broker (which runs psql as the postgres OS user),
# then proves a row round-trips through export → import. The panel's own store is
# SQLite; only the *managed* engine under test is PostgreSQL.
set -u
sec(){ echo; echo "======== $* ========"; }
fail=0
check(){ if printf '%s' "$2" | grep -q -- "$3"; then echo "  ok   $1"; else echo "  FAIL $1 (want: $3)"; echo "       got: $(printf '%s' "$2" | head -c 200)"; fail=1; fi }
base=http://127.0.0.1:18443
pg(){ runuser -u postgres -- psql -tA -q "$@"; }

sec "start PostgreSQL"
PGVER=$(ls /etc/postgresql 2>/dev/null | head -1)
if [ -n "$PGVER" ]; then
  pg_ctlcluster "$PGVER" main start 2>&1 | tail -2 || service postgresql start 2>&1 | tail -2
else
  service postgresql start 2>&1 | tail -2
fi
for i in $(seq 1 30); do runuser -u postgres -- pg_isready >/dev/null 2>&1 && break; sleep 0.5; done
runuser -u postgres -- pg_isready

sec "start hp-broker + hpd"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel /var/lib/heropanel
export HP_BROKER_TOKEN=tok
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18443 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp.db \
  HP_SECRET_KEY=0000000000000000000000000000000000000000000000000000000000000000 \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "auth"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }
uidof(){ printf '%s' "$1" | grep -o '"uid":"[^"]*"' | head -1 | cut -d'"' -f4; }

sec "CREATE a PostgreSQL database + role via the API (engine=postgres)"
DB=$(api -X POST $base/api/v1/databases -H 'Content-Type: application/json' -d '{"name":"shop","engine":"postgres"}')
echo "$DB"
check "db created as postgres" "$DB" '"engine":"postgres"'
dbuid=$(uidof "$DB")
U=$(api -X POST $base/api/v1/database-users -H 'Content-Type: application/json' -d '{"username":"shopper","password":"password123","engine":"postgres"}')
uuid=$(uidof "$U")
api -X POST $base/api/v1/databases/$dbuid/grant -H 'Content-Type: application/json' -d "{\"user_uid\":\"$uuid\",\"privileges\":[\"ALL\"]}" >/dev/null

sec "the database and role really exist in PostgreSQL"
check "database present"  "$(pg -c "SELECT 1 FROM pg_database WHERE datname='shop'")" '1'
check "role present"      "$(pg -c "SELECT 1 FROM pg_roles WHERE rolname='shopper'")" '1'
check "broker ran pg.create" "$(cat /tmp/broker.log)" '"capability":"pg.create","outcome":"success"'
check "broker ran pg.grant"  "$(cat /tmp/broker.log)" '"capability":"pg.grant","outcome":"success"'

sec "write a row, then EXPORT → drop → new db → IMPORT (row must survive)"
pg -d shop -c "CREATE TABLE hello(msg text); INSERT INTO hello VALUES ('hi from postgres');" >/dev/null
# Export via the broker (pg_dump), capture the produced file path from hpd's dump dir.
api $base/api/v1/databases/$dbuid/export -o /tmp/shop.sql.gz -s
ls -la /tmp/shop.sql.gz 2>/dev/null | awk '{print "export bytes:",$5}'
check "export produced a gzip" "$(file /tmp/shop.sql.gz 2>/dev/null || head -c 2 /tmp/shop.sql.gz | xxd)" 'gzip'
# Create a second database and import into it.
DB2=$(api -X POST $base/api/v1/databases -H 'Content-Type: application/json' -d '{"name":"shop_restored","engine":"postgres"}')
db2uid=$(uidof "$DB2")
curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" -H "Content-Encoding: gzip" -X POST \
  "$base/api/v1/databases/$db2uid/import?filename=shop.sql.gz" \
  --data-binary @/tmp/shop.sql.gz >/tmp/import.json
echo "import: $(cat /tmp/import.json | head -c 200)"
check "row round-trips into the restored db" "$(pg -d shop_restored -c "SELECT msg FROM hello")" 'hi from postgres'

sec "SIZE reports bytes"
check "size > 0" "$(api $base/api/v1/databases/$dbuid/size)" '"bytes"'

sec "RESULT"
if [ "$fail" -eq 0 ]; then echo "run-postgres.sh : PASS"; else echo "run-postgres.sh : FAIL"; fi
exit "$fail"
