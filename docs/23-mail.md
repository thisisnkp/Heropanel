# 23 — Mail

Virtual mail domains, mailboxes and aliases on **Postfix + Dovecot**, with
DKIM/SPF/DMARC generated and wired into DNS, per-mailbox quotas, and a queue
view. In-core, satellite-ready like its siblings.

Back to [index](README.md).

## 1. The MTAs never read the panel

The panel's database is the source of truth, but Postfix and Dovecot read
**rendered flat maps** — `virtual_mailbox_domains`, `virtual_mailbox_maps`,
`virtual_alias_maps` (hash files) and a Dovecot passwd-file. Every change
re-renders the complete desired state and applies it through the broker
(render-all, apply, rollback — the `webserver.apply` discipline). A panel
outage therefore never stops mail flow, and a diff in the audit log always
means a real change.

The privileged surface is small and fixed: `mail.provision` (vmail user,
directories, postfix virtual settings via `postconf -e` with **constant keys
and values**, the dovecot drop-in — idempotent), `mail.apply` (write the four
rendered files, `postmap`, reload, roll back on failure), `mail.purge` (delete
stored mail at a path **derived** from validated parts), `mail.dkim.apply`,
and the queue/quota verbs. No user input ever reaches a shell — arg arrays
against pinned binaries, like every other capability.

## 2. Accounts

A mailbox's password is accepted, hashed to **`{BLF-CRYPT}`** (bcrypt —
already in the tree via x/crypto, verified by libxcrypt on every current
distro), stored, and never returned: write-only both directions. Delivery is
Postfix → **Dovecot LMTP** over the postfix-private socket into a
vmail-owned Maildir (`/var/lib/nexpanel/mail/<domain>/<local>/Maildir`);
quotas are Dovecot's maildir quota with the per-user override carried in the
passwd-file, read back live through `doveadm quota`.

**Suspension blocks logins, not receipt**: a suspended account leaves the
passwd-file (IMAP refuses) but keeps its `virtual_mailbox_maps` entry — mail
keeps landing. Suspending someone must not bounce their mail. Deleting is the
opposite kind of explicit: removing the account and destroying its stored
mail (`?purge=true`) are separate acts.

An alias is one `virtual_alias_maps` pair; an internal destination is an
alias, an external one is a forwarder — same mechanism, one table.

## 3. DKIM, SPF, DMARC

The DKIM pair is generated in npd (stdlib RSA-2048, the interoperability
baseline). The private key is **sealed with the panel data key before it
touches the database** (AAD-bound to its domain row, write-only; no
`NP_SECRET_KEY`, no DKIM) and unsealed only to hand OpenDKIM its key file
(0600, opendkim-owned) through the broker. The public half is a TXT value,
shown freely.

The expected record set — MX, SPF (`v=spf1 mx ~all`), DKIM, DMARC
(`p=quarantine`) — **auto-wires into a panel-managed zone** when one covers
the domain. MX/DKIM/DMARC replace at their label (the panel's value is
authoritative); SPF **appends** at the apex, because clobbering an operator's
existing TXT verification records would be destructive. On external DNS the
same set is the copy-paste list.

The DNS check (`GET /mail/domains/{uid}/dns`) resolves each record against
**live DNS** — the panel's own zone data would only prove the panel agrees
with itself. `mail.resolver` / `NP_MAIL_RESOLVER` pins the resolver for
split-DNS setups (and lets e2e ask the local authoritative server).

## 4. Queue

`postqueue -j` through a broker read verb, parsed in npd where the schema is
unit-tested over fixtures; flush is `postqueue -f`; delete is `postsuper -d`
per **explicit, validated ID** — there is deliberately no delete-ALL, because
making the whole queue disappear must not be one compromised call away.
A postfix that is down reports `running:false` — an answer, not an error.

## 5. Definition of done

Broker capabilities unit-tested with the fake runner/fs: provision is
idempotent (exit-9 useradd tolerated, maps seeded only when absent), apply
writes the users file 0600 and rolls every file back on a failed postmap,
purge derives its path and refuses traversal, DKIM apply validates
domain/selector/PEM before anything runs, queue deletes refuse `ALL` and
argv-unsafe IDs, quota parsing handles the never-delivered mailbox. Renderers
are pure over fixtures (suspended accounts keep mailboxes, leave the
passwd-file; quota rides as a userdb extra field). DKIM sealing is proven at
the service level: ciphertext at rest, PEM only in the broker hand-off.

Live proof: **`deploy/docker/e2e/run-mail.sh`** (in CI) — real Postfix,
Dovecot, OpenDKIM and BIND: one API call provisions the host and the domain,
the DKIM key lands 0600 with ciphertext in the DB, **all four records resolve
from live DNS** and the served `p=` value is **byte-identical to the public
half derived from the private key file** (openssl); a real SMTP session
delivers through LMTP into the vmail Maildir **carrying a DKIM signature**;
IMAP reads it back against the BLF-CRYPT credential; an alias hops; a
suspended mailbox refuses login while mail **still lands**; a genuinely
deferred message (TEST-NET blackhole) appears in the queue view and is
deleted by ID; every capability is on the broker's audit chain.

