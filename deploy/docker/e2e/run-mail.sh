#!/usr/bin/env bash
# Phase 7, Mail module: REAL Postfix + Dovecot + OpenDKIM driven end to end
# through the panel — domain + mailbox provisioned via the API, MX/SPF/DKIM/
# DMARC wired into the panel's own BIND zone, a real SMTP session delivering
# through LMTP into the vmail Maildir with a DKIM signature attached, IMAP
# reading it back against the BLF-CRYPT credential, aliases, suspension
# semantics (no login, still receives), the queue view on a genuinely
# deferred message, and per-mailbox quota through doveadm.
set -u
sec(){ echo; echo "======== $* ========"; }
pass(){ echo "PASS: $*"; }
fail(){ echo "FAIL: $*"; FAILED=1; }
FAILED=0
base=http://127.0.0.1:18488

sec "start BIND9 — the mail domain's records must resolve from REAL DNS"
mkdir -p /run/named && chown bind:bind /run/named 2>/dev/null || true
/usr/sbin/named -u bind 2>/tmp/named-mail.log || true
sleep 1

sec "seed postfix base config (the image installed it unconfigured)"
cp /usr/share/postfix/main.cf.debian /etc/postfix/main.cf
# smtp_connect_timeout is shortened so the queue test's blackhole delivery
# (TEST-NET address) defers in seconds instead of postfix's default 30s.
postconf -e "myhostname=mail.shop.test" "mydestination=localhost" "inet_interfaces=loopback-only" \
  "smtp_connect_timeout=3s"

sec "start hp-broker + hpd (sqlite, HP_SECRET_KEY so DKIM keys can be sealed)"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel
export HP_BROKER_TOKEN=tok
export HP_SECRET_KEY=$(head -c32 /dev/urandom | base64 -w0)
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 HP_BROKER_PANEL_USER=root \
  hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker-mail.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18488 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp-mail.db \
  HP_MAIL_RESOLVER=127.0.0.1:53 \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd-mail.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "auth"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/cm.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/cm.txt)
api(){ curl -s -b /tmp/cm.txt -H "X-CSRF-Token: $CSRF" "$@"; }
jget(){ python3 -c "import json,sys; d=json.load(sys.stdin)['data']; print(eval(sys.argv[1]))" "$1"; }

sec "create the DNS zone shop.test — mail records must auto-wire into it"
api -X POST $base/api/v1/dns/zones -H 'Content-Type: application/json' \
  -d '{"name":"shop.test","primary_ns":"ns1.shop.test","admin_email":"admin@shop.test","ns_ip":"127.0.0.1"}' >/dev/null
zuid=$(api $base/api/v1/dns/zones | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
# A blackhole host (TEST-NET, never routable) for the queue test: mail to it
# resolves fine and then genuinely defers on connect timeout.
api -X POST $base/api/v1/dns/zones/$zuid/records -H 'Content-Type: application/json' \
  -d '{"name":"blackhole","type":"A","content":"203.0.113.99"}' >/dev/null

sec "*** CREATE THE MAIL DOMAIN: PROVISION + DKIM + DNS WIRING, ONE CALL ***"
dom=$(api -X POST $base/api/v1/mail/domains -H 'Content-Type: application/json' -d '{"domain":"shop.test"}')
echo "$dom"
duid=$(echo "$dom" | jget "d['uid']")
echo "$dom" | grep -q '"dkim_public":"v=DKIM1; k=rsa; p=' \
  && pass "a DKIM key pair was generated (public half returned)" \
  || fail "no DKIM public record on the domain"
grep -q '"dkim_private"' <<<"$dom" && fail "THE PRIVATE KEY LEAKED INTO THE API" || pass "the private key is not in the API response"

grep -q 'shop.test OK' /etc/postfix/heropanel/domains && pass "postfix virtual domains map rendered" \
  || fail "domains map missing shop.test"
[ -f /etc/postfix/heropanel/domains.db ] && pass "postmap built the hash map" || fail "no domains.db"
[ -f /etc/dovecot/conf.d/95-heropanel.conf ] && pass "dovecot drop-in written" || fail "no dovecot drop-in"
id vmail >/dev/null 2>&1 && pass "the vmail user exists" || fail "no vmail user"
key=/etc/opendkim/heropanel/keys/shop.test/hp1.private
[ -f "$key" ] && pass "the DKIM private key reached opendkim" || fail "no DKIM key file"
[ "$(stat -c %a "$key" 2>/dev/null)" = "600" ] && pass "the key file is private (0600)" || fail "key mode $(stat -c %a "$key" 2>/dev/null)"
db_priv=$(python3 -c "import sqlite3;print(sqlite3.connect('/tmp/hp-mail.db').execute('select dkim_private from mail_domains').fetchone()[0][:30])")
case "$db_priv" in *"BEGIN RSA"*) fail "THE DKIM KEY IS PLAINTEXT AT REST" ;; *) pass "the DKIM key is SEALED at rest (db holds ciphertext)" ;; esac

