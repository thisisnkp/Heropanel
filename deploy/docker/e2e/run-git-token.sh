#!/usr/bin/env bash
# Token / HTTPS clone, end to end against a real HTTPS git server.
#
# HeroPanel seals a personal access token with the panel's master key and, at
# deploy time, hands the broker a git-credential-store line so `git clone` can
# authenticate over HTTPS. This proves that path live: a private-CA TLS cert
# (installed into the system trust store, so the clone verifies TLS normally —
# no --insecure), HTTP Basic auth over it, and the smart-git protocol served by
# `git http-backend`. The correct token clones; a wrong token is refused.
set -u
sec(){ echo; echo "======== $* ========"; }
base=http://127.0.0.1:18443
site=/srv/heropanel/sites/1
host=githttp.test
port=9443
TOKEN=ghp_$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')

sec "start OpenLiteSpeed"
/usr/local/lsws/bin/lswsctrl start >/dev/null 2>&1

sec "issue a private-CA cert for $host and install it into the trust store"
echo "127.0.0.1 $host" >> /etc/hosts
openssl req -x509 -newkey rsa:2048 -nodes -keyout /tmp/git.key -out /tmp/git.crt \
  -days 1 -subj "/CN=$host" \
  -addext "subjectAltName=DNS:$host,IP:127.0.0.1" \
  -addext "basicConstraints=critical,CA:TRUE" >/dev/null 2>&1
cp /tmp/git.crt /usr/local/share/ca-certificates/githttp.crt
update-ca-certificates >/dev/null 2>&1
echo "CA installed: $(ls /etc/ssl/certs | grep -c githttp) entry"

sec "stand up the bare repo + HTTPS git server"
export GIT_PROJECT_ROOT=/srv/githttp
mkdir -p "$GIT_PROJECT_ROOT"
git init --bare -q "$GIT_PROJECT_ROOT/app.git"
work=$(mktemp -d)
git init -q "$work"; git -C "$work" config user.email ci@heropanel.test; git -C "$work" config user.name CI
mkdir -p "$work/site"; printf '<h1>token repo via HeroPanel</h1>' > "$work/site/index.html"
git -C "$work" add -A; git -C "$work" commit -qm seed; git -C "$work" branch -M main
git -C "$work" remote add origin "$GIT_PROJECT_ROOT/app.git"; git -C "$work" push -q origin main
# http-backend must be allowed to serve this repo without a per-repo flag.
git config -f "$GIT_PROJECT_ROOT/app.git/config" http.receivepack false

GIT_USER=deployer GIT_TOKEN="$TOKEN" TLS_CERT=/tmp/git.crt TLS_KEY=/tmp/git.key PORT=$port \
  python3 /e2e/githttp-server.py >/tmp/githttp.log 2>&1 &
for i in $(seq 1 40); do (echo > /dev/tcp/127.0.0.1/$port) >/dev/null 2>&1 && break; sleep 0.2; done
# Sanity: an unauthenticated info/refs must be 401; with the token it must be 200.
echo -n "no-auth  info/refs -> "; curl -s -o /dev/null -w '%{http_code}\n' "https://$host:$port/app.git/info/refs?service=git-upload-pack"
echo -n "with-tok info/refs -> "; curl -s -o /dev/null -w '%{http_code}\n' -u "deployer:$TOKEN" "https://$host:$port/app.git/info/refs?service=git-upload-pack"

sec "start hp-broker + hpd (master key set, so token sealing is enabled)"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel /srv/heropanel/sites
export HP_BROKER_TOKEN=tok
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
SECRET_KEY=$(head -c 32 /dev/urandom | base64 -w0)
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18443 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp.db \
  HP_SECRET_KEY="$SECRET_KEY" \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "auth + create git site"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }
api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"Tok","primary_domain":"tok.test","type":"static","deploy_mode":"git"}' >/dev/null
uid=$(api $base/api/v1/sites | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
echo "site uid=$uid"

sec "*** SET TOKEN SOURCE (https, PAT) + DEPLOY -> clone over real TLS ***"
printf '{"repo_url":"https://%s:%s/app.git","branch":"main","web_root":"site","auth_kind":"token","auth_username":"deployer","token":"%s"}' "$host" "$port" "$TOKEN" >/tmp/src.json
api -X PUT $base/api/v1/sites/$uid/git -H 'Content-Type: application/json' --data @/tmp/src.json >/dev/null
# The token must never come back out.
if api $base/api/v1/sites/$uid/git | grep -q "$TOKEN"; then echo "FAIL: token echoed by the API"; else echo "token absent from API response: OK"; fi
if grep -aq "$TOKEN" /tmp/hp.db; then echo "FAIL: token stored in the clear"; else echo "token sealed at rest (not in the db): OK"; fi
api -X POST $base/api/v1/sites/$uid/git/deploy >/tmp/dep.json 2>&1
echo -n "token deploy "; grep -oE '"status":"[a-z]+"' /tmp/dep.json | head -1

sec "perms for OLS(nobody) + reload"
chmod o+x $site $site/releases 2>/dev/null
chmod -R o+rX $site/releases 2>/dev/null
chmod o+rwx $site/logs 2>/dev/null
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1

sec "*** CURL THE PAGE CLONED OVER HTTPS ***"
echo -n "tok.test -> "; curl -s -o /tmp/body -w '%{http_code} ' -H 'Host: tok.test' http://127.0.0.1/; cat /tmp/body; echo

sec "*** WRONG TOKEN IS REFUSED (auth is really enforced) ***"
printf '{"repo_url":"https://%s:%s/app.git","branch":"main","web_root":"site","auth_kind":"token","auth_username":"deployer","token":"wrong-%s"}' "$host" "$port" "$TOKEN" >/tmp/src_bad.json
api -X PUT $base/api/v1/sites/$uid/git -H 'Content-Type: application/json' --data @/tmp/src_bad.json >/dev/null
api -X POST $base/api/v1/sites/$uid/git/deploy >/tmp/dep_bad.json 2>&1
echo -n "wrong-token deploy -> "; head -c 200 /tmp/dep_bad.json; echo

sec "broker audit (git.deploy: success with the right token, failure with the wrong one)"
grep -oE '"capability":"git\.deploy","outcome":"[^"]+"' /tmp/broker.log

sec "RESULT"
fail=0
grep -q '"status":"success"' /tmp/dep.json || { echo "  FAIL token deploy did not succeed"; fail=1; }
if [ "$(cat /tmp/body)" = '<h1>token repo via HeroPanel</h1>' ]; then echo "  ok   page served from HTTPS token clone"; else echo "  FAIL page not served"; fail=1; fi
# A wrong token fails the clone: the deploy comes back as a failed deployment or
# an error envelope (never success), and the broker records the failure.
if grep -q '"status":"success"' /tmp/dep_bad.json; then
  echo "  FAIL wrong token was accepted"; fail=1
elif grep -qE '"status":"failed"|"error"' /tmp/dep_bad.json && grep -q '"capability":"git.deploy","outcome":"failure"' /tmp/broker.log; then
  echo "  ok   wrong token refused (clone failed, broker logged the failure)"
else
  echo "  FAIL wrong token outcome unclear"; fail=1
fi
if [ "$fail" -eq 0 ]; then echo "run-git-token.sh : PASS"; else echo "run-git-token.sh : FAIL"; fi
exit "$fail"
