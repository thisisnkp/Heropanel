#!/usr/bin/env bash
set -euo pipefail

echo "==> [NexPanel Container] Starting MariaDB..."
mkdir -p /run/mysqld && chown mysql:mysql /run/mysqld
[ -d /var/lib/mysql/mysql ] || mariadb-install-db --user=mysql --datadir=/var/lib/mysql >/dev/null 2>&1
mariadbd --user=mysql >/var/log/mariadb.log 2>&1 &
for i in $(seq 1 40); do mysqladmin ping >/dev/null 2>&1 && break; sleep 0.5; done
if mysqladmin ping >/dev/null 2>&1; then
    echo "==> [NexPanel Container] MariaDB is ready."
else
    echo "==> [NexPanel Container] Warning: MariaDB ping failed."
fi

echo "==> [NexPanel Container] Starting OpenLiteSpeed..."
/usr/local/lsws/bin/lswsctrl start 2>&1 || true

echo "==> [NexPanel Container] Starting np-broker..."
mkdir -p /run/nexpanel /srv/nexpanel/sites /srv/nexpanel/data
export NP_BROKER_TOKEN="${NP_BROKER_TOKEN:-nexpanel-docker-secret-token}"
export NP_BROKER_ALLOWED_UID=0
np-broker --serve --socket /run/nexpanel/broker.sock >/var/log/np-broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/nexpanel/broker.sock ] && break; sleep 0.2; done
echo "==> [NexPanel Container] np-broker started on /run/nexpanel/broker.sock."

# Default environment variables for npd if not set
export NP_SERVER_HOST="${NP_SERVER_HOST:-0.0.0.0}"
export NP_SERVER_PORT="${NP_SERVER_PORT:-18443}"
export NP_LOG_FORMAT="${NP_LOG_FORMAT:-text}"
export NP_DATABASE_DRIVER="${NP_DATABASE_DRIVER:-sqlite}"
export NP_DATABASE_DSN="${NP_DATABASE_DSN:-/srv/nexpanel/data/np.db}"
export NP_BROKER_SOCKET="/run/nexpanel/broker.sock"

echo "==> [NexPanel Container] Starting npd control plane daemon on ${NP_SERVER_HOST}:${NP_SERVER_PORT}..."
exec npd "$@"
