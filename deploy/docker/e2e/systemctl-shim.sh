#!/bin/bash
# Container shim for `systemctl` (no systemd in a test container). It is enough of
# a systemd stand-in to run HeroPanel's per-site app units (parsing User /
# WorkingDirectory / Environment / ExecStart out of the unit file and supervising
# the process), plus the php-fpm reload the broker issues. Production uses real
# systemd; this only exists so the app-runtime e2e can prove the reverse proxy.
verb="${1:-}"; unit="${2:-}"
[ "$unit" = "--now" ] && unit="${3:-}"

pidfile(){ echo "/run/${1%.service}.pid"; }

fpm_reload(){
  if [ -f /run/php/php8.3-fpm.pid ]; then
    kill -USR2 "$(cat /run/php/php8.3-fpm.pid)" 2>/dev/null || true
  else
    pkill -USR2 -f 'php-fpm8.3' 2>/dev/null || true
  fi
}

# A real restart of a php-fpm master, as systemd would do it. SIGUSR2 (reload)
# re-reads pool config but does not re-link extensions into a running master —
# so an extension change needs the master torn down and re-exec'd. Extract the
# version from the service name (php8.3-fpm -> 8.3) and bring a fresh master up.
fpm_restart(){
  local svc="$1" ver
  ver="${svc#php}"; ver="${ver%-fpm}"
  local pf="/run/php/php${ver}-fpm.pid"
  if [ -f "$pf" ]; then kill -QUIT "$(cat "$pf")" 2>/dev/null || true; fi
  pkill -QUIT -f "php-fpm${ver}" 2>/dev/null || true
  sleep 0.5
  "/usr/sbin/php-fpm${ver}" --daemonize 2>/dev/null || true
  sleep 0.5
}

stop_app(){
  local pf; pf="$(pidfile "$1")"
  if [ -f "$pf" ]; then
    kill -TERM "-$(cat "$pf")" 2>/dev/null || true   # kill the process group
    rm -f "$pf"
  fi
}

# Run a oneshot unit (a cron job) to completion, as its User in its
# WorkingDirectory — synchronously, as systemd does for Type=oneshot. Enough for
# the e2e to prove the command runs as the site user; real timer *firing* is
# systemd behaviour the unit-rendering tests pin.
run_oneshot(){
  local u="$1" f="/etc/systemd/system/$1"
  [ -f "$f" ] || { echo "shim: no unit $u" >&2; return 1; }
  local user wd es
  user=$(sed -n 's/^User=//p' "$f")
  wd=$(sed -n 's/^WorkingDirectory=//p' "$f")
  es=$(sed -n 's/^ExecStart=//p' "$f")
  runuser -u "$user" -- env -C "$wd" HOME="$wd" "$es"
}

start_app(){
  local u="$1" f="/etc/systemd/system/$1"
  [ -f "$f" ] || { echo "shim: no unit $u" >&2; return 1; }
  local user wd es home
  user=$(sed -n 's/^User=//p' "$f")
  wd=$(sed -n 's/^WorkingDirectory=//p' "$f")
  es=$(sed -n 's/^ExecStart=//p' "$f")
  home=$(dirname "$wd")
  local envargs=()
  while IFS= read -r line; do
    v="${line#Environment=}"; v="${v%\"}"; v="${v#\"}"
    envargs+=("$v")
  done < <(grep '^Environment=' "$f")
  stop_app "$u"
  # New session (own process group) so we can stop the whole app cleanly.
  setsid runuser -u "$user" -- env -C "$wd" HOME="$home" "${envargs[@]}" "$es" \
    >"/tmp/app-${u%.service}.log" 2>&1 &
  echo $! > "$(pidfile "$u")"
  sleep 0.5
}

case "$verb" in
  daemon-reload|enable) exit 0 ;;
  is-active)
    # Report each queried service honestly by probing for its process — enough for
    # the monitor e2e to prove the service.status pipeline (broker -> systemctl ->
    # parsed state -> API). Real systemd answers from unit state in production.
    shift
    rc=0
    for u in "$@"; do
      case "$u" in
        openlitespeed|lshttpd) pgrep -f 'lshttpd|litespeed' >/dev/null 2>&1 ;;
        mariadb|mysql|mysqld)  pgrep -x mariadbd >/dev/null 2>&1 || pgrep -x mysqld >/dev/null 2>&1 ;;
        redis|redis-server)    pgrep -x redis-server >/dev/null 2>&1 ;;
        postfix)               pgrep -x master >/dev/null 2>&1 ;;
        dovecot)               pgrep -x dovecot >/dev/null 2>&1 ;;
        opendkim)              pgrep -x opendkim >/dev/null 2>&1 ;;
        *) false ;;
      esac
      if [ $? -eq 0 ]; then echo active; else echo inactive; rc=3; fi
    done
    exit $rc ;;
  disable|stop) stop_app "$unit"; exit 0 ;;
  start|restart)
    case "$unit" in
      heropanel-cron-*) run_oneshot "$unit"; exit $? ;;
      heropanel-app-*) start_app "$unit" ;;
      opendkim)
        # The broker restarts the DKIM signer after pushing keys (it has no
        # reload). Real systemd supervises it in production.
        pkill -x opendkim 2>/dev/null || true
        sleep 0.3
        opendkim -x /etc/opendkim.conf 2>/tmp/opendkim.log || true
        sleep 0.3 ;;
      postfix) postfix reload 2>/dev/null || postfix start 2>/dev/null || true ;;
      dovecot) doveadm reload 2>/dev/null || dovecot 2>/dev/null || true ;;
      php*-fpm)
        # A reload cannot pick up an extension; a restart must be a real one.
        if [ "$verb" = "restart" ]; then fpm_restart "$unit"; else fpm_reload; fi ;;
      *) fpm_reload ;;
    esac
    exit 0 ;;
  reload) fpm_reload; exit 0 ;;
esac
exit 0
