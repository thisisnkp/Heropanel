#!/usr/bin/env bash
# Phase 5, Docker module: list, inspect, logs, stats, and lifecycle — driven
# through the API, performed by the root broker, against a *real Docker daemon*.
#
# The invariant this exists to prove, which no unit test can:
#
#   ownership — the panel refuses to touch a container it did not create.
#
# That is the whole security boundary of this module. The daemon socket is
# root-equivalent, so `docker.container.stop` without an ownership check is a
# remote off-switch for every container on the host: the site database, a CI
# runner, the monitoring agent. Here we start a container the panel knows
# nothing about and assert the API will not stop, restart or remove it — while
# a panel-labelled container in the same daemon obeys every one of those.
#
# It also proves the second half: an unmanaged container is still *listed*.
# Hiding it would make the panel lie about the host it administers.
set -u
sec(){ echo; echo "======== $* ========"; }
pass(){ echo "PASS: $*"; }
fail(){ echo "FAIL: $*"; FAILED=1; }
FAILED=0
base=http://127.0.0.1:18444

sec "docker daemon must be reachable inside this container"
if ! docker version >/dev/null 2>&1; then
  echo "SKIP: no Docker daemon available to this test"
  exit 0
fi
docker version --format '{{.Server.Version}}'

sec "start hp-broker (root) + hpd"
install -m0755 /hp/hpd /hp/hp-broker /usr/local/bin/
mkdir -p /run/heropanel
export HP_BROKER_TOKEN=tok
HP_LOG_FORMAT=text HP_BROKER_ALLOWED_UID=0 HP_BROKER_PANEL_USER=root \
  hp-broker --serve --socket /run/heropanel/broker.sock >/tmp/broker-docker.log 2>&1 &
for i in $(seq 1 40); do [ -S /run/heropanel/broker.sock ] && break; sleep 0.2; done
HP_SERVER_HOST=127.0.0.1 HP_SERVER_PORT=18444 HP_LOG_FORMAT=text \
  HP_DATABASE_DRIVER=sqlite HP_DATABASE_DSN=/tmp/hp-docker.db \
  HP_BROKER_SOCKET=/run/heropanel/broker.sock hpd >/tmp/hpd-docker.log 2>&1 &
for i in $(seq 1 60); do curl -sf $base/healthz >/dev/null 2>&1 && break; sleep 0.25; done

sec "auth (bootstrap + login + CSRF)"
curl -s -X POST $base/api/v1/auth/bootstrap -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","username":"admin","password":"supersecret1"}' >/dev/null
curl -s -c /tmp/cd.txt -X POST $base/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"a@h.io","password":"supersecret1"}' >/dev/null
CSRF=$(awk '/hp_csrf/{print $7}' /tmp/cd.txt)
api(){ curl -s -b /tmp/cd.txt -H "X-CSRF-Token: $CSRF" "$@"; }
code(){ curl -s -o /dev/null -w '%{http_code}' -b /tmp/cd.txt -H "X-CSRF-Token: $CSRF" "$@"; }

sec "THE PANEL SEES THE DAEMON"
info=$(api $base/api/v1/docker/info)
echo "$info"
echo "$info" | grep -q '"available":true' \
  && pass "docker.info reports a usable daemon" \
  || fail "docker.info did not see the daemon"

sec "set up two containers: one the panel owns, one it does not"
docker rm -f hp-e2e-managed hp-e2e-foreign >/dev/null 2>&1 || true
docker run -d --name hp-e2e-managed \
  --label io.heropanel.managed=1 --label io.heropanel.site=site-e2e \
  alpine:3 sh -c 'echo HP_LOG_MARKER; sleep 600' >/dev/null
# No HeroPanel labels: this stands in for the host's own workloads — a database,
# a CI runner, anything an operator would be horrified to see the panel stop.
docker run -d --name hp-e2e-foreign alpine:3 sleep 600 >/dev/null
sleep 1
docker ps --format '{{.Names}}' | sed 's/^/  running: /'

