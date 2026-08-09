#!/usr/bin/env bash
# Real ZeroSSL-style ACME issuance with **External Account Binding**, against
# Pebble configured to require EAB. ZeroSSL is a standard RFC 8555 CA that adds
# EAB (a KID + HMAC key from the operator's dashboard) at account registration;
# this proves NexPanel's EAB path end-to-end without a real ZeroSSL account:
# the account is bound with the EAB, the HTTP-01 order runs, and Pebble (which
# would reject a missing/invalid EAB) issues the certificate — recorded under the
# "zerossl" provider.
set -u
sec(){ echo; echo "======== $* ========"; }
fail=0
domain=zerossl.test
base=http://127.0.0.1:18443

sec "issue a CA + install it into the trust store"
mkdir -p /tmp/pebble
openssl req -x509 -newkey rsa:2048 -nodes -keyout /tmp/pebble/key.pem -out /tmp/pebble/cert.pem \
  -days 1 -subj "/CN=pebble" \
  -addext "subjectAltName=DNS:pebble,DNS:localhost,IP:127.0.0.1" \
  -addext "basicConstraints=critical,CA:TRUE" >/dev/null 2>&1
cp /tmp/pebble/cert.pem /usr/local/share/ca-certificates/pebble.crt
update-ca-certificates >/dev/null 2>&1

sec "start Pebble with EAB REQUIRED (a known KID + HMAC key)"
# The MAC key is shared out-of-band with the operator; here we generate one and
# hand the same base64url key to both Pebble (config) and npd (secret env).
KID="kid-np-1"
MAC=$(openssl rand 32 | openssl base64 -A | tr '+/' '-_' | tr -d '=')
echo "EAB kid=$KID mac=${MAC:0:12}…"
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
    "externalAccountBindingRequired": true,
    "externalAccountMACKeys": { "$KID": "$MAC" }
  }
}
EOF
echo "127.0.0.1 $domain pebble" >> /etc/hosts
PEBBLE_VA_NOSLEEP=1 pebble -config /tmp/pebble/config.json >/tmp/pebble.log 2>&1 &
for i in $(seq 1 50); do curl -sf https://127.0.0.1:14000/dir >/dev/null 2>&1 && break; sleep 0.2; done
echo -n "directory reachable: "; curl -s -o /dev/null -w '%{http_code}\n' https://127.0.0.1:14000/dir

sec "start OpenLiteSpeed + np-broker + npd (ZeroSSL pointed at Pebble, EAB via env)"
/usr/local/lsws/bin/lswsctrl start >/dev/null 2>&1
install -m0755 /np/npd /np/np-broker /usr/local/bin/
mkdir -p /run/nexpanel /srv/nexpanel/sites
export NP_BROKER_TOKEN=tok
NP_LOG_FORMAT=text NP_BROKER_ALLOWED_UID=0 np-broker --serve --socket /run/nexpanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/nexpanel/broker.sock ] && break; sleep 0.2; done
NP_SERVER_HOST=127.0.0.1 NP_SERVER_PORT=18443 NP_LOG_FORMAT=text \
  NP_DATABASE_DRIVER=sqlite NP_DATABASE_DSN=/tmp/np.db \
  NP_SSL_EMAIL="admin@$domain" \
  NP_SSL_ZEROSSL_DIRECTORY="https://127.0.0.1:14000/dir" \
  NP_SSL_ZEROSSL_EAB_KID="$KID" NP_SSL_ZEROSSL_EAB_HMAC="$MAC" \
  NP_BROKER_SOCKET=/run/nexpanel/broker.sock npd >/tmp/npd.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done
grep -o "ZeroSSL enabled (EAB)" /tmp/npd.log | head -1

sec "auth + create the site to certify"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/np_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }
api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d "{\"name\":\"ZeroSSL\",\"primary_domain\":\"$domain\",\"type\":\"static\"}" >/dev/null
webroot=/srv/nexpanel/sites/1/public
chmod o+x /srv/nexpanel/sites/1 2>/dev/null; chmod -R o+rX "$webroot" 2>/dev/null
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1

sec "*** ISSUE VIA ZeroSSL (provider=zerossl, EAB-bound account, Pebble validates) ***"
printf '{"domain":"%s","method":"http-01","webroot":"%s","provider":"zerossl"}' "$domain" "$webroot" >/tmp/issue.json
api -X POST $base/api/v1/ssl/issue -H 'Content-Type: application/json' --data @/tmp/issue.json >/tmp/cert.json 2>&1
echo "issue response:"; head -c 400 /tmp/cert.json; echo
api $base/api/v1/ssl/certificates >/tmp/certs.json

sec "RESULT"
grep -q "ZeroSSL enabled (EAB)" /tmp/npd.log || { echo "  FAIL ZeroSSL issuer not enabled"; fail=1; }
# The account could only register because the EAB verified against Pebble's key.
if grep -q '"provider":"zerossl"' /tmp/cert.json /tmp/certs.json; then
  echo "  ok   certificate issued and recorded under the zerossl provider"
else
  echo "  FAIL no zerossl certificate was issued"; fail=1
fi
leaf=/etc/nexpanel/ssl/$domain/fullchain.pem
if [ -f "$leaf" ] && openssl x509 -in "$leaf" -noout -issuer 2>/dev/null | grep -qi pebble; then
  echo "  ok   leaf is signed by the Pebble CA (real EAB-bound ACME issuance)"
else
  echo "  FAIL issued cert is not signed by Pebble"; fail=1
fi
if [ "$fail" -eq 0 ]; then echo "run-zerossl.sh : PASS"; else echo "run-zerossl.sh : FAIL"; fi
exit "$fail"
