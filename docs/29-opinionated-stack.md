# 29 — The Opinionated Stack

NexPanel manages one hosting stack. This document says which, why the
alternatives were removed rather than left in, and what an install that predates
the decision does now.

Implemented across [internal/setup](../internal/setup),
[internal/webserver](../internal/webserver), [internal/database](../internal/database)
and [broker/capabilities](../broker/capabilities).

## 1. The stack

| Layer | Choice | Alternative |
|---|---|---|
| Web server | **OpenLiteSpeed** | **LiteSpeed Enterprise** (licensed) |
| Database | **MariaDB** | MySQL and PostgreSQL install, unmanaged (§6) |
| Page cache | **LiteSpeed Cache** | — (built into the web server) |
| DB management | **phpMyAdmin** | — |
| Malware | **ClamAV** + **maldet** | — |
| Intrusion | **Fail2Ban** | — |
| WAF | **ModSecurity + OWASP CRS** | — |
| Firewall | **nftables** | — |

The one choice is the web server, and it exists only because one of the two
costs money. Everything else in the table is installed on every host by
`BuildPlan`, whatever the operator answered.

## 2. Why the baseline is not a set of questions

A panel that asks "do you want a web application firewall?" produces a fleet
where some hosts have one. That is worse than either answer taken uniformly,
for a reason that has nothing to do with the WAF: it makes every subsequent
statement about the fleet conditional. "Sites are protected by ModSecurity"
becomes "sites are protected by ModSecurity where it was installed", which is
not a claim anyone can act on, and nobody remembers which hosts are which.

So the wizard **lists** the baseline and does not offer to skip it. The
operator is told what is going on their machine; they are not asked to design
a security posture during a first-run form.

The questions that remain are the ones where the answer genuinely differs
between deployments and the panel cannot infer it: whether DNS is served here
or at a registrar, whether this host sends mail, and what this installation's
own domain is.

## 3. Why nginx, Apache and PostgreSQL were removed

All three worked. Nginx and Apache had renderers in `internal/webserver` and
apply/test/reload paths in the broker; PostgreSQL had nine `pg.*` capabilities,
peer auth through `runuser -u postgres`, and its own dump/restore staging
because `pg_dump` cannot write into the panel's 0700 dump directory.

They were deleted rather than marked unsupported, and the argument is the same
for each: **an engine nobody installs is an engine nobody tests.** Left in, each
renderer still has to compile, still has to be reasoned about in every change to
the `Site` struct, and still has to be updated whenever a vhost gains a feature
— by someone who has never run it. The failure mode is not that the code rots
quietly; it is that it ships, and the one operator who selects it gets a broken
vhost the test suite was never going to catch.

Marking them "unsupported" in the catalog would have been the reversible choice,
and it was considered. It loses on the same grounds: the code stays, the
maintenance cost stays, and all that changes is that the panel now carries a
list of things it declines to do.

LiteSpeed Enterprise survives because it is not a fourth web server. It is a
drop-in Apache replacement, so it parses httpd-syntax config — which is why the
httpd renderer stayed, now named for what it is (`lsweTmpl`) rather than for the
server it was originally written against.

## 4. What an upgraded install does

An install from a release that offered more engines still has `nginx` or
`postgresql` sitting in its setup row and, possibly, PostgreSQL databases in
`db_instances`. Three different rules apply, and the differences are the point.

**A stored web server is repaired.** `Selection.NormalizeStored` rewrites a
retired webserver to OpenLiteSpeed on the way out of the datastore. The wizard's
state gates the whole panel, so refusing the stored value would leave that
install permanently stuck behind a form it cannot submit. The same fallback
exists one layer down, in `webserver.RenderFor` and in the broker's
`webserver.apply`, so a config can still be applied while the row is being
fixed.

**A submitted web server is refused.** `Selection.Validate` rejects `nginx` on
input. Nginx and Apache are real, different servers the panel can no longer
configure; quietly substituting OpenLiteSpeed would answer a question the
operator did not ask.

**A PostgreSQL row is refused, not migrated.** `internal/database` loads records
through `getDatabase`/`getUser`, which refuse anything whose engine is not
MariaDB. This is the case where being helpful is dangerous: `brokerCap` now
always returns a `db.*` capability, so without the guard "drop the database
called `reports`" — recorded against PostgreSQL — would be sent to MariaDB. At
best that fails confusingly. At worst a MariaDB database of the same name exists
and is dropped instead. The operator is told which engine the row belongs to and
removes it with that engine's own tools.

