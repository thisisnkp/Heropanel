#!/usr/bin/env bash
# Real PHP: HeroPanel provisions a PHP site whose vhost talks FastCGI to a
# per-site php-fpm pool; OpenLiteSpeed then serves a live PHP page.
set -u
sec(){ echo; echo "======== $* ========"; }

sec "start OpenLiteSpeed + php-fpm 8.3"
/usr/local/lsws/bin/lswsctrl start >/dev/null 2>&1
mkdir -p /run/php /run/heropanel/fpm && chmod 755 /run/heropanel /run/heropanel/fpm
/usr/sbin/php-fpm8.3 --daemonize 2>/tmp/fpm.log
sleep 1
echo "php-fpm master: $(cat /run/php/php8.3-fpm.pid 2>/dev/null || echo none)"

sec "start hp-broker + hpd"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel /srv/heropanel/sites
export HP_BROKER_TOKEN=tok
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18443 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp.db \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd.log 2>&1 &
for i in $(seq 1 60); do curl -sf http://127.0.0.1:18443/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "auth"
curl -s -X POST http://127.0.0.1:18443/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST http://127.0.0.1:18443/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }

sec "CREATE PHP SITE (useradd + install -d + php.write_pool + fcgi vhost)"
api -X POST http://127.0.0.1:18443/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"App","primary_domain":"php.test","type":"php"}'; echo
echo "-- status --"; api http://127.0.0.1:18443/api/v1/sites | head -c 400; echo
echo "-- audit --"; grep -oE '"capability":"(php.write_pool|webserver.apply)","outcome":"[^"]+"' /tmp/broker.log

sec "generated php-fpm pool + vhost"
echo "--- /etc/php/8.3/fpm/pool.d/hps1.conf ---"; sed -n '1,12p' /etc/php/8.3/fpm/pool.d/hps1.conf 2>&1
echo "--- fpm sockets ---"; ls -la /run/heropanel/fpm/ 2>&1
echo "--- generated OLS config (extProcessor + scriptHandler) ---"; grep -A5 -E "extProcessor|scriptHandler" /usr/local/lsws/conf/heropanel.conf 2>&1 | head -18

sec "demo perms + index.php, reload OLS"
chmod o+x /srv/heropanel/sites/1 && chmod o+rx /srv/heropanel/sites/1/public && chmod o+rwx /srv/heropanel/sites/1/logs
chmod 0666 /run/heropanel/fpm/hps1.sock 2>/dev/null
printf '<?php echo "PHP ".PHP_VERSION." live via HeroPanel + php-fpm + OpenLiteSpeed\\n"; ?>' \
  > /srv/heropanel/sites/1/public/index.php
chmod o+r /srv/heropanel/sites/1/public/index.php
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1

sec "*** CURL THE LIVE PHP PAGE ***"
curl -si -H 'Host: php.test' http://127.0.0.1/index.php 2>&1 | head -12

sec "OLS error log tail"
tail -8 /usr/local/lsws/logs/error.log 2>&1