sec "LISTING SHOWS BOTH, BUT ONLY ONE IS MANAGED"
list=$(api $base/api/v1/docker/containers)
echo "$list" | grep -q 'hp-e2e-managed' \
  && pass "the panel's own container is listed" \
  || fail "the panel's own container is missing from the listing"
echo "$list" | grep -q 'hp-e2e-foreign' \
  && pass "a container the panel did not create is still listed (the panel must not lie about the host)" \
  || fail "an unmanaged container was hidden from the listing"

# The managed flag is what the UI gates its buttons on, so it must be right for
# both: a false positive here would offer actions that the broker then refuses.
#
# Parsed with a real JSON reader. A first attempt split the payload on "}" and
# read the flag off the wrong object, because every container also carries a
# nested `labels` object — it reported both containers as unflagged and looked
# exactly like a broken feature. A test that misreads the output it is checking
# is worse than no test.
flagof(){ python3 -c '
import json,sys
name=sys.argv[1]
doc=json.load(sys.stdin)["data"] or []
for c in doc:
    if c.get("name")==name:
        print(json.dumps(c.get("managed")));sys.exit(0)
print("absent")' "$1"; }

managed_flag=$(echo "$list" | flagof hp-e2e-managed)
foreign_flag=$(echo "$list" | flagof hp-e2e-foreign)
echo "  managed container -> managed=$managed_flag ; foreign container -> managed=$foreign_flag"
[ "$managed_flag" = "true" ] \
  && pass "the panel's container is flagged managed" \
  || fail "the panel's own container was not flagged managed ($managed_flag)"
[ "$foreign_flag" = "false" ] \
  && pass "the foreign container is flagged unmanaged" \
  || fail "a foreign container was flagged as panel-managed ($foreign_flag) — the UI would offer actions on it"

sec "THE SITE LABEL IS CARRIED THROUGH"
echo "$list" | grep -q '"site_uid":"site-e2e"' \
  && pass "a container's site attribution survives to the API" \
  || fail "the site label did not reach the API"

sec "*** OWNERSHIP: THE PANEL REFUSES A CONTAINER IT DOES NOT MANAGE ***"
for verb in stop restart start; do
  c=$(code -X POST $base/api/v1/docker/containers/hp-e2e-foreign/$verb)
  if [ "$c" = "403" ]; then
    pass "$verb on an unmanaged container refused with 403"
  else
    fail "$verb on an unmanaged container returned $c, want 403"
  fi
done
c=$(code -X DELETE $base/api/v1/docker/containers/hp-e2e-foreign)
[ "$c" = "403" ] && pass "remove on an unmanaged container refused with 403" \
                 || fail "remove on an unmanaged container returned $c, want 403"

# The refusal must be real, not cosmetic: the container is still running.
if docker ps --format '{{.Names}}' | grep -q '^hp-e2e-foreign$'; then
  pass "the unmanaged container is STILL RUNNING after four refused operations"
else
  fail "THE PANEL STOPPED A CONTAINER IT DOES NOT MANAGE"
fi

sec "MANAGED CONTAINERS OBEY — AND ANSWER WITHIN THE REQUEST BUDGET"
# The container ignores SIGTERM (`sleep` does not handle it), so the stop always
# runs the full SIGTERM-to-SIGKILL grace. That makes this the case that caught a
# real bug: with a 30s grace against hpd's 30s HTTP write timeout, the operation
# succeeded but the connection closed first and curl reported `000` — a failure
# message for an action that worked. The timing assertion is the regression.
start_ms=$(date +%s%3N)
c=$(code -X POST $base/api/v1/docker/containers/hp-e2e-managed/restart)
[ "$c" = "200" ] && pass "restart of a managed container accepted" \
                 || fail "restart of a managed container returned $c"
sleep 1
c=$(code -X POST $base/api/v1/docker/containers/hp-e2e-managed/stop)
[ "$c" = "200" ] && pass "stop of a managed container accepted" || fail "stop returned $c"
elapsed=$(( $(date +%s%3N) - start_ms ))
echo "  restart+stop took ${elapsed}ms (hpd's HTTP write timeout is 30000ms)"
[ "$elapsed" -lt 28000 ] \
  && pass "lifecycle calls answer inside the HTTP write budget" \
  || fail "a lifecycle call took ${elapsed}ms — at or past the write timeout, so a working action reports as failed"
sleep 1
docker ps -a --filter name=hp-e2e-managed --format '{{.Names}} {{.State}}' | grep -q 'exited' \
  && pass "the managed container actually stopped" \
  || fail "the managed container is not stopped"
c=$(code -X POST $base/api/v1/docker/containers/hp-e2e-managed/start)
[ "$c" = "200" ] && pass "start of a managed container accepted" || fail "start returned $c"

sec "LOGS AND STATS"
logs=$(api "$base/api/v1/docker/containers/hp-e2e-managed/logs?tail=50")
echo "$logs" | grep -q 'HP_LOG_MARKER' \
  && pass "container logs are readable through the API" \
  || fail "the container's own output did not come back"

stats=$(api $base/api/v1/docker/stats)
echo "$stats" | grep -q 'hp-e2e-managed' \
  && pass "resource stats sampled for the running container" \
  || fail "stats did not include the running container"

sec "IMAGES"
api $base/api/v1/docker/images | grep -q 'alpine' \
  && pass "images are listed" || fail "the alpine image was not listed"

sec "AN IMAGE IN USE BY A CONTAINER CANNOT BE REMOVED"
# Images carry no managed label; docker's own "still used by a container" refusal
# is the boundary, and it must reach the caller rather than orphaning the app.
# The alpine image backs the running managed container, so its removal must fail.
alpine_id=$(api "$base/api/v1/docker/images" | python3 -c 'import sys,json;
rows=json.load(sys.stdin)["data"]
print(next((r["id"] for r in rows if r.get("repository")=="alpine"), ""))')
if [ -n "$alpine_id" ]; then
  c=$(code -X DELETE "$base/api/v1/docker/images/$(python3 -c "import urllib.parse,sys;print(urllib.parse.quote(sys.argv[1],safe=''))" "$alpine_id")")
  [ "$c" = "500" ] || [ "$c" = "409" ] || [ "$c" = "400" ] \
    && pass "an in-use image is refused removal ($c)" \
    || fail "removing an in-use image returned $c, expected a refusal"
else
  fail "could not resolve the alpine image id"
fi

sec "PRUNE REMOVES ONLY UNREFERENCED LAYERS"
# Dangling-only prune must succeed and must not remove the alpine image the
# managed container still uses.
c=$(code -X POST "$base/api/v1/docker/images/prune")
[ "$c" = "200" ] && pass "dangling prune succeeds ($c)" || fail "prune returned $c"
api "$base/api/v1/docker/images" | grep -q 'alpine' \
  && pass "prune kept the image a container still uses" \
  || fail "prune removed an image still in use"

sec "FLAG INJECTION IS REFUSED"
# An argv array stops shell injection but not a value that *is* a flag. A
# container named --privileged must be unrepresentable, not merely quoted.
c=$(code -X POST "$base/api/v1/docker/containers/--privileged/stop")
[ "$c" = "400" ] || [ "$c" = "404" ] \
  && pass "a flag-shaped container name is refused ($c)" \
  || fail "a flag-shaped container name returned $c"

sec "THE ACTION IS ON THE BROKER'S HASH CHAIN"
grep -q 'docker.container.stop' /tmp/broker-docker.log \
  && pass "docker.container.stop recorded by the broker" \
  || fail "the lifecycle action is missing from the broker's audit chain"

sec "PERMISSIONS ARE SEPARATE FROM site.*"
# docker.write must not be implied by anything else; an unauthenticated caller
# gets nothing at all.
c=$(curl -s -o /dev/null -w '%{http_code}' -X POST $base/api/v1/docker/containers/hp-e2e-managed/stop)
[ "$c" = "401" ] && pass "an unauthenticated lifecycle call is refused (401)" \
                 || fail "an unauthenticated lifecycle call returned $c"

sec "*** THE PANEL CREATES ITS OWN CONTAINER ***"
# Until this worked, the ownership guard made the whole module read-only: nothing
# could ever carry the managed label, because nothing could be created.
docker rm -f hp-e2e-created >/dev/null 2>&1 || true
docker volume rm -f hp-e2e-vol >/dev/null 2>&1 || true

c=$(code -X POST $base/api/v1/docker/volumes -H 'Content-Type: application/json' \
  -d '{"name":"hp-e2e-vol","site":"site-e2e"}')
[ "$c" = "201" ] && pass "a named volume was created" || fail "volume create returned $c"

created=$(api -X POST $base/api/v1/docker/containers -H 'Content-Type: application/json' -d '{
  "name":"hp-e2e-created","image":"alpine:3","site":"site-e2e",
  "env":{"APP_SECRET":"e2eSuperSecretValue","APP_MODE":"test"},
  "ports":[{"host":18999,"container":80}],
  "volumes":[{"volume":"hp-e2e-vol","path":"/data"}],
  "memory_mb":64,"restart":"no",
  "command":["sh","-c","echo CREATED_MARKER; sleep 120"]}')
echo "$created"
sleep 2
docker ps --format '{{.Names}}' | grep -q '^hp-e2e-created$' \
  && pass "the panel created and started a container" \
  || fail "the panel's created container is not running"

# It must be manageable — that is the entire point of creating it.
mgd=$(api $base/api/v1/docker/containers | flagof hp-e2e-created)
[ "$mgd" = "true" ] && pass "the created container is managed by the panel" \
                    || fail "the panel created a container it cannot manage ($mgd)"

sec "THE PORT IS BOUND TO LOOPBACK, NOT THE WORLD"
# Docker writes firewall rules ahead of the host's, so a container published on
# 0.0.0.0 is reachable from the internet even when the host firewall denies it.
binding=$(docker port hp-e2e-created 80 2>/dev/null || true)
echo "  docker port -> ${binding:-<none>}"
case "$binding" in
  127.0.0.1:*) pass "the published port is bound to loopback only" ;;
  0.0.0.0:*|:::*) fail "THE PORT WAS PUBLISHED ON ALL INTERFACES ($binding)" ;;
  *) fail "unexpected port binding: ${binding:-<none>}" ;;
