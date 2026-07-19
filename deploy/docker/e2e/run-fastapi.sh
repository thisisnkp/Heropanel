#!/usr/bin/env bash
# Phase 3 exit criteria, the other half: "deploy a Next.js/FastAPI app via Git
# with auto-deploy + rollback".
#
# A REAL FastAPI app: a real requirements.txt resolved from PyPI into a real
# venv, served by real uvicorn, reverse-proxied by real OpenLiteSpeed. This is
# also the only live proof that the **python** runtime works at all — every
# earlier app-runtime test was a Node one-liner.
#
# It then proves the two verbs the exit criteria actually name:
#   auto-deploy -- `git push` + the webhook endpoint, no panel session involved
#   rollback    -- back to the previous release, app restarted, old code served
#
# Ubuntu 24.04 is PEP-668 ("externally managed"), so the build command has to
# create a venv — which is what a real Python deploy looks like anyway.
set -u
sec(){ echo; echo "======== $* ========"; }
base=http://127.0.0.1:18443
site=/srv/heropanel/sites/1

sec "start OpenLiteSpeed"
/usr/local/lsws/bin/lswsctrl start >/dev/null 2>&1

sec "stand up a private git server with a FastAPI app"
useradd -m -s /bin/bash git 2>/dev/null
mkdir -p /srv/git /home/git/.ssh
git init --bare -q /srv/git/app.git
work=$(mktemp -d)
git init -q "$work"
git -C "$work" config user.email ci@heropanel.test
git -C "$work" config user.name CI

cat > "$work/requirements.txt" <<'EOF'
fastapi==0.115.6
uvicorn==0.34.0
EOF

# writeapp <version> — rewrite the app so a redeploy serves visibly new bytes.
writeapp(){
cat > "$work/main.py" <<EOF
from fastapi import FastAPI

app = FastAPI()

@app.get("/healthz")
def healthz():
    return {"ok": True}

@app.get("/")
def root():
    return {"app": "fastapi", "version": "$1"}
EOF
}
writeapp v1
git -C "$work" add -A
git -C "$work" commit -qm "v1"
git -C "$work" branch -M main
git -C "$work" remote add origin /srv/git/app.git
git -C "$work" push -q origin main
chown -R git:git /srv/git /home/git
chmod 700 /home/git/.ssh
# The bare repo now belongs to `git`, but this script pushes to it as root, and
# git refuses to touch a repo owned by someone else. Harness-only: HeroPanel's
# own clones go over SSH as the git user.
git config --global --add safe.directory /srv/git/app.git
ssh-keygen -A >/dev/null 2>&1
/usr/sbin/sshd >/tmp/sshd.log 2>&1
for i in $(seq 1 40); do (echo > /dev/tcp/127.0.0.1/22) >/dev/null 2>&1 && break; sleep 0.2; done
echo "python: $(python3 --version), requirements: $(tr '\n' ' ' < "$work/requirements.txt")"

sec "start hp-broker + hpd"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel /srv/heropanel/sites
export HP_BROKER_TOKEN=tok
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 HP_BROKER_PANEL_USER=root \
  hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
SECRET_KEY=$(head -c 32 /dev/urandom | base64 -w0)
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18443 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp.db \
  HP_SECRET_KEY="$SECRET_KEY" \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "auth"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }

sec "CREATE PROXY SITE"
api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"Api","primary_domain":"api.test","type":"proxy","deploy_mode":"git"}' >/dev/null
uid=$(api $base/api/v1/sites | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
echo "site uid=$uid"

sec "*** THE SITE GOT ITS CGROUP SLICE AT PROVISIONING ***"
echo "--- /etc/systemd/system/heropanel-site-hps1.slice ---"
cat /etc/systemd/system/heropanel-site-hps1.slice 2>&1
grep -q 'site.apply_slice' /tmp/broker.log && echo "site.apply_slice invoked: OK"

sec "SET LIMITS (CPU 50%, 512 MiB, 100 tasks)"
api -X PUT $base/api/v1/sites/$uid/limits -H 'Content-Type: application/json' \
  -d '{"cpu_quota_pct":50,"mem_limit_bytes":536870912,"pids_max":100}'; echo
echo "--- slice after limits ---"
cat /etc/systemd/system/heropanel-site-hps1.slice 2>&1

sec "SET GIT SOURCE (deploy key) + build a venv from requirements.txt"
cat >/tmp/src.json <<'EOF'
{"repo_url":"git@127.0.0.1:/srv/git/app.git","branch":"main","web_root":"",
 "auth_kind":"ssh_key",
 "build_command":"python3 -m venv .venv && .venv/bin/pip install -q -r requirements.txt"}
EOF
src=$(api -X PUT $base/api/v1/sites/$uid/git -H 'Content-Type: application/json' --data @/tmp/src.json)
pub=$(echo "$src" | grep -oE '"public_key":"[^"]+"' | cut -d'"' -f4)
echo "$pub" > /home/git/.ssh/authorized_keys
chown git:git /home/git/.ssh/authorized_keys; chmod 600 /home/git/.ssh/authorized_keys
hook=$(echo "$src" | grep -oE '"webhook_url":"[^"]+"' | cut -d'"' -f4)
echo "webhook: ${hook%%\?*}?secret=<redacted>"

sec "*** DEPLOY #1 (real pip install into a real venv, as the site user) ***"
api -X POST $base/api/v1/sites/$uid/git/deploy > /tmp/d1.json
grep -oE '"status":"[a-z_]+"' /tmp/d1.json | head -1
dep1=$(grep -oE '"uid":"[^"]+"' /tmp/d1.json | head -1 | cut -d'"' -f4)
echo "deployment #1 uid=$dep1"
echo -n "uvicorn installed in the release venv: "
ls $site/current/.venv/bin/uvicorn >/dev/null 2>&1 && echo OK || echo "FAIL"
echo -n "venv is owned by the site user: "; stat -c %U $site/current/.venv 2>/dev/null

sec "SET PYTHON RUNTIME (uvicorn from the release's own venv)"
cat >/tmp/rt.json <<'EOF'
{"runtime":"python","command":".venv/bin/uvicorn main:app --host 127.0.0.1 --port 8000",
 "port":8000,"env":{"PYTHONUNBUFFERED":"1"},"health_path":"/healthz"}
EOF
api -X PUT $base/api/v1/sites/$uid/runtime -H 'Content-Type: application/json' --data @/tmp/rt.json \
  | grep -oE '"(runtime|status|health_path)":"[^"]*"'

sec "*** THE APP UNIT IS INSIDE THE SITE SLICE ***"
grep -E '^(Slice|User|WorkingDirectory|ExecStart)=' /etc/systemd/system/heropanel-app-hps1.service 2>&1

sec "health"
api $base/api/v1/sites/$uid/runtime/health; echo

sec "reload OLS"
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1

sec "*** CURL THE FASTAPI APP THROUGH OPENLITESPEED ***"
echo -n "api.test -> "; curl -s -H 'Host: api.test' http://127.0.0.1/; echo

sec "*** AUTO-DEPLOY: git push + webhook (no panel session) ***"
writeapp v2
git -C "$work" add -A
git -C "$work" commit -qm "v2"
git -C "$work" push -q origin main
echo "pushed v2; calling the webhook exactly as GitHub would (no cookie, no CSRF):"
curl -s -X POST "$base$hook" | head -c 200; echo
sleep 2
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1
echo -n "after webhook  -> "; curl -s -H 'Host: api.test' http://127.0.0.1/; echo

sec "*** WEBHOOK AUTH: GitHub HMAC signature over the body (no ?secret) ***"
# GitHub signs the payload, it does not put the secret in the URL. Prove the
# signature path: a correct sha256 HMAC authorizes; a tampered one is denied —
# which a bare shared-secret compare cannot distinguish.
hookpath="${hook%%\?*}"           # /hooks/git/<uid>
secret="${hook##*secret=}"        # the raw webhook secret
gbody='{"ref":"refs/heads/main"}'
gsig="sha256=$(printf '%s' "$gbody" | openssl dgst -sha256 -hmac "$secret" | awk '{print $NF}')"
good=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$base$hookpath" \
  -H "X-Hub-Signature-256: $gsig" -H 'Content-Type: application/json' --data "$gbody")
# 201 (synchronous, no job queue here) or 202 (async with Redis) both mean accepted.
case "$good" in 20[12]) echo "signed valid signature: OK ($good)";; *) echo "signed valid signature: FAIL ($good)";; esac
bad=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$base$hookpath" \
  -H "X-Hub-Signature-256: sha256=deadbeef" -H 'Content-Type: application/json' --data "$gbody")
case "$bad" in 403) echo "tampered signature denied: OK ($bad)";; *) echo "tampered signature denied: FAIL ($bad)";; esac
# The audit log (the real store, not the slog) must record which proof kind was
# presented, never its value. detail is JSON-escaped in the list response.
echo -n "audit proof kinds: "; api $base/api/v1/audit | grep -aoE 'webhook_auth[^,}]*' | grep -aoE '(github_signature|shared_secret|gitlab_token|none)' | sort -u | tr '\n' ' '; echo

sec "*** ROLLBACK to deployment #1 (app restarts, old code serves) ***"
api -X POST $base/api/v1/sites/$uid/git/rollback/$dep1 | head -c 200; echo
sleep 2
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1
echo -n "after rollback -> "; curl -s -H 'Host: api.test' http://127.0.0.1/; echo

sec "deployment history (manual, webhook, rollback)"
api $base/api/v1/sites/$uid/git/deployments | grep -oE '"trigger":"[a-z]+"' | head -5

sec "broker audit"
grep -oE '"capability":"(site\.apply_slice|git\.deploy|app\.unit_[a-z]+)","outcome":"[^"]+"' /tmp/broker.log | sort | uniq -c

sec "app log tail"
tail -5 /tmp/app-heropanel-app-hps1.log 2>&1
