#!/usr/bin/env bash
# The three things only a real service manager can decide.
#
# Every other e2e runs against `systemctl-shim.sh`, which parses a unit and
# supervises the process. That proves the panel's logic and is blind to systemd
# itself, so three claims have been carried as honest limits since Phase 0:
#
#   1. the units the installer writes are *valid* — a misspelled directive is
#      silently ignored by a shim and refused by systemd;
#   2. the hardening in pkg/unitharden actually *takes effect* — a
#      CapabilityBoundingSet nobody enforces is a comment;
#   3. a transient unit really does *outlive* the process that created it, which
#      is the entire mechanism self-update depends on (docs/26).
#
# This runs inside the nexpanel-systemd image, where PID 1 is systemd. Binaries
# are mounted at /np and scripts at /e2e, same as every other suite.
set -u
sec(){ echo; echo "======== $* ========"; }
fail=0
ok(){ echo "  ok   $1"; }
bad(){ echo "  FAIL $1"; fail=1; }
check(){ if printf '%s' "$2" | grep -q -- "$3"; then ok "$1"; else bad "$1 (want: $3)"; echo "       got: $(printf '%s' "$2" | head -c 300)"; fi }

sec "the service manager is real"
if [ "$(readlink -f /proc/1/exe)" != "/usr/lib/systemd/systemd" ] && [ "$(readlink -f /proc/1/exe)" != "/lib/systemd/systemd" ]; then
  bad "PID 1 is $(readlink -f /proc/1/exe), not systemd — this suite is meaningless without it"
  echo "run-systemd.sh : FAIL"; exit 1
fi
ok "PID 1 is systemd"
systemctl --version | head -1

sec "stage a signed release and install for real"
mkdir -p /stage
install -m0755 /np/npd /np/np-broker /np/np-installer /stage/
eval "$(/stage/np-installer --gen-key | grep '^NP_RELEASE')"
/stage/np-installer --sign /stage --key "$NP_RELEASE_KEY"
/stage/np-installer --execute --yes --minimal --no-webserver \
  --source /stage --pubkey "$NP_RELEASE_PUBKEY" 2>/tmp/exec.log || {
    bad "installer execute failed"; tail -30 /tmp/exec.log; echo "run-systemd.sh : FAIL"; exit 1; }
ok "np-installer --execute completed under real systemd"

sec "systemd accepts every unit the panel wrote"
# `systemd-analyze verify` is the cheapest possible catch for a directive that
# does not exist or a value that does not parse — the exact class of mistake a
# shim can never surface, and one that would otherwise be discovered as "the
# hardening silently did nothing".
for unit in /etc/systemd/system/npd.service /etc/systemd/system/np-broker.service; do
  out=$(systemd-analyze verify "$unit" 2>&1)
  # Ignore the advisory notes systemd emits about unit file permissions and
  # missing optional dependencies; only real parse/directive errors matter.
  real=$(printf '%s' "$out" | grep -viE "world-writable|Unit .* not found|is marked (world|group)-" || true)
  if [ -n "$real" ]; then bad "systemd-analyze verify $unit"; echo "$real" | head -10; else ok "valid: $(basename "$unit")"; fi
done

sec "both services are actually running"
systemctl is-active np-broker >/dev/null 2>&1 && ok "np-broker active" || { bad "np-broker not active"; systemctl status np-broker --no-pager -l | head -30; }
systemctl is-active npd        >/dev/null 2>&1 && ok "npd active"       || { bad "npd not active";       systemctl status npd        --no-pager -l | head -30; }

sec "the hardening reached the processes"
# This is the claim the whole of docs/28 rests on. A CapabilityBoundingSet in a
# unit file proves nothing; CapBnd in /proc proves it was applied.
broker_pid=$(systemctl show -p MainPID --value np-broker 2>/dev/null)
npd_pid=$(systemctl show -p MainPID --value npd 2>/dev/null)

capbnd_of(){ awk '/^CapBnd:/{print $2}' "/proc/$1/status" 2>/dev/null; }

if [ -n "${npd_pid:-}" ] && [ "$npd_pid" != "0" ]; then
  bnd=$(capbnd_of "$npd_pid")
  if [ "$((16#${bnd:-ffffffff}))" -eq 0 ]; then
    ok "npd holds no capabilities at all (CapBnd=$bnd)"
  else
    bad "npd CapBnd=$bnd, want 0 — the empty CapabilityBoundingSet did not apply"
  fi
