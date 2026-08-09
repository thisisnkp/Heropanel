#!/usr/bin/env bash
# Phase 8, Security suite: the two exit criteria that can be shown live —
# a firewall change that REVERTS ITSELF when it is not confirmed, and a real
# ClamAV scan that finds the EICAR test file and quarantines it out of the
# site tree. WebAuthn is proven by Go integration tests (it is pure crypto,
# with no system daemon), so it is not driven here.
#
# nft is shimmed (real kernel nftables would disturb the container's own
# networking); the shim faithfully models snapshot/apply/revert, which is the
# panel's actual logic. ClamAV is REAL — the suite ships a one-line custom
# signature for EICAR so no virus-database download is needed.
set -u
sec(){ echo; echo "======== $* ========"; }
pass(){ echo "PASS: $*"; }
fail(){ echo "FAIL: $*"; FAILED=1; }
FAILED=0
base=http://127.0.0.1:18499

sec "start np-broker + npd (short firewall window so the auto-revert is observable)"
install -m0755 /np/npd /np/np-broker /usr/local/bin/
mkdir -p /run/nexpanel /srv/nexpanel/sites
export NP_BROKER_TOKEN=tok
export NP_SECRET_KEY=$(head -c32 /dev/urandom | base64 -w0)
NP_LOG_FORMAT=text NP_BROKER_ALLOWED_UID=0 NP_BROKER_PANEL_USER=root \
  np-broker --serve --socket /run/nexpanel/broker.sock >/tmp/broker-sec.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/nexpanel/broker.sock ] && break; sleep 0.2; done

# A local stand-in for the public geo-CIDR mirror, so the country import exercises
# the real HTTP fetch → parse → bulk-store → render path without leaving the box.
mkdir -p /tmp/geo/v4 /tmp/geo/v6
printf '# Testland\n45.66.0.0/16\n45.67.0.0/16\ngarbage\n45.66.0.0/16\n' >/tmp/geo/v4/tl.zone
printf '2a0f:dead::/32\n' >/tmp/geo/v6/tl.zone
( cd /tmp/geo && python3 -m http.server 18477 >/tmp/geo.log 2>&1 & )
for i in $(seq 1 40); do curl -sf http://127.0.0.1:18477/v4/tl.zone >/dev/null 2>&1 && break; sleep 0.2; done

NP_SERVER_HOST=127.0.0.1 NP_SERVER_PORT=18499 NP_LOG_FORMAT=text \
  NP_DATABASE_DRIVER=sqlite NP_DATABASE_DSN=/tmp/np-sec.db \
  NP_FIREWALL_WINDOW_SEC=10 \
  NP_SECURITY_GEODB_URL='http://127.0.0.1:18477/v4/%s.zone' \
  NP_SECURITY_GEODB_URL6='http://127.0.0.1:18477/v6/%s.zone' \
  NP_BROKER_SOCKET=/run/nexpanel/broker.sock npd >/tmp/npd-sec.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "auth"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/cs.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/np_csrf/{print $7}' /tmp/cs.txt)
api(){ curl -s -b /tmp/cs.txt -H "X-CSRF-Token: $CSRF" "$@"; }
juid(){ python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["uid"])'; }

sec "*** FIREWALL: A CHANGE THAT REVERTS ITSELF IF NOT CONFIRMED ***"
api -X POST $base/api/v1/firewall/rules -H 'Content-Type: application/json' \
  -d '{"action":"accept","protocol":"tcp","port":22,"comment":"ssh"}' >/dev/null
applied=$(api -X POST $base/api/v1/firewall/apply -H 'Content-Type: application/json')
echo "$applied"
token=$(echo "$applied" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["token"])')
[ -n "$token" ] && pass "apply armed a pending change (token issued)" || fail "apply did not return a token"
api $base/api/v1/firewall/status | grep -q 'nexpanel' \
  && pass "the new ruleset is live in nft after apply" || fail "the ruleset was not applied"
api $base/api/v1/firewall | grep -q '"pending":true' && pass "the change is pending confirmation" || fail "the change is not pending"

echo "... not confirming; waiting out the 10s window ..."
sleep 14
api $base/api/v1/firewall | grep -q '"pending":false' \
  && pass "the unconfirmed change is no longer pending (guard fired)" || fail "the change is still pending after the window"
if api $base/api/v1/firewall/status | grep -q 'nexpanel'; then
  fail "THE FIREWALL DID NOT AUTO-REVERT — the unconfirmed ruleset is still live"