## Transport security (TLS)

The mail host presents **one** certificate on every mail port — its own FQDN
(`NP_MAIL_HOSTNAME`, e.g. `mail.example.com`), *not* a per-domain cert. A
client connects to the mail host, not to each of the domains it carries, so
one host certificate is the correct model (SNI per virtual domain is a
deliberate non-goal).

npd delegates to the SSL module ([internal/ssl](../internal/ssl)) to make sure
that certificate is installed: a real Let's Encrypt cert when the operator has
issued one for the host, otherwise a **self-signed fallback** so TLS works out
of the box (the same posture as every panel-served site). The broker's
**`mail.tls`** capability then wires it in, keyed only by a validated hostname
(the cert path is *derived* from it, so nothing outside `sslRoot` can be named):

- **Postfix** — `main.cf` gets the host cert + opportunistic TLS on 25 both
  ways; the **submission/587** and **smtps/465** services are defined via
  idempotent `postconf -M`/`-P`. Both are **authenticated-relay-only**
  (`permit_sasl_authenticated,reject`): the one mistake a mail server must
  never make is being an open relay, so it is made unrepresentable rather than
  merely discouraged. Submission requires STARTTLS (`encrypt`); smtps is
  implicit TLS (`wrappermode`).
- **Dovecot** — a `96-nexpanel-ssl.conf` drop-in (`ssl = required`, the host
  cert, TLS ≥ 1.2) plus the **postfix-private SASL socket** submission
  authenticates against. imaps/993 (and pop3s/995 where `dovecot-pop3d` is
  present) come up automatically once `ssl` is set.

TLS is best-effort-wired when a domain is created (if `NP_MAIL_HOSTNAME` is
set) and re-appliable from the UI's Mail-TLS card or `POST /api/v1/mail/tls`.

Live proof (`run-mail.sh`): `openssl s_client` completes **STARTTLS on 587**
and **implicit TLS on 993** against the host cert; an **authenticated**
submission over 587 is delivered end-to-end; an **unauthenticated** relay to
an external domain is **refused**; `mail.tls` and `cert.install` are on the
broker's audit chain.

## Webmail (Roundcube)

Roundcube is served by the panel's **own** OpenLiteSpeed + PHP against the
**local** Dovecot and Postfix — the same host the mailboxes live on, so the
webmail talks to the MTAs over the loopback (TLS) rather than reaching out over
a network. It is not a customer site: it renders as a *system vhost* into the
one OLS config (`site.WithSystemVhosts`), on a configured hostname
(`NP_WEBMAIL_HOSTNAME`, e.g. `webmail.example.com`), with a dedicated `webmail`
Linux user and FPM pool.

One API call (`POST /api/v1/webmail/install`) lays the whole runtime down: the
broker's **`webmail.install`** creates the user, the writable data tree
(temp/logs and a **sqlite** metadata db — Roundcube self-initialises the schema
on first connect), and the rendered `config.inc.php` pointing at
`tls://127.0.0.1:143` (IMAP) and `tls://127.0.0.1:587` (submission); npd then
writes the FPM pool through the same **`php.write_pool`** every site pool uses
and re-applies the web server so the vhost serves. **No mailbox password is
ever handled** — Roundcube authenticates each user against Dovecot at login;
the panel only wires the client to the local MTAs. The application files
themselves are provisioned out of band (package/installer) at the pinned path,
so this is configuration, not a 40 MB download over the wire.

Live proof (`run-webmail.sh`): the install lays down the user/pool/config/db,
OpenLiteSpeed serves Roundcube's login page (PHP+FPM working), and a **real
mailbox user logs in through Roundcube** — IMAP auth against Dovecot over TLS,
the full OLS → PHP → Roundcube → Dovecot chain — while a **wrong password is
refused**; `webmail.install`, `php.write_pool` and `webserver.apply` are all on
the broker's audit chain.

Honest gaps: **passwordless SSO** (a Dovecot master user + a signed one-time
handoff so a signed-in panel user opens webmail without re-entering the mailbox
password) is a follow-up — today the user signs in with their own mailbox
credentials.

## Inbound verification policy

What the host does with mail it **receives**. DKIM is already verified inbound
(OpenDKIM runs `Mode sv` and stamps an `Authentication-Results` header); on top
of that, a three-level policy (`off` / `standard` / `strict`) applies Postfix's
HELO, sender and recipient restrictions through the broker's **`mail.inbound`**
capability. `standard` rejects non-FQDN and unknown-domain senders and
open-relay attempts; `strict` adds HELO and sender verification. **Local
submission stays exempt** — every level puts `permit_mynetworks` /
`permit_sasl_authenticated` first. `mail.inbound.status` reads the effective
sender-restriction line back from `postconf`.

Live proof (`run-mail.sh`): with `standard` applied (the loopback client made
untrusted for the test), mail from a **forged/unknown sender domain is
rejected** while a real, resolvable sender still delivers; `off` lifts the
sender-domain check; `mail.inbound` is on the broker's audit chain. Deeper
follow-up: full **SPF** and **DMARC** *rejection with alignment*
(`policyd-spf` / `OpenDMARC` daemons).

---
Back to [index](README.md).
