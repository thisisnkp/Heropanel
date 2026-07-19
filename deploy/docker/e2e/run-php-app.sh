#!/usr/bin/env bash
# Phase 3 exit criteria, PHP half: a Laravel-shaped app deployed from Git with a
# real database and real Composer dependencies.
#
# The repo carries a composer.json and a front controller in public/. HeroPanel
# clones it, runs `composer install` on its own (no build command is configured),
# and OpenLiteSpeed serves the app through php-fpm. The page then talks to a
# MariaDB database created through the panel, which is what actually proves
# "deploy a Laravel app (DB + composer)" rather than just "PHP renders".
#
# Also exercises the rest of the database surface: size, export, import, revoke,
# user delete, and the Adminer hand-off.
set -u
sec(){ echo; echo "======== $* ========"; }
base=http://127.0.0.1:18443
site=/srv/heropanel/sites/1

sec "start MariaDB + OpenLiteSpeed"
# php-fpm's per-site pool socket lives here; the pool cannot bind without it.
mkdir -p /run/php /run/heropanel/fpm && chmod 755 /run/heropanel /run/heropanel/fpm
mkdir -p /run/mysqld && chown mysql:mysql /run/mysqld
mysqld_safe --skip-grant-tables=0 >/tmp/mysqld.log 2>&1 &
for i in $(seq 1 60); do mysqladmin ping >/dev/null 2>&1 && break; sleep 0.5; done
mysqladmin ping 2>&1 | head -1
/usr/local/lsws/bin/lswsctrl start >/dev/null 2>&1

sec "stand up a private git server holding a Laravel-shaped app"
useradd -m -s /bin/bash git 2>/dev/null
mkdir -p /srv/git /home/git/.ssh
git init --bare -q /srv/git/app.git
work=$(mktemp -d)
git init -q "$work"
git -C "$work" config user.email ci@heropanel.test
git -C "$work" config user.name CI

# A real composer.json with a real dependency, so `composer install` has to
# resolve and download something rather than no-op.
cat > "$work/composer.json" <<'EOF'
{
  "name": "heropanel/e2e-app",
  "require": { "vlucas/phpdotenv": "^5.6" },
  "config": { "vendor-dir": "vendor" }
}
EOF
mkdir -p "$work/public"
# The front controller proves three things at once: Composer's autoloader exists,
# the dependency is loadable, and the app can reach the panel-created database.
cat > "$work/public/index.php" <<'EOF'
<?php
require __DIR__ . '/../vendor/autoload.php';
$dep = class_exists(\Dotenv\Dotenv::class) ? 'loaded' : 'MISSING';
$pdo = new PDO('mysql:host=localhost;dbname=laravel_db;charset=utf8mb4', 'laravel_user', 'password123',
    [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION]);
$row = $pdo->query('SELECT title FROM posts ORDER BY id LIMIT 1')->fetch();
echo "<h1>Laravel-shaped app via HeroPanel</h1>";
echo "<p>composer dependency: {$dep}</p>";
echo "<p>db row: {$row['title']}</p>";
EOF
git -C "$work" add -A
git -C "$work" commit -qm seed
git -C "$work" branch -M main
git -C "$work" remote add origin /srv/git/app.git
git -C "$work" push -q origin main
chown -R git:git /srv/git /home/git
chmod 700 /home/git/.ssh
ssh-keygen -A >/dev/null 2>&1
/usr/sbin/sshd >/tmp/sshd.log 2>&1
for i in $(seq 1 40); do (echo > /dev/tcp/127.0.0.1/22) >/dev/null 2>&1 && break; sleep 0.2; done
echo "composer.json requires: $(grep -o 'vlucas/phpdotenv' "$work/composer.json")"

sec "start hp-broker + hpd"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel /srv/heropanel/sites
export HP_BROKER_TOKEN=tok
# In this harness hpd runs as root rather than the packaged `heropanel` account,
# so tell the broker who to hand exported dumps to.
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 HP_BROKER_PANEL_USER=root \
  hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
SECRET_KEY=$(head -c 32 /dev/urandom | base64 -w0)
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18443 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp.db \
  HP_SECRET_KEY="$SECRET_KEY" \
  HP_DATABASE_ADMINER_URL=http://127.0.0.1/adminer.php \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "auth"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }

sec "CREATE DATABASE + USER + GRANT (through the panel)"
dbj=$(api -X POST $base/api/v1/databases -H 'Content-Type: application/json' -d '{"name":"laravel_db"}')
echo "$dbj"
dbuid=$(echo "$dbj" | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
uj=$(api -X POST $base/api/v1/database-users -H 'Content-Type: application/json' \
  -d '{"username":"laravel_user","host":"localhost","password":"password123"}')
useruid=$(echo "$uj" | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
api -X POST $base/api/v1/databases/$dbuid/grant -H 'Content-Type: application/json' \
  -d "{\"user_uid\":\"$useruid\",\"privileges\":[\"ALL\"]}"; echo
mysql -e "SHOW DATABASES;" | grep laravel_db

sec "seed a table the app will read"
mysql laravel_db -e "CREATE TABLE posts (id INT AUTO_INCREMENT PRIMARY KEY, title VARCHAR(190)); INSERT INTO posts (title) VALUES ('hello from mariadb');"

sec "CREATE PHP GIT SITE (no build command — Composer must run on its own)"
api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"App","primary_domain":"app.test","type":"php","deploy_mode":"git","php_version":"8.3"}' >/dev/null
uid=$(api $base/api/v1/sites | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
echo "site uid=$uid"
cat >/tmp/src.json <<'EOF'
{"repo_url":"git@127.0.0.1:/srv/git/app.git","branch":"main","web_root":"public","auth_kind":"ssh_key"}
EOF
src=$(api -X PUT $base/api/v1/sites/$uid/git -H 'Content-Type: application/json' --data @/tmp/src.json)
pub=$(echo "$src" | grep -oE '"public_key":"[^"]+"' | cut -d'"' -f4)
echo "$pub" > /home/git/.ssh/authorized_keys
chown git:git /home/git/.ssh/authorized_keys; chmod 600 /home/git/.ssh/authorized_keys
echo -n "auto_composer: "; echo "$src" | grep -oE '"auto_composer":(true|false)'

sec "*** DEPLOY: clone + composer install (no build command configured) ***"
api -X POST $base/api/v1/sites/$uid/git/deploy > /tmp/deploy.json
head -c 900 /tmp/deploy.json; echo

sec "composer actually installed the dependency"
ls -d $site/current/vendor 2>&1
ls -d $site/current/vendor/vlucas/phpdotenv 2>&1 && echo "dependency present on disk: OK"
echo -n "composer ran in the deploy log: "
grep -oE '\[composer\]' /tmp/deploy.json | head -1

sec "perms for OLS(nobody) + php-fpm + reload"
chmod o+x $site $site/releases 2>/dev/null
chmod -R o+rX $site/releases 2>/dev/null
chmod o+rwx $site/logs 2>/dev/null
/usr/sbin/php-fpm8.3 --daemonize 2>/tmp/fpm.log
sleep 1
# OLS runs as nobody and talks to the pool socket; the pool is owned by the site
# user. Production uses per-vhost suEXEC instead of loosening the socket.
chmod 0666 /run/heropanel/fpm/hps1.sock 2>/dev/null
/usr/local/lsws/bin/lswsctrl reload >/dev/null 2>&1; sleep 1

sec "*** CURL THE LARAVEL-SHAPED APP (composer autoload + MariaDB) ***"
curl -s -H 'Host: app.test' http://127.0.0.1/ 2>&1 | head -5

sec "DATABASE SIZE"
api $base/api/v1/databases/$dbuid/size; echo

sec "DATABASE EXPORT (streams a gzip, then deletes the server-side dump)"
api -o /tmp/dump.sql.gz -D /tmp/dump.hdr $base/api/v1/databases/$dbuid/export
grep -iE 'content-type|content-disposition' /tmp/dump.hdr
echo -n "dump is gzip: "
if gunzip -t /tmp/dump.sql.gz 2>/dev/null; then echo OK; else echo "FAIL:"; head -c 300 /tmp/dump.sql.gz; echo; fi
echo -n "dump contains the row: "; gunzip -c /tmp/dump.sql.gz 2>/dev/null | grep -c 'hello from mariadb'
echo -n "server-side dump cleaned up: "
if [ -z "$(ls -A /var/lib/heropanel/dumps 2>/dev/null)" ]; then echo OK; else echo "FAIL:"; ls -la /var/lib/heropanel/dumps; fi

sec "DATABASE IMPORT (drop a row, restore it from the dump)"
mysql laravel_db -e "DELETE FROM posts;"
echo -n "rows after delete: "; mysql -N -B laravel_db -e "SELECT COUNT(*) FROM posts;"
api -X POST --data-binary @/tmp/dump.sql.gz "$base/api/v1/databases/$dbuid/import?filename=dump.sql.gz"; echo
echo -n "rows after import: "; mysql -N -B laravel_db -e "SELECT COUNT(*) FROM posts;"
echo -n "restored title:    "; mysql -N -B laravel_db -e "SELECT title FROM posts LIMIT 1;"

sec "ADMINER HAND-OFF (a throwaway account, not a stored password)"
sso=$(api -X POST $base/api/v1/databases/$dbuid/adminer-sso)
echo "$sso" | head -c 300; echo
ssouser=$(echo "$sso" | grep -oE '"username":"[^"]+"' | cut -d'"' -f4)
echo -n "hand-off account exists in MariaDB: "
mysql -N -B -e "SELECT COUNT(*) FROM mysql.user WHERE user='$ssouser';"
echo -n "it is scoped to just this database: "
mysql -N -B -e "SHOW GRANTS FOR '$ssouser'@'localhost';" | grep -c 'laravel_db'

sec "REVOKE + DELETE USER"
api -X POST $base/api/v1/databases/$dbuid/revoke -H 'Content-Type: application/json' \
  -d "{\"user_uid\":\"$useruid\",\"privileges\":[\"ALL\"]}"; echo
echo -n "grants after revoke: "; mysql -N -B -e "SHOW GRANTS FOR 'laravel_user'@'localhost';" | grep -c 'laravel_db'
api -X DELETE $base/api/v1/database-users/$useruid; echo
echo -n "user rows in MariaDB after delete: "; mysql -N -B -e "SELECT COUNT(*) FROM mysql.user WHERE user='laravel_user';"

sec "broker audit (database + git capability outcomes)"
grep -oE '"capability":"(db\.[a-z._]+|git\.deploy)","outcome":"[^"]+"' /tmp/broker.log | sort | uniq -c

sec "OLS error log tail"
tail -5 /usr/local/lsws/logs/error.log 2>&1
