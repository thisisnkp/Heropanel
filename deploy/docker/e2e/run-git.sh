#!/usr/bin/env bash
# Real Git deployment: HeroPanel points a site at a public repo, clones + builds
# it as the site's own unprivileged Linux user, atomically activates the release,
# and OpenLiteSpeed serves it. Then a second deploy + rollback proves the swap is
# reversible. Everything privileged goes through hp-broker (git.deploy/rollback).
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

sec "CREATE GIT SITE (deploy_mode=git)"
api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"Git","primary_domain":"git.test","type":"static","deploy_mode":"git"}'; echo
uid=$(api $base/api/v1/sites | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
echo "site uid=$uid"

# The build command creates deterministic output regardless of the cloned repo's
# files, so the served bytes prove the clone+build+activate pipeline end to end.
srcjson(){ cat >/tmp/src.json <<EOF
{"repo_url":"https://github.com/octocat/Hello-World.git","branch":"master","web_root":"site","build_command":"mkdir -p site && printf '<h1>$1 via HeroPanel Git</h1>' > site/index.html"}
EOF
}

sec "SET GIT SOURCE + DEPLOY #1 (clone octocat/Hello-World, build as site user)"
srcjson v1
api -X PUT $base/api/v1/sites/$uid/git -H 'Content-Type: application/json' --data @/tmp/src.json; echo
d1=$(api -X POST $base/api/v1/sites/$uid/git/deploy); echo "$d1"
dep1=$(echo "$d1" | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)

sec "release layout"
ls -la $site 2>&1
echo "-- current ->"; readlink $site/current
echo "-- public  ->"; readlink $site/public

sec "perms for OLS(nobody) + reload"
chmod o+x $site $site/releases 2>/dev/null
chmod -R o+rX $site/releases 2>/dev/null
chmod o+rwx $site/logs 2>/dev/null
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1

sec "*** CURL THE LIVE GIT-DEPLOYED PAGE ***"
curl -si -H 'Host: git.test' http://127.0.0.1/ 2>&1 | head -12

sec "DEPLOY #2 (new content, new release)"
srcjson v2
api -X PUT $base/api/v1/sites/$uid/git -H 'Content-Type: application/json' --data @/tmp/src.json >/dev/null
api -X POST $base/api/v1/sites/$uid/git/deploy >/dev/null
chmod o+x $site/releases 2>/dev/null; chmod -R o+rX $site/releases 2>/dev/null
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1
echo -n "after deploy #2: "; curl -s -H 'Host: git.test' http://127.0.0.1/

sec "ROLLBACK to deploy #1"
api -X POST $base/api/v1/sites/$uid/git/rollback/$dep1; echo
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1
echo -n "after rollback:  "; curl -s -H 'Host: git.test' http://127.0.0.1/

sec "deployment history"
api $base/api/v1/sites/$uid/git/deployments | head -c 700; echo

sec "broker audit (git.deploy / git.rollback outcomes)"
grep -oE '"capability":"git\.(deploy|rollback)","outcome":"[^"]+"' /tmp/broker.log

sec "OLS error log tail"
tail -6 /usr/local/lsws/logs/error.log 2>&1
