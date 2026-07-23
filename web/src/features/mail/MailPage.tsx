import { useState } from "react";
import { Alert, Badge, Button, Card, EmptyState, Field, Input, Modal, Spinner, StatusBadge, Tabs } from "@/components/ui";
import { toast } from "@/stores/toast";
import {
  fmtBytes,
  fmtKB,
  useCreateMailAlias,
  useCreateMailbox,
  useCreateMailDomain,
  useDeleteMailAlias,
  useDeleteMailbox,
  useDeleteMailDomain,
  useDeleteQueued,
  useFlushMailQueue,
  useMailDNS,
  useMailDomain,
  useMailDomains,
  useMailQueue,
  useMailUsage,
  useSetMailboxPassword,
  useSetMailboxStatus,
  type MailDNSCheck,
  type Mailbox,
} from "./mail";

export function MailPage() {
  const domains = useMailDomains();
  const [selected, setSelected] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [showQueue, setShowQueue] = useState(false);

  const list = domains.data?.domains ?? [];
  const active = list.find((d) => d.uid === selected) ?? null;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-fg">Mail</h1>
          <p className="text-sm text-muted">Domains, mailboxes and aliases on Postfix + Dovecot.</p>
        </div>
        <div className="flex gap-2">
          <Button variant="ghost" onClick={() => setShowQueue(true)}>
            Queue
          </Button>
          <Button onClick={() => setCreating(true)}>New domain</Button>
        </div>
      </div>

      {domains.error && <Alert>You do not have permission to view mail.</Alert>}
      {domains.isLoading && <Spinner />}
      {domains.data && !domains.data.available && (
        <Alert>Mail management needs the privileged broker — it is not available on this host.</Alert>
      )}

      {domains.data && list.length === 0 && (
        <Card>
          <EmptyState
            title="No mail domains yet"
            hint="Add a domain to create mailboxes, aliases and its DKIM/SPF/DMARC records."
            action={<Button onClick={() => setCreating(true)}>New domain</Button>}
          />
        </Card>
      )}

      {list.length > 0 && (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-[16rem_1fr]">
          <Card className="h-fit overflow-hidden">
            {list.map((d) => (
              <button
                key={d.uid}
                onClick={() => setSelected(d.uid)}
                className={`flex w-full items-center justify-between border-b border-border/60 px-4 py-3 text-left text-sm last:border-0 ${
                  selected === d.uid ? "bg-brand/10" : "hover:bg-border/20"
                }`}
              >
                <span className="font-medium text-fg">{d.domain}</span>
                {d.dkim_public ? <Badge>DKIM</Badge> : null}
              </button>
            ))}
          </Card>
          {active ? (
            <DomainDetail uid={active.uid} onDeleted={() => setSelected(null)} />
          ) : (
            <Card>
              <EmptyState title="Select a domain" hint="Pick a mail domain on the left." />
            </Card>
          )}
        </div>
      )}

      {creating && <CreateDomainModal onClose={() => setCreating(false)} onCreated={(uid) => setSelected(uid)} />}
      {showQueue && <QueueModal onClose={() => setShowQueue(false)} />}
    </div>
  );
}

function CreateDomainModal({ onClose, onCreated }: { onClose: () => void; onCreated: (uid: string) => void }) {
  const create = useCreateMailDomain();
  const [domain, setDomain] = useState("");
  return (
    <Modal title="New mail domain" onClose={onClose}>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate(
            { domain },
            {
              onSuccess: (d) => {
                toast.success(`${d.domain} added`);
                onCreated(d.uid);
                onClose();
              },
              onError: (err) => toast.error(err.message),
            },
          );
        }}
      >
        <Field label="Domain" hint="DKIM keys are generated and sealed automatically; MX/SPF/DMARC records wire into a panel-managed zone when one covers the domain.">
          <Input value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="example.com" required />
        </Field>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={create.isPending}>
            Add domain
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function DomainDetail({ uid, onDeleted }: { uid: string; onDeleted: () => void }) {
  const detail = useMailDomain(uid);
  const del = useDeleteMailDomain();
  const [tab, setTab] = useState("mailboxes");

  if (detail.isLoading) return <Card><div className="p-4"><Spinner /></div></Card>;
  if (!detail.data) return <Card><EmptyState title="Not found" /></Card>;
  const { domain, accounts, aliases } = detail.data;

  return (
    <Card className="overflow-hidden">
      <div className="flex items-center justify-between border-b border-border px-4 py-3">
        <span className="text-sm font-medium text-fg">{domain.domain}</span>
        <Button
          variant="ghost"
          className="h-8 px-2 text-danger"
          onClick={() => {
            const purge = confirm(
              `Delete ${domain.domain}?\n\nOK = also delete stored mail (purge).\nCancel here, then choose again, to keep the mail on disk.`,
            );
            if (!confirm(`Really delete mail domain ${domain.domain}?`)) return;
            del.mutate(
              { uid, purge },
              { onSuccess: () => { toast.success("Domain deleted"); onDeleted(); }, onError: (e) => toast.error(e.message) },
            );
          }}
        >
          Delete
        </Button>
      </div>
      <Tabs
        tabs={[
          { id: "mailboxes", label: `Mailboxes (${accounts.length})` },
          { id: "aliases", label: `Aliases (${aliases.length})` },
          { id: "dns", label: "DNS" },
        ]}
        active={tab}
        onChange={setTab}
      />
      {tab === "mailboxes" && <MailboxesTab domainUid={uid} accounts={accounts} />}
      {tab === "aliases" && <AliasesTab domainUid={uid} aliases={aliases} domain={domain.domain} />}
      {tab === "dns" && <DNSTab domainUid={uid} />}
    </Card>
  );
}

