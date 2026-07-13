#!/bin/bash
# Container shim for `systemctl` (no systemd in a test container). Handles the
# reload/restart the broker issues for php-fpm by signalling the fpm master.
case "${1:-}" in
  reload|restart)
    if [ -f /run/php/php8.3-fpm.pid ]; then
      kill -USR2 "$(cat /run/php/php8.3-fpm.pid)" 2>/dev/null || true
    else
      pkill -USR2 -f 'php-fpm8.3' 2>/dev/null || true
    fi
    exit 0 ;;
esac
exit 0