else
  pass "THE FIREWALL AUTO-REVERTED to the previous ruleset (unconfirmed change undone)"
fi

sec "add an IPv6 rule, a port RANGE, and a geo/IP BLOCK entry, then apply + CONFIRM"
# The table is inet (dual-stack): an IPv6 source must render on ip6 saddr, and a
# port range as dport start-end. A block-list entry becomes an nftables set.
api -X POST $base/api/v1/firewall/rules -H 'Content-Type: application/json' \
  -d '{"action":"accept","protocol":"tcp","port":443,"source":"2001:db8::/32","comment":"v6 web"}' >/dev/null
api -X POST $base/api/v1/firewall/rules -H 'Content-Type: application/json' \
  -d '{"action":"accept","protocol":"udp","port":8000,"port_end":9000,"comment":"media range"}' >/dev/null
ipe=$(api -X POST $base/api/v1/firewall/iplist -H 'Content-Type: application/json' \
  -d '{"cidr":"203.0.113.0/24","mode":"block","comment":"bad range"}')
echo "$ipe" | grep -q '"cidr":"203.0.113.0/24"' && pass "a geo/IP block entry was stored" || fail "iplist add failed: $ipe"
api -X POST $base/api/v1/firewall/iplist -H 'Content-Type: application/json' \
  -d '{"cidr":"198.51.100.7","mode":"allow","comment":"trusted admin"}' >/dev/null
applied=$(api -X POST $base/api/v1/firewall/apply -H 'Content-Type: application/json')
token=$(echo "$applied" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["token"])')
api -X POST $base/api/v1/firewall/confirm -H 'Content-Type: application/json' -d "{\"token\":\"$token\"}" >/dev/null
sleep 13
api $base/api/v1/firewall | grep -q '"pending":false' && pass "the confirmed change is settled" || fail "still pending after confirm"
rs=$(api $base/api/v1/firewall/status)
echo "$rs" | grep -q 'nexpanel' \
  && pass "the CONFIRMED ruleset is still live after the window (it stuck)" || fail "a confirmed change was reverted"
echo "$rs" | grep -q 'ip6 saddr 2001:db8::/32' \
  && pass "the IPv6 source rule rendered on ip6 saddr (dual-stack inet table)" || fail "no ip6 saddr in the live ruleset"
echo "$rs" | grep -q 'dport 8000-9000' \
  && pass "the port RANGE rendered as dport 8000-9000" || fail "no port range in the live ruleset"
echo "$rs" | grep -q 'set np_block4' && echo "$rs" | grep -q '203.0.113.0/24' \
  && pass "the block-list rendered as an nftables set (np_block4 with the CIDR)" || fail "no block set in the live ruleset"
echo "$rs" | grep -q 'ip saddr @np_block4 drop' \
  && pass "the input chain DROPS traffic from the block set" || fail "no block drop rule in the live ruleset"
echo "$rs" | grep -q 'ip saddr @np_allow4 accept' \
  && pass "the allow set is accepted ahead of the block set" || fail "no allow rule in the live ruleset"

sec "COUNTRY IMPORT: bulk-load a country's CIDR ranges and render them into the block set"
# Fetches from the local mirror above (real HTTP), parses/dedupes, bulk-stores.
imp=$(api -X POST $base/api/v1/firewall/countries -H 'Content-Type: application/json' \
  -d '{"country":"tl","mode":"block"}')
echo "$imp"
# 2 unique v4 (dupe + garbage dropped) + 1 v6 = 3.
echo "$imp" | grep -q '"count":3' && echo "$imp" | grep -q '"country":"TL"' \
  && pass "the country import stored 3 de-duplicated ranges for TL" || fail "country import wrong: $imp"
api $base/api/v1/firewall/countries | grep -q '"country":"TL"' \
  && pass "the imported country is listed with its count" || fail "country not listed"
applied=$(api -X POST $base/api/v1/firewall/apply -H 'Content-Type: application/json')
token=$(echo "$applied" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["token"])')
api -X POST $base/api/v1/firewall/confirm -H 'Content-Type: application/json' -d "{\"token\":\"$token\"}" >/dev/null
rs=$(api $base/api/v1/firewall/status)
echo "$rs" | grep -q '45.66.0.0/16' && echo "$rs" | grep -q '45.67.0.0/16' \
  && pass "the country's v4 ranges rendered into the live block set" || fail "country v4 ranges not in ruleset"
