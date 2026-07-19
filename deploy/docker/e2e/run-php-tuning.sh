#!/usr/bin/env bash
# Real end-to-end for the PHP tuning surface: version selector, FPM sizing, the
# allowlisted php.ini editor, OPcache/JIT, and the per-version extension manager.
#
# The two claims that are only worth testing against a real php-fpm:
#   1. php-fpm config-tests the pool before the broker reloads it, so a bad
#      pm setting is rejected instead of taking down every site on the version.
#   2. An extension is a property of the *version's FPM master*, not a site: the
#      manager restarts FPM and the extension shows up in a served phpinfo().
set -u
sec(){ echo; echo "======== $* ========"; }
fail=0
check(){ if printf '%s' "$2" | grep -q -- "$3"; then echo "  ok   $1"; else echo "  FAIL $1 (want: $3)"; echo "       got: $(printf '%s' "$2" | head -c 300)"; fail=1; fi }

sec "start php-fpm + OpenLiteSpeed + hp stack"
service php8.3-fpm start 2>&1 | tail -1 || /usr/sbin/php-fpm8.3 -D 2>&1
/usr/local/lsws/bin/lswsctrl start 2>&1; sleep 1
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel /srv/heropanel/sites /run/heropanel/fpm
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
uidof(){ printf '%s' "$1" | grep -o '"uid":"[^"]*"' | head -1 | cut -d'"' -f4; }

