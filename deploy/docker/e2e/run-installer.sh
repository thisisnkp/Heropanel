#!/usr/bin/env bash
# Verifies the np-installer execute / resume / rollback path on a fresh distro
# image (run once on ubuntu:24.04 and once on rockylinux:9 — see CI). The whole
# point is cross-distro: package management is the only thing that differs
# between apt and dnf, and this proves both.
#
# There is no systemd in the container, so we install a shim as `systemctl` and
# create /run/systemd/system; the installer then genuinely starts npd + np-broker
# through the shim and its own verify step proves the panel answers. Everything
# else runs for real: apt/dnf installs, useradd, directory + ownership setup,
# secret generation, config + unit rendering, the SQLite migration (as the
# service user), and — at the end — a full reverse rollback.
#
# Binaries are mounted at /np, scripts at /e2e (same convention as the other
# suites), but this runs on a base distro image, not nexpanel-e2e.
set -u
sec(){ echo; echo "======== $* ========"; }
fail=0
check(){ if printf '%s' "$2" | grep -q -- "$3"; then echo "  ok   $1"; else echo "  FAIL $1 (want: $3)"; echo "       got: $(printf '%s' "$2" | head -c 300)"; fail=1; fi }
present(){ if [ -e "$1" ]; then echo "  ok   exists $1"; else echo "  FAIL missing $1"; fail=1; fi }
absent(){ if [ -e "$1" ]; then echo "  FAIL still present $1"; fail=1; else echo "  ok   removed $1"; fi }
have(){ command -v "$1" >/dev/null 2>&1; }

ensure_curl(){
  have curl && return 0
  if have apt-get; then apt-get update -y >/dev/null 2>&1 || true; apt-get install -y curl >/dev/null 2>&1 || true; fi
  have curl || { if have dnf; then dnf install -y curl >/dev/null 2>&1 || true; fi; }
}

sec "distro"
. /etc/os-release; echo "ID=$ID VERSION_ID=${VERSION_ID:-?} PRETTY=${PRETTY_NAME:-?}"
ensure_curl
have curl || { echo "  FAIL curl unavailable for health checks"; exit 1; }

sec "install the systemctl shim (no systemd in a container)"
install -m0755 /e2e/systemctl-installer-shim.sh /usr/local/bin/systemctl
mkdir -p /run/systemd/system   # make the installer detect a service manager
hash -r
command -v systemctl

sec "stage the binaries + a SHA256SUMS manifest (a release ships this)"
mkdir -p /stage
install -m0755 /np/npd /np/np-broker /np/np-installer /stage/
( cd /stage && sha256sum npd np-broker > SHA256SUMS )
echo "manifest:"; cat /stage/SHA256SUMS
/stage/np-installer --version

sec "sign the manifest with a fresh ed25519 release key (the trust root)"
eval "$(/stage/np-installer --gen-key | grep '^NP_RELEASE')"
echo "  pinned pubkey: ${NP_RELEASE_PUBKEY:0:16}…"
/stage/np-installer --sign /stage --key "$NP_RELEASE_KEY"
present /stage/SHA256SUMS.sig
# A tampered manifest must fail verification live: corrupt the manifest after
# signing, run the integrity check via --execute --pubkey, expect refusal + no
# install. Done in a throwaway copy so the real install starts clean.
sec "tampered manifest is refused (signature no longer matches)"
cp -r /stage /stage-bad
echo "0000000000000000000000000000000000000000000000000000000000000000  npd" > /stage-bad/SHA256SUMS
( cd /stage-bad && sha256sum np-broker >> SHA256SUMS )
BAD=$(/stage-bad/np-installer --execute --yes --minimal --no-webserver --source /stage-bad --pubkey "$NP_RELEASE_PUBKEY" 2>&1 || true)
check "tampered manifest fails signature" "$BAD" 'signature does not verify'
absent /opt/nexpanel/bin/npd
rm -rf /stage-bad /var/lib/nexpanel/install-journal.json /opt/nexpanel /etc/nexpanel /var/lib/nexpanel 2>/dev/null || true
id nexpanel >/dev/null 2>&1 && userdel -r nexpanel 2>/dev/null || true

sec "detect + plan (dry run, no changes)"
/stage/np-installer --detect || true
/stage/np-installer --plan --minimal --no-webserver