else
  bad "could not read npd's main PID"
fi

if [ -n "${broker_pid:-}" ] && [ "$broker_pid" != "0" ]; then
  bnd=$(capbnd_of "$broker_pid")
  val=$((16#${bnd:-0}))
  # Bit positions from <linux/capability.h>. These are the capabilities
  # pkg/unitharden's RootBroker profile denies; each must be absent even though
  # the process runs as root.
  denied="16:CAP_SYS_MODULE 17:CAP_SYS_RAWIO 22:CAP_SYS_BOOT 25:CAP_SYS_TIME 28:CAP_LEASE 30:CAP_AUDIT_CONTROL 32:CAP_MAC_OVERRIDE 33:CAP_MAC_ADMIN 35:CAP_WAKE_ALARM 36:CAP_BLOCK_SUSPEND 37:CAP_AUDIT_READ"
  for entry in $denied; do
    bit=${entry%%:*}; name=${entry#*:}
    if [ $(( (val >> bit) & 1 )) -eq 0 ]; then ok "broker cannot $name"; else bad "broker still holds $name (CapBnd=$bnd)"; fi
  done
  # And the ones it genuinely needs must survive, or the deny list was too broad
  # and the panel cannot provision a site.
  for entry in 0:CAP_CHOWN 1:CAP_DAC_OVERRIDE 6:CAP_SETGID 7:CAP_SETUID 12:CAP_NET_ADMIN; do
    bit=${entry%%:*}; name=${entry#*:}
    if [ $(( (val >> bit) & 1 )) -eq 1 ]; then ok "broker retains $name"; else bad "broker lost $name — privileged operations will fail"; fi
  done
else
  bad "could not read np-broker's main PID"
fi

sec "a transient unit outlives the process that created it"
# This is the mechanism self-update depends on (docs/26 §2): np-installer is
# started by the broker but must survive the broker being restarted underneath
# it. Proven here by starting a transient unit from a shell that then exits.
rm -f /tmp/transient.out
bash -c 'systemd-run --unit=np-e2e-transient --collect --service-type=oneshot \
           /bin/bash -c "sleep 3; echo survived > /tmp/transient.out" >/dev/null 2>&1' &
parent=$!
wait "$parent" 2>/dev/null
ok "the creating shell has exited"
for _ in $(seq 1 40); do [ -f /tmp/transient.out ] && break; sleep 0.5; done
if [ "$(cat /tmp/transient.out 2>/dev/null)" = "survived" ]; then
  ok "the transient unit ran to completion after its creator was gone"
else
  bad "transient unit did not complete (self-update's hand-off would not work)"
  systemctl status np-e2e-transient --no-pager -l 2>&1 | head -15
fi
# --collect means systemd garbage-collects the unit once it has finished.
systemctl status np-e2e-transient >/dev/null 2>&1 && bad "--collect did not reap the finished unit" || ok "--collect reaped the finished unit"

sec "a site slice actually enforces its limits"
# The cgroup limits the panel writes per site (docs/10 records that container
# e2e cannot enforce these). With real systemd and real cgroups they can be read
# back from the kernel.
cat >/etc/systemd/system/np-e2e-test.slice <<'EOF'
[Unit]
Description=NexPanel e2e slice

[Slice]
MemoryMax=64M
CPUQuota=25%
TasksMax=50
EOF
systemctl daemon-reload
systemctl start np-e2e-test.slice
cg=/sys/fs/cgroup/np-e2e-test.slice
if [ -d "$cg" ]; then
  mem=$(cat "$cg/memory.max" 2>/dev/null || echo "?")
  tasks=$(cat "$cg/pids.max" 2>/dev/null || echo "?")
  echo "  memory.max=$mem pids.max=$tasks"
  [ "$mem" = "67108864" ] && ok "MemoryMax reached the kernel" || bad "memory.max=$mem, want 67108864"
  [ "$tasks" = "50" ] && ok "TasksMax reached the kernel" || bad "pids.max=$tasks, want 50"
else
  bad "slice cgroup directory was not created at $cg"
fi
systemctl stop np-e2e-test.slice 2>/dev/null || true

sec "exposure scores (informational)"
systemd-analyze security npd.service 2>/dev/null | tail -3 || true
systemd-analyze security np-broker.service 2>/dev/null | tail -3 || true

echo
if [ "$fail" = 0 ]; then echo "run-systemd.sh : PASS"; else echo "run-systemd.sh : FAIL"; fi
exit "$fail"