echo "$rs" | grep -q '2a0f:dead::/32' \
  && pass "the country's v6 range rendered into np_block6" || fail "country v6 range not in ruleset"
# Remove the country and confirm it clears out of the set.
api -X DELETE $base/api/v1/firewall/countries/tl >/dev/null
applied=$(api -X POST $base/api/v1/firewall/apply -H 'Content-Type: application/json')
token=$(echo "$applied" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["token"])')
api -X POST $base/api/v1/firewall/confirm -H 'Content-Type: application/json' -d "{\"token\":\"$token\"}" >/dev/null
if api $base/api/v1/firewall/status | grep -q '45.66.0.0/16'; then
  fail "the removed country is still in the live ruleset"
else
  pass "removing the country cleared its ranges from the live ruleset"
fi

sec "*** MALWARE: A REAL CLAMAV SCAN FINDS EICAR AND QUARANTINES IT ***"
# A minimal custom signature: EICAR's well-known MD5:size:name. No DB download.
mkdir -p /var/lib/clamav
echo '44d88612fea8a8f36de82e1278abb02f:68:Eicar-Test-Signature' > /var/lib/clamav/eicar.hdb

uid=$(api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"Victim","primary_domain":"victim.test","type":"static"}' | juid)
[ -n "$uid" ] && pass "site created ($uid)" || fail "site create failed"
# The exact 68-byte EICAR test string.
printf '%s' 'X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*' \
  > /srv/nexpanel/sites/1/public/eicar.txt
chown nps1:nps1 /srv/nexpanel/sites/1/public/eicar.txt
[ "$(md5sum /srv/nexpanel/sites/1/public/eicar.txt | cut -d' ' -f1)" = "44d88612fea8a8f36de82e1278abb02f" ] \
  && pass "the EICAR test file is in place (correct MD5)" || fail "EICAR file MD5 mismatch"

scan=$(api -X POST "$base/api/v1/sites/$uid/scan" -H 'Content-Type: application/json')
echo "$scan"
echo "$scan" | grep -q 'Eicar-Test-Signature' \
  && pass "the REAL clamscan detected EICAR in the site tree" || fail "clamscan did not detect EICAR"
detpath=$(echo "$scan" | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]["findings"]; print(d[0]["path"] if d else "")')

sec "quarantine the detection"
q=$(api -X POST "$base/api/v1/security/quarantine" -H 'Content-Type: application/json' \
  -d "{\"site_uid\":\"$uid\",\"path\":\"$detpath\",\"signature\":\"Eicar-Test-Signature\"}")
echo "$q"
qid=$(echo "$q" | juid)
[ ! -f /srv/nexpanel/sites/1/public/eicar.txt ] \
  && pass "the infected file is GONE from the site tree" || fail "the infected file is still in the site"
qfile="/var/lib/nexpanel/quarantine/$qid"
[ -f "$qfile" ] && pass "the file is held in the root-only quarantine area" || fail "no quarantined file on disk"
[ "$(stat -c '%U %a' "$qfile" 2>/dev/null)" = "root 600" ] \
  && pass "the quarantined file is locked down (root, 0600)" || fail "quarantine perms are $(stat -c '%U %a' "$qfile" 2>/dev/null)"
# The site user can no longer read it (it left their tree, now root-only).
runuser -u nps1 -- cat "$qfile" >/dev/null 2>&1 \
  && fail "the site user can still read the quarantined file" || pass "the site user cannot read the quarantined file"

sec "restore it (a false positive) — back to the site, owned by the site user"
api -X POST "$base/api/v1/security/quarantine/$qid/restore" -H 'Content-Type: application/json' >/dev/null
[ -f /srv/nexpanel/sites/1/public/eicar.txt ] \
  && pass "the file was restored to its original path" || fail "restore did not return the file"
[ "$(stat -c %U /srv/nexpanel/sites/1/public/eicar.txt 2>/dev/null)" = "nps1" ] \
  && pass "the restored file belongs to the site user again" || fail "restored file owner wrong"

sec "*** SSH HARDENING: a panel-owned sshd drop-in, sshd -t tested, effective ***"
# sshd -t / -T need host keys and the drop-in Include line (both are the distro
# default once keys are generated).
ssh-keygen -A >/dev/null 2>&1
grep -q 'sshd_config.d' /etc/ssh/sshd_config || echo 'Include /etc/ssh/sshd_config.d/*.conf' >> /etc/ssh/sshd_config
h=$(api -X POST $base/api/v1/security/ssh -H 'Content-Type: application/json' \
  -d '{"port":2222,"permit_root_login":"no","password_authentication":false}')
