#!/usr/bin/env bash
# Webmail (Roundcube) served by the panel's OWN OpenLiteSpeed + PHP against the
# LOCAL Dovecot/Postfix — the same host that carries the mailboxes. One API call
# installs Roundcube's runtime (dedicated user, FPM pool, config, sqlite schema)
# and re-renders the OLS config so a `webmail.<host>` vhost starts serving; then
# a real mailbox user logs in THROUGH Roundcube, which authenticates against
# Dovecot over TLS. Nothing here handles a mailbox password: the panel only
# wires the client to the local MTAs.
set -u
sec(){ echo; echo "======== $* ========"; }
pass(){ echo "PASS: $*"; }
fail(){ echo "FAIL: $*"; FAILED=1; }
FAILED=0
base=http://127.0.0.1:18499

sec "start OpenLiteSpeed + php-fpm 8.3"
/usr/local/lsws/bin/lswsctrl start >/dev/null 2>&1
mkdir -p /run/php /run/nexpanel/fpm && chmod 755 /run/nexpanel /run/nexpanel/fpm
/usr/sbin/php-fpm8.3 --daemonize 2>/tmp/fpm-wm.log
sleep 1

sec "seed postfix base config"
cp /usr/share/postfix/main.cf.debian /etc/postfix/main.cf
postconf -e "myhostname=mail.shop.test" "mydestination=localhost" "inet_interfaces=loopback-only"

sec "start np-broker + npd (mail host + webmail host configured)"
install -m0755 /np/npd /np/np-broker /usr/local/bin/
mkdir -p /run/nexpanel /srv/nexpanel/sites
export NP_BROKER_TOKEN=tok
NP_LOG_FORMAT=text NP_BROKER_ALLOWED_UID=0 NP_BROKER_PANEL_USER=root \
  np-broker --serve --socket /run/nexpanel/broker.sock >/tmp/broker-wm.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/nexpanel/broker.sock ] && break; sleep 0.2; done
NP_SERVER_HOST=127.0.0.1 NP_SERVER_PORT=18499 NP_LOG_FORMAT=text \
  NP_DATABASE_DRIVER=sqlite NP_DATABASE_DSN=/tmp/np-wm.db \
  NP_MAIL_HOSTNAME=mail.shop.test \
  NP_WEBMAIL_HOSTNAME=webmail.shop.test \
  NP_BROKER_SOCKET=/run/nexpanel/broker.sock npd >/tmp/npd-wm.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "auth"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/cw.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/np_csrf/{print $7}' /tmp/cw.txt)
api(){ curl -s -b /tmp/cw.txt -H "X-CSRF-Token: $CSRF" "$@"; }
jget(){ python3 -c "import json,sys; d=json.load(sys.stdin)['data']; print(eval(sys.argv[1]))" "$1"; }

sec "provision the mail domain + mailbox (mail TLS self-signs for the host)"
dom=$(api -X POST $base/api/v1/mail/domains -H 'Content-Type: application/json' -d '{"domain":"shop.test"}')
duid=$(echo "$dom" | jget "d['uid']")
[ -n "$duid" ] && pass "mail domain provisioned" || fail "mail domain create failed: $dom"
api -X POST $base/api/v1/mail/domains/$duid/accounts -H 'Content-Type: application/json' \
  -d '{"local_part":"info","password":"s3cretpass1","quota_mb":64}' >/dev/null
grep -q 'info@shop.test:{BLF-CRYPT}' /etc/dovecot/nexpanel-users && pass "mailbox created" || fail "mailbox missing"

sec "enable mail TLS explicitly (Roundcube connects over TLS)"
tls=$(api -X POST $base/api/v1/mail/tls -H 'Content-Type: application/json' -d '{}')
echo "$tls"
echo "$tls" | grep -q '"enabled":true' && pass "mail TLS wired for the host" || fail "mail TLS failed: $tls"

sec "start dovecot + postfix (mail TLS was wired at domain create)"
dovecot 2>/tmp/dovecot-wm.log || true
postfix start 2>/tmp/postfix-wm.log || true
sleep 2
pgrep -x dovecot >/dev/null && pass "dovecot is running" || fail "dovecot is not running"
# imaps/143-STARTTLS must be up for Roundcube to authenticate. `-starttls imap`
# consumes the plaintext greeting internally, so the honest signal is that the
# handshake completes and the host cert is presented (the login below is the
# true end-to-end proof).
echo 'a LOGOUT' | openssl s_client -starttls imap -connect 127.0.0.1:143 2>/dev/null | grep -q "BEGIN CERTIFICATE" \
  && pass "dovecot completes STARTTLS on 143 (Roundcube's IMAP path)" || fail "no STARTTLS handshake on 143"