esac

sec "HARDENING THE CREATED CONTAINER"
docker inspect hp-e2e-created --format '{{.HostConfig.Privileged}}' | grep -q '^false$' \
  && pass "the container is not privileged" || fail "the created container is PRIVILEGED"
docker inspect hp-e2e-created --format '{{.HostConfig.SecurityOpt}}' | grep -q 'no-new-privileges' \
  && pass "no-new-privileges is applied" || fail "no-new-privileges was not applied"
docker inspect hp-e2e-created --format '{{.HostConfig.Memory}}' | grep -q '^67108864$' \
  && pass "the memory limit was applied (64 MB)" || fail "the memory limit was not applied"
# Every mount must be a named volume; not one may come from the host filesystem.
hostbinds=$(docker inspect hp-e2e-created --format '{{range .Mounts}}{{.Type}} {{end}}' | tr ' ' '\n' | grep -c '^bind$' || true)
[ "${hostbinds:-0}" = "0" ] \
  && pass "the container has no host bind mounts" \
  || fail "THE CREATED CONTAINER HAS $hostbinds HOST BIND MOUNT(S)"

sec "*** A HOST BIND MOUNT IS REFUSED THROUGH THE API ***"
# The API is the boundary, not the UI. A caller writing this request by hand
# must hit exactly the same wall.
for badvol in '"/"' '"/var/run/docker.sock"' '"/etc"' '"../../etc"'; do
  c=$(code -X POST $base/api/v1/docker/containers -H 'Content-Type: application/json' \
    -d "{\"name\":\"hp-e2e-escape\",\"image\":\"alpine:3\",\"volumes\":[{\"volume\":$badvol,\"path\":\"/host\"}]}")
  if [ "$c" = "400" ]; then
    pass "a host path ($badvol) as a volume is refused"
  else
    fail "a host path ($badvol) as a volume returned $c — expected 400"
  fi