echo "$h"
echo "$h" | grep -q '"ok":true' && pass "the hardening was applied" || fail "ssh harden failed: $h"
[ -f /etc/ssh/sshd_config.d/50-nexpanel.conf ] && pass "the panel sshd drop-in was written" || fail "no sshd drop-in"
# sshd -T is the honest source of truth: what the daemon WOULD enforce.
eff=$(/usr/sbin/sshd -T 2>/dev/null)
echo "$eff" | grep -qi '^port 2222' && pass "sshd would listen on the new port (2222)" || fail "port not effective"
echo "$eff" | grep -qi '^permitrootlogin no' && pass "root login is disabled in the effective config" || fail "PermitRootLogin not effective"
echo "$eff" | grep -qi '^passwordauthentication no' && pass "password auth is OFF (key-only) in the effective config" || fail "PasswordAuthentication not effective"
echo "$eff" | grep -qi '^permitemptypasswords no' && pass "empty passwords are refused (fixed hardening)" || fail "PermitEmptyPasswords not effective"
# A self-lockout (both auth methods off) must be refused, not applied.
lock=$(curl -s -o /dev/null -w '%{http_code}' -b /tmp/cs.txt -H "X-CSRF-Token: $CSRF" \
  -X POST $base/api/v1/security/ssh -H 'Content-Type: application/json' \
  -d '{"pubkey_authentication":false,"password_authentication":false}')
[ "$lock" = "400" ] && pass "disabling BOTH auth methods is refused (no self-lockout)" || fail "self-lockout not refused (got $lock)"
# The effective config must be readable back through the API.
st=$(api $base/api/v1/security/ssh)
echo "$st" | grep -q '"permitrootlogin":"no"' && pass "the API reports the effective config" || fail "ssh status wrong: $st"

sec "*** AUTOMATIC SECURITY UPDATES: unattended-upgrades drop-in, apt-effective ***"
up=$(api -X POST $base/api/v1/security/updates -H 'Content-Type: application/json' \
  -d '{"enabled":true,"security_only":true,"automatic_reboot":false}')
echo "$up"
echo "$up" | grep -q '"ok":true' && pass "the update policy was applied" || fail "updates configure failed: $up"
[ -f /etc/apt/apt.conf.d/52nexpanel-unattended ] && pass "the unattended-upgrades drop-in was written" || fail "no updates drop-in"
grep -q 'distro_codename}-security' /etc/apt/apt.conf.d/52nexpanel-unattended \
  && pass "the policy scopes to the security origin" || fail "security origin missing"
# apt-config dump is the effective, merged truth — like sshd -T for SSH.
apt-config dump 2>/dev/null | grep -q 'APT::Periodic::Unattended-Upgrade "1"' \
  && pass "apt's effective config has unattended-upgrade ON" || fail "apt effective config not on"
echo "$up" | grep -q '"unattended_effective":"1"' && pass "the API reports the effective state (on)" || fail "status not on: $up"
# Disabling flips the effective value to 0.
api -X POST $base/api/v1/security/updates -H 'Content-Type: application/json' -d '{"enabled":false}' >/dev/null
apt-config dump 2>/dev/null | grep -q 'APT::Periodic::Unattended-Upgrade "0"' \
  && pass "disabling flips apt's effective config OFF" || fail "disable did not take effect"
# Re-enable so the box is left in the recommended state.
api -X POST $base/api/v1/security/updates -H 'Content-Type: application/json' -d '{"enabled":true}' >/dev/null

sec "audit chain"
for cap in firewall.apply firewall.rollback firewall.confirm malware.scan malware.quarantine malware.restore ssh.harden ssh.status updates.configure updates.status; do
  grep -q "\"capability\":\"$cap\",\"outcome\":\"success\"" /tmp/broker-sec.log \
    && pass "$cap is on the broker's audit chain" || fail "$cap missing from the audit chain"
done

sec "cleanup"
pkill -f 'npd' 2>/dev/null; pkill -f 'np-broker' 2>/dev/null; true

if [ "$FAILED" = "0" ]; then echo "run-security.sh : PASS"; else echo "run-security.sh : FAIL"; fi
