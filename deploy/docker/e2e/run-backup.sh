#!/usr/bin/env bash
# Phase 6, Backup module: full + incremental, zstd, ALWAYS encrypted, restored
# into a NEW site — the roadmap's exit criterion, observed end to end.
#
# What only a live run can prove: the chain (full + incremental via GNU tar's
# own snapshots) actually reproduces the site's files, the plaintext staging
# archive never outlives the call, what sits at rest is ciphertext (blobcrypt
# magic, unreadable as tar), and the restored tree belongs to the NEW site's
# user. Then the three once-deferred legs, all live: the S3 target against a
# REAL MinIO bucket (the signer is also unit-tested against a recomputing
# fake), a database dump riding along and restored into a NEW database on a
# real MariaDB, and the panel's self-backup opened offline with `hpd decrypt`.
set -u
sec(){ echo; echo "======== $* ========"; }
pass(){ echo "PASS: $*"; }
fail(){ echo "FAIL: $*"; FAILED=1; }
FAILED=0
base=http://127.0.0.1:18477

sec "start MariaDB (for the database-in-backup leg)"
mkdir -p /run/mysqld && chown mysql:mysql /run/mysqld
[ -d /var/lib/mysql/mysql ] || mariadb-install-db --user=mysql --datadir=/var/lib/mysql >/dev/null 2>&1
mariadbd --user=mysql >/tmp/mariadb-backup.log 2>&1 &
for i in $(seq 1 40); do mysqladmin ping >/dev/null 2>&1 && break; sleep 0.5; done
echo "mariadb: $(mysqladmin ping 2>&1)"

sec "start MinIO — a real, independent S3 implementation"
MINIO_ROOT_USER=hpaccess MINIO_ROOT_PASSWORD=hpsecret12 \
  minio server /tmp/minio-data --address 127.0.0.1:19000 >/tmp/minio.log 2>&1 &
for i in $(seq 1 60); do curl -sf http://127.0.0.1:19000/minio/health/live >/dev/null 2>&1 && break; sleep 0.25; done
curl -sf http://127.0.0.1:19000/minio/health/live >/dev/null && echo "minio: up" || echo "minio: DID NOT START"

sec "start OpenLiteSpeed + hp-broker + hpd (sqlite, sealed backups, s3 configured)"
/usr/local/lsws/bin/lswsctrl start 2>&1
sleep 1
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel /srv/heropanel/sites
export HP_BROKER_TOKEN=tok
export HP_SECRET_KEY=$(head -c32 /dev/urandom | base64 -w0)
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 HP_BROKER_PANEL_USER=root \
  hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker-backup.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18477 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp-backup.db \
  HP_BACKUP_S3_ENDPOINT=http://127.0.0.1:19000 HP_BACKUP_S3_BUCKET=heropanel-e2e \
  HP_BACKUP_S3_ACCESS_KEY=hpaccess HP_BACKUP_S3_SECRET_KEY=hpsecret12 \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd-backup.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "auth"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/cb.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/cb.txt)
api(){ curl -s -b /tmp/cb.txt -H "X-CSRF-Token: $CSRF" "$@"; }
code(){ curl -s -o /dev/null -w '%{http_code}' -b /tmp/cb.txt -H "X-CSRF-Token: $CSRF" "$@"; }
juid(){ python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["uid"])'; }

sec "create the ORIGINAL site with real content"
uid=$(api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"Original","primary_domain":"orig.test","type":"static"}' | juid)
[ -n "$uid" ] && pass "site created ($uid)" || fail "site create failed"
echo "v1 content" > /srv/heropanel/sites/1/public/hello.txt
chown hps1:hps1 /srv/heropanel/sites/1/public/hello.txt

sec "*** FULL BACKUP: SEALED BEFORE IT TOUCHES STORAGE ***"
full=$(api -X POST "$base/api/v1/sites/$uid/backups" -H 'Content-Type: application/json' -d '{}')
echo "$full"
fuid=$(echo "$full" | juid)
echo "$full" | grep -q '"level":"full"' && pass "the first backup is a full" || fail "first backup was not full"
fsize=$(echo "$full" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["size_bytes"])')
[ "$fsize" -gt 0 ] && pass "the sealed archive has size ($fsize bytes)" || fail "zero-size backup"

