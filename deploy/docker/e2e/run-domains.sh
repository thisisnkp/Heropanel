#!/usr/bin/env bash
# Real domain management: an alias serves the same site, a redirect domain 301s
# to its target, and force-HTTPS redirects plain HTTP — all through real
# OpenLiteSpeed, driven entirely by the NexPanel API.
set -u
sec(){ echo; echo "======== $* ========"; }
base=http://127.0.0.1:18443
site=/srv/nexpanel/sites/1

sec "start OpenLiteSpeed"
/usr/local/lsws/bin/lswsctrl start >/dev/null 2>&1

sec "start np-broker + npd"
install -m0755 /np/npd /np/np-broker /usr/local/bin/
mkdir -p /run/nexpanel /srv/nexpanel/sites
export NP_BROKER_TOKEN=tok
NP_LOG_FORMAT=text NP_BROKER_ALLOWED_UID=0 np-broker --serve --socket /run/nexpanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/nexpanel/broker.sock ] && break; sleep 0.2; done
NP_SERVER_HOST=127.0.0.1 NP_SERVER_PORT=18443 NP_LOG_FORMAT=text \
  NP_DATABASE_DRIVER=sqlite NP_DATABASE_DSN=/tmp/np.db \
  NP_BROKER_SOCKET=/run/nexpanel/broker.sock npd >/tmp/npd.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "auth"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/np_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }

sec "create site acme.test + content"
api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"Acme","primary_domain":"acme.test","type":"static"}' >/dev/null
uid=$(api $base/api/v1/sites | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
echo '<h1>Hello from NexPanel</h1>' > $site/public/index.html
chown nps1:nps1 $site/public/index.html; chmod 644 $site/public/index.html
chmod o+x /srv/nexpanel/sites/1 /srv/nexpanel/sites/1/public 2>/dev/null

sec "ADD ALIAS www.acme.test + REDIRECT old.acme.test -> https://acme.test"
api -X POST $base/api/v1/sites/$uid/domains -H 'Content-Type: application/json' \
  -d '{"fqdn":"www.acme.test","kind":"alias"}'; echo
api -X POST $base/api/v1/sites/$uid/domains -H 'Content-Type: application/json' \
  -d '{"fqdn":"old.acme.test","kind":"redirect","redirect_to":"https://acme.test","redirect_code":301}'; echo
echo "domains:"; api $base/api/v1/sites/$uid/domains; echo

sec "generated vhost map + rewrite"
grep -E "map |RewriteCond|RewriteRule|rewrite" /usr/local/lsws/conf/nexpanel.conf 2>&1 | head -12
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1

sec "*** PRIMARY serves ***"
echo -n "acme.test          -> "; curl -s -o /dev/null -w '%{http_code} ' -H 'Host: acme.test' http://127.0.0.1/; curl -s -H 'Host: acme.test' http://127.0.0.1/

sec "*** ALIAS serves the same site ***"
echo -n "www.acme.test      -> "; curl -s -o /dev/null -w '%{http_code} ' -H 'Host: www.acme.test' http://127.0.0.1/; curl -s -H 'Host: www.acme.test' http://127.0.0.1/

sec "*** REDIRECT domain 301s ***"
curl -s -o /dev/null -w 'old.acme.test      -> %{http_code} Location: %{redirect_url}\n' -H 'Host: old.acme.test' http://127.0.0.1/

sec "ENABLE force-HTTPS, then plain HTTP must 301 to https"
api -X PUT $base/api/v1/sites/$uid/force-https -H 'Content-Type: application/json' -d '{"enabled":true}'; echo
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1
curl -s -o /dev/null -w 'acme.test (force)  -> %{http_code} Location: %{redirect_url}\n' -H 'Host: acme.test' http://127.0.0.1/

sec "DISABLE force-HTTPS -> serves plainly again"
api -X PUT $base/api/v1/sites/$uid/force-https -H 'Content-Type: application/json' -d '{"enabled":false}' >/dev/null
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1
echo -n "acme.test (off)    -> "; curl -s -o /dev/null -w '%{http_code}\n' -H 'Host: acme.test' http://127.0.0.1/

sec "DELETE the alias -> dropped from the vhost map"
did=$(api $base/api/v1/sites/$uid/domains | grep -oE '"uid":"[^"]+","fqdn":"www.acme.test"' | grep -oE '"uid":"[^"]+"' | cut -d'"' -f4)
api -X DELETE $base/api/v1/sites/$uid/domains/$did; echo
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1
# NOTE: assert on the rendered map, not on curl. OpenLiteSpeed routes an
# unmatched Host to the default (first) vhost, so with a single site every Host
# would answer 200 regardless — a curl here would prove nothing.
echo -n "map after delete: "; grep -E "^  map " /usr/local/lsws/conf/nexpanel.conf
if grep -qE "^  map .*www\.acme\.test" /usr/local/lsws/conf/nexpanel.conf; then
  echo "ALIAS STILL MAPPED (unexpected)"
else
  echo "alias dropped from map: OK"
fi

sec "OLS error log tail"
tail -4 /usr/local/lsws/logs/error.log 2>&1
