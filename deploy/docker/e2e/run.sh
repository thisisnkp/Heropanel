#!/usr/bin/env bash
# Real end-to-end: OpenLiteSpeed serves a HeroPanel-provisioned site, and
# MariaDB gets a real database — all driven through the API + root broker.
set -u
sec(){ echo; echo "======== $* ========"; }

sec "start MariaDB"
mkdir -p /run/mysqld && chown mysql:mysql /run/mysqld
[ -d /var/lib/mysql/mysql ] || mariadb-install-db --user=mysql --datadir=/var/lib/mysql >/dev/null 2>&1
mariadbd --user=mysql >/tmp/mariadb.log 2>&1 &
for i in $(seq 1 40); do mysqladmin ping >/dev/null 2>&1 && break; sleep 0.5; done
echo "mysql: $(mysqladmin ping 2>&1)"

sec "start OpenLiteSpeed"
/usr/local/lsws/bin/lswsctrl start 2>&1
sleep 1

sec "start hp-broker (root, SO_PEERCRED) + hpd"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel /srv/heropanel/sites
export HP_BROKER_TOKEN=tok
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18443 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp.db \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd.log 2>&1 &
for i in $(seq 1 60); do curl -sf http://127.0.0.1:18443/healthz >/dev/null 2>&1 && break; sleep 0.25; done
echo "readyz: $(curl -s http://127.0.0.1:18443/readyz)"

sec "auth (bootstrap + login + CSRF)"
curl -s -X POST http://127.0.0.1:18443/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST http://127.0.0.1:18443/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }

sec "CREATE STATIC SITE  (real webserver.apply -> lshttpd -t + lswsctrl reload)"
api -X POST http://127.0.0.1:18443/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"Acme","primary_domain":"acme.test","type":"static"}'; echo
echo "-- site status --"; api http://127.0.0.1:18443/api/v1/sites; echo
echo "-- webserver.apply audit --"; grep -oE '"capability":"webserver.apply","outcome":"[^"]+"' /tmp/broker.log | tail -2

sec "generated OLS config"
echo "--- heropanel.conf ---"; cat /usr/local/lsws/conf/heropanel.conf 2>&1
echo "--- vhconf ---"; cat /usr/local/lsws/conf/vhosts/hps1/vhconf.conf 2>&1

sec "demo perms + index.html, reload OLS"
chmod o+x /srv/heropanel/sites/1
chmod o+rx /srv/heropanel/sites/1/public
chmod o+rwx /srv/heropanel/sites/1/logs
echo '<!doctype html><title>HeroPanel</title><h1>Hello from HeroPanel, served by OpenLiteSpeed</h1>' \
  > /srv/heropanel/sites/1/public/index.html
chmod o+r /srv/heropanel/sites/1/public/index.html
/usr/local/lsws/bin/lswsctrl reload 2>&1; echo "reload_exit=$?"
sleep 1
(ss -tlnp 2>/dev/null || true) | grep -E ':80 ' && echo "OLS is listening on :80" || echo "NOT listening on :80"

sec "*** CURL THE REAL SITE ***"
curl -si -H 'Host: acme.test' http://127.0.0.1/ 2>&1 | head -12

sec "OLS error log tail"
tail -12 /usr/local/lsws/logs/error.log 2>&1

sec "REAL MARIADB: create database via API"
api -X POST http://127.0.0.1:18443/api/v1/databases -H 'Content-Type: application/json' -d '{"name":"acme_db"}'; echo
echo "-- SHOW DATABASES --"; mysql --protocol=socket -e 'SHOW DATABASES;' 2>&1 | grep -iE 'acme|Database'
echo "-- db.create audit --"; grep -oE '"capability":"db.create","outcome":"[^"]+"' /tmp/broker.log | tail -1
