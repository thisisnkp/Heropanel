# 13 — DNS (module design)

Phase 2. Authoritative DNS management: HeroPanel hosts zones and records and
serves them from a real authoritative nameserver (BIND9). hpd renders the zone
file and the zone declaration from DB state; the privileged broker writes them,
validates with `named-checkzone`, and reloads BIND — the same render → broker →
reload shape as the OpenLiteSpeed and php-fpm flows.

This closes the Phase-2 loop (domain → zone → SSL): with authoritative zones in
place, the SSL module can later issue **wildcard** certificates via DNS-01.

---

## 1. Scope

**In (slice 1):**
- **Zones**: create/list/get/delete an authoritative zone (its SOA + primary NS).
- **Records**: CRUD for `A, AAAA, CNAME, MX, TXT, NS, SRV, CAA`, with per-type
  content validation, TTL, and priority (MX/SRV).
- Rendering a standard **BIND zone file** and the `zone {}` declaration.
- Broker `dns.write_zone` (validate + reload, fail-safe) / `dns.remove_zone`.
- Auto-incrementing **SOA serial** on every change.

**Deferred (documented):**
- Running DNS as a **satellite module** (docs/06 gRPC process). Slice 1 is
  in-core, like every other module so far; the registry/gRPC migration is later.
- DNSSEC signing, zone import/export (AXFR/BIND text), secondary/AXFR, GeoDNS,
  ALIAS/flattening, per-record analytics.
- Alternate backends (PowerDNS). The service renders standard zone-file text, so
  a PowerDNS-API backend can be added behind the same service later.

## 2. Data model (migration 0008)

```
dns_zones
  id, uid, owner_id, name (FQDN, UNIQUE), primary_ns, admin_email,
  serial, refresh, retry, expire, minimum, ttl, status, created_at, updated_at

dns_records
  id, uid, zone_id (FK dns_zones), name, type, content, ttl, priority,
  created_at, updated_at
```

`name` is the record label relative to the zone (`@` for the apex, `www`, …).
A zone is owned by a user and is independent of a site (a site can point its
domain at a zone, but the zone is the unit of authority).

## 3. Zone file rendering

```
$TTL 3600
@   IN SOA ns1.example.test. admin.example.test. (
        2026071501 ; serial
        3600       ; refresh
        900        ; retry
        1209600    ; expire
        300 )      ; minimum
@       IN NS    ns1.example.test.
@       IN A     203.0.113.10
www     IN CNAME @
        IN MX 10 mail.example.test.
_dmarc  IN TXT   "v=DMARC1; p=none"
```

- The **SOA serial** is the zone's `serial` column, bumped on every write so BIND
  reloads the change.
- `admin_email` (`admin@example.test`) is rendered as the SOA RNAME with the `@`
  → `.` convention.
- Content is validated per type before it ever reaches the zone file, and
  `named-checkzone` is the final authority in the broker (a bad zone is rolled
  back, never loaded).

## 4. Broker capabilities

`dns.write_zone` — input `{zone, zone_file, named_conf}`:
1. `ValidateFQDN(zone)`; write `/etc/bind/zones/db.<zone>` (the records) and
   `/etc/bind/named.conf.heropanel` (the full set of `zone {}` blocks — declarative,
   so there is no drift, mirroring the single OLS config file).
2. `named-checkzone <zone> /etc/bind/zones/db.<zone>` — on failure, roll back the
   files and return `zone_invalid` (a broken zone is never served).
3. `rndc reload` — apply.

`dns.remove_zone` — input `{zone, named_conf}`: rewrite the declarative
`named.conf.heropanel` without the zone, delete `db.<zone>`, reload.

Both write to fixed system paths derived from a `ValidateFQDN`-checked zone name
(same pattern as `php.write_pool` / `cert.install`). BIND's config include is
wired once at install time: `include "/etc/bind/named.conf.heropanel";`.

## 5. API

RBAC scopes `dns.read` / `dns.write` already exist in the seed catalog.

```
GET    /api/v1/dns/zones                       dns.read
POST   /api/v1/dns/zones                       dns.write   → create zone
GET    /api/v1/dns/zones/{uid}                 dns.read
DELETE /api/v1/dns/zones/{uid}                 dns.write
GET    /api/v1/dns/zones/{uid}/records         dns.read
POST   /api/v1/dns/zones/{uid}/records         dns.write   → add record (reloads)
DELETE /api/v1/dns/records/{uid}               dns.write   → delete record (reloads)
```

Every mutation re-renders the zone and applies it via the broker, so the change
is authoritative immediately.

## 6. Definition of Done (this slice)

- [x] Domain + service + repo interface; zone-file + named-conf rendering
- [x] Broker capabilities with validation, `named-checkzone`, rollback, reload
- [x] REST endpoints + RBAC + audit
- [x] Unit tests: render, service validation, capability (files + argv)
- [x] **Live e2e** (`deploy/docker/e2e/run-dns.sh`): BIND9 in the image; create a
      zone + A/MX/TXT records via the API; `dig @127.0.0.1` returns the
      authoritative answers, and deleting a record removes it. An in-zone
      nameserver auto-seeds its glue A record so `named-checkzone` passes and BIND
      loads the zone. Wired into CI.

---
Back to [index](README.md). Related: [03 — Schema](03-database-schema.md),
[05 — Security](05-security-architecture.md), [10 — Roadmap](10-roadmap.md) Phase 2.
