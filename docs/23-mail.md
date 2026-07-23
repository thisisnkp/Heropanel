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
vmail-owned Maildir (`/var/lib/heropanel/mail/<domain>/<local>/Maildir`);
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

The DKIM pair is generated in hpd (stdlib RSA-2048, the interoperability
baseline). The private key is **sealed with the panel data key before it
touches the database** (AAD-bound to its domain row, write-only; no
`HP_SECRET_KEY`, no DKIM) and unsealed only to hand OpenDKIM its key file
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
with itself. `mail.resolver` / `HP_MAIL_RESOLVER` pins the resolver for
split-DNS setups (and lets e2e ask the local authoritative server).

## 4. Queue

`postqueue -j` through a broker read verb, parsed in hpd where the schema is
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

Honest gaps: inbound SPF/DKIM/DMARC *verification* policy (what this host
does to mail it receives) is Phase 8 security-suite territory; SMTP
submission (587) + IMAPS with the panel's certificates ride on the SSL
module and land with the security phase; webmail is deliberately out of
scope for core.

---
Back to [index](README.md).