enc="/var/lib/heropanel/backups/$fuid.enc"
[ -f "$enc" ] && pass "the sealed archive exists at rest" || fail "no sealed archive on disk"
[ "$(head -c4 "$enc")" = "HPB1" ] && pass "what is at rest is blobcrypt ciphertext (HPB1 magic)" \
  || fail "the at-rest file is not sealed: $(head -c8 "$enc" | xxd | head -1)"
tar --zstd -tf "$enc" >/dev/null 2>&1 && fail "THE AT-REST ARCHIVE IS READABLE AS TAR — NOT ENCRYPTED" \
  || pass "the at-rest archive is unreadable as tar"
ls /var/lib/heropanel/backups/*.tar.zst >/dev/null 2>&1 \
  && fail "A PLAINTEXT STAGING ARCHIVE OUTLIVED THE CALL" \
  || pass "no plaintext staging archive remains"

sec "*** INCREMENTAL: ONLY THE CHANGES ***"
echo "v2 extra" > /srv/heropanel/sites/1/public/extra.txt
echo "v1 content, edited" > /srv/heropanel/sites/1/public/hello.txt
chown hps1:hps1 /srv/heropanel/sites/1/public/extra.txt /srv/heropanel/sites/1/public/hello.txt
incr=$(api -X POST "$base/api/v1/sites/$uid/backups" -H 'Content-Type: application/json' -d '{}')
echo "$incr"
iuid=$(echo "$incr" | juid)
echo "$incr" | grep -q '"level":"incr"' && pass "the second backup is an incremental" || fail "second backup was not incr"
isize=$(echo "$incr" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["size_bytes"])')
[ "$isize" -lt "$fsize" ] && pass "the incremental is smaller than the full ($isize < $fsize)" \
  || fail "the incremental ($isize) is not smaller than the full ($fsize)"

sec "*** RESTORE THE CHAIN INTO A NEW SITE ***"
restored=$(api -X POST "$base/api/v1/sites/$uid/backups/$iuid/restore" -H 'Content-Type: application/json' \
  -d '{"name":"Restored","primary_domain":"restored.test"}')
echo "$restored"
ruid=$(echo "$restored" | juid)
[ -n "$ruid" ] && pass "restore returned the new site ($ruid)" || fail "restore did not return a site"

rhome=/srv/heropanel/sites/2
grep -q 'v1 content, edited' "$rhome/public/hello.txt" 2>/dev/null \
  && pass "the edited file restored with its LATEST content (incremental applied)" \
  || fail "hello.txt wrong after restore: $(cat "$rhome/public/hello.txt" 2>&1)"
grep -q 'v2 extra' "$rhome/public/extra.txt" 2>/dev/null \
  && pass "the file added after the full restored (chain replayed in order)" \
  || fail "extra.txt missing after restore"
[ "$(stat -c %U "$rhome/public/hello.txt")" = "hps2" ] \
  && pass "restored files belong to the NEW site's user (hps2)" \
  || fail "restored files owned by $(stat -c %U "$rhome/public/hello.txt"), want hps2"
# The original is untouched.
grep -q 'v1 content, edited' /srv/heropanel/sites/1/public/hello.txt \
  && pass "the original site is untouched" || fail "the restore modified the original"

grep -q '"capability":"backup.create","outcome":"success"' /tmp/broker-backup.log \
  && grep -q '"capability":"backup.restore","outcome":"success"' /tmp/broker-backup.log \
  && pass "backup.create and backup.restore are on the broker's audit chain" \
  || fail "backup capabilities missing from the broker log"

sec "DELETING A FULL TAKES ITS DEPENDENTS WITH IT — EXPLICITLY"
del=$(api -X DELETE "$base/api/v1/sites/$uid/backups/$fuid")
echo "$del"
echo "$del" | grep -q "$iuid" \
  && pass "deleting the full also removed the incremental that depends on it" \
  || fail "the dependent incremental survived: $del"
[ ! -f "$enc" ] && pass "the sealed archives are gone from disk" || fail "sealed archive survived deletion"

sec "*** DATABASE RIDES ALONG: SEALED DUMP + RESTORE INTO A NEW DATABASE ***"
# The bucket hpd was configured with must already exist (EnsureBucket at boot).
[ -d /tmp/minio-data/heropanel-e2e ] && pass "hpd created the S3 bucket at boot (idempotent PUT)" \
  || fail "the bucket was not created: $(ls /tmp/minio-data 2>&1)"

dbuid=$(api -X POST $base/api/v1/databases -H 'Content-Type: application/json' -d '{"name":"shopdb"}' | juid)
[ -n "$dbuid" ] && pass "database created ($dbuid)" || fail "database create failed"
mysql --protocol=socket shopdb -e "CREATE TABLE t (v VARCHAR(64)); INSERT INTO t VALUES ('row survives the backup');"
api -X PUT "$base/api/v1/sites/$uid/backups/config" -H 'Content-Type: application/json' \
  -d "{\"enabled\":true,\"interval_hours\":24,\"target\":\"local\",\"keep_chains\":2,\"db_uid\":\"$dbuid\"}" >/dev/null

dbb=$(api -X POST "$base/api/v1/sites/$uid/backups" -H 'Content-Type: application/json' -d '{}')
echo "$dbb"
dbbuid=$(echo "$dbb" | juid)
echo "$dbb" | grep -q '"db_name":"shopdb"' && pass "the backup reports the database it carries" \
  || fail "no db_name on the backup: $dbb"
dbenc="/var/lib/heropanel/backups/$dbbuid.db.enc"
[ -f "$dbenc" ] && [ "$(head -c4 "$dbenc")" = "HPB1" ] \
  && pass "the dump at rest is a second sealed object (HPB1)" \
  || fail "no sealed dump object at $dbenc"
ls /var/lib/heropanel/dumps/*.sql* >/dev/null 2>&1 \
  && fail "A PLAINTEXT DATABASE DUMP OUTLIVED THE CALL" \
  || pass "no plaintext dump remains"

# Change the live database AFTER the backup: the restore must bring back the
# value as of backup time, into a NEW database, leaving this one alone.
mysql --protocol=socket shopdb -e "UPDATE t SET v='changed after the backup';"
rdb=$(api -X POST "$base/api/v1/sites/$uid/backups/$dbbuid/restore" -H 'Content-Type: application/json' \
  -d '{"name":"Restored DB","primary_domain":"rdb.test","db_name":"shopdb_restored"}')
echo "$rdb"
echo "$rdb" | grep -q '"database"' && pass "restore returned the new database" || fail "no database in restore response"
V=$(mysql --protocol=socket -N -B shopdb_restored -e "SELECT v FROM t;" 2>&1)
[ "$V" = "row survives the backup" ] \
  && pass "THE ROW CAME BACK IN A NEW DATABASE (as of backup time): $V" \
  || fail "restored database row = '$V'"
VO=$(mysql --protocol=socket -N -B shopdb -e "SELECT v FROM t;" 2>&1)
[ "$VO" = "changed after the backup" ] \
  && pass "the original database is untouched" || fail "original database row = '$VO'"

sec "*** LIVE S3: THE SEALED BACKUP LANDS IN A REAL BUCKET (MinIO) ***"
grep -q 'backup s3 target configured' /tmp/hpd-backup.log \
  && pass "hpd configured the s3 target" || fail "s3 target not configured in hpd"
echo "went to s3" > /srv/heropanel/sites/1/public/s3file.txt
chown hps1:hps1 /srv/heropanel/sites/1/public/s3file.txt
s3b=$(api -X POST "$base/api/v1/sites/$uid/backups" -H 'Content-Type: application/json' -d '{"target":"s3"}')
echo "$s3b"
s3uid=$(echo "$s3b" | juid)
echo "$s3b" | grep -q '"target":"s3"' && pass "backup went to the s3 target" || fail "backup did not use s3"
[ ! -f "/var/lib/heropanel/backups/$s3uid.enc" ] \
  && pass "the sealed archive is NOT on local disk" || fail "an s3 backup left a local copy"
find /tmp/minio-data -name "*$s3uid.enc*" 2>/dev/null | grep -q "$s3uid.enc" \
  && pass "the sealed archive is IN the bucket" || fail "the archive never reached the bucket"
find /tmp/minio-data -name "*$s3uid.db.enc*" 2>/dev/null | grep -q "$s3uid.db.enc" \
  && pass "the sealed dump is in the bucket too" || fail "the dump never reached the bucket"

rs3=$(api -X POST "$base/api/v1/sites/$uid/backups/$s3uid/restore" -H 'Content-Type: application/json' \
  -d '{"name":"S3 Restored","primary_domain":"rs3.test"}')
rs3uid=$(echo "$rs3" | juid)
[ -n "$rs3uid" ] && pass "restore from the bucket returned a site" || fail "s3 restore failed: $rs3"
grep -q 'went to s3' /srv/heropanel/sites/4/public/s3file.txt 2>/dev/null \
  && pass "RESTORED FROM THE BUCKET (cross-target chain: local full + s3 incremental)" \
  || fail "s3file.txt missing after s3 restore"

api -X DELETE "$base/api/v1/sites/$uid/backups/$s3uid" >/dev/null
find /tmp/minio-data -name "*$s3uid*" 2>/dev/null | grep -q "$s3uid" \
  && fail "deleting the backup left objects in the bucket" \
  || pass "deleting the backup emptied its objects from the bucket"

sec "*** PANEL SELF-BACKUP: SEALED SNAPSHOT, OPENED OFFLINE WITH hpd decrypt ***"
pb=$(api -X POST $base/api/v1/system/backups -H 'Content-Type: application/json' -d '{}')
echo "$pb"
puid=$(echo "$pb" | juid)
penc="/var/lib/heropanel/backups/$puid.enc"
[ -f "$penc" ] && [ "$(head -c4 "$penc")" = "HPB1" ] \
  && pass "the panel snapshot at rest is ciphertext" || fail "no sealed panel snapshot at $penc"
api $base/api/v1/system/backups | grep -q "$puid" && pass "the snapshot is listed" || fail "snapshot missing from list"

# Recovery needs nothing but the binary and the master key — no config, no DB.
hpd decrypt "$penc" /tmp/panel.tar.gz \
  && pass "hpd decrypt opened the snapshot offline" || fail "hpd decrypt failed"
mkdir -p /tmp/panelx && tar -xzf /tmp/panel.tar.gz -C /tmp/panelx 2>/dev/null
[ -f /tmp/panelx/panel.db ] && [ -f /tmp/panelx/manifest.json ] \
  && pass "the snapshot is a tarball with the panel DB + manifest" \
  || fail "snapshot contents: $(ls /tmp/panelx 2>&1)"
grep -q '"driver":"sqlite"' /tmp/panelx/manifest.json && pass "the manifest names the dialect" \
  || fail "manifest: $(cat /tmp/panelx/manifest.json 2>&1)"
grep -qa 'orig.test' /tmp/panelx/panel.db \
  && pass "THE SNAPSHOT HOLDS THE PANEL'S OWN DATA (the site row is in it)" \
  || fail "the panel DB copy does not contain the site row"

HP_SECRET_KEY=$(head -c32 /dev/urandom | base64 -w0) hpd decrypt "$penc" /tmp/nope.bin 2>/dev/null \
  && fail "A WRONG KEY DECRYPTED THE SNAPSHOT" \
  || pass "a wrong key is refused"
[ ! -f /tmp/nope.bin ] && pass "nothing was written on the failed decrypt" || fail "a half-decrypt was left behind"

api -X DELETE "$base/api/v1/system/backups/$puid" >/dev/null
[ ! -f "$penc" ] && pass "the panel snapshot deleted, row and object" || fail "the snapshot object survived deletion"

sec "cleanup"
pkill -f 'hpd' 2>/dev/null; pkill -f 'hp-broker' 2>/dev/null; pkill -f 'minio' 2>/dev/null; true

if [ "$FAILED" = "0" ]; then echo "run-backup.sh : PASS"; else echo "run-backup.sh : FAIL"; fi