`mysql` is the one value that *is* rewritten rather than refused, on input and
on read alike: it names the same server MariaDB speaks for and was always driven
through the same `db.*` capabilities. It is a rename, not a migration.

## 5. Stack vs type

`Site.Stack` and `Site.Type` answer different questions and both are in the API.

`Type` is how the vhost is built — `static`, `php` or `proxy`. `Stack` is what
the site runs — `static`, `php`, `node`, `python` or `app`. Three stacks share
the `proxy` answer, so a client holding only `Type` has to guess which, and will
be wrong for two of the three. The server knows, because it holds the runtime
record; so it answers, and no client has to.

Creation takes `stack` for the same reason. The client picks what to run; npd
maps it to the vhost shape. `POST /sites` still accepts `type` for callers
written against the older shape, and ignores it when `stack` is set.

`wp` is **refused** at creation until there is a WordPress module. Accepting it
would hand back a site the panel badges "WordPress" with no WordPress installed
on it, and no way for the operator to tell until they visited it.

## 6. The Apps catalogue

`/apps/install` is not a marketplace. Nothing is fetched from anywhere, so every
card has to be something this panel manages or means to; the catalogue is the
stack, written down. Nginx, Apache, Caddy and MongoDB left it for the reason in
§3 — the panel has no code for any of them.

**Web servers: two, and they are alternatives.** LiteSpeed Enterprise replaces
OpenLiteSpeed in place rather than joining it.

**Runtimes: a range, with the support status on every card.** PHP is offered from
7.4 to the current release, Node from 14, Python 3.8 upward — and each version
carries a chip saying whether it is current, in long-term support, on security
fixes only, or dead.

The end-of-life versions are listed deliberately, and this is the argument for
it: refusing PHP 7.4 does not move anybody's legacy application forward. It moves
that application to a host with no panel, where nothing is confined, no malware
scanner runs, and no one is watching. So the version is selectable here and the
interface says plainly what it is. `php.SupportedVersions` is the same list on
the server, and it is an allowlist rather than a reading of the host: it says
what npd will write a pool for, and `php-fpm<version>` still has to exist on the
machine (Sury on Debian/Ubuntu, Remi on RHEL).

**Databases: one seat for the MySQL family, and a second engine the panel does
not manage.** MariaDB and MySQL both want port 3306 and `/var/lib/mysql`, so
installing MySQL replaces MariaDB rather than joining it. PostgreSQL is the more
interesting case: it runs perfectly well alongside on 5432, and `internal/
database` refuses to manage it (§4). Both facts are true at once, so the
catalogue lists PostgreSQL *and* pgAdmin, and the category says which of the two
manages it. Listing the engine without pgAdmin would offer a database with no
way to administer it; listing neither would pretend a panel that installs a
Postgres somebody asked for is a panel that manages it.

**The default stack** — what a fresh install has, with no answer given to
anything: OpenLiteSpeed, MariaDB 11.4 LTS, LiteSpeed Cache (built in), PHP 8.4,
Node.js 24, Redis, phpMyAdmin, ClamAV, maldet, Fail2Ban, ModSecurity + CRS,
nftables. No Python: an interpreter arrives when a Python site needs one.

## 7. What is not here yet

- **The WordPress module.** No wp-cli, no install, no migration, no staging, no
  LiteSpeed Cache plugin. `stack: "wp"` is refused until it exists.
- **The databases screen is still fixtures.** The phpMyAdmin hand-off is live on
  the server and `openPhpMyAdmin()` is in the API client, but the list the
  button sits in carries no database uid to hand it yet.
- **Nothing installs from the catalogue.** The Install button says so, and no
  card on that screen reads the host — the badges describe the stack, not this
  machine.
- **No runtime provisioning.** `BuildPlan` installs the web server, MariaDB,
  Redis and the security baseline; it installs no PHP and no Node. Both are in
  the wizard's baseline list marked **planned** rather than ticked, because a
  panel whose sites run PHP should not describe a stack with no PHP in it — and
  should not claim to have installed one either.

---
Back to [index](README.md).