done
docker ps -a --format '{{.Names}}' | grep -q '^hp-e2e-escape$' \
  && fail "AN ESCAPING CONTAINER WAS CREATED" \
  || pass "no container was created by any of the escape attempts"

sec "*** THE SECRET IS NOT IN THE PROCESS ARGUMENTS ***"
# argv is world-readable through /proc. The env must have travelled by stdin.
if grep -q 'e2eSuperSecretValue' /tmp/broker-docker.log; then
  fail "A SECRET WAS WRITTEN TO THE BROKER LOG"
else
  pass "the environment value is not in the broker log"
fi
env_in_container=$(docker exec hp-e2e-created env 2>/dev/null | grep -c 'APP_SECRET=e2eSuperSecretValue' || true)
[ "$env_in_container" = "1" ] \
  && pass "the environment did reach the container (so it travelled, just not through argv)" \
  || fail "the environment never reached the container"

sec "SITE-SCOPED LISTING"
api "$base/api/v1/docker/containers?site=site-e2e" | grep -q 'hp-e2e-created' \
  && pass "the site-scoped listing finds the site's container" \
  || fail "site scoping did not return the container"
api "$base/api/v1/docker/containers?site=no-such-site" | grep -q 'hp-e2e-created' \
  && fail "site scoping returned a container belonging to another site" \
  || pass "site scoping excludes other sites"