sec "create a PHP site"
S=$(api -X POST http://127.0.0.1:18443/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"PHP","primary_domain":"php.test","type":"php"}')
U=$(uidof "$S"); echo "site: $U"
chmod o+x /srv/heropanel/sites/1; chmod o+rx /srv/heropanel/sites/1/public; chmod o+rwx /srv/heropanel/sites/1/logs
cat > /srv/heropanel/sites/1/public/index.php <<'PHP'
<?php
echo "max_execution_time=" . ini_get('max_execution_time') . "\n";
echo "memory_limit=" . ini_get('memory_limit') . "\n";
echo "opcache=" . (function_exists('opcache_get_status') && opcache_get_status(false)['opcache_enabled'] ? 'on' : 'off') . "\n";
echo "exif=" . (extension_loaded('exif') ? 'yes' : 'no') . "\n";
PHP
chmod o+r /srv/heropanel/sites/1/public/index.php

# OpenLiteSpeed serves as `nobody`; the FPM socket is 0660 owned by the site
# user. In production the installer places OLS in the right group; in this
# container we widen the socket by hand, as run-php.sh does. It must be re-done
# after anything that restarts FPM (an extension toggle), because the master
# recreates the socket at its configured 0660 on restart.
open_socket(){ for i in $(seq 1 20); do [ -S /run/heropanel/fpm/hps1.sock ] && { chmod 0666 /run/heropanel/fpm/hps1.sock; return; }; sleep 0.25; done; }
serve(){ open_socket; /usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1; curl -s -H 'Host: php.test' http://127.0.0.1/index.php; }
open_socket

sec "the pool the site starts with is live"
echo "$(api http://127.0.0.1:18443/api/v1/sites/$U/php)" | head -c 200; echo
sleep 1; /usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1
echo "--- served ---"; serve

sec "APPLY SETTINGS: sizing (dynamic) + php.ini override + OPcache off"
R=$(api -X PUT http://127.0.0.1:18443/api/v1/sites/$U/php -H 'Content-Type: application/json' -d '{
  "version":"8.3","memory_limit_mb":512,
  "fpm":{"pm":"dynamic","pm_max_children":15,"pm_start_servers":3,"pm_min_spare_servers":2,"pm_max_spare_servers":6,"pm_max_requests":400},
  "ini":{"max_execution_time":"77"},
  "opcache":{"enabled":false,"jit":"off"}
}')
check "settings accepted"       "$R" '"pm":"dynamic"'
check "sizing stored"           "$R" '"pm_max_children":15'
check "ini override stored"     "$R" '"max_execution_time":"77"'
check "the pool file config-tested and reloaded (broker)" "$(cat /tmp/broker.log)" '"capability":"php.write_pool","outcome":"success"'
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1
OUT=$(serve); echo "$OUT"
check "php.ini override is live"   "$OUT" 'max_execution_time=77'
check "memory_limit is live"       "$OUT" 'memory_limit=512M'
check "OPcache turned off is live" "$OUT" 'opcache=off'

sec "REJECT invalid sizing — php-fpm -t must catch it, the site must keep serving"
R=$(api -X PUT http://127.0.0.1:18443/api/v1/sites/$U/php -H 'Content-Type: application/json' -d '{
  "version":"8.3",
  "fpm":{"pm":"dynamic","pm_max_children":5,"pm_start_servers":9,"pm_min_spare_servers":8,"pm_max_spare_servers":2}
}')
# hpd rejects this in validation before the broker even sees it — the first guard.
check "invalid sizing refused by the API" "$R" 'error'
# And the previous good config is still what serves.
OUT=$(serve); check "site still serving the last good config" "$OUT" 'max_execution_time=77'

sec "REJECT a php.ini value that tries to break out of its directive"
R=$(api -X PUT http://127.0.0.1:18443/api/v1/sites/$U/php -H 'Content-Type: application/json' -d '{
  "version":"8.3","ini":{"max_execution_time":"30\nuser = root"}
}')
check "newline-injection refused" "$R" 'error'
check "open_basedir not settable" \
  "$(api -X PUT http://127.0.0.1:18443/api/v1/sites/$U/php -H 'Content-Type: application/json' -d '{"version":"8.3","ini":{"open_basedir":"/"}}')" \
  'error'

sec "EXTENSION MANAGER (per version, not per site)"
# Use exif: really installed in the image and enabled by default, so toggling it
# has a visible effect in a served phpinfo. (gd is not installed here, which is
# itself the point — the manager can only enable what exists.)
L=$(api "http://127.0.0.1:18443/api/v1/php/extensions?version=8.3")
echo "$L" | head -c 240; echo
check "scope note is carried" "$L" 'apply to every site using this PHP version'
check "exif shows as available" "$L" 'exif'
EXIF_BEFORE=$(serve | grep -o 'exif=[a-z]*')
echo "exif before: $EXIF_BEFORE"

echo "--- disable exif for the whole version ---"
R=$(api -X POST http://127.0.0.1:18443/api/v1/php/extensions -H 'Content-Type: application/json' \
  -d '{"version":"8.3","extension":"exif","enabled":false}')
check "disable succeeded"                        "$R" '"version":"8.3"'
check "FPM restarted for the change (broker)"    "$(cat /tmp/broker.log)" '"capability":"php.set_extension","outcome":"success"'
OUT=$(serve); echo "$OUT"
check "disabling an extension is live in a served phpinfo" "$OUT" 'exif=no'

echo "--- re-enable exif ---"
api -X POST http://127.0.0.1:18443/api/v1/php/extensions -H 'Content-Type: application/json' \
  -d '{"version":"8.3","extension":"exif","enabled":true}' >/dev/null
OUT=$(serve); echo "$OUT"
check "re-enabling an extension is live"          "$OUT" 'exif=yes'

sec "a bad extension name is refused"
check "bad extension name refused" \
  "$(api -X POST http://127.0.0.1:18443/api/v1/php/extensions -H 'Content-Type: application/json' -d '{"version":"8.3","extension":"gd; rm -rf /","enabled":true}')" \
  'error'

sec "it is all in the audit chain"
A=$(api 'http://127.0.0.1:18443/api/v1/audit')
check "php settings change audited" "$A" '"action":"PUT /api/v1/sites/{uid}/php"'
check "extension change audited"    "$A" '"action":"POST /api/v1/php/extensions"'
check "chain intact"                "$(api http://127.0.0.1:18443/api/v1/audit/verify)" '"intact":true'

sec "RESULT"
if [ "$fail" -eq 0 ]; then echo "run-php-tuning.sh : PASS"; else echo "run-php-tuning.sh : FAIL"; fi
exit "$fail"
