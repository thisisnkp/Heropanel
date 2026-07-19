#!/usr/bin/env bash
# A REAL Next.js app deployed from Git, built as the site's unprivileged user,
# and reverse-proxied by real OpenLiteSpeed.
#
# The earlier app-runtime test ran a `node -e` one-liner, which proves the proxy
# but says nothing about whether a real framework's install+build survives the
# deploy pipeline: a Next.js deploy runs `npm install` and `next build` inside a
# release directory, as a user with no write access outside its own home, and
# then has to start from `<home>/current` after an atomic symlink swap.
#
# This is the "Next.js" half of the Phase 3 exit criteria (run-fastapi.sh does
# the FastAPI half, plus auto-deploy and rollback).
#
# It is also the only e2e that runs the REAL deploy path: with Redis configured a
# deploy returns 202 + a job and runs on a worker. That is not incidental — an
# `npm install && next build` takes minutes, far longer than the panel's HTTP
# write timeout, so the synchronous fallback physically cannot deploy a real
# framework app. Every other e2e deploys something small enough to finish inline.
set -u
sec(){ echo; echo "======== $* ========"; }
base=http://127.0.0.1:18443
site=/srv/heropanel/sites/1

sec "start OpenLiteSpeed + Redis"
/usr/local/lsws/bin/lswsctrl start >/dev/null 2>&1
redis-server --daemonize yes --save '' --appendonly no >/tmp/redis.log 2>&1
for i in $(seq 1 40); do redis-cli ping >/dev/null 2>&1 && break; sleep 0.25; done
echo "redis: $(redis-cli ping 2>&1)"

sec "stand up a git server with a real Next.js app"
useradd -m -s /bin/bash git 2>/dev/null
mkdir -p /srv/git /home/git/.ssh
git init --bare -q /srv/git/app.git
work=$(mktemp -d)
git init -q "$work"
git -C "$work" config user.email ci@heropanel.test
git -C "$work" config user.name CI

cat > "$work/package.json" <<'EOF'
{
  "name": "heropanel-nextjs-e2e",
  "private": true,
  "scripts": { "build": "next build", "start": "next start" },
  "dependencies": {
    "next": "15.1.3",
    "react": "19.0.0",
    "react-dom": "19.0.0"
  }
}
EOF
# A plain `next build` + `next start`, which is what an operator deploying to a
# panel actually does. (`output: standalone` is for container images and is
# explicitly incompatible with `next start`.)
cat > "$work/next.config.js" <<'EOF'
module.exports = {};
EOF
mkdir -p "$work/app"
cat > "$work/app/layout.js" <<'EOF'
export default function RootLayout({ children }) {
  return (<html lang="en"><body>{children}</body></html>);
}
EOF
cat > "$work/app/page.js" <<'EOF'
export const dynamic = 'force-dynamic';
export default function Page() {
  return <h1>Next.js live via HeroPanel pid {process.pid}</h1>;
}
EOF
mkdir -p "$work/app/healthz"
cat > "$work/app/healthz/route.js" <<'EOF'
export const dynamic = 'force-dynamic';
export function GET() {
  return new Response(JSON.stringify({ ok: true }), {
    headers: { 'content-type': 'application/json' },
  });
}
EOF
git -C "$work" add -A
git -C "$work" commit -qm "next app"
git -C "$work" branch -M main
git -C "$work" remote add origin /srv/git/app.git
git -C "$work" push -q origin main
chown -R git:git /srv/git /home/git
chmod 700 /home/git/.ssh
ssh-keygen -A >/dev/null 2>&1
/usr/sbin/sshd >/tmp/sshd.log 2>&1
for i in $(seq 1 40); do (echo > /dev/tcp/127.0.0.1/22) >/dev/null 2>&1 && break; sleep 0.2; done
echo "node $(node --version), npm $(npm --version), next $(grep -o '"next": "[^"]*"' "$work/package.json")"

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
  HP_REDIS_ADDR=127.0.0.1:6379 \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done
grep -o 'job queue enabled' /tmp/hpd.log | head -1

sec "auth"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }

# waitjob <id> <max-polls> — with a queue configured EVERY long op returns a job,
# site creation included, so the script has to wait like a real client would.
waitjob(){
  local id="$1" max="${2:-24}" st
  for i in $(seq 1 "$max"); do
    st=$(api $base/api/v1/jobs/$id | grep -oE '"status":"[a-z]+"' | head -1 | cut -d'"' -f4)
    case "$st" in
      succeeded) echo "  job $id succeeded (~$((i*5))s)"; return 0 ;;
      failed)    echo "  job $id FAILED:"; api $base/api/v1/jobs/$id | head -c 1000; echo; return 1 ;;
    esac
    sleep 5
  done
  echo "  job $id still $st after $((max*5))s"; return 1
}

sec "CREATE PROXY SITE + SOURCE (build = npm install && next build)"
cj=$(api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"Next","primary_domain":"next.test","type":"proxy","deploy_mode":"git"}')
waitjob "$(echo "$cj" | grep -oE '"id":"[^"]+"' | head -1 | cut -d'"' -f4)" 12
uid=$(api $base/api/v1/sites | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
cat >/tmp/src.json <<'EOF'
{"repo_url":"git@127.0.0.1:/srv/git/app.git","branch":"main","web_root":"",
 "auth_kind":"ssh_key",
 "build_command":"npm install --no-audit --no-fund --loglevel=error && npx next build"}
EOF
src=$(api -X PUT $base/api/v1/sites/$uid/git -H 'Content-Type: application/json' --data @/tmp/src.json)
pub=$(echo "$src" | grep -oE '"public_key":"[^"]+"' | cut -d'"' -f4)
echo "$pub" > /home/git/.ssh/authorized_keys
chown git:git /home/git/.ssh/authorized_keys; chmod 600 /home/git/.ssh/authorized_keys
echo "site uid=$uid"

sec "*** DEPLOY (async job): npm install + next build as the site user ***"
# The real path: 202 + a job id, worked off the Redis queue. A build this long
# could never complete inside an HTTP request.
api -X POST $base/api/v1/sites/$uid/git/deploy > /tmp/d.json
head -c 240 /tmp/d.json; echo
job=$(grep -oE '"id":"[^"]+"' /tmp/d.json | head -1 | cut -d'"' -f4)
echo "polling job $job (npm install + next build takes a few minutes)"
waitjob "$job" 120
echo -n "next build output present: "
ls -d $site/current/.next >/dev/null 2>&1 && echo OK || echo "FAIL"
echo -n ".next is owned by the site user: "; stat -c %U $site/current/.next 2>/dev/null

sec "SET NODE RUNTIME (next start)"
cat >/tmp/rt.json <<'EOF'
{"runtime":"node","command":"npx next start --hostname 127.0.0.1 --port 3000",
 "port":3000,"env":{"NODE_ENV":"production"},"health_path":"/healthz"}
EOF
api -X PUT $base/api/v1/sites/$uid/runtime -H 'Content-Type: application/json' --data @/tmp/rt.json \
  | grep -oE '"(runtime|status)":"[^"]*"'

sec "health + slice placement"
api $base/api/v1/sites/$uid/runtime/health; echo
grep -E '^Slice=' /etc/systemd/system/heropanel-app-hps1.service 2>&1

sec "reload OLS"
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1

sec "*** CURL THE NEXT.JS APP THROUGH OPENLITESPEED ***"
curl -si -H 'Host: next.test' http://127.0.0.1/ 2>&1 | head -8
echo "--- rendered by Next.js ---"
# React splits text around an interpolation with <!-- --> markers, so match the
# literal prefix rather than the whole sentence.
curl -s -H 'Host: next.test' http://127.0.0.1/ 2>&1 | grep -o 'Next.js live via HeroPanel' | head -1
echo -n "next start is serving (x-powered-by): "
curl -sI -H 'Host: next.test' http://127.0.0.1/ 2>&1 | grep -io 'x-powered-by: Next.js'

sec "app log tail"
tail -5 /tmp/app-heropanel-app-hps1.log 2>&1