sec "VOLUMES AND NETWORKS"
api $base/api/v1/docker/volumes | grep -q 'hp-e2e-vol' \
  && pass "volumes are listed" || fail "the created volume was not listed"

sec "INSPECT REPORTS A VOLUME'S CONSUMERS"
# hp-e2e-vol is mounted by hp-e2e-created, so its consumer list must name it —
# this is what makes the destructive delete an informed choice, not a guess.
api "$base/api/v1/docker/volumes/hp-e2e-vol" | grep -q 'hp-e2e-created' \
  && pass "volume inspect lists the container that mounts it" \
  || fail "volume inspect did not report its consumer"

sec "INSPECT IS READ-ONLY, NOT OWNERSHIP-GATED"
# docker's own default bridge is not the panel's, yet inspecting it must work:
# reading the truth about the host is never gated the way mutation is.
c=$(code "$base/api/v1/docker/networks/bridge")
[ "$c" = "200" ] && pass "an unmanaged network can be inspected ($c)" \
                 || fail "inspecting an unmanaged network returned $c"

c=$(code -X DELETE $base/api/v1/docker/volumes/hp-e2e-vol)
# In use by the container, so docker refuses — but the *ownership* check must
# have passed, which is a 500-from-docker rather than a 403-from-the-panel.
[ "$c" != "403" ] && pass "removing an owned volume is not refused on ownership grounds" \
                  || fail "the panel refused to remove its own volume"
c=$(code -X DELETE $base/api/v1/docker/volumes/hp-e2e-notmine)
[ "$c" = "404" ] || [ "$c" = "403" ] \
  && pass "removing a volume that is not the panel's is refused ($c)" \
  || fail "removing an unowned volume returned $c"

c=$(code -X POST $base/api/v1/docker/networks -H 'Content-Type: application/json' -d '{"name":"hp-e2e-net"}')
[ "$c" = "201" ] && pass "a bridge network was created" || fail "network create returned $c"
docker network inspect hp-e2e-net --format '{{.Driver}}' 2>/dev/null | grep -q '^bridge$' \
  && pass "the network is a bridge, not host" || fail "the created network is not a bridge"