sec "*** THE DNS CHECK: MX/SPF/DKIM/DMARC RESOLVE FROM THE LIVE ZONE ***"
dnsres=$(api $base/api/v1/mail/domains/$duid/dns)
echo "$dnsres" | python3 -m json.tool | head -40
found=$(echo "$dnsres" | python3 -c "import json,sys; rs=json.load(sys.stdin)['data']['records']; print(sum(1 for r in rs if r['found']), len(rs))")
[ "$found" = "4 4" ] && pass "all 4 records (MX, SPF, DKIM, DMARC) FOUND in live DNS" \
  || fail "dns check reported $found"
# The record and the key must be the same pair. opendkim-testkey's libunbound
# resolver does full recursion and cannot be pointed at the local BIND, so the
# correspondence is proven directly: the public half derived from the PRIVATE
# key file must be byte-identical to the p= value served by DNS.
priv_pub=$(openssl rsa -in "$key" -pubout -outform DER 2>/dev/null | base64 -w0)
dns_p=$(dig @127.0.0.1 hp1._domainkey.shop.test TXT +short | tr -d '" ' | grep -oE 'p=[A-Za-z0-9+/=]+' | cut -c3-)
[ -n "$priv_pub" ] && [ "$priv_pub" = "$dns_p" ] \
  && pass "the DNS-published DKIM key IS the public half of the signing key (byte-identical)" \
  || fail "the DNS p= value does not match the private key's public half"

sec "create the mailbox info@shop.test"
box=$(api -X POST $base/api/v1/mail/domains/$duid/accounts -H 'Content-Type: application/json' \
  -d '{"local_part":"info","password":"s3cretpass1","quota_mb":64}')
echo "$box"
buid=$(echo "$box" | jget "d['uid']")
grep -q 'info@shop.test:{BLF-CRYPT}\$2' /etc/dovecot/heropanel-users \
  && pass "dovecot passwd-file carries a BLF-CRYPT hash (never the password)" \
  || fail "users file wrong: $(cat /etc/dovecot/heropanel-users)"
grep -q 'storage=64M' /etc/dovecot/heropanel-users && pass "the quota rides in the passwd-file (64M)" || fail "quota missing"

sec "start dovecot + postfix (opendkim already started by the broker's apply)"
dovecot 2>/tmp/dovecot.log || true
postfix start 2>/tmp/postfix-start.log || true
sleep 2
pgrep -x opendkim >/dev/null && pass "opendkim is running" || fail "opendkim is not running"
pgrep -x master >/dev/null && pass "postfix is running" || fail "postfix is not running"
pgrep -x dovecot >/dev/null && pass "dovecot is running" || fail "dovecot is not running"

sec "*** SEND A REAL MAIL OVER SMTP; IT MUST LAND IN THE MAILDIR, DKIM-SIGNED ***"
python3 - <<'EOF'
import smtplib
s = smtplib.SMTP("127.0.0.1", 25, timeout=20)
s.sendmail("info@shop.test", ["info@shop.test"],
  "From: info@shop.test\r\nTo: info@shop.test\r\nSubject: e2e-proof\r\n\r\nhello from the e2e\r\n")
s.quit()
print("smtp: accepted")
EOF
msg=""
for i in $(seq 1 40); do
  msg=$(ls /var/lib/heropanel/mail/shop.test/info/Maildir/new/ 2>/dev/null | head -1)
  [ -n "$msg" ] && break; sleep 0.5
done
[ -n "$msg" ] && pass "the message was delivered into the vmail Maildir (LMTP)" || fail "no message in the Maildir"
mfile="/var/lib/heropanel/mail/shop.test/info/Maildir/new/$msg"
grep -q 'Subject: e2e-proof' "$mfile" 2>/dev/null && pass "the delivered mail carries the sent subject" || fail "wrong content"
grep -q 'DKIM-Signature:.*d=shop.test.*s=hp1' "$mfile" 2>/dev/null \
  && pass "THE STORED MAIL IS DKIM-SIGNED (d=shop.test s=hp1)" \
  || { grep -q 'DKIM-Signature' "$mfile" 2>/dev/null && pass "the stored mail is DKIM-signed" || fail "no DKIM signature: $(head -5 "$mfile" 2>/dev/null)"; }
[ "$(stat -c %U "$mfile")" = "vmail" ] && pass "the Maildir belongs to vmail" || fail "maildir owner $(stat -c %U "$mfile")"

sec "*** IMAP READS IT BACK WITH THE MAILBOX CREDENTIAL ***"
curl -s --url "imap://127.0.0.1/INBOX;MAILINDEX=1" -u "info@shop.test:s3cretpass1" | grep -q "e2e-proof" \
  && pass "IMAP login + fetch works against the BLF-CRYPT credential" \
  || fail "IMAP could not read the message back"

