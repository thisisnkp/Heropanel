#!/usr/bin/env bash
# Architecture smoke test: proves the cross-compiled binaries actually run on the
# target arch (run under `docker run --platform linux/arm64 …` to exercise
# aarch64 via qemu).
#
# The full install (run-installer.sh) is arch-agnostic Go plus the distro package
# manager, already proven on amd64. Re-running a whole apt/dnf install under qemu
# emulation mostly re-tests the emulator, not our code — it is minutes of little
# additional signal. What genuinely needs per-arch validation is that the Go
# binaries execute and the pure-Go pieces work: arch detection, the SQLite driver
# and every migration, and the broker's offline self-check. That runs in seconds.
set -u
sec(){ echo; echo "======== $* ========"; }
fail=0
check(){ if printf '%s' "$2" | grep -q -- "$3"; then echo "  ok   $1"; else echo "  FAIL $1 (want: $3)"; echo "       got: $(printf '%s' "$2" | head -c 200)"; fail=1; fi }

sec "target architecture"
echo "uname -m: $(uname -m)"

sec "the binaries execute and detect the arch"
check "hp-installer runs"  "$(/hp/hp-installer --version 2>&1)" 'hp-installer'
DET=$(/hp/hp-installer --detect 2>&1)
echo "$DET" | grep -iE 'OS/arch'
check "arch detected as linux/*" "$DET" 'linux/'

sec "the DB layer works on this arch (SQLite + every migration)"
MIG=$(HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp.db /hp/hpd --migrate 2>&1)
echo "$MIG" | tail -1
check "migrations applied" "$MIG" '"msg":"migrations applied"'

sec "the broker's offline self-check passes on this arch"
SC=$(/hp/hp-broker --check 2>&1)
echo "$SC" | tail -1
check "broker self-check OK" "$SC" 'self-check OK'

sec "RESULT"
if [ "$fail" -eq 0 ]; then echo "run-arch-smoke.sh : PASS"; else echo "run-arch-smoke.sh : FAIL"; fi
exit "$fail"