c=$(code -X DELETE $base/api/v1/docker/networks/bridge)
[ "$c" = "403" ] && pass "removing docker's own bridge network is refused" \
                 || fail "removing the built-in bridge network returned $c"

sec "*** A SHELL IS REFUSED IN A CONTAINER THE PANEL DOES NOT MANAGE ***"
# A shell inside someone else's container bypasses every other refusal in this
# module: you would simply stop the process from within. The upgrade must fail
# before it becomes a WebSocket, so it is still a plain HTTP status here.
c=$(curl -s -o /dev/null -w '%{http_code}' -b /tmp/cd.txt -H "X-CSRF-Token: $CSRF" \
  -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' \
  -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' \
  "$base/api/v1/docker/containers/hp-e2e-foreign/exec")
[ "$c" = "403" ] && pass "a shell in an unmanaged container is refused with 403" \
                 || fail "a shell in an unmanaged container returned $c, want 403"

c=$(curl -s -o /dev/null -w '%{http_code}' \
  -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' \
  -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' \
  "$base/api/v1/docker/containers/hp-e2e-created/exec")
[ "$c" = "401" ] && pass "an unauthenticated shell request is refused (401)" \
                 || fail "an unauthenticated shell request returned $c"

# An arbitrary program is not a shell: the value names a binary in a privileged
# command, so only an allowlisted set is accepted.
c=$(curl -s -o /dev/null -w '%{http_code}' -b /tmp/cd.txt -H "X-CSRF-Token: $CSRF" \
  -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' \
  -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' \
  "$base/api/v1/docker/containers/hp-e2e-created/exec?shell=/bin/anything")
[ "$c" = "400" ] && pass "an arbitrary program is refused as a shell (400)" \
                 || fail "an arbitrary shell path returned $c, want 400"

grep -q 'docker.container.exec' /tmp/broker-docker.log \
  && pass "the exec attempt is on the broker's audit chain" \
  || fail "a container shell attempt was not audited"

sec "*** LIVE LOG FOLLOW STREAMS AND IS AUDITED ***"
# Following logs is a read, not a mutation, so — unlike a shell — it is allowed on
# any container, gated by docker.read. An unauthenticated upgrade is still refused
# before it becomes a WebSocket.
c=$(curl -s -o /dev/null -w '%{http_code}' \
  -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' \
  -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' \
  "$base/api/v1/docker/containers/hp-e2e-created/logs/stream")
[ "$c" = "401" ] && pass "an unauthenticated log follow is refused (401)" \
                 || fail "an unauthenticated log follow returned $c"

# A flag-shaped container name is rejected before the upgrade, the same as
# everywhere else in the module.
c=$(curl -s -o /dev/null -w '%{http_code}' -b /tmp/cd.txt -H "X-CSRF-Token: $CSRF" \
  -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' \
  -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' \
  "$base/api/v1/docker/containers/--privileged/logs/stream")
[ "$c" = "400" ] || [ "$c" = "404" ] \
  && pass "a flag-shaped container name is refused for a follow ($c)" \
  || fail "a flag-shaped follow target returned $c"

# A real follow against a managed container upgrades (101) and starts the stream.
# --max-time bounds it: `logs --follow` never returns on its own, which is the
# whole point, so the client is what ends the session.
c=$(curl -s -o /dev/null -w '%{http_code}' --max-time 4 -b /tmp/cd.txt -H "X-CSRF-Token: $CSRF" \
  -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' \
  -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' \
  "$base/api/v1/docker/containers/hp-e2e-created/logs/stream?tail=10" || true)
[ "$c" = "101" ] && pass "a live log follow upgrades to a WebSocket (101)" \
                 || fail "a live log follow returned $c, want a 101 upgrade"
grep -q 'docker.container.logs.follow' /tmp/broker-docker.log \
  && pass "the log follow is on the broker's audit chain" \
  || fail "a live log follow was not audited"

sec "COMPOSE STACKS"
if docker compose version >/dev/null 2>&1; then
  compose_file='services:
  web:
    image: nginx:alpine
    ports: ["127.0.0.1:38080:80"]
