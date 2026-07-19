#!/bin/bash
# systemctl stand-in for the installer e2e. A fresh ubuntu/rocky container has no
# systemd, but the installer's execute path calls `systemctl enable --now` to
# bring up the units it just wrote — and we want its own verify step to prove the
# panel actually serves. This shim is just enough of systemd to start/stop the
# hpd and hp-broker units it is handed: it parses User / EnvironmentFile /
# Environment / ExecStart out of the unit file and supervises the process.
# Production uses real systemd; this exists only so the installer e2e can prove an
# end-to-end install brings the panel up.
set -u
verb="${1:-}"; unit="${2:-}"
[ "$unit" = "--now" ] && unit="${3:-}"

pidfile(){ echo "/run/${1%.service}.pid"; }

stop_unit(){
  local pf; pf="$(pidfile "$1")"
  if [ -f "$pf" ]; then
    kill -TERM "-$(cat "$pf")" 2>/dev/null || kill -TERM "$(cat "$pf")" 2>/dev/null || true
    rm -f "$pf"
  fi
}

start_unit(){
  local u="$1" f="/etc/systemd/system/$1"
  [ -f "$f" ] || { echo "shim: no unit $u" >&2; return 1; }
  local user es envfile
  user=$(sed -n 's/^User=//p' "$f"); [ -z "$user" ] && user=root
  es=$(sed -n 's/^ExecStart=//p' "$f")
  envfile=$(sed -n 's/^EnvironmentFile=//p' "$f")

  # Build a prelude that loads the EnvironmentFile and any inline Environment=
  # lines, then execs the ExecStart command (word-split by the shell, as systemd
  # does for a simple command line).
  local pre="set -a; "
  [ -n "$envfile" ] && [ -f "$envfile" ] && pre+=". '$envfile'; "
  while IFS= read -r line; do
    pre+="export ${line#Environment=}; "
  done < <(grep '^Environment=' "$f")
  pre+="set +a; "

  stop_unit "$u"
  # setsid => own process group, so stop_unit can signal the whole tree. Detached
  # from this shim so it survives the installer process that invoked systemctl.
  if [ "$user" = "root" ]; then
    setsid bash -c "${pre} exec ${es}" >"/tmp/${u%.service}.log" 2>&1 &
  else
    setsid runuser -u "$user" -- bash -c "${pre} exec ${es}" >"/tmp/${u%.service}.log" 2>&1 &
  fi
  echo $! > "$(pidfile "$u")"
  sleep 0.6
}

# `enable --now` / `disable --now` both enable/disable AND start/stop, exactly as
# systemd does; plain enable/disable only toggle the (here no-op) wanted state.
case "$verb" in
  daemon-reload|is-enabled|is-active) exit 0 ;;
  enable)  [ "${2:-}" = "--now" ] && start_unit "$unit"; exit 0 ;;
  disable) [ "${2:-}" = "--now" ] && stop_unit "$unit"; exit 0 ;;
  stop) stop_unit "$unit"; exit 0 ;;
  start|restart) start_unit "$unit"; exit 0 ;;
esac
exit 0
