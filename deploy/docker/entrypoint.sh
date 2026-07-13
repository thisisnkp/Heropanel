#!/usr/bin/env bash
set -euo pipefail

echo "==> [HeroPanel Container] Starting MariaDB..."
mkdir -p /run/mysqld && chown mysql:mysql /run/mysqld
[ -d /var/lib/mysql/mysql ] || mariadb-install-db --user=mysql --datadir=/var/lib/mysql >/dev/null 2>&1
mariadbd --user=mysql >/var/log/mariadb.log 2>&1 &
for i in $(seq 1 40); do mysqladmin ping >/dev/null 2>&1 && break; sleep 0.5; done
if mysqladmin ping >/dev/null 2>&1; then
    echo "==> [HeroPanel Container] MariaDB is ready."
else
    echo "==> [HeroPanel Container] Warning: MariaDB ping failed."
fi

echo "==> [HeroPanel Container] Starting OpenLiteSpeed..."
/usr/local/lsws/bin/lswsctrl start 2>&1 || true

echo "==> [HeroPanel Container] Starting hp-broker..."
mkdir -p /run/heropanel /srv/heropanel/sites /srv/heropanel/data
export HP_BROKER_TOKEN="${HP_BROKER_TOKEN:-heropanel-docker-secret-token}"
export HP_BROKER_ALLOWED_UID=0
hp-broker --serve --socket /run/heropanel/broker.sock >/var/log/hp-broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
echo "==> [HeroPanel Container] hp-broker started on /run/heropanel/broker.sock."

# Default environment variables for hpd if not set
export HP_SERVER_HOST="${HP_SERVER_HOST:-0.0.0.0}"
export HP_SERVER_PORT="${HP_SERVER_PORT:-18443}"
export HP_LOG_FORMAT="${HP_LOG_FORMAT:-text}"
export HP_DATABASE_DRIVER="${HP_DATABASE_DRIVER:-sqlite}"
export HP_DATABASE_DSN="${HP_DATABASE_DSN:-/srv/heropanel/data/hp.db}"
export HP_BROKER_SOCKET="/run/heropanel/broker.sock"

echo "==> [HeroPanel Container] Starting hpd control plane daemon on ${HP_SERVER_HOST}:${HP_SERVER_PORT}..."
exec hpd "$@"