sec "EXECUTE  (minimal profile: SQLite, no web server; signature-verified)"
/stage/np-installer --execute --yes --minimal --no-webserver --source /stage --pubkey "$NP_RELEASE_PUBKEY" 2>/tmp/exec.log
rc=$?
cat /tmp/exec.log
check "execute returned 0" "$rc" '^0$'
check "SHA256SUMS signature verified" "$(cat /tmp/exec.log)" 'SHA256SUMS signature verified against the release key'
check "binaries verified against SHA256SUMS" "$(cat /tmp/exec.log)" 'binaries verified against SHA256SUMS'

sec "artifacts were installed"
present /opt/nexpanel/bin/npd
present /opt/nexpanel/bin/np-broker
present /etc/nexpanel/config.yaml
present /etc/nexpanel/secrets.env
present /var/lib/nexpanel/nexpanel.db
present /etc/systemd/system/npd.service
present /etc/systemd/system/np-broker.service
present /var/lib/nexpanel/install-journal.json

sec "the service user was created and the config is SQLite/no-redis"
id nexpanel >/dev/null 2>&1 && echo "  ok   user nexpanel exists" || { echo "  FAIL user nexpanel missing"; fail=1; }
CFG=$(cat /etc/nexpanel/config.yaml)
check "config: sqlite driver" "$CFG" 'driver: sqlite'
if grep -q 'addr: 127.0.0.1:6379' <<<"$CFG"; then echo "  FAIL minimal config should omit redis"; fail=1; else echo "  ok   redis omitted (L1-only)"; fi
# The generated secret must be a real 64-hex token, not a placeholder.
check "secret token present" "$(cat /etc/nexpanel/secrets.env)" 'NP_BROKER_TOKEN=[0-9a-f]\{64\}'

sec "journal records every step done"
JB=$(cat /var/lib/nexpanel/install-journal.json)
check "journal has done steps" "$JB" '"status": "done"'
PEND=$(grep -c '"status": "pending"' <<<"$JB" || true)
check "no steps left pending" "$PEND" '^0$'

sec "the panel is serving — started by the shim during execute's verify step"
CODE=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8443/healthz || echo 000)
check "healthz => 200" "$CODE" '200'
check "system/info answers"  "$(curl -s http://127.0.0.1:8443/api/v1/system/info)" 'version'
check "openapi.json served"  "$(curl -s http://127.0.0.1:8443/api/v1/openapi.json)" '"openapi"'
# The broker socket exists and is group-owned by the panel group so npd (running
# as nexpanel, not root) can reach it — the privilege-separation contract.
present /run/nexpanel/broker.sock
SGRP=$(stat -c '%G' /run/nexpanel/broker.sock 2>/dev/null || echo "?")
check "broker socket group is nexpanel" "$SGRP" 'nexpanel'

sec "RESUME is idempotent (all steps already done => no-op, exit 0)"
OUT=$(/stage/np-installer --resume --yes --minimal --no-webserver --source /stage 2>&1)
rc=$?
check "resume returned 0" "$rc" '^0$'
check "resume loaded the existing journal" "$OUT" 'resuming from existing journal'

sec "ROLLBACK reverses the install"
/stage/np-installer --rollback --minimal --no-webserver --source /stage
rc=$?
check "rollback returned 0" "$rc" '^0$'
absent /etc/nexpanel/config.yaml
absent /opt/nexpanel/bin/npd
absent /etc/systemd/system/npd.service
absent /var/lib/nexpanel/nexpanel.db
if id nexpanel >/dev/null 2>&1; then echo "  FAIL user nexpanel still present after rollback"; fail=1; else echo "  ok   user nexpanel removed"; fi
# The journal survives rollback as its record, marked reverted.
check "journal marks steps reverted" "$(cat /var/lib/nexpanel/install-journal.json 2>/dev/null)" '"status": "reverted"'
# The panel is no longer answering (services were stopped).
CODE=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 http://127.0.0.1:8443/healthz || echo 000)
if [ "$CODE" = "200" ]; then echo "  FAIL panel still serving after rollback"; fail=1; else echo "  ok   panel stopped ($CODE)"; fi

sec "RESULT"
if [ "$fail" -eq 0 ]; then echo "run-installer.sh : PASS"; else echo "run-installer.sh : FAIL"; fi
exit "$fail"