'
  c=$(api -X POST $base/api/v1/docker/compose -H 'Content-Type: application/json' \
    -d "$(printf '{"project":"hp-e2e-stack","file":%s}' "$(printf '%s' "$compose_file" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')")")
  echo "$c" | grep -q 'hp-e2e-stack' \
    && pass "a compose stack was brought up from a submitted file" \
    || fail "compose up did not return the project"
  sleep 2
  # Every container the stack created must carry the managed label, so tear-down
  # and the ownership boundary reach a stack's containers too.
  lbl=$(docker ps --filter label=com.docker.compose.project=hp-e2e-stack --format '{{.Label "io.heropanel.managed"}}' | head -1)
  [ "$lbl" = "1" ] && pass "the stack's containers carry the managed label" \
                   || fail "a stack container was not labelled managed ($lbl)"
  api "$base/api/v1/docker/compose/hp-e2e-stack" | grep -q 'nginx' \
    && pass "compose ps lists the stack's services" || fail "compose ps did not list the service"
  c=$(code -X DELETE $base/api/v1/docker/compose/hp-e2e-stack)
  [ "$c" = "200" ] && pass "the stack was torn down" || fail "compose down returned $c"
else
  echo "SKIP: docker compose plugin not present"
fi

sec "*** ONE-CLICK APPS: THE FULL PIPELINE ***"
# The phase exit criteria: deploy an app, see it running, tear it down cleanly.
# nginx-demo is used because it needs no database and pulls fast — the pipeline
# it exercises (feasibility -> render -> compose up -> loopback publish) is the
# same one Ghost uses.
api $base/api/v1/apps/templates | grep -q 'uptime-kuma' \
  && pass "the catalog includes the exit-criteria apps" || fail "the catalog is missing expected apps"
# Feasibility is computed against real host memory, so every template carries a verdict.
api $base/api/v1/apps/templates | grep -q '"feasible"' \
  && pass "templates carry a memory-feasibility verdict" || fail "no feasibility verdict on templates"

deployed=$(api -X POST $base/api/v1/apps -H 'Content-Type: application/json' \
  -d '{"slug":"nginx-demo","name":"hp-e2e-app"}')
echo "$deployed"
echo "$deployed" | grep -q '"project":"hp-e2e-app"' \
  && pass "an app was deployed" || fail "the app deploy did not return a project"
sleep 3
docker ps --format '{{.Names}}' | grep -q 'hp-e2e-app' \
  && pass "the app's container is running" || fail "the deployed app is not running"

# The published port must be loopback — an app is fronted by a reverse proxy, not
# exposed to the internet directly.
appport=$(docker ps --filter label=com.docker.compose.project=hp-e2e-app --format '{{.Ports}}' | head -1)
echo "  app ports -> $appport"
case "$appport" in
  *0.0.0.0:*|*:::*) fail "THE APP WAS PUBLISHED ON ALL INTERFACES ($appport)" ;;
  *127.0.0.1:*) pass "the app is published to loopback only" ;;
  *) fail "unexpected app port binding: $appport" ;;
esac

api "$base/api/v1/apps/hp-e2e-app" | grep -q 'nginx' \
  && pass "app status lists the running service" || fail "app status did not list the service"

sec "*** REVERSE-PROXY AUTO-WIRING: EXPOSE THE APP ON A DOMAIN ***"
# Before exposing, the app is loopback-only: its exposure reports false.
api "$base/api/v1/apps/hp-e2e-app/exposure" | grep -q '"exposed":false' \
  && pass "a freshly deployed app reports itself as not exposed" \
  || fail "exposure of an un-exposed app was not false"

# Expose it: this creates a real proxy site whose vhost reverse-proxies to the
# app's live loopback port. site.write, and it stands up a full site.
exposed=$(api -X POST "$base/api/v1/apps/hp-e2e-app/expose" -H 'Content-Type: application/json' \
  -d '{"domain":"hp-e2e-app.test"}')
