#!/usr/bin/env bash
# Verifies the embedded SPA is actually served by hpd and that the endpoints the
# UI depends on answer. It is not a browser test — the e2e image has no headless
# Chrome — but it proves the binary ships the built frontend and that every page
# the UI renders has a live endpoint behind it. The per-feature behaviour is
# already covered by the other suites; this is the "the UI is wired to the API"
# check.
set -u
sec(){ echo; echo "======== $* ========"; }
fail=0
check(){ if printf '%s' "$2" | grep -q -- "$3"; then echo "  ok   $1"; else echo "  FAIL $1 (want: $3)"; echo "       got: $(printf '%s' "$2" | head -c 200)"; fail=1; fi }

sec "start stack"
mkdir -p /run/mysqld && chown mysql:mysql /run/mysqld
[ -d /var/lib/mysql/mysql ] || mariadb-install-db --user=mysql --datadir=/var/lib/mysql >/dev/null 2>&1
mariadbd --user=mysql >/tmp/mariadb.log 2>&1 &
for i in $(seq 1 40); do mysqladmin ping >/dev/null 2>&1 && break; sleep 0.5; done
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel /srv/heropanel/sites
export HP_BROKER_TOKEN=tok
HP_BROKER_ALLOWED_UID=0 hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18443 \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp.db \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd.log 2>&1 &
for i in $(seq 1 60); do curl -sf http://127.0.0.1:18443/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "the built SPA is embedded and served"
INDEX=$(curl -s http://127.0.0.1:18443/)
check "index.html served"          "$INDEX" '<div id="root">'
check "it is the built bundle"     "$INDEX" '/assets/index-'
# A deep link must serve index.html too (client-side routing), not 404.
DEEP=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:18443/sites/anything)
check "deep links fall through to the SPA" "$DEEP" '200'
# The JS asset itself must load.
ASSET=$(printf '%s' "$INDEX" | grep -o '/assets/index-[^"]*\.js' | head -1)
echo "asset: $ASSET"
JSCODE=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:18443$ASSET")
check "the JS bundle loads" "$JSCODE" '200'

sec "bootstrap + login (the UI's first-run + auth path)"
STATUS=$(curl -s http://127.0.0.1:18443/api/v1/auth/status)
check "auth status offers bootstrap" "$STATUS" 'needs_bootstrap'
curl -s -X POST http://127.0.0.1:18443/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST http://127.0.0.1:18443/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }
check "me returns the principal" "$(api http://127.0.0.1:18443/api/v1/auth/me)" '"email":"a@h.io"'

sec "every page the UI renders has a live endpoint"
check "sidebar: sites"        "$(api http://127.0.0.1:18443/api/v1/sites)" '\['
check "sidebar: databases"    "$(api http://127.0.0.1:18443/api/v1/databases)" '\['
check "sidebar: dns zones"    "$(api http://127.0.0.1:18443/api/v1/dns/zones)" '\['
check "sidebar: ssl certs"    "$(api http://127.0.0.1:18443/api/v1/ssl/certificates)" '\['
check "sidebar: audit"        "$(api http://127.0.0.1:18443/api/v1/audit)" '\['
check "sidebar: users"        "$(api http://127.0.0.1:18443/api/v1/users)" '\['

sec "the capability set the UI gates on"
CAPS=$(api http://127.0.0.1:18443/api/v1/capabilities)
echo "$CAPS" | head -c 300; echo
check "capabilities returned"  "$CAPS" '"capabilities"'
check "sites capability present" "$CAPS" 'site.manage'
check "database capability present" "$CAPS" 'database.manage'
MODS=$(api http://127.0.0.1:18443/api/v1/modules)
check "modules registered"     "$MODS" '"slug":"sites"'
check "modules show running"   "$MODS" '"state":"running"'

sec "the OpenAPI spec + the /api/docs viewer are served"
OA=$(api http://127.0.0.1:18443/api/v1/openapi.json)
check "openapi 3.1 document"     "$OA" '"openapi": "3.1.0"'
check "spec documents /sites"    "$OA" '/api/v1/sites'
DOCS=$(curl -s http://127.0.0.1:18443/api/docs)
check "docs viewer html"         "$DOCS" '<title>HeroPanel API reference'
check "docs viewer loads its js" "$DOCS" '/api/docs.js'
DJS=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:18443/api/docs.js)
check "docs js asset served"     "$DJS" '200'
DCSS=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:18443/api/docs.css)
check "docs css asset served"    "$DCSS" '200'

sec "a site detail page's facets all answer"
api -X POST http://127.0.0.1:18443/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"UI","primary_domain":"ui.test","type":"php"}' >/dev/null
U=$(api http://127.0.0.1:18443/api/v1/sites | grep -o '"uid":"[^"]*"' | head -1 | cut -d'"' -f4)
echo "site: $U"
check "overview (site read)"   "$(api http://127.0.0.1:18443/api/v1/sites/$U)" '"primary_domain":"ui.test"'
check "domains tab"            "$(api http://127.0.0.1:18443/api/v1/sites/$U/domains)" '\['
check "php tab"                "$(api http://127.0.0.1:18443/api/v1/sites/$U/php)" '"allowed_ini"'
check "logs tab"               "$(api "http://127.0.0.1:18443/api/v1/sites/$U/logs?kind=access")" '"kind":"access"'
check "limits (advanced tab)"  "$(api http://127.0.0.1:18443/api/v1/sites/$U/limits)" 'cpu_quota_pct'
check "git tab (404 until set)" "$(api -o /dev/null -w '%{http_code}' http://127.0.0.1:18443/api/v1/sites/$U/git)" '404'

sec "RESULT"
if [ "$fail" -eq 0 ]; then echo "run-ui.sh : PASS"; else echo "run-ui.sh : FAIL"; fi
exit "$fail"
