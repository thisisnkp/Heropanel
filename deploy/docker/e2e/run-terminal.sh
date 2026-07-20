#!/usr/bin/env bash
# Phase 4 exit criteria, terminal half: an audited interactive shell scoped to a
# single site user.
#
# The invariant that matters: the PTY runs as the *site's* Linux account, not
# root. Everything else (I/O round-trip, cwd, teardown, audit) is verified around
# that. A real WebSocket client (termclient, built from the same coder/websocket
# library hpd uses) drives it, because a terminal cannot be exercised with curl.
set -u
sec(){ echo; echo "======== $* ========"; }
pass(){ echo "PASS: $*"; }
fail(){ echo "FAIL: $*"; FAILED=1; }
FAILED=0
base=http://127.0.0.1:18443
hostport=127.0.0.1:18443

sec "start hp-broker (root) + hpd"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
install -m0755 /hp/hp-termtest /usr/local/bin/ 2>/dev/null || true
mkdir -p /run/heropanel /srv/heropanel/sites
export HP_BROKER_TOKEN=tok
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 HP_BROKER_PANEL_USER=root \
  hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18443 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp.db \
  HP_TERMINAL_RECORDING_DIR=/var/lib/heropanel/recordings \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done
echo "readyz: $(curl -s $base/readyz)"

sec "auth"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/c.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/c.txt)
SESSION=$(awk '/hp_session/{print $7}' /tmp/c.txt)
api(){ curl -s -b /tmp/c.txt -H "X-CSRF-Token: $CSRF" "$@"; }
echo -n "have a session cookie: "; [ -n "$SESSION" ] && echo OK || echo MISSING

sec "CREATE A SITE (a real Linux user is created for it)"
api -X POST $base/api/v1/sites -H 'Content-Type: application/json' \
  -d '{"name":"Term","primary_domain":"term.test","type":"php"}' >/dev/null
