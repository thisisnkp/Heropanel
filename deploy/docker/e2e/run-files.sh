#!/usr/bin/env bash
# Phase 4 exit criteria, File Manager: browse, edit, upload, download, mkdir,
# rename, chmod, delete, and archive extraction — all driven through the API and
# performed by the root broker *as the site's own Linux user*, confined to the
# site home.
#
# The two invariants this proves against real software (not mocks):
#   1. run-as-user — a file written through the API is owned by the site's uid,
#      not root. That is what actually contains the blast radius.
#   2. path confinement — a request naming ../../etc/passwd is clamped under the
#      site root; the real /etc/passwd is never touched.
#
# It also proves the baremetal-only gate: a git-managed site refuses file ops.
set -u
sec(){ echo; echo "======== $* ========"; }
pass(){ echo "PASS: $*"; }
fail(){ echo "FAIL: $*"; FAILED=1; }
FAILED=0
base=http://127.0.0.1:18443
site=/srv/heropanel/sites/1

sec "start hp-broker (root) + hpd"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel /srv/heropanel/sites
export HP_BROKER_TOKEN=tok
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 HP_BROKER_PANEL_USER=root \
  hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18443 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp.db \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done
echo "readyz: $(curl -s $base/readyz)"

sec "auth (bootstrap + login + CSRF)"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }

sec "CREATE A BAREMETAL SITE (a real Linux user is created for it)"
api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"Files","primary_domain":"files.test","type":"php"}' >/dev/null
sj=$(api $base/api/v1/sites)
uid=$(echo "$sj" | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
SU=$(echo "$sj" | grep -oE '"system_user":"[^"]+"' | head -1 | cut -d'"' -f4)
echo "site uid=$uid  system_user=$SU  deploy_mode=$(echo "$sj" | grep -oE '"deploy_mode":"[^"]+"' | head -1 | cut -d'"' -f4)"
echo -n "the site's Linux user exists: "; id "$SU" >/dev/null 2>&1 && echo "OK ($SU)" || echo "MISSING"

sec "LIST the freshly provisioned site root"
api "$base/api/v1/sites/$uid/files?path=" ; echo

sec "WRITE a file through the API (editor save / upload path)"
printf '<?php echo "hello from the file manager";' > /tmp/index.php
api -X PUT --data-binary @/tmp/index.php "$base/api/v1/sites/$uid/files/content?path=public/index.php"; echo
if [ -f "$site/public/index.php" ]; then pass "file exists on disk"; else fail "file was not written to disk"; fi

sec ">>> INVARIANT 1: the file is owned by the SITE USER, not root <<<"
owner=$(stat -c '%U' "$site/public/index.php" 2>/dev/null)
echo "owner of public/index.php = $owner"
if [ "$owner" = "$SU" ]; then pass "write ran as the site user ($SU), not root"; else fail "expected owner $SU, got $owner"; fi

sec "READ / DOWNLOAD the file back (bytes must round-trip)"
got=$(api "$base/api/v1/sites/$uid/files/content?path=public/index.php")
echo "downloaded: $got"
if [ "$got" = '<?php echo "hello from the file manager";' ]; then pass "download matches what was written"; else fail "content mismatch"; fi

sec "BINARY round-trip (a file with NUL and high bytes)"
printf '\x00\x01\xff\x10\x00\x7f BINARY' > /tmp/blob.bin
api -X PUT --data-binary @/tmp/blob.bin "$base/api/v1/sites/$uid/files/content?path=blob.bin" >/dev/null
api "$base/api/v1/sites/$uid/files/content?path=blob.bin" -o /tmp/blob.out
if cmp -s /tmp/blob.bin /tmp/blob.out; then pass "binary content survived the round-trip"; else fail "binary content corrupted"; fi

sec "MKDIR"
api -X POST "$base/api/v1/sites/$uid/files/mkdir" -H 'Content-Type: application/json' -d '{"path":"assets/img"}'; echo
if [ -d "$site/assets/img" ]; then pass "directory created"; else fail "mkdir did not create the directory"; fi
echo -n "mkdir owner: "; stat -c '%U' "$site/assets/img"

sec "EXTRACT a zip archive (staged on disk, unpacked via the API)"
# The image ships unzip but not the `zip` CLI, so build the archive with Python.
python3 - <<'PY'
import zipfile
z = zipfile.ZipFile('/tmp/pkg.zip', 'w', zipfile.ZIP_DEFLATED)
z.writestr('a.txt', 'packed-file-A\n')
z.writestr('sub/b.txt', 'packed-file-B\n')
z.close()
PY
# Upload the archive through the API, then extract it into assets/.
api -X PUT --data-binary @/tmp/pkg.zip "$base/api/v1/sites/$uid/files/content?path=pkg.zip" >/dev/null
api -X POST "$base/api/v1/sites/$uid/files/extract" -H 'Content-Type: application/json' \
  -d '{"archive":"pkg.zip","dest":"assets"}'; echo
if [ -f "$site/assets/a.txt" ] && [ -f "$site/assets/sub/b.txt" ]; then pass "archive extracted (nested entries too)"; else fail "extract did not produce the files"; fi
echo -n "extracted file owner: "; stat -c '%U' "$site/assets/a.txt"

sec "RENAME"
api -X POST "$base/api/v1/sites/$uid/files/rename" -H 'Content-Type: application/json' \
  -d '{"from":"public/index.php","to":"public/home.php"}'; echo
if [ -f "$site/public/home.php" ] && [ ! -f "$site/public/index.php" ]; then pass "renamed"; else fail "rename did not move the file"; fi

sec "CHMOD"
api -X POST "$base/api/v1/sites/$uid/files/chmod" -H 'Content-Type: application/json' \
  -d '{"path":"public/home.php","mode":"600"}'; echo
mode=$(stat -c '%a' "$site/public/home.php")
if [ "$mode" = "600" ]; then pass "mode is now 600"; else fail "expected 600, got $mode"; fi

sec "DELETE"
api -X DELETE "$base/api/v1/sites/$uid/files?path=blob.bin"; echo
if [ ! -e "$site/blob.bin" ]; then pass "file deleted"; else fail "delete left the file behind"; fi

sec "COMPRESS (create an archive from selected entries)"
api -X POST "$base/api/v1/sites/$uid/files/compress" -H 'Content-Type: application/json' \
  -d '{"sources":["assets/a.txt","assets/sub"],"archive":"assets/bundle.zip","format":"zip"}'; echo
if [ -f "$site/assets/bundle.zip" ]; then pass "archive created"; else fail "compress did not produce an archive"; fi
echo -n "archive owner: "; stat -c '%U' "$site/assets/bundle.zip" 2>/dev/null
# The archive must contain the entries by their *relative* names, not the server's
# absolute paths — otherwise unzipping it elsewhere would recreate /srv/....
# -Z1 lists entry names only. `unzip -l` would print the archive's own absolute
# path in its header, which is not part of the archive's contents.
names=$(unzip -Z1 "$site/assets/bundle.zip" 2>/dev/null)
echo "-- archive entries --"; echo "$names"
if echo "$names" | grep -qx 'a.txt' && echo "$names" | grep -qx 'sub/b.txt' && \
   ! echo "$names" | grep -q 'srv/heropanel'; then
  pass "archive holds relative paths, not the server's absolute tree"
else
  fail "archive contents are wrong"
fi
# Round-trip: extract the archive somewhere else and compare.
api -X POST "$base/api/v1/sites/$uid/files/extract" -H 'Content-Type: application/json' \
  -d '{"archive":"assets/bundle.zip","dest":"roundtrip"}' >/dev/null
if [ -f "$site/roundtrip/a.txt" ] && grep -q packed-file-A "$site/roundtrip/a.txt"; then
  pass "compress -> extract round-trips the content"
else
  fail "the compressed archive did not extract back correctly"
fi

sec "COPY / MOVE (the paste half of copy-cut-paste)"
api -X POST "$base/api/v1/sites/$uid/files/copy" -H 'Content-Type: application/json' \
  -d '{"from":"assets/a.txt","to":"roundtrip/a-copy.txt"}'; echo
if [ -f "$site/assets/a.txt" ] && [ -f "$site/roundtrip/a-copy.txt" ]; then
  pass "copy left the original and produced the destination"
else
  fail "copy did not produce both files"
fi
echo -n "copied file owner: "; stat -c '%U' "$site/roundtrip/a-copy.txt"
if [ "$(stat -c '%U' "$site/roundtrip/a-copy.txt")" = "$SU" ]; then
  pass "copy ran as the site user"
else
  fail "copy did not run as $SU"
fi
# Refusing to overwrite is the point: cp would clobber silently.
code=$(api -o /tmp/clash.json -w '%{http_code}' -X POST "$base/api/v1/sites/$uid/files/copy" \
  -H 'Content-Type: application/json' -d '{"from":"assets/a.txt","to":"roundtrip/a-copy.txt"}')
echo "copy onto an existing name -> HTTP $code : $(head -c 160 /tmp/clash.json)"
if [ "$code" = "409" ] && grep -q destination_exists /tmp/clash.json; then
  pass "copy refuses to overwrite by default"
else
  fail "copy did not refuse an existing destination"
fi
# ...unless the caller asks for a free name (the Duplicate action).
dup=$(api -X POST "$base/api/v1/sites/$uid/files/copy" -H 'Content-Type: application/json' \
  -d '{"from":"assets/a.txt","to":"roundtrip/a-copy.txt","on_conflict":"rename"}')
echo "duplicate -> $dup"
if [ -f "$site/roundtrip/a-copy copy.txt" ]; then
  pass "on_conflict=rename landed beside the original"
else
  fail "on_conflict=rename did not pick a free name"
fi
# Move relocates rather than duplicating.
api -X POST "$base/api/v1/sites/$uid/files/move" -H 'Content-Type: application/json' \
  -d '{"from":"roundtrip/a-copy.txt","to":"assets/moved.txt"}'; echo
if [ -f "$site/assets/moved.txt" ] && [ ! -e "$site/roundtrip/a-copy.txt" ]; then
  pass "move relocated the file"
else
  fail "move did not relocate the file"
fi
# Both ends of a copy are clamped, so a traversing destination cannot escape.
api -X POST "$base/api/v1/sites/$uid/files/copy" -H 'Content-Type: application/json' \
  -d '{"from":"assets/a.txt","to":"../../../../../../tmp/escaped.txt"}' >/dev/null
if [ ! -e /tmp/escaped.txt ]; then
  pass "a traversing copy destination was clamped inside the site"
else
  fail "copy escaped the site root"
  rm -f /tmp/escaped.txt
fi

sec "FOLDER DOWNLOAD (server builds the zip, streams it, and cleans up)"
before_count=$(find "$site" -name '.hp-download-*' | wc -l)
api "$base/api/v1/sites/$uid/files/archive?path=assets" -o /tmp/folder.zip
if [ -s /tmp/folder.zip ] && unzip -tq /tmp/folder.zip >/dev/null 2>&1; then
  pass "the folder downloaded as a valid zip ($(stat -c '%s' /tmp/folder.zip) bytes)"
else
  fail "the folder download was not a valid zip"
fi
if unzip -Z1 /tmp/folder.zip | grep -q 'assets/a.txt'; then
  pass "the archive contains the folder's entries"
else
  echo "-- entries --"; unzip -Z1 /tmp/folder.zip
  fail "the archive did not contain the expected entries"
fi
# The whole reason this endpoint exists: nothing is left behind in the tree.
after_count=$(find "$site" -name '.hp-download-*' | wc -l)
echo "temp archives before=$before_count after=$after_count"
if [ "$after_count" = "0" ]; then
  pass "no temporary archive was left in the site tree"
else
  fail "the download left $after_count temp archive(s) behind"
fi

sec "SEARCH (recursive, as the site user)"
echo -n "by name:    "
api "$base/api/v1/sites/$uid/files/search?q=a.txt&mode=name" | head -c 300; echo
if api "$base/api/v1/sites/$uid/files/search?q=a.txt&mode=name" | grep -q '"name":"a.txt"'; then
  pass "name search found the file"
else
  fail "name search did not find a known file"
fi
echo -n "by content: "
api "$base/api/v1/sites/$uid/files/search?q=packed-file-A&mode=content" | head -c 300; echo
if api "$base/api/v1/sites/$uid/files/search?q=packed-file-A&mode=content" | grep -q 'a.txt'; then
  pass "content search found the file containing the string"
else
  fail "content search did not find a known string"
fi
# A search must not be able to reach outside the site, even with a traversing path.
esc=$(api "$base/api/v1/sites/$uid/files/search?q=passwd&mode=name&path=../../../../etc")
if ! echo "$esc" | grep -q '"name":"passwd"'; then
  pass "a traversing search path cannot list /etc"
else
  fail "search escaped the site root"
fi

sec "REPAIR OWNERSHIP (root-run chown, but only ever to the site's own user)"
# Break ownership deliberately, then let the panel repair it.
chown -R root:root "$site/assets" && echo "broke ownership -> $(stat -c '%U' "$site/assets/a.txt")"
api -X POST "$base/api/v1/sites/$uid/files/chown" -H 'Content-Type: application/json' \
  -d '{"path":"assets"}'; echo
owner_after=$(stat -c '%U' "$site/assets/a.txt")
echo "after repair -> $owner_after"
if [ "$owner_after" = "$SU" ]; then pass "ownership repaired to the site user"; else fail "expected $SU, got $owner_after"; fi

sec ">>> INVARIANT 2: PATH ESCAPE is clamped under the site root <<<"
# (a) Safety: a traversal aimed at /etc/passwd must never touch the real file.
before=$(stat -c '%Y' /etc/passwd)
api -X PUT --data-binary 'PWNED' "$base/api/v1/sites/$uid/files/content?path=../../../../../../etc/passwd" >/tmp/escape.json 2>&1
echo "escape -> $(head -c 160 /tmp/escape.json)"
after=$(stat -c '%Y' /etc/passwd)
if [ "$before" = "$after" ] && ! grep -q PWNED /etc/passwd; then
  pass "the real /etc/passwd was NOT modified"
else
  fail "path traversal reached /etc/passwd"
fi
# (b) Clamp lands inside: a traversal that resolves to a writable in-root path is
# redirected under the site root, and the write there still runs as the site user.
api -X PUT --data-binary 'CLAMPED' "$base/api/v1/sites/$uid/files/content?path=deep/dir/../../../../../clamped.txt" >/dev/null
if [ -f "$site/clamped.txt" ] && grep -q CLAMPED "$site/clamped.txt"; then
  pass "the traversal was clamped to <site-root>/clamped.txt (owner $(stat -c '%U' "$site/clamped.txt"))"
else
  fail "clamped write did not land under the site root"
fi

sec ">>> BAREMETAL-ONLY GATE: a git site refuses file ops <<<"
api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"GitSite","primary_domain":"git.test","type":"php","deploy_mode":"git"}' >/dev/null
guid=$(api $base/api/v1/sites | grep -oE '"uid":"[^"]+"' | tail -1 | cut -d'"' -f4)
code=$(api -o /tmp/gate.json -w '%{http_code}' "$base/api/v1/sites/$guid/files?path=")
echo "git-site file.list -> HTTP $code : $(head -c 160 /tmp/gate.json)"
if [ "$code" = "403" ] && grep -q not_baremetal /tmp/gate.json; then pass "git site is refused (not_baremetal)"; else fail "git site was not refused as expected"; fi

sec "broker audit — every file.* op allowed by policy (the one write failure is the escape attempt above)"
grep -oE '"capability":"file\.[a-z]+","outcome":"[^"]+"' /tmp/broker.log | sort | uniq -c

sec "RESULT"
if [ "$FAILED" = "0" ]; then echo "run-files.sh : PASS"; else echo "run-files.sh : FAIL"; exit 1; fi
