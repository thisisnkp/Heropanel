#!/usr/bin/env bash
# Real app runtime: HeroPanel provisions a `proxy` site, git-deploys it, runs the
# app as a supervised per-site process (systemd unit, here via the systemctl
# shim), and OpenLiteSpeed reverse-proxies the domain to it. The response is
# generated dynamically by the Node process, proving the full proxy path.
set -u
sec(){ echo; echo "======== $* ========"; }
base=http://127.0.0.1:18443
site=/srv/heropanel/sites/1

sec "start OpenLiteSpeed"
/usr/local/lsws/bin/lswsctrl start >/dev/null 2>&1

sec "start hp-broker + hpd"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel /srv/heropanel/sites
export HP_BROKER_TOKEN=tok
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18443 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp.db \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "auth"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }

sec "CREATE PROXY SITE (type=proxy, deploy_mode=git)"
api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"App","primary_domain":"app.test","type":"proxy","deploy_mode":"git"}'; echo
uid=$(api $base/api/v1/sites | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
echo "site uid=$uid"

sec "GIT DEPLOY (clone the repo into the release the app runs from)"
api -X PUT $base/api/v1/sites/$uid/git -H 'Content-Type: application/json' \
  -d '{"repo_url":"https://github.com/octocat/Hello-World.git","branch":"master","web_root":"","build_command":""}' >/dev/null
api -X POST $base/api/v1/sites/$uid/git/deploy >/dev/null
echo "current -> $(readlink $site/current)"

sec "SET RUNTIME (writes hardened unit, starts Node as the site user, reproxies vhost)"
# The app serves /healthz, so the panel can verify it is actually up rather than
# trusting systemd's "started".
cat >/tmp/rt.json <<'EOF'
{"runtime":"node","command":"node -e \"require('http').createServer((q,r)=>r.end('HeroPanel app live on port '+process.env.PORT+' pid '+process.pid)).listen(process.env.PORT,'127.0.0.1')\"","port":3000,"env":{"NODE_ENV":"production"},"health_path":"/healthz"}
EOF
api -X PUT $base/api/v1/sites/$uid/runtime -H 'Content-Type: application/json' --data @/tmp/rt.json; echo

sec "*** HEALTH CHECK — the panel probes the app itself ***"
api $base/api/v1/sites/$uid/runtime/health; echo

sec "generated systemd unit"
sed -n '1,20p' /etc/systemd/system/heropanel-app-hps1.service 2>&1
echo "-- launcher --"; cat $site/.heropanel-run 2>&1
echo "-- app listening? --"; sleep 1; (curl -s http://127.0.0.1:3000/ && echo) || echo "(direct curl failed)"

sec "generated OLS proxy vhost"
grep -A3 -E "extProcessor proxy_|context /" /usr/local/lsws/conf/heropanel.conf 2>&1 | head -16
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1

sec "*** CURL THE DOMAIN — OLS REVERSE-PROXIES TO THE NODE APP ***"
curl -si -H 'Host: app.test' http://127.0.0.1/ 2>&1 | head -12

sec "RESTART the app (new pid), curl again"
api -X POST $base/api/v1/sites/$uid/runtime/restart; echo
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1
echo -n "after restart: "; curl -s -H 'Host: app.test' http://127.0.0.1/; echo

sec "*** A CRASHING APP MUST REPORT error, NOT running ***"
# systemd reporting "started" says nothing about whether the app works. Point the
# runtime at a command that exits immediately: without a probe the panel would
# happily call this green.
cat >/tmp/bad.json <<'EOF'
{"runtime":"node","command":"node -e \"process.exit(1)\"","port":3000,"env":{},"health_path":"/healthz"}
EOF
api -X PUT $base/api/v1/sites/$uid/runtime -H 'Content-Type: application/json' --data @/tmp/bad.json | grep -oE '"status":"[a-z]+"'
echo -n "health of the crashed app: "; api $base/api/v1/sites/$uid/runtime/health | grep -oE '"healthy":(true|false)'

sec "broker audit (app.unit_apply / app.unit_control outcomes)"
grep -oE '"capability":"app\.unit_[a-z]+","outcome":"[^"]+"' /tmp/broker.log

sec "app log"
tail -4 /tmp/app-heropanel-app-hps1.log 2>&1
sec "OLS error log tail"
tail -6 /usr/local/lsws/logs/error.log 2>&1
