#!/usr/bin/env bash
# Private Git repository over SSH, end to end against a real sshd.
#
# HeroPanel generates an ed25519 deploy key, seals the private half with the
# panel's master key, and hands only the public half back. We register that
# public key on a local git account, then deploy: the clone must succeed using a
# key the operator never saw, and the private half must never appear in argv or
# survive on disk once the clone is done.
#
# The token/HTTPS path is covered by unit tests only — standing up an HTTPS git
# server with a trusted cert inside this image is not worth the fidelity it buys.
set -u
sec(){ echo; echo "======== $* ========"; }
base=http://127.0.0.1:18443
site=/srv/heropanel/sites/1

sec "start OpenLiteSpeed"
/usr/local/lsws/bin/lswsctrl start >/dev/null 2>&1

sec "stand up a private git server (real sshd + bare repo)"
# A dedicated `git` account owns the repo — the same shape as GitHub's remote,
# so the deploy URL under test is the one an operator would actually paste.
# Key-only auth: no password exists on this account.
useradd -m -s /bin/bash git 2>/dev/null
mkdir -p /srv/git /home/git/.ssh
git init --bare -q /srv/git/app.git

work=$(mktemp -d)
git init -q "$work"
git -C "$work" config user.email ci@heropanel.test
git -C "$work" config user.name CI
mkdir -p "$work/site"
printf '<h1>private repo via HeroPanel</h1>' > "$work/site/index.html"
git -C "$work" add -A
git -C "$work" commit -qm seed
git -C "$work" branch -M main
git -C "$work" remote add origin /srv/git/app.git
git -C "$work" push -q origin main

chown -R git:git /srv/git /home/git
chmod 700 /home/git/.ssh
ssh-keygen -A >/dev/null 2>&1
/usr/sbin/sshd >/tmp/sshd.log 2>&1
for i in $(seq 1 40); do (echo > /dev/tcp/127.0.0.1/22) >/dev/null 2>&1 && break; sleep 0.2; done
echo "sshd up; bare repo /srv/git/app.git owned by $(stat -c %U /srv/git/app.git)"

sec "start hp-broker + hpd (with a master key, so private repos are enabled)"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel /srv/heropanel/sites
export HP_BROKER_TOKEN=tok
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
# 32 random bytes, base64 — what the installer would write into secrets.env.
SECRET_KEY=$(head -c 32 /dev/urandom | base64 -w0)
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18443 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp.db \
  HP_SECRET_KEY="$SECRET_KEY" \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done
grep -o 'secret encryption enabled' /tmp/hpd.log | head -1

sec "auth"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }

sec "CREATE GIT SITE"
api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"Priv","primary_domain":"priv.test","type":"static","deploy_mode":"git"}' >/dev/null
uid=$(api $base/api/v1/sites | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
echo "site uid=$uid"

sec "SET SSH SOURCE -> panel generates a deploy key"
cat >/tmp/src.json <<'EOF'
{"repo_url":"git@127.0.0.1:/srv/git/app.git","branch":"main","web_root":"site","auth_kind":"ssh_key"}
EOF
src=$(api -X PUT $base/api/v1/sites/$uid/git -H 'Content-Type: application/json' --data @/tmp/src.json)
echo "$src" | head -c 400; echo
pub=$(echo "$src" | grep -oE '"public_key":"[^"]+"' | cut -d'"' -f4)
echo "generated deploy key: ${pub:0:60}..."

sec "*** THE PRIVATE HALF NEVER LEAVES THE PANEL ***"
if echo "$src" | grep -q 'PRIVATE KEY'; then echo 'FAIL: private key in API response'; else echo 'private key absent from API response: OK'; fi
# At rest it is ciphertext, not PEM.
if grep -aq 'BEGIN OPENSSH PRIVATE KEY' /tmp/hp.db; then
  echo 'FAIL: private key stored in the clear'
else
  echo 'private key sealed at rest (no PEM in the database): OK'
fi
echo -n 'stored credential looks like: '
strings /tmp/hp.db 2>/dev/null | grep -oE 'hp1\.[A-Za-z0-9_-]{20}' | head -1

sec "DEPLOY BEFORE REGISTERING THE KEY (must fail — the repo does not trust us yet)"
api -X POST $base/api/v1/sites/$uid/git/deploy | head -c 300; echo

sec "REGISTER THE PUBLIC KEY ON THE REPO (what an operator pastes into GitHub)"
echo "$pub" > /home/git/.ssh/authorized_keys
chown git:git /home/git/.ssh/authorized_keys
chmod 600 /home/git/.ssh/authorized_keys
echo "deploy key registered as an authorized key for the git account"

sec "*** DEPLOY WITH THE DEPLOY KEY (real SSH auth) ***"
api -X POST $base/api/v1/sites/$uid/git/deploy | head -c 400; echo

sec "release layout"
echo "-- current ->"; readlink $site/current
echo "-- public  ->"; readlink $site/public

sec "perms for OLS(nobody) + reload"
chmod o+x $site $site/releases 2>/dev/null
chmod -R o+rX $site/releases 2>/dev/null
chmod o+rwx $site/logs 2>/dev/null
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1

sec "*** CURL THE PAGE CLONED FROM THE PRIVATE REPO ***"
echo -n "priv.test -> "; curl -s -o /tmp/body -w '%{http_code} ' -H 'Host: priv.test' http://127.0.0.1/; cat /tmp/body; echo

sec "*** THE CREDENTIAL IS GONE FROM DISK ***"
if [ -d /run/heropanel/gitauth ] && [ -n "$(ls -A /run/heropanel/gitauth 2>/dev/null)" ]; then
  echo "FAIL: credential material left behind:"; ls -laR /run/heropanel/gitauth
else
  echo "no credential material left on /run: OK"
fi

sec "broker audit (git.deploy outcomes: the first fails, the second succeeds)"
grep -oE '"capability":"git\.deploy","outcome":"[^"]+"' /tmp/broker.log

sec "deployment history"
api $base/api/v1/sites/$uid/git/deployments | head -c 500; echo

sec "*** HOST-KEY PINNING: strict checking verifies the first connection too ***"
# The real server key, in known_hosts form (host + keytype + key).
realkey="127.0.0.1 $(cut -d' ' -f1,2 /etc/ssh/ssh_host_ed25519_key.pub)"
echo "server host key: ${realkey:0:50}..."
# A valid-format but WRONG key (a throwaway) — the MITM case.
ssh-keygen -t ed25519 -f /tmp/fake -N '' -q
fakekey="127.0.0.1 ssh-ed25519 $(cut -d' ' -f2 /tmp/fake.pub)"

# 1) Pin the WRONG key: the deploy key is still valid, but strict host-key
#    checking must refuse to talk to a server whose key does not match the pin.
printf '{"repo_url":"git@127.0.0.1:/srv/git/app.git","branch":"main","web_root":"site","auth_kind":"ssh_key","host_key":"%s"}' "$fakekey" >/tmp/src_bad.json
api -X PUT $base/api/v1/sites/$uid/git -H 'Content-Type: application/json' --data @/tmp/src_bad.json >/dev/null
api -X POST $base/api/v1/sites/$uid/git/deploy >/tmp/bad.json 2>&1
badstatus=$(grep -oE '"status":"[a-z]+"' /tmp/bad.json | head -1)
echo "wrong-pin deploy $badstatus"
if grep -qi 'host key verification failed' /tmp/bad.json /tmp/broker.log; then
  echo "wrong pin rejected: OK (host key verification failed)"
else
  echo "wrong pin NOT rejected:"; head -c 300 /tmp/bad.json; echo
fi

# 2) Pin the CORRECT key: the very same clone now succeeds under strict checking.
printf '{"repo_url":"git@127.0.0.1:/srv/git/app.git","branch":"main","web_root":"site","auth_kind":"ssh_key","host_key":"%s"}' "$realkey" >/tmp/src_good.json
api -X PUT $base/api/v1/sites/$uid/git -H 'Content-Type: application/json' --data @/tmp/src_good.json >/dev/null
api -X POST $base/api/v1/sites/$uid/git/deploy >/tmp/good.json 2>&1
goodstatus=$(grep -oE '"status":"[a-z]+"' /tmp/good.json | head -1)
echo "correct-pin deploy $goodstatus"
case "$goodstatus" in *success*) echo "correct pin deploy: success";; *) echo "correct pin FAILED:"; head -c 300 /tmp/good.json; echo;; esac
# The pinned key is public, so the API returns it (unlike the sealed private key).
api $base/api/v1/sites/$uid/git | grep -qE '"host_key":"127.0.0.1 ssh-ed25519' && echo "pinned host key returned by the API: OK"
