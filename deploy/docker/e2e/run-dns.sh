#!/usr/bin/env bash
# Real DNS: HeroPanel creates an authoritative zone + records via the API; the
# broker writes the zone file + BIND include, validates with named-checkzone, and
# reloads BIND; then `dig` against the local nameserver proves the records are
# served authoritatively.
set -u
sec(){ echo; echo "======== $* ========"; }
base=http://127.0.0.1:18443

sec "start BIND9 (named)"
mkdir -p /run/named && chown bind:bind /run/named 2>/dev/null || true
/usr/sbin/named -u bind 2>/tmp/named.log
sleep 1
rndc status 2>&1 | head -4 || echo "(rndc status unavailable)"

sec "start hp-broker + hpd"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel
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

sec "CREATE ZONE example.test (broker writes zone file + named.conf, checkzone, reload)"
api -X POST $base/api/v1/dns/zones -H 'Content-Type: application/json' \
  -d '{"name":"example.test","primary_ns":"ns1.example.test","admin_email":"admin@example.test","ns_ip":"203.0.113.2"}'; echo
uid=$(api $base/api/v1/dns/zones | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
echo "zone uid=$uid"

sec "ADD RECORDS (each reloads BIND)"
rec(){ api -X POST $base/api/v1/dns/zones/$uid/records -H 'Content-Type: application/json' -d "$1" >/dev/null; }
rec '{"name":"@","type":"A","content":"203.0.113.10"}'
rec '{"name":"www","type":"A","content":"203.0.113.20"}'
rec '{"name":"@","type":"MX","content":"mail.example.test.","priority":10}'
rec '{"name":"@","type":"TXT","content":"v=spf1 -all"}'
echo "records:"; api $base/api/v1/dns/zones/$uid/records | head -c 500; echo

sec "generated zone file"
cat /etc/bind/zones/db.example.test 2>&1

sec "*** DIG THE AUTHORITATIVE NAMESERVER ***"
echo -n "www.example.test A  = "; dig @127.0.0.1 www.example.test A +short
echo -n "example.test  A     = "; dig @127.0.0.1 example.test A +short
echo -n "example.test  MX    = "; dig @127.0.0.1 example.test MX +short
echo -n "example.test  TXT   = "; dig @127.0.0.1 example.test TXT +short
echo -n "example.test  SOA   = "; dig @127.0.0.1 example.test SOA +short

sec "DELETE the www record, dig again (should be empty)"
wwwuid=$(api $base/api/v1/dns/zones/$uid/records | grep -oE '"uid":"[^"]+","name":"www"' | grep -oE '"uid":"[^"]+"' | cut -d'"' -f4)
api -X DELETE $base/api/v1/dns/records/$wwwuid >/dev/null
sleep 1
echo -n "www.example.test A after delete = "; dig @127.0.0.1 www.example.test A +short; echo "(empty above = deleted)"

sec "*** EXPORT the zone as a master file ***"
EXP=$(api $base/api/v1/dns/zones/$uid/export)
echo "$EXP" | head -6
if grep -q "IN\s*SOA" <<<"$EXP" && grep -q "203.0.113.10" <<<"$EXP"; then
  echo "zone export = ok"
else
  echo "zone export = FAIL"
fi

sec "*** IMPORT a master file into a fresh zone, then dig it ***"
api -X POST $base/api/v1/dns/zones -H 'Content-Type: application/json' \
  -d '{"name":"import.test","primary_ns":"ns1.import.test","admin_email":"admin@import.test","ns_ip":"203.0.113.3"}' >/dev/null
iuid=$(api "$base/api/v1/dns/zones" | grep -oE '"uid":"[^"]+","name":"import.test"' | grep -oE '"uid":"[^"]+"' | cut -d'"' -f4)
IMPORTED=$(api -X POST $base/api/v1/dns/zones/$iuid/import -H 'Content-Type: application/json' -d '{
  "zone_file":"$ORIGIN import.test.\n$TTL 3600\nshop\tIN\tA\t198.51.100.7\nmail\tIN\tMX\t5 mx.import.test.\n@\tIN\tTXT\t\"hello world\"\nbad\tIN\tDNSKEY\t257 3 13 xx\n"
}')
echo "import result: $IMPORTED"
grep -q '"imported":3' <<<"$IMPORTED" && echo "import count = ok (3)" || echo "import count = FAIL"
grep -q 'skipped' <<<"$IMPORTED" && echo "import skipped unsupported = ok"
sleep 1
echo -n "shop.import.test A = "; dig @127.0.0.1 shop.import.test A +short
dig @127.0.0.1 shop.import.test A +short | grep -q '198.51.100.7' && echo "imported record served = yes" || echo "imported record served = no"

sec "*** DNSSEC: enable inline signing, dig DNSKEY, read DS for the registrar ***"
# BIND inline-signing writes the signed zone + journal next to the zone file, so
# in this container we make the zones dir writable by bind (production runs named
# with the right ownership from the installer). The broker already chowns the
# key-directory when it sees a dnssec-policy in named.conf.
chown -R bind:bind /etc/bind/zones 2>/dev/null || true
api -X PUT $base/api/v1/dns/zones/$uid/dnssec -H 'Content-Type: application/json' -d '{"enabled":true}' >/dev/null
echo "dnssec enabled; waiting for BIND to sign…"
signed=""
for i in $(seq 1 40); do
  DK=$(dig @127.0.0.1 example.test DNSKEY +short 2>/dev/null)
  if [ -n "$DK" ]; then signed=yes; break; fi
  sleep 1
done
echo -n "example.test DNSKEY = "; dig @127.0.0.1 example.test DNSKEY +short | head -2
echo "DNSSEC signed = ${signed:-no}"
ST=$(api $base/api/v1/dns/zones/$uid/dnssec)
echo "dnssec status: $ST"
if grep -q '"signed":true' <<<"$ST" && grep -qE '"ds":\["[^"]' <<<"$ST"; then
  echo "DS present via API = yes"
else
  echo "DS present via API = no"
fi
# The zone view now reports dnssec on.
api $base/api/v1/dns/zones/$uid | grep -q '"dnssec_enabled":true' && echo "zone view dnssec_enabled = yes"

sec "broker audit (dns.write_zone / dns.remove_zone / dns.dnssec_status outcomes)"
grep -oE '"capability":"dns\.[a-z_]+","outcome":"[^"]+"' /tmp/broker.log

sec "named log tail"
tail -4 /tmp/named.log 2>&1