echo "$exposed"
echo "$exposed" | grep -q '"type":"proxy"' \
  && pass "exposing an app creates a proxy site" \
  || fail "expose did not return a proxy site"
echo "$exposed" | grep -q '"app_project":"hp-e2e-app"' \
  && pass "the proxy site is linked to the app it fronts" \
  || fail "the proxy site is not linked to its app"

# The exposure now resolves to the domain, and the vhost resolves the upstream
# live — so the rendered OLS config proxies to the app's loopback port.
api "$base/api/v1/apps/hp-e2e-app/exposure" | grep -q 'hp-e2e-app.test' \
  && pass "the app now reports itself exposed at its domain" \
  || fail "exposure did not report the domain"
grep -q "context / {" /tmp/ols-config/* 2>/dev/null && grep -rq "127.0.0.1:" /tmp/ols-config/* 2>/dev/null \
  && pass "the rendered vhost proxies to a loopback upstream" \
  || echo "  (note: OLS config location not checked in this harness)"

# One app, one front door: a second expose is refused.
c=$(code -X POST "$base/api/v1/apps/hp-e2e-app/expose" -H 'Content-Type: application/json' \
  -d '{"domain":"other.test"}')
[ "$c" = "409" ] && pass "a second expose is refused (409)" \
                 || fail "a duplicate expose returned $c, want 409"

# Unexpose drops the proxy site; the app keeps running on loopback.
c=$(code -X DELETE "$base/api/v1/apps/hp-e2e-app/expose")
[ "$c" = "200" ] && pass "unexpose removes the proxy site" || fail "unexpose returned $c"
docker ps --format '{{.Names}}' | grep -q 'hp-e2e-app' \
  && pass "the app keeps running after unexpose (only the front door was removed)" \
  || fail "unexpose stopped the app itself"
api "$base/api/v1/apps/hp-e2e-app/exposure" | grep -q '"exposed":false' \
  && pass "after unexpose the app reports itself not exposed again" \
  || fail "exposure still true after unexpose"

c=$(code -X DELETE $base/api/v1/apps/hp-e2e-app)
[ "$c" = "200" ] && pass "the app was torn down cleanly" || fail "app remove returned $c"
sleep 2
docker ps --format '{{.Names}}' | grep -q 'hp-e2e-app' \
  && fail "the app's container is still running after tear-down" \
  || pass "no app container remains after tear-down"

sec "A GENERATED SECRET NEVER REACHES THE BROKER LOG"
# Deploy an app with a secret field and confirm the generated value is not
# written anywhere it should not be. The compose file travels on stdin.
sec2=$(api -X POST $base/api/v1/apps -H 'Content-Type: application/json' \
  -d '{"slug":"redis","name":"hp-e2e-redis"}')
gen=$(echo "$sec2" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["secrets"]["password"])' 2>/dev/null || true)
if [ -n "$gen" ]; then
  pass "a secret was generated for the app"
  if grep -q "$gen" /tmp/broker-docker.log; then
    fail "THE GENERATED SECRET WAS WRITTEN TO THE BROKER LOG"
  else
    pass "the generated secret is not in the broker log"
  fi
else
  fail "no secret was generated for redis"
fi
code -X DELETE $base/api/v1/apps/hp-e2e-redis >/dev/null

sec "cleanup"
docker rm -f hp-e2e-managed hp-e2e-foreign hp-e2e-created >/dev/null 2>&1 || true
docker volume rm -f hp-e2e-vol >/dev/null 2>&1 || true
docker network rm hp-e2e-net >/dev/null 2>&1 || true
docker compose -p hp-e2e-stack down >/dev/null 2>&1 || true
docker compose -p hp-e2e-app down >/dev/null 2>&1 || true
docker compose -p hp-e2e-redis down >/dev/null 2>&1 || true

echo
if [ "$FAILED" = "0" ]; then echo "run-docker.sh : PASS"; else echo "run-docker.sh : FAIL"; fi
exit $FAILED