sec "*** INSTALL WEBMAIL: one API call lays down the whole runtime ***"
inst=$(api -X POST $base/api/v1/webmail/install -H 'Content-Type: application/json' -d '{}')
echo "$inst"
echo "$inst" | grep -q '"installed":true' && pass "the API reports webmail installed" || fail "install did not report success: $inst"
echo "$inst" | grep -q '"url":"https://webmail.shop.test/"' && pass "the webmail URL is reported" || fail "no webmail url"
id webmail >/dev/null 2>&1 && pass "the dedicated webmail user exists" || fail "no webmail user"
[ -f /usr/share/nexpanel/roundcube/config/config.inc.php ] && pass "the Roundcube config was written" || fail "no roundcube config"
grep -q "imap_host'] = 'tls://127.0.0.1:143'" /usr/share/nexpanel/roundcube/config/config.inc.php \
  && pass "Roundcube is wired to the LOCAL Dovecot over TLS" || fail "roundcube imap_host wrong"
grep -q "smtp_host'] = 'tls://127.0.0.1:587'" /usr/share/nexpanel/roundcube/config/config.inc.php \
  && pass "Roundcube sends through the LOCAL submission (587)" || fail "roundcube smtp_host wrong"
[ -f /var/lib/nexpanel/webmail/roundcube.db ] && pass "the sqlite schema was initialised" || fail "no roundcube.db"
[ -f /etc/php/8.3/fpm/pool.d/webmail.conf ] && pass "the webmail FPM pool was written" || fail "no webmail pool"
grep -q 'webmail' /usr/local/lsws/conf/nexpanel.conf && pass "the webmail vhost is in the OLS config" || fail "no webmail vhost in OLS"

sec "make the FPM socket reachable by OLS (container-only; prod shares a group) + reload"
chmod 0666 /run/nexpanel/fpm/webmail.sock 2>/dev/null
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1

sec "*** SERVE: the Roundcube login page loads over the webmail vhost ***"
page=$(curl -s -c /tmp/rc.txt -H 'Host: webmail.shop.test' http://127.0.0.1/ 2>&1)
echo "$page" | grep -qiE 'roundcube|rcmloginuser|_token' \
  && pass "OpenLiteSpeed serves Roundcube's login page (PHP + FPM working)" \
  || { echo "$page" | head -c 400; fail "the webmail login page did not render"; }


sec "*** LOGIN THROUGH ROUNDCUBE: IMAP auth reaches Dovecot ***"
# Roundcube's login is CSRF-protected and regenerates its session on submit, so
# each attempt takes a FRESH cookie jar + token. Success is unambiguous: Roundcube
# sets the roundcube_sessauth cookie ONLY once a login authenticates. This proves
# the whole chain (OLS -> PHP -> Roundcube -> Dovecot over TLS) with a real mailbox
# password the panel never stored. A short retry loop absorbs the session-token
# raciness of driving that form over raw curl.
rc_login(){ # rc_login <jar> <user> <pass>  -> prints AUTH on success
  local jar="$1" user="$2" pass="$3" tok
  rm -f "$jar"
  tok=$(curl -s -c "$jar" -H 'Host: webmail.shop.test' http://127.0.0.1/ \
        | grep -oE 'name="_token" value="[^"]+"' | head -1 | sed -E 's/.*value="([^"]+)".*/\1/')
  [ -n "$tok" ] || return 1
  curl -s -o /dev/null -b "$jar" -c "$jar" -H 'Host: webmail.shop.test' -H 'Referer: http://webmail.shop.test/' \
    --data-urlencode "_token=$tok" --data-urlencode "_task=login" --data-urlencode "_action=login" \
    --data-urlencode "_url=" --data-urlencode "_user=$user" --data-urlencode "_pass=$pass" \
    "http://127.0.0.1/?_task=login&_action=login"
  grep -q 'roundcube_sessauth' "$jar" && echo AUTH
}
authed=""
for i in $(seq 1 4); do
  [ "$(rc_login /tmp/rc.txt info@shop.test s3cretpass1)" = "AUTH" ] && authed=1 && break
  sleep 1
done
[ -n "$authed" ] && pass "LOGGED IN THROUGH ROUNDCUBE - IMAP auth against Dovecot succeeded" \
  || fail "could not log in through Roundcube"

sec "a wrong password is refused (the auth is real, not a stub)"
badauth=""
for i in $(seq 1 3); do
  [ "$(rc_login /tmp/rc2.txt info@shop.test wrongpass)" = "AUTH" ] && badauth=1 && break
done
[ -z "$badauth" ] && pass "a wrong password is refused by Roundcube/Dovecot" \
  || fail "a WRONG password authenticated through Roundcube"

sec "audit chain"
for cap in webmail.install php.write_pool webserver.apply; do
  grep -q "\"capability\":\"$cap\",\"outcome\":\"success\"" /tmp/broker-wm.log \
    && pass "$cap is on the broker's audit chain" || fail "$cap missing from the audit chain"
done

sec "cleanup"
pkill -f 'npd' 2>/dev/null; pkill -f 'np-broker' 2>/dev/null
postfix stop 2>/dev/null; doveadm stop 2>/dev/null; /usr/local/lsws/bin/lswsctrl stop >/dev/null 2>&1; true

if [ "$FAILED" = "0" ]; then echo "run-webmail.sh : PASS"; else echo "run-webmail.sh : FAIL"; fi
exit "$FAILED"