sj=$(api $base/api/v1/sites)
uid=$(echo "$sj" | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
SU=$(echo "$sj" | grep -oE '"system_user":"[^"]+"' | head -1 | cut -d'"' -f4)
echo "site uid=$uid  system_user=$SU"
echo -n "the site's Linux user exists: "; id "$SU" >/dev/null 2>&1 && echo "OK ($SU)" || echo "MISSING"

sec "*** OPEN A TERMINAL AND RUN COMMANDS OVER THE WEBSOCKET ***"
hp-termtest -base "$hostport" -cookie "hp_session=$SESSION" -site "$uid" \
  -script 'id -un; pwd; echo MARKER_$((6*7)); exit
' > /tmp/term.out 2>&1
cat /tmp/term.out

sec ">>> INVARIANT: the shell runs as the SITE USER, not root <<<"
# `id -un` printed the account the shell is actually running as. Terminal escape
# sequences share the line, so match the name anywhere and rule out root.
if grep -qE "(^|[^a-z])$SU([^a-z]|$)" /tmp/term.out && ! grep -qE '(^|[^a-z])root([^a-z]|$)' /tmp/term.out; then
  pass "the PTY reported the site user ($SU), never root"
else
  fail "the shell did not report running as $SU"
fi

sec "the session starts in the site home, and I/O round-trips"
if grep -q "/srv/heropanel/sites/1" /tmp/term.out; then pass "pwd is the site home"; else fail "pwd was not the site home"; fi
if grep -q "MARKER_42" /tmp/term.out; then pass "the shell executed input and returned its output"; else fail "command output did not round-trip"; fi
if grep -q "CONNECTED" /tmp/term.out; then pass "websocket upgrade accepted"; else fail "websocket did not connect"; fi
if grep -q 'CONTROL {"type":"exit"' /tmp/term.out; then pass "clean exit control frame delivered"; else echo "note: session closed without an explicit exit frame"; fi

sec ">>> A TRAVERSING START DIRECTORY IS CLAMPED, NEVER /etc <<<"
# "../../../../etc" clamps to <site-root>/etc, which does not exist, so the
# session falls back to the site home. Either way it must never be the real /etc.
hp-termtest -base "$hostport" -cookie "hp_session=$SESSION" -site "$uid" -cwd '../../../../etc' \
  -script 'pwd; exit
' > /tmp/term-cwd.out 2>&1
grep -aoE '/[a-z/0-9]+' /tmp/term-cwd.out | grep -E '^/(etc|srv)' | head -3
if grep -q "/srv/heropanel/sites/1" /tmp/term-cwd.out && ! grep -qE '(^|[^a-z])/etc([^a-z]|$)' /tmp/term-cwd.out; then
  pass "the traversing cwd was clamped under the site root (landed in the site home)"
else
  fail "the starting directory escaped the site root"
fi

sec ">>> UNAUTHENTICATED ACCESS IS REFUSED <<<"
hp-termtest -base "$hostport" -site "$uid" -script 'id -un
' > /tmp/term-anon.out 2>&1
head -2 /tmp/term-anon.out
if grep -q "DIAL_FAILED" /tmp/term-anon.out; then pass "no session cookie -> upgrade refused"; else fail "an unauthenticated client got a terminal"; fi

sec "processes are cleaned up when the session ends"
sleep 1
leftover=$(pgrep -u "$SU" -f 'bash' | wc -l)
echo "shell processes still running as $SU: $leftover"
if [ "$leftover" = "0" ]; then pass "no orphaned shell left behind"; else fail "a shell survived the disconnect"; fi

sec "broker audit — the session is recorded, with the account it ran as"
grep -oE '"capability":"terminal.open","outcome":"[^"]+"' /tmp/broker.log | sort | uniq -c
if grep -q '"capability":"terminal.open","outcome":"success"' /tmp/broker.log; then
  pass "terminal.open recorded as success on the hash chain"
else
  fail "the terminal session was not audited"
fi

sec "hpd audit log — the HTTP side recorded the session too"
api "$base/api/v1/audit?limit=50" > /tmp/audit.json
if grep -q 'terminal' /tmp/audit.json; then pass "hpd audited the terminal request"; else echo "note: no terminal row in the hpd audit page"; fi

sec "*** SESSION RECORDING: a typed PASSWORD must never reach the transcript ***"
# `read -s` is a real password prompt: it turns terminal echo off, reads the
# secret, and consumes it — exactly what sudo and mysql -p do. Recording captures
# keystrokes, so without redaction the secret would land in the transcript.
#
# The lines are typed one at a time (the client pauses between them) because echo
# only goes off in *response to* the previous line, which is how a real prompt
# works. Typing a secret at a plain shell prompt would not test this: the shell
# would then try to run it as a command and print it back in an error, putting it
# in the recording's *output* — a leak the program itself caused, which no input
# redaction can prevent, and which this test would wrongly blame on us.
SECRET='hunter2SuperSecret'
/hp/hp-termtest -base 127.0.0.1:18443 -cookie "$(awk '/hp_session/{print "hp_session="$7}' /tmp/c.txt)" \
  -site "$uid" -step 700ms -timeout 25s \
  -script "echo VISIBLE_MARKER
read -s -p 'Password: ' PW
$SECRET
echo AFTER_MARKER
exit
" > /tmp/rec-session.txt 2>&1
echo "--- session ---"; tail -c 500 /tmp/rec-session.txt

# Give hpd a moment to close the file and finish the row.
sleep 1
# Newest, not arbitrary: earlier sections in this script opened their own
# sessions, and `head -1` was picking one of those instead of the password one.
cast=$(ls -t $(find /var/lib/heropanel/recordings -name '*.cast') 2>/dev/null | head -1)
echo "recording file: ${cast:-<none>}"
if [ -n "$cast" ] && [ -s "$cast" ]; then
  pass "a recording was written for the session"
else
  fail "no recording file was produced"
fi

if [ -n "$cast" ]; then
  echo "--- recording (first 800 bytes) ---"; head -c 800 "$cast"; echo
  # THE invariant. If this ever fails, the panel is storing plaintext passwords.
  if grep -q "$SECRET" "$cast"; then
    fail "THE TYPED PASSWORD IS IN THE RECORDING — redaction is broken"
  else
    pass "the typed password is NOT in the recording"
  fi
  if grep -q '\[redacted\]' "$cast"; then
    pass "the redaction marker is present, so playback shows something happened"
  else
    fail "no redaction marker: input during the no-echo window was dropped silently"
  fi
  # Redaction must be surgical, not a blanket "stop recording input".
  if grep -q 'VISIBLE_MARKER' "$cast" && grep -q 'AFTER_MARKER' "$cast"; then
    pass "normal input either side of the prompt was still recorded"
  else
    fail "redaction swallowed input outside the no-echo window"
  fi
  # A recording is only useful if it is the documented format.
  if head -1 "$cast" | grep -q '"version":\s*2'; then
    pass "the file is a valid asciicast v2 (readable outside this panel)"
  else
    fail "the recording header is not asciicast v2"
  fi
fi

sec "the recording is listed by the API, and reading it is itself audited"
recs=$(api "$base/api/v1/sites/$uid/terminal/recordings")
echo "$recs" | head -c 400; echo
rid=$(echo "$recs" | grep -oE '"uid":"[^"]+"' | head -1 | cut -d'"' -f4)
if [ -n "$rid" ]; then
  pass "the recording is listed with its metadata"
  code=$(api -o /tmp/cast.out -w '%{http_code}' "$base/api/v1/terminal/recordings/$rid/cast")
  if [ "$code" = "200" ] && [ -s /tmp/cast.out ]; then
    pass "the recording downloads over the API"
  else
    fail "the recording could not be downloaded (HTTP $code)"
  fi
  if grep -q "$SECRET" /tmp/cast.out; then
    fail "the password came back through the API"
  else
    pass "the downloaded transcript also holds no password"
  fi
else
  fail "no recording was listed by the API"
fi

sec "RESULT"
if [ "$FAILED" = "0" ]; then echo "run-terminal.sh : PASS"; else echo "run-terminal.sh : FAIL"; exit 1; fi
