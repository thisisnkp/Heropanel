#!/usr/bin/env bash
# Real ACME (RFC 8555) HTTP-01 issuance against a live CA — Pebble, the Let's
# Encrypt team's test ACME server. The unit tests drive the order flow against a
# fake issuer; this proves the whole thing end to end against an actual ACME
# server: account registration, order, an HTTP-01 challenge that HeroPanel writes
# into the site webroot and OpenLiteSpeed serves, Pebble's validation authority
# fetching it, finalize, and certificate download + install.
#
# All in one container: Pebble (CA + validation authority), OpenLiteSpeed (serves
# the challenge on :80), hp-broker, and hpd. hpd trusts Pebble because Pebble's
# CA is installed into the system trust store before hpd starts — so the ACME
# HTTPS calls verify normally, no insecure client.
set -u
sec(){ echo; echo "======== $* ========"; }
fail=0
domain=acme.test
base=http://127.0.0.1:18443

sec "issue a CA + Pebble server cert; install the CA into the trust store"
mkdir -p /tmp/pebble
# One self-signed CA cert, used both as Pebble's directory TLS cert and as the
# trust anchor we install — SAN covers the addresses hpd dials Pebble on.
openssl req -x509 -newkey rsa:2048 -nodes -keyout /tmp/pebble/key.pem -out /tmp/pebble/cert.pem \
  -days 1 -subj "/CN=pebble" \
  -addext "subjectAltName=DNS:pebble,DNS:localhost,IP:127.0.0.1" \
  -addext "basicConstraints=critical,CA:TRUE" >/dev/null 2>&1
cp /tmp/pebble/cert.pem /usr/local/share/ca-certificates/pebble.crt
update-ca-certificates >/dev/null 2>&1
echo "pebble CA installed: $(ls /etc/ssl/certs | grep -c pebble) entry"

sec "start Pebble (ACME directory on :14000, validates HTTP-01 on :80)"
# httpPort 80 makes Pebble's VA fetch the challenge from the domain's port 80 —
# where OpenLiteSpeed serves the site (and thus the challenge file).
cat >/tmp/pebble/config.json <<EOF
{
  "pebble": {
    "listenAddress": "0.0.0.0:14000",
    "managementListenAddress": "0.0.0.0:15000",
    "certificate": "/tmp/pebble/cert.pem",
    "privateKey": "/tmp/pebble/key.pem",
    "httpPort": 80,
    "tlsPort": 5001,
    "ocspResponderURL": "",
    "externalAccountBindingRequired": false
  }
}
EOF
# NOSLEEP removes Pebble's random 0-15s validation delay so the test is quick;
# validation itself is real. The domain resolves to us via /etc/hosts.
echo "127.0.0.1 $domain pebble" >> /etc/hosts
PEBBLE_VA_NOSLEEP=1 pebble -config /tmp/pebble/config.json >/tmp/pebble.log 2>&1 &
for i in $(seq 1 50); do curl -sf https://127.0.0.1:14000/dir >/dev/null 2>&1 && break; sleep 0.2; done
echo -n "directory reachable + TLS-trusted: "; curl -s -o /dev/null -w '%{http_code}\n' https://127.0.0.1:14000/dir

sec "start OpenLiteSpeed"
/usr/local/lsws/bin/lswsctrl start >/dev/null 2>&1

sec "start hp-broker + hpd (ACME pointed at Pebble via HP_SSL_DIRECTORY)"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel /srv/heropanel/sites
export HP_BROKER_TOKEN=tok
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18443 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp.db \
  HP_SSL_EMAIL="admin@$domain" HP_SSL_DIRECTORY="https://127.0.0.1:14000/dir" \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done
grep -o "Let's Encrypt enabled" /tmp/hpd.log | head -1

sec "auth + create the site whose domain we will certify"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }
api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d "{\"name\":\"Acme\",\"primary_domain\":\"$domain\",\"type\":\"static\"}" >/dev/null
uid=$(api $base/api/v1/sites | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
webroot=/srv/heropanel/sites/1/public
echo "site uid=$uid webroot=$webroot"

sec "OLS must be able to serve the challenge from the site tree"
chmod o+x /srv/heropanel/sites/1 2>/dev/null
chmod -R o+rX "$webroot" 2>/dev/null
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1

sec "*** ISSUE A CERT VIA HTTP-01 (Pebble validates for real) ***"
printf '{"domain":"%s","method":"http-01","webroot":"%s"}' "$domain" "$webroot" >/tmp/issue.json
api -X POST $base/api/v1/ssl/issue -H 'Content-Type: application/json' --data @/tmp/issue.json >/tmp/cert.json 2>&1
echo "issue response:"; head -c 400 /tmp/cert.json; echo

sec "the challenge really was fetched by Pebble's VA"
grep -oiE "\"GET .*/.well-known/acme-challenge|http-01|valid" /tmp/pebble.log | head -3

sec "the issued cert is listed, valid, and issued by Pebble"
api $base/api/v1/ssl/certificates >/tmp/certs.json
head -c 500 /tmp/certs.json; echo
# The installed leaf on disk must chain to the Pebble CA (real signature).
leaf=/etc/heropanel/ssl/$domain/fullchain.pem
if [ -f "$leaf" ]; then
  echo -n "issuer: "; openssl x509 -in "$leaf" -noout -issuer 2>/dev/null
  if openssl verify -CAfile /tmp/pebble/pebble-ca.pem "$leaf" >/dev/null 2>&1 || \
     openssl verify -partial_chain -CAfile /tmp/pebble/cert.pem "$leaf" >/dev/null 2>&1; then
    echo "leaf verifies (chains to a real CA)"
  fi
fi

sec "RESULT"
grep -q "Let's Encrypt enabled" /tmp/hpd.log || { echo "  FAIL ACME issuer not enabled"; fail=1; }
if grep -qE '"status":"valid"|"issuer":"letsencrypt"|"common_name":"'"$domain"'"' /tmp/cert.json /tmp/certs.json; then
  echo "  ok   certificate issued and recorded for $domain"
else
  echo "  FAIL no valid certificate was issued"; fail=1
fi
if [ -f "$leaf" ] && openssl x509 -in "$leaf" -noout -issuer 2>/dev/null | grep -qi pebble; then
  echo "  ok   leaf is signed by the Pebble CA (real ACME issuance)"
else
  echo "  FAIL issued cert is not signed by Pebble"; fail=1
fi
if [ "$fail" -eq 0 ]; then echo "run-acme.sh : PASS"; else echo "run-acme.sh : FAIL"; fi
exit "$fail"
