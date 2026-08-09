#!/usr/bin/env bash
# Real end-to-end: OpenLiteSpeed serves a NexPanel-provisioned site, and
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

sec "start np-broker (root, SO_PEERCRED) + npd"
install -m0755 /np/npd /np/np-broker /usr/local/bin/
mkdir -p /run/nexpanel /srv/nexpanel/sites
export NP_BROKER_TOKEN=tok
NP_LOG_FORMAT=text NP_BROKER_ALLOWED_UID=0 np-broker --serve --socket /run/nexpanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/nexpanel/broker.sock ] && break; sleep 0.2; done
NP_SERVER_HOST=127.0.0.1 NP_SERVER_PORT=18443 NP_LOG_FORMAT=text \
  NP_DATABASE_DRIVER=sqlite NP_DATABASE_DSN=/tmp/np.db \
  NP_BROKER_SOCKET=/run/nexpanel/broker.sock npd >/tmp/npd.log 2>&1 &
for i in $(seq 1 60); do curl -sf http://127.0.0.1:18443/healthz >/dev/null 2>&1 && break; sleep 0.25; done
echo "readyz: $(curl -s http://127.0.0.1:18443/readyz)"

sec "auth (bootstrap + login + CSRF)"
curl -s -X POST http://127.0.0.1:18443/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST http://127.0.0.1:18443/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/np_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }

sec "CREATE STATIC SITE  (real webserver.apply -> lshttpd -t + lswsctrl reload)"
api -X POST http://127.0.0.1:18443/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"Acme","primary_domain":"acme.test","type":"static"}'; echo
echo "-- site status --"; api http://127.0.0.1:18443/api/v1/sites; echo
echo "-- webserver.apply audit --"; grep -oE '"capability":"webserver.apply","outcome":"[^"]+"' /tmp/broker.log | tail -2

sec "generated OLS config"
echo "--- nexpanel.conf ---"; cat /usr/local/lsws/conf/nexpanel.conf 2>&1
echo "--- vhconf ---"; cat /usr/local/lsws/conf/vhosts/nps1/vhconf.conf 2>&1

sec "demo perms + index.html, reload OLS"
chmod o+x /srv/nexpanel/sites/1
chmod o+rx /srv/nexpanel/sites/1/public
chmod o+rwx /srv/nexpanel/sites/1/logs
echo '<!doctype html><title>NexPanel</title><h1>Hello from NexPanel, served by OpenLiteSpeed</h1>' \
  > /srv/nexpanel/sites/1/public/index.html
chmod o+r /srv/nexpanel/sites/1/public/index.html
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
