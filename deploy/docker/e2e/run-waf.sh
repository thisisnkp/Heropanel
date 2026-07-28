#!/usr/bin/env bash
# Per-site WAF: HeroPanel toggles ModSecurity + the OWASP Core Rule Set on a
# site's OpenLiteSpeed vhost. The honest proof is behavioural: with the WAF ON a
# real attack request is BLOCKED (403) while a normal request still serves, and
# with the WAF OFF the same attack is allowed through — the CRS is really in the
# request path, not just referenced in a config file.
set -u
sec(){ echo; echo "======== $* ========"; }
pass(){ echo "PASS: $*"; }
fail(){ echo "FAIL: $*"; FAILED=1; }
FAILED=0
base=http://127.0.0.1:18466

sec "start OpenLiteSpeed + php-fpm 8.3"
/usr/local/lsws/bin/lswsctrl start >/dev/null 2>&1
mkdir -p /run/php /run/heropanel/fpm && chmod 755 /run/heropanel /run/heropanel/fpm
/usr/sbin/php-fpm8.3 --daemonize 2>/tmp/fpm-waf.log
sleep 1
[ -f /usr/local/lsws/modules/mod_security.so ] && pass "the OLS ModSecurity module is installed" || fail "no mod_security.so"

sec "start hp-broker + hpd"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel /srv/heropanel/sites
export HP_BROKER_TOKEN=tok
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker-waf.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18466 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp-waf.db \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd-waf.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "auth"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/cwaf.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/cwaf.txt)
api(){ curl -s -b /tmp/cwaf.txt -H "X-CSRF-Token: $CSRF" "$@"; }
juid(){ python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["uid"])'; }

sec "create a static site behind OpenLiteSpeed"
uid=$(api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"WAF","primary_domain":"waf.test","type":"static"}' | juid)
[ -n "$uid" ] && pass "site created ($uid)" || fail "site create failed"
echo '<h1>hello behind the WAF</h1>' > /srv/heropanel/sites/1/public/index.html
chmod o+x /srv/heropanel/sites/1 && chmod o+rx /srv/heropanel/sites/1/public
chmod o+r /srv/heropanel/sites/1/public/index.html
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1

# A representative attack: an SQL-injection probe in the query string, which the
# CRS REQUEST-942 rules score and (over threshold) block. Reused for both phases.
ATTACK="http://127.0.0.1/?id=1%27%20OR%20%271%27%3D%271%27%20--%20"
code(){ curl -s -o /dev/null -w '%{http_code}' -H 'Host: waf.test' "$1"; }

sec "*** WAF OFF: the attack is allowed through (baseline) ***"
normal_off=$(code "http://127.0.0.1/")
attack_off=$(code "$ATTACK")
echo "off: normal=$normal_off attack=$attack_off"
[ "$normal_off" = "200" ] && pass "a normal request serves (200) with the WAF off" || fail "normal request not 200 off"
[ "$attack_off" != "403" ] && pass "the attack is NOT blocked while the WAF is off (baseline: $attack_off)" \
  || fail "the attack was blocked before the WAF was even enabled"

sec "*** ENABLE THE WAF for this site ***"
en=$(api -X PUT "$base/api/v1/sites/$uid/waf" -H 'Content-Type: application/json' -d '{"enabled":true}')
echo "$en"
echo "$en" | grep -q '"waf_enabled":true' && pass "the API reports the WAF enabled" || fail "waf enable failed: $en"
[ -f /etc/heropanel/waf/main.conf ] && pass "the WAF rules file was written" || fail "no WAF rules file"
grep -q 'modsecurity-crs/rules' /etc/heropanel/waf/main.conf && pass "the rules file pulls in the OWASP CRS" || fail "CRS not referenced"
grep -q 'module mod_security' /usr/local/lsws/conf/heropanel.conf && pass "the vhost has the ModSecurity block" || fail "no modsec block in the vhost"
sleep 1

sec "*** WAF ON: the SAME attack is now BLOCKED (403), normal still serves ***"
normal_on=$(code "http://127.0.0.1/")
attack_on=$(code "$ATTACK")
echo "on: normal=$normal_on attack=$attack_on"
[ "$normal_on" = "200" ] && pass "a normal request STILL serves (200) with the WAF on" || fail "the WAF broke a normal request ($normal_on)"
[ "$attack_on" = "403" ] && pass "THE WAF BLOCKED THE ATTACK (403) — CRS is live in the request path" \
  || fail "the attack was not blocked with the WAF on (got $attack_on)"

sec "*** DISABLE the WAF: the attack is allowed again ***"
api -X PUT "$base/api/v1/sites/$uid/waf" -H 'Content-Type: application/json' -d '{"enabled":false}' >/dev/null
sleep 1
grep -q 'module mod_security' /usr/local/lsws/conf/heropanel.conf && fail "the modsec block survived disabling" \
  || pass "disabling removed the ModSecurity block from the vhost"
attack_dis=$(code "$ATTACK")
[ "$attack_dis" != "403" ] && pass "the attack is allowed again after disabling ($attack_dis)" \
  || fail "the attack is still blocked after disabling"

sec "audit chain"
grep -q '"capability":"waf.provision","outcome":"success"' /tmp/broker-waf.log \
  && pass "waf.provision is on the broker's audit chain" || fail "waf.provision missing from the audit chain"

sec "cleanup"
pkill -f 'hpd' 2>/dev/null; pkill -f 'hp-broker' 2>/dev/null; /usr/local/lsws/bin/lswsctrl stop >/dev/null 2>&1; true

if [ "$FAILED" = "0" ]; then echo "run-waf.sh : PASS"; else echo "run-waf.sh : FAIL"; fi
exit "$FAILED"
