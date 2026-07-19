#!/usr/bin/env bash
# A REAL Go app deployed from Git: the site's build command compiles a binary as
# the unprivileged site user, and the runtime supervises that binary.
#
# Go is the third of the three runtime labels HeroPanel advertises
# (node|python|go). The label itself is informational — the operator supplies the
# command — but "you can host a Go app" is a claim, and a compiled binary is a
# meaningfully different shape from an interpreter: the build produces the
# executable that the unit then execs out of the release directory, so an atomic
# release swap has to land a new binary and the restart has to pick it up.
set -u
sec(){ echo; echo "======== $* ========"; }
base=http://127.0.0.1:18443
site=/srv/heropanel/sites/1

sec "start OpenLiteSpeed"
/usr/local/lsws/bin/lswsctrl start >/dev/null 2>&1

sec "stand up a git server with a real Go app"
useradd -m -s /bin/bash git 2>/dev/null
mkdir -p /srv/git /home/git/.ssh
git init --bare -q /srv/git/app.git
work=$(mktemp -d)
git init -q "$work"
git -C "$work" config user.email ci@heropanel.test
git -C "$work" config user.name CI

cat > "$work/go.mod" <<'EOF'
module heropanel.test/e2e

go 1.23
EOF
cat > "$work/main.go" <<'EOF'
package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true}`)
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Go app live via HeroPanel pid %d", os.Getpid())
	})
	addr := "127.0.0.1:" + os.Getenv("PORT")
	if err := http.ListenAndServe(addr, nil); err != nil {
		panic(err)
	}
}
EOF
git -C "$work" add -A
git -C "$work" commit -qm "go app"
git -C "$work" branch -M main
git -C "$work" remote add origin /srv/git/app.git
git -C "$work" push -q origin main
chown -R git:git /srv/git /home/git
chmod 700 /home/git/.ssh
ssh-keygen -A >/dev/null 2>&1
/usr/sbin/sshd >/tmp/sshd.log 2>&1
for i in $(seq 1 40); do (echo > /dev/tcp/127.0.0.1/22) >/dev/null 2>&1 && break; sleep 0.2; done
echo "toolchain: $(go version)"

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

sec "CREATE PROXY SITE + SOURCE (build = go build)"
api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"Go","primary_domain":"go.test","type":"proxy","deploy_mode":"git"}' >/dev/null
uid=$(api $base/api/v1/sites | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
# GOCACHE must land inside the site's own tree: the unit is ProtectHome/
# ProtectSystem=strict and the site user has nowhere else to write.
cat >/tmp/src.json <<'EOF'
{"repo_url":"git@127.0.0.1:/srv/git/app.git","branch":"main","web_root":"",
 "auth_kind":"ssh_key",
 "build_command":"GOCACHE=$PWD/.gocache GOFLAGS=-mod=mod /usr/local/bin/go build -o app ."}
EOF
src=$(api -X PUT $base/api/v1/sites/$uid/git -H 'Content-Type: application/json' --data @/tmp/src.json)
pub=$(echo "$src" | grep -oE '"public_key":"[^"]+"' | cut -d'"' -f4)
echo "$pub" > /home/git/.ssh/authorized_keys
chown git:git /home/git/.ssh/authorized_keys; chmod 600 /home/git/.ssh/authorized_keys
echo "site uid=$uid"

sec "*** DEPLOY: go build as the unprivileged site user ***"
api -X POST $base/api/v1/sites/$uid/git/deploy > /tmp/d.json
grep -oE '"status":"[a-z_]+"' /tmp/d.json | head -1
echo -n "compiled binary present: "
if [ -x "$site/current/app" ]; then echo "OK ($(file -b "$site/current/app" 2>/dev/null | cut -d, -f1))"; else echo FAIL; head -c 1200 /tmp/d.json; fi
echo -n "binary is owned by the site user: "; stat -c %U $site/current/app 2>/dev/null

sec "SET GO RUNTIME"
cat >/tmp/rt.json <<'EOF'
{"runtime":"go","command":"./app","port":3000,"env":{},"health_path":"/healthz"}
EOF
api -X PUT $base/api/v1/sites/$uid/runtime -H 'Content-Type: application/json' --data @/tmp/rt.json \
  | grep -oE '"(runtime|status)":"[^"]*"'

sec "health + slice placement"
api $base/api/v1/sites/$uid/runtime/health; echo
grep -E '^Slice=' /etc/systemd/system/heropanel-app-hps1.service 2>&1

sec "reload OLS"
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1

sec "*** CURL THE GO APP THROUGH OPENLITESPEED ***"
curl -si -H 'Host: go.test' http://127.0.0.1/ 2>&1 | head -6
echo "--- body ---"
curl -s -H 'Host: go.test' http://127.0.0.1/ 2>&1 | grep -o 'Go app live via HeroPanel pid [0-9]*'

sec "app log tail"
tail -5 /tmp/app-heropanel-app-hps1.log 2>&1