function MailboxesTab({ domainUid, accounts }: { domainUid: string; accounts: Mailbox[] }) {
  const usage = useMailUsage(domainUid);
  const del = useDeleteMailbox(domainUid);
  const setStatus = useSetMailboxStatus(domainUid);
  const [adding, setAdding] = useState(false);
  const [pwFor, setPwFor] = useState<Mailbox | null>(null);

  return (
    <div className="p-4 space-y-3">
      <div className="flex justify-end">
        <Button className="h-8 px-3" onClick={() => setAdding(true)}>Add mailbox</Button>
      </div>
      {accounts.length === 0 && <EmptyState title="No mailboxes" hint="Add one to start receiving mail." />}
      {accounts.map((a) => {
        const u = usage.data?.usage?.[a.address];
        return (
          <div key={a.uid} className="flex items-center justify-between rounded-md border border-border/60 px-3 py-2 text-sm">
            <div>
              <div className="font-medium text-fg">{a.address}</div>
              <div className="text-xs text-muted">
                {u?.known ? `${fmtKB(u.used_kb)} of ` : ""}
                {a.quota_mb} MB
                {u && !u.known ? " · no mail yet" : ""}
              </div>
            </div>
            <div className="flex items-center gap-2">
              <StatusBadge status={a.status} />
              <Button variant="ghost" className="h-7 px-2" onClick={() => setPwFor(a)}>Password</Button>
              <Button
                variant="ghost"
                className="h-7 px-2"
                onClick={() =>
                  setStatus.mutate(
                    { uid: a.uid, status: a.status === "active" ? "suspended" : "active" },
                    { onError: (e) => toast.error(e.message) },
                  )
                }
              >
                {a.status === "active" ? "Suspend" : "Activate"}
              </Button>
              <Button
                variant="ghost"
                className="h-7 px-2 text-danger"
                onClick={() => {
                  const purge = confirm(`Delete ${a.address}?\n\nOK = also delete its stored mail.`);
                  if (!confirm(`Really delete mailbox ${a.address}?`)) return;
                  del.mutate({ uid: a.uid, purge }, { onError: (e) => toast.error(e.message) });
                }}
              >
                Delete
              </Button>
            </div>
          </div>
        );
      })}
      {adding && <AddMailboxModal domainUid={domainUid} onClose={() => setAdding(false)} />}
      {pwFor && <PasswordModal box={pwFor} onClose={() => setPwFor(null)} />}
    </div>
  );
}

function AddMailboxModal({ domainUid, onClose }: { domainUid: string; onClose: () => void }) {
  const create = useCreateMailbox(domainUid);
  const [local, setLocal] = useState("");
  const [password, setPassword] = useState("");
  const [quota, setQuota] = useState("1024");
  return (
    <Modal title="Add mailbox" onClose={onClose}>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate(
            { local_part: local, password, quota_mb: Number(quota) || 1024 },
            {
              onSuccess: (b) => { toast.success(`${b.address} created`); onClose(); },
              onError: (err) => toast.error(err.message),
            },
          );
        }}
      >
        <Field label="Name">
          <Input value={local} onChange={(e) => setLocal(e.target.value)} placeholder="info" required />
        </Field>
        <Field label="Password" hint="8–128 characters; stored hashed, write-only.">
          <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
        </Field>
        <Field label="Quota (MB)">
          <Input type="number" value={quota} onChange={(e) => setQuota(e.target.value)} min={1} max={102400} />
        </Field>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose}>Cancel</Button>
          <Button type="submit" loading={create.isPending}>Create</Button>
        </div>
      </form>
    </Modal>
  );
}

function PasswordModal({ box, onClose }: { box: Mailbox; onClose: () => void }) {
  const set = useSetMailboxPassword();
  const [password, setPassword] = useState("");
  return (
    <Modal title={`Password — ${box.address}`} onClose={onClose}>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          set.mutate(
            { uid: box.uid, password },
            {
              onSuccess: () => { toast.success("Password updated"); onClose(); },
              onError: (err) => toast.error(err.message),
            },
          );
        }}
      >
        <Field label="New password">
          <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
        </Field>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose}>Cancel</Button>
          <Button type="submit" loading={set.isPending}>Update</Button>
        </div>
      </form>
    </Modal>
  );
}