sec "alias sales@ -> info@ delivers into info's mailbox"
api -X POST $base/api/v1/mail/domains/$duid/aliases -H 'Content-Type: application/json' \
  -d '{"source":"sales","destination":"info@shop.test"}' >/dev/null
python3 - <<'EOF'
import smtplib
s = smtplib.SMTP("127.0.0.1", 25, timeout=20)
s.sendmail("info@shop.test", ["sales@shop.test"],
  "From: info@shop.test\r\nTo: sales@shop.test\r\nSubject: via-alias\r\n\r\nalias hop\r\n")
s.quit()
EOF
ok=""
for i in $(seq 1 40); do
  grep -rq 'via-alias' /var/lib/heropanel/mail/shop.test/info/Maildir/new/ 2>/dev/null && ok=1 && break
  sleep 0.5
done
[ -n "$ok" ] && pass "mail to the alias landed in the destination mailbox" || fail "alias mail never arrived"

sec "quota through doveadm"
usage=$(api $base/api/v1/mail/domains/$duid/usage)
echo "$usage"
echo "$usage" | grep -q '"known":true' && pass "doveadm reports the mailbox usage" || fail "usage unknown"

sec "*** SUSPENSION: NO LOGIN, STILL RECEIVES ***"
api -X PUT $base/api/v1/mail/accounts/$buid/status -H 'Content-Type: application/json' -d '{"status":"suspended"}' >/dev/null
grep -q 'info@shop.test' /etc/dovecot/heropanel-users && fail "suspended account still in the passwd-file" \
  || pass "the suspended account left the passwd-file"
curl -s --max-time 10 --url "imap://127.0.0.1/INBOX" -u "info@shop.test:s3cretpass1" >/dev/null 2>&1 \
  && fail "A SUSPENDED MAILBOX LOGGED IN" || pass "IMAP login is refused while suspended"
before=$(ls /var/lib/heropanel/mail/shop.test/info/Maildir/new/ | wc -l)
python3 - <<'EOF'
import smtplib
s = smtplib.SMTP("127.0.0.1", 25, timeout=20)
s.sendmail("x@shop.test", ["info@shop.test"],
  "Subject: while-suspended\r\n\r\nstill delivered\r\n")
s.quit()
EOF
ok=""
for i in $(seq 1 40); do
  [ "$(ls /var/lib/heropanel/mail/shop.test/info/Maildir/new/ | wc -l)" -gt "$before" ] && ok=1 && break
  sleep 0.5
done
[ -n "$ok" ] && pass "mail STILL DELIVERS while suspended (suspend blocks logins, not receipt)" \
  || fail "suspension bounced incoming mail"
api -X PUT $base/api/v1/mail/accounts/$buid/status -H 'Content-Type: application/json' -d '{"status":"active"}' >/dev/null

sec "*** THE QUEUE: A GENUINELY DEFERRED MESSAGE, VIEWED AND DELETED ***"
python3 - <<'EOF'
import smtplib
s = smtplib.SMTP("127.0.0.1", 25, timeout=20)
s.sendmail("info@shop.test", ["nobody@blackhole.shop.test"],
  "Subject: will-defer\r\n\r\ngoing nowhere\r\n")
s.quit()
EOF
qid=""
for i in $(seq 1 60); do
  q=$(api $base/api/v1/mail/queue)
  qid=$(echo "$q" | python3 -c "import json,sys; ms=[m for m in json.load(sys.stdin)['data']['messages'] if m['queue']=='deferred']; print(ms[0]['id'] if ms else '')" 2>/dev/null)
  [ -n "$qid" ] && break; sleep 0.5
done
echo "$q"
[ -n "$qid" ] && pass "the queue view shows the deferred message ($qid)" || fail "queue view empty"
echo "$q" | grep -q '"running":true' && pass "the queue reports postfix running" || fail "running flag wrong"
del=$(api -X POST $base/api/v1/mail/queue/delete -H 'Content-Type: application/json' -d "{\"ids\":[\"$qid\"]}")
echo "$del" | grep -q '"deleted":1' && pass "the queued message was deleted by ID" || fail "queue delete failed: $del"

sec "audit chain"
for cap in mail.provision mail.apply mail.dkim.apply mail.queue.list mail.queue.delete; do
  grep -q "\"capability\":\"$cap\",\"outcome\":\"success\"" /tmp/broker-mail.log \
    && pass "$cap is on the broker's audit chain" || fail "$cap missing from the audit chain"
done

sec "cleanup"
pkill -f 'hpd' 2>/dev/null; pkill -f 'hp-broker' 2>/dev/null
postfix stop 2>/dev/null; doveadm stop 2>/dev/null; pkill -x opendkim 2>/dev/null; true

if [ "$FAILED" = "0" ]; then echo "run-mail.sh : PASS"; else echo "run-mail.sh : FAIL"; fi