function AliasesTab({ domainUid, aliases, domain }: { domainUid: string; aliases: { uid: string; source: string; destination: string }[]; domain: string }) {
  const create = useCreateMailAlias(domainUid);
  const del = useDeleteMailAlias(domainUid);
  const [source, setSource] = useState("");
  const [dest, setDest] = useState("");

  return (
    <div className="p-4 space-y-3">
      <form
        className="flex items-end gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate(
            { source, destination: dest },
            {
              onSuccess: () => { setSource(""); setDest(""); toast.success("Alias added"); },
              onError: (err) => toast.error(err.message),
            },
          );
        }}
      >
        <Field label="Alias">
          <Input value={source} onChange={(e) => setSource(e.target.value)} placeholder="sales" required />
        </Field>
        <span className="pb-2 text-sm text-muted">@{domain} →</span>
        <Field label="Destination">
          <Input value={dest} onChange={(e) => setDest(e.target.value)} placeholder="info@example.com" required />
        </Field>
        <Button type="submit" className="h-9" loading={create.isPending}>Add</Button>
      </form>
      {aliases.length === 0 && <EmptyState title="No aliases" hint="An internal destination is an alias; an external one is a forwarder." />}
      {aliases.map((a) => (
        <div key={a.uid} className="flex items-center justify-between rounded-md border border-border/60 px-3 py-2 text-sm">
          <span className="text-fg">
            {a.source} <span className="text-muted">→</span> {a.destination}
          </span>
          <Button
            variant="ghost"
            className="h-7 px-2 text-danger"
            onClick={() => del.mutate(a.uid, { onError: (e) => toast.error(e.message) })}
          >
            Delete
          </Button>
        </div>
      ))}
    </div>
  );
}

function DNSTab({ domainUid }: { domainUid: string }) {
  const dns = useMailDNS(domainUid);
  return (
    <div className="p-4 space-y-3">
      <p className="text-sm text-muted">
        Checked against live DNS. In a panel-managed zone these records are wired automatically; on external DNS,
        copy them to your provider.
      </p>
      {dns.isLoading && <Spinner />}
      {dns.data?.records?.map((r: MailDNSCheck, i: number) => (
        <div key={i} className="rounded-md border border-border/60 px-3 py-2 text-sm">
          <div className="flex items-center justify-between">
            <span className="font-medium text-fg">
              {r.label} <Badge>{r.type}</Badge>
              {r.priority ? <span className="ml-1 text-xs text-muted">prio {r.priority}</span> : null}
            </span>
            <StatusBadge status={r.found ? "active" : "error"} />
          </div>
          <code className="mt-1 block break-all text-xs text-muted">{r.value}</code>
        </div>
      ))}
    </div>
  );
}

function QueueModal({ onClose }: { onClose: () => void }) {
  const queue = useMailQueue();
  const flush = useFlushMailQueue();
  const del = useDeleteQueued();

  return (
    <Modal title="Mail queue" onClose={onClose}>
      <div className="space-y-3">
        {queue.data && !queue.data.running && <Alert>Postfix is not running.</Alert>}
        <div className="flex justify-end">
          <Button
            className="h-8 px-3"
            loading={flush.isPending}
            onClick={() =>
              flush.mutate(undefined, {
                onSuccess: () => toast.success("Delivery attempt scheduled"),
                onError: (e) => toast.error(e.message),
              })
            }
          >
            Flush queue
          </Button>
        </div>
        {queue.isLoading && <Spinner />}
        {queue.data?.messages?.length === 0 && <EmptyState title="The queue is empty" />}
        {queue.data?.messages?.map((m) => (
          <div key={m.id} className="rounded-md border border-border/60 px-3 py-2 text-sm">
            <div className="flex items-center justify-between">
              <span className="font-mono text-xs text-fg">{m.id}</span>
              <div className="flex items-center gap-2">
                <Badge>{m.queue}</Badge>
                <span className="text-xs text-muted">{fmtBytes(m.size_bytes)}</span>
                <Button
                  variant="ghost"
                  className="h-7 px-2 text-danger"
                  onClick={() =>
                    del.mutate([m.id], {
                      onSuccess: () => toast.success("Message deleted"),
                      onError: (e) => toast.error(e.message),
                    })
                  }
                >
                  Delete
                </Button>
              </div>
            </div>
            <div className="mt-1 text-xs text-muted">
              from {m.sender || "<>"} · {m.recipients.map((r) => r.address).join(", ")}
            </div>
            {m.recipients.some((r) => r.delay_reason) && (
              <div className="mt-1 text-xs text-amber-600">
                {m.recipients.find((r) => r.delay_reason)?.delay_reason}
              </div>
            )}
          </div>
        ))}
      </div>
    </Modal>
  );
}
