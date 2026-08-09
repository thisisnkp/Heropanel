import { useEffect, useState } from "react";
import { Alert, Badge, Button, Card, EmptyState, Field, Input, Select, Spinner, StatusBadge, Tabs } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useSites } from "@/features/sites/sites";
import {
  useAddFirewallRule,
  useApplyFirewall,
  useConfirmFirewall,
  useDeleteFirewallRule,
  useFirewallIPList,
  useAddFirewallIPEntry,
  useDeleteFirewallIPEntry,
  useFirewallCountries,
  useImportFirewallCountry,
  useRemoveFirewallCountry,
  useFIM,
  useFIMInit,
  useFIMCheck,
  useAuditScan,
  useDeletePasskey,
  useDeleteQuarantine,
  useFail2Ban,
  useFirewall,
  usePasskeys,
  useQuarantine,
  useQuarantineFile,
  useRegisterPasskey,
  useRestoreQuarantine,
  useRollbackFirewall,
  useScanSite,
  useSSH,
  useHardenSSH,
  useUpdates,
  useConfigureUpdates,
  useUnban,
  type Finding,
  type FirewallRule,
  type FIMResult,
} from "./security";

export function SecurityPage() {
  const [tab, setTab] = useState("firewall");
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-fg">Security</h1>
        <p className="text-sm text-muted">Host firewall, malware and passkeys.</p>
      </div>
      <Tabs
        tabs={[
          { id: "firewall", label: "Firewall" },
          { id: "ssh", label: "SSH" },
          { id: "updates", label: "Updates" },
          { id: "malware", label: "Malware" },
          { id: "integrity", label: "Integrity" },
          { id: "bans", label: "Bans" },
          { id: "passkeys", label: "Passkeys" },
        ]}
        active={tab}
        onChange={setTab}
      />
      {tab === "firewall" && <FirewallTab />}
      {tab === "ssh" && <SSHTab />}
      {tab === "updates" && <UpdatesTab />}
      {tab === "malware" && <MalwareTab />}
      {tab === "integrity" && <IntegrityTab />}
      {tab === "bans" && <BansTab />}
      {tab === "passkeys" && <PasskeysTab />}
    </div>
  );
}

function BansTab() {
  const f2b = useFail2Ban();
  const unban = useUnban();

  if (f2b.error) return <Alert>You do not have permission to view bans.</Alert>;
  if (f2b.isLoading || !f2b.data) return <Spinner />;
  if (!f2b.data.available)
    return <Alert>Fail2Ban is not available on this host (the broker is not configured).</Alert>;
  if (!f2b.data.running) return <Alert>The Fail2Ban server is not running.</Alert>;

  return (
    <div className="space-y-4">
      {f2b.data.jails.length === 0 && (
        <Card>
          <EmptyState title="No jails" hint="Fail2Ban is running but has no configured jails." />
        </Card>
      )}
      {f2b.data.jails.map((jail) => (
        <Card key={jail.name} className="overflow-hidden">
          <div className="flex items-center justify-between border-b border-border px-4 py-3 text-sm">
            <span className="font-medium text-fg">{jail.name}</span>
            <Badge>{jail.banned.length} banned</Badge>
          </div>
          {jail.banned.length === 0 ? (
            <p className="px-4 py-3 text-sm text-muted">No IPs currently banned.</p>
          ) : (
            <div className="divide-y divide-border/60">
              {jail.banned.map((ip) => (
                <div key={ip} className="flex items-center justify-between px-4 py-2 text-sm">
                  <span className="font-mono text-xs text-fg">{ip}</span>
                  <Button
                    variant="ghost"
                    className="h-7 px-2"
                    onClick={() =>
                      unban.mutate(
                        { jail: jail.name, ip },
                        { onSuccess: () => toast.success(`Unbanned ${ip}`), onError: (e) => toast.error(e.message) },
                      )
                    }
                  >
                    Unban
                  </Button>
                </div>
              ))}
            </div>
          )}
        </Card>
      ))}
    </div>
  );
}

function PasskeysTab() {
  const keys = usePasskeys();
  const register = useRegisterPasskey();
  const del = useDeletePasskey();
  const [name, setName] = useState("");

  if (keys.error) return <Alert>You need to be signed in to manage passkeys.</Alert>;
  if (keys.isLoading || !keys.data) return <Spinner />;
  if (!keys.data.enabled)
    return <Alert>Passkeys are not configured on this panel (set webauthn.rp_id and origin).</Alert>;

  return (
    <div className="space-y-4">
      <Card>
        <form
          className="flex flex-wrap items-end gap-3 p-4"
          onSubmit={(e) => {
            e.preventDefault();
            register.mutate(name || "Passkey", {
              onSuccess: () => {
                setName("");
                toast.success("Passkey registered");
              },
              onError: (err) => toast.error(err instanceof Error ? err.message : "Registration cancelled"),
            });
          }}
        >
          <Field label="New passkey">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. YubiKey, this laptop" />
          </Field>
          <Button type="submit" className="h-9" loading={register.isPending}>
            Register a passkey
          </Button>
        </form>
      </Card>

      <Card className="overflow-hidden">
        <div className="border-b border-border px-4 py-3 text-sm font-medium text-fg">Your passkeys</div>
        {keys.data.passkeys.length === 0 ? (
          <div className="p-4">
            <EmptyState title="No passkeys yet" hint="Register one to sign in without a password — phishing-resistant by design." />
          </div>
        ) : (
          <div className="divide-y divide-border/60">
            {keys.data.passkeys.map((k) => (
              <div key={k.uid} className="flex items-center justify-between px-4 py-2.5 text-sm">
                <span className="text-fg">{k.name}</span>
                <Button
                  variant="ghost"
                  className="h-7 px-2 text-danger"
                  onClick={() => del.mutate(k.uid, { onSuccess: () => toast.success("Removed"), onError: (e) => toast.error(e.message) })}
                >
                  Remove
                </Button>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

function MalwareTab() {
  const quarantine = useQuarantine();
  const sites = useSites();
  const scan = useScanSite();
  const quar = useQuarantineFile();
  const restore = useRestoreQuarantine();
  const del = useDeleteQuarantine();
  const [siteUid, setSiteUid] = useState("");
  const [findings, setFindings] = useState<Finding[] | null>(null);

  if (quarantine.error) return <Alert>You do not have permission to view quarantine.</Alert>;

  return (
    <div className="space-y-4">
      <Card>
        <div className="flex flex-wrap items-end gap-3 p-4">
          <Field label="Scan a site">
            <Select value={siteUid} onChange={(e) => setSiteUid(e.target.value)}>
              <option value="">Select a site…</option>
              {(sites.data ?? []).map((s) => (
                <option key={s.uid} value={s.uid}>
                  {s.name}
                </option>
              ))}
            </Select>
          </Field>
          <Button
            className="h-9"
            disabled={!siteUid}
            loading={scan.isPending}
            onClick={() =>
              scan.mutate(siteUid, {
                onSuccess: (r) => {
                  setFindings(r.findings);
                  toast.success(r.scan.infected ? `${r.scan.infected} detection(s)` : "Clean — no detections");
                },
                onError: (e) => toast.error(e.message),
              })
            }
          >
            Scan now
          </Button>
        </div>
        {findings && findings.length > 0 && (
          <div className="divide-y divide-border/60 border-t border-border">
            {findings.map((f) => (
              <div key={f.path} className="flex items-center justify-between px-4 py-2.5 text-sm">
                <div>
                  <div className="font-mono text-xs text-fg">{f.path}</div>
                  <div className="text-xs text-danger">{f.signature}</div>
                </div>
                <Button
                  className="h-7 px-2"
                  onClick={() =>
                    quar.mutate(
                      { site_uid: siteUid, path: f.path, signature: f.signature },
                      {
                        onSuccess: () => {
                          setFindings((prev) => (prev ?? []).filter((x) => x.path !== f.path));
                          toast.success("Quarantined");
                        },
                        onError: (e) => toast.error(e.message),
                      },
                    )
                  }
                >
                  Quarantine
                </Button>
              </div>
            ))}
          </div>
        )}
        {findings && findings.length === 0 && <p className="border-t border-border px-4 py-3 text-sm text-muted">No detections.</p>}
      </Card>

      <Card className="overflow-hidden">
        <div className="border-b border-border px-4 py-3 text-sm font-medium text-fg">Quarantine</div>
        {quarantine.isLoading ? (
          <div className="p-4">
            <Spinner />
          </div>
        ) : (quarantine.data?.items.length ?? 0) === 0 ? (
          <div className="p-4">
            <EmptyState title="Quarantine is empty" hint="Detections you quarantine are moved out of the site tree to here." />
          </div>
        ) : (
          <div className="divide-y divide-border/60">
            {quarantine.data!.items.map((it) => (
              <div key={it.uid} className="flex items-center justify-between px-4 py-2.5 text-sm">
                <div>
                  <div className="font-mono text-xs text-fg">{it.original_path}</div>
                  <div className="text-xs text-muted">
                    {it.signature} · {it.site_uid}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <StatusBadge status={it.status === "quarantined" ? "suspended" : "stopped"} />
                  {it.status === "quarantined" && (
                    <Button
                      variant="ghost"
                      className="h-7 px-2"
                      onClick={() => restore.mutate(it.uid, { onSuccess: () => toast.success("Restored"), onError: (e) => toast.error(e.message) })}
                    >
                      Restore
                    </Button>
                  )}
                  <Button
                    variant="ghost"
                    className="h-7 px-2 text-danger"
                    onClick={() => {
                      if (confirm("Permanently delete this quarantined file?"))
                        del.mutate(it.uid, { onSuccess: () => toast.success("Deleted"), onError: (e) => toast.error(e.message) });
                    }}
                  >
                    Delete
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

function FirewallTab() {
  const fw = useFirewall();
  const add = useAddFirewallRule();
  const del = useDeleteFirewallRule();
  const apply = useApplyFirewall();
  const confirm = useConfirmFirewall();
  const rollback = useRollbackFirewall();
  // The confirm token is only returned by apply — remember it for this session.
  const [token, setToken] = useState<string | null>(null);

  if (fw.error) return <Alert>You do not have permission to manage the firewall.</Alert>;
  if (fw.isLoading || !fw.data) return <Spinner />;
  if (!fw.data.available)
    return <Alert>Firewall management needs the privileged broker — it is not available on this host.</Alert>;

  const { rules, pending, deadline } = fw.data;

  return (
    <div className="space-y-4">
      {pending && (
        <PendingBanner
          deadline={deadline}
          canConfirm={!!token}
          onConfirm={() => {
            if (!token) return;
            confirm.mutate(token, {
              onSuccess: () => {
                setToken(null);
                toast.success("Firewall change confirmed");
              },
              onError: (e) => toast.error(e.message),
            });
          }}
          onRollback={() =>
            rollback.mutate(undefined, {
              onSuccess: () => {
                setToken(null);
                toast.success("Firewall change reverted");
              },
              onError: (e) => toast.error(e.message),
            })
          }
        />
      )}

      <Card className="overflow-hidden">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <span className="text-sm font-medium text-fg">Rules (default-drop; established + loopback always allowed)</span>
          <Button
            className="h-8 px-3"
            disabled={pending}
            loading={apply.isPending}
            onClick={() =>
              apply.mutate(undefined, {
                onSuccess: (r) => {
                  setToken(r.token);
                  toast.success(`Applied — confirm within ${r.window_sec}s or it reverts`);
                },
                onError: (e) => toast.error(e.message),
              })
            }
          >
            Apply ruleset
          </Button>
        </div>
        {rules.length === 0 ? (
          <div className="p-4">
            <EmptyState title="No rules yet" hint="Add allow/deny rules, then Apply. An unconfirmed apply reverts itself." />
          </div>
        ) : (
          <div className="divide-y divide-border/60">
            {rules.map((r) => (
              <RuleRow key={r.uid} rule={r} onDelete={() => del.mutate(r.uid, { onError: (e) => toast.error(e.message) })} />
            ))}
          </div>
        )}
      </Card>

      <AddRuleForm onAdd={(v) => add.mutate(v, { onError: (e) => toast.error(e.message) })} busy={add.isPending} />
      <IPListPanel />
      <CountryImportPanel />
    </div>
  );
}

// IPListPanel manages the geo/IP allow-block lists (nftables sets). Entries take
// effect at the next firewall apply, like rules.
function IPListPanel() {
  const list = useFirewallIPList();
  const add = useAddFirewallIPEntry();
  const del = useDeleteFirewallIPEntry();
  const [cidr, setCidr] = useState("");
  const [mode, setMode] = useState("block");
  const [comment, setComment] = useState("");
  // Country-imported entries are thousands of rows managed as a unit in the
  // country panel below — list only the manually-added ones here.
  const entries = (list.data?.entries ?? []).filter((e) => !e.country);

  return (
    <Card className="overflow-hidden">
      <div className="border-b border-border px-4 py-3">
        <h3 className="text-sm font-semibold text-fg">Allow / block lists</h3>
        <p className="text-xs text-muted">
          CIDRs enforced as nftables sets ahead of the rules — allow is always let in, block is always dropped. Applied
          at the next firewall apply. Whole-country ranges are imported below.
        </p>
      </div>
      <form
        className="flex flex-wrap items-end gap-3 p-4"
        onSubmit={(e) => {
          e.preventDefault();
          add.mutate(
            { cidr: cidr.trim(), mode, comment: comment.trim() },
            {
              onSuccess: () => {
                setCidr("");
                setComment("");
              },
              onError: (err) => toast.error(err.message),
            },
          );
        }}
      >
        <Field label="CIDR / IP">
          <Input value={cidr} onChange={(e) => setCidr(e.target.value)} placeholder="203.0.113.0/24" />
        </Field>
        <Field label="Mode">
          <Select value={mode} onChange={(e) => setMode(e.target.value)}>
            <option value="block">block</option>
            <option value="allow">allow</option>
          </Select>
        </Field>
        <Field label="Comment">
          <Input value={comment} onChange={(e) => setComment(e.target.value)} placeholder="e.g. country / reason" />
        </Field>
        <Button type="submit" className="h-9" loading={add.isPending}>
          Add
        </Button>
      </form>
      {entries.length > 0 && (
        <div className="divide-y divide-border/60 border-t border-border/60">
          {entries.map((e) => (
            <div key={e.uid} className="flex items-center justify-between px-4 py-2 text-sm">
              <div className="flex items-center gap-2">
                <Badge>{e.mode}</Badge>
                <span className="font-mono text-xs text-fg">{e.cidr}</span>
                {e.comment && <span className="text-xs text-muted">— {e.comment}</span>}
              </div>
              <Button
                variant="ghost"
                className="h-7 px-2 text-danger"
                onClick={() => del.mutate(e.uid, { onError: (err) => toast.error(err.message) })}
              >
                Remove
              </Button>
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

// CountryImportPanel bulk-imports a country's published CIDR ranges as one
// allow/block set, managed as a unit (a country is thousands of ranges, so they
// are never listed individually). Applied at the next firewall apply.
function CountryImportPanel() {
  const countries = useFirewallCountries();
  const imp = useImportFirewallCountry();
  const rm = useRemoveFirewallCountry();
  const [cc, setCc] = useState("");
  const [mode, setMode] = useState("block");
  const available = countries.data?.available ?? false;
  const imported = countries.data?.countries ?? [];

  return (
    <Card className="overflow-hidden">
      <div className="border-b border-border px-4 py-3">
        <h3 className="text-sm font-semibold text-fg">Country ranges</h3>
        <p className="text-xs text-muted">
          Import all of a country's published CIDR ranges as one allow/block set. Fetched from the configured geo source
          and enforced through the same set membership test. Re-importing a country refreshes it.
        </p>
      </div>
      {!available ? (
        <p className="p-4 text-xs text-muted">
          Country import is not configured — set a geo CIDR source URL (<span className="font-mono">NP_SECURITY_GEODB_URL</span>).
        </p>
      ) : (
        <form
          className="flex flex-wrap items-end gap-3 p-4"
          onSubmit={(e) => {
            e.preventDefault();
            imp.mutate(
              { country: cc.trim(), mode },
              {
                onSuccess: (r) => {
                  toast.success(`Imported ${r.count} ranges for ${r.country}.`);
                  setCc("");
                },
                onError: (err) => toast.error(err.message),
              },
            );
          }}
        >
          <Field label="Country code">
            <Input
              value={cc}
              onChange={(e) => setCc(e.target.value.toUpperCase())}
              placeholder="e.g. CN"
              maxLength={2}
              className="w-24 uppercase"
            />
          </Field>
          <Field label="Mode">
            <Select value={mode} onChange={(e) => setMode(e.target.value)}>
              <option value="block">block</option>
              <option value="allow">allow</option>
            </Select>
          </Field>
          <Button type="submit" className="h-9" loading={imp.isPending}>
            Import
          </Button>
        </form>
      )}
      {imported.length > 0 && (
        <div className="divide-y divide-border/60 border-t border-border/60">
          {imported.map((c) => (
            <div key={`${c.country}-${c.mode}`} className="flex items-center justify-between px-4 py-2 text-sm">
              <div className="flex items-center gap-2">
                <Badge>{c.mode}</Badge>
                <span className="font-mono text-xs text-fg">{c.country}</span>
                <span className="text-xs text-muted">— {c.count.toLocaleString()} ranges</span>
              </div>
              <Button
                variant="ghost"
                className="h-7 px-2 text-danger"
                loading={rm.isPending && rm.variables === c.country}
                onClick={() => rm.mutate(c.country, { onError: (err) => toast.error(err.message) })}
              >
                Remove
              </Button>
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

function PendingBanner({
  deadline,
  canConfirm,
  onConfirm,
  onRollback,
}: {
  deadline: string;
  canConfirm: boolean;
  onConfirm: () => void;
  onRollback: () => void;
}) {
  const [left, setLeft] = useState(() => secondsLeft(deadline));
  useEffect(() => {
    const t = setInterval(() => setLeft(secondsLeft(deadline)), 1000);
    return () => clearInterval(t);
  }, [deadline]);

  return (
    <Card className="border-warning/40 bg-warning/10">
      <div className="flex flex-wrap items-center justify-between gap-3 p-4">
        <div className="text-sm text-fg">
          <span className="font-semibold">A firewall change is pending.</span> It reverts automatically in{" "}
          <span className="font-mono">{left}s</span> unless confirmed — so a rule that locked you out will undo itself.
        </div>
        <div className="flex gap-2">
          <Button className="h-8 px-3" disabled={!canConfirm} onClick={onConfirm}>
            Confirm
          </Button>
          <Button variant="ghost" className="h-8 px-3" onClick={onRollback}>
            Revert now
          </Button>
        </div>
      </div>
      {!canConfirm && (
        <p className="px-4 pb-3 text-xs text-muted">
          The confirmation token was issued to the session that applied this change; from here you can revert now or wait
          for the automatic revert.
        </p>
      )}
    </Card>
  );
}

function RuleRow({ rule, onDelete }: { rule: FirewallRule; onDelete: () => void }) {
  return (
    <div className="flex items-center justify-between px-4 py-2.5 text-sm">
      <div className="flex items-center gap-2">
        <Badge>{rule.action}</Badge>
        <span className="text-fg">
          {rule.protocol}
          {rule.port ? `/${rule.port}${rule.port_end ? `-${rule.port_end}` : ""}` : ""}
          {rule.source ? ` from ${rule.source}` : ""}
        </span>
        {rule.comment && <span className="text-xs text-muted">— {rule.comment}</span>}
      </div>
      <Button variant="ghost" className="h-7 px-2 text-danger" onClick={onDelete}>
        Delete
      </Button>
    </div>
  );
}

function AddRuleForm({
  onAdd,
  busy,
}: {
  onAdd: (v: { action: string; protocol: string; port: number; port_end: number; source: string; comment: string }) => void;
  busy: boolean;
}) {
  const [action, setAction] = useState("accept");
  const [protocol, setProtocol] = useState("tcp");
  const [port, setPort] = useState("");
  const [portEnd, setPortEnd] = useState("");
  const [source, setSource] = useState("");
  const [comment, setComment] = useState("");

  return (
    <Card>
      <form
        className="flex flex-wrap items-end gap-3 p-4"
        onSubmit={(e) => {
          e.preventDefault();
          onAdd({
            action,
            protocol,
            port: Number(port) || 0,
            port_end: Number(portEnd) || 0,
            source: source.trim(),
            comment: comment.trim(),
          });
          setPort("");
          setPortEnd("");
          setSource("");
          setComment("");
        }}
      >
        <Field label="Action">
          <Select value={action} onChange={(e) => setAction(e.target.value)}>
            <option value="accept">accept</option>
            <option value="drop">drop</option>
          </Select>
        </Field>
        <Field label="Protocol">
          <Select value={protocol} onChange={(e) => setProtocol(e.target.value)}>
            <option value="tcp">tcp</option>
            <option value="udp">udp</option>
            <option value="any">any</option>
          </Select>
        </Field>
        <Field label="Port">
          <Input value={port} onChange={(e) => setPort(e.target.value)} placeholder="any" className="w-20" />
        </Field>
        <Field label="…to (range)">
          <Input value={portEnd} onChange={(e) => setPortEnd(e.target.value)} placeholder="—" className="w-20" />
        </Field>
        <Field label="Source">
          <Input value={source} onChange={(e) => setSource(e.target.value)} placeholder="any (IPv4/IPv6/CIDR)" />
        </Field>
        <Field label="Comment">
          <Input value={comment} onChange={(e) => setComment(e.target.value)} placeholder="optional" />
        </Field>
        <Button type="submit" className="h-9" loading={busy}>
          Add rule
        </Button>
      </form>
    </Card>
  );
}

function secondsLeft(deadline: string): number {
  // The API sends UTC "YYYY-MM-DD HH:MM:SS"; treat it as UTC.
  const ms = Date.parse(deadline.replace(" ", "T") + "Z") - Date.now();
  return Math.max(0, Math.round(ms / 1000));
}

// SSHTab shows the effective sshd configuration the panel manages and applies a
// hardening profile (key-only auth, port, root-login policy, auth-try budget).
// The form starts from the current effective config so an operator tweaks one
// knob without silently resetting the rest.
function SSHTab() {
  const ssh = useSSH();
  const harden = useHardenSSH();
  const eff = ssh.data?.effective ?? {};
  const [port, setPort] = useState("");
  const [permitRoot, setPermitRoot] = useState("");
  const [passwordAuth, setPasswordAuth] = useState<string>("");

  useEffect(() => {
    if (!ssh.data) return;
    setPort(eff.port ?? "22");
    setPermitRoot(eff.permitrootlogin ?? "prohibit-password");
    setPasswordAuth(eff.passwordauthentication ?? "no");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ssh.data]);

  if (ssh.isLoading) return <Spinner />;
  if (ssh.error || !ssh.data?.available)
    return <Alert>SSH hardening needs the privileged broker — it is not available on this host.</Alert>;

  return (
    <div className="space-y-4">
      <Card>
        <h2 className="text-sm font-semibold text-fg">Effective sshd configuration</h2>
        <p className="mt-1 text-xs text-muted">Read live from <code>sshd -T</code> — what the daemon would actually enforce.</p>
        <div className="mt-3 grid grid-cols-2 gap-2 text-sm md:grid-cols-3">
          {["port", "permitrootlogin", "passwordauthentication", "pubkeyauthentication", "maxauthtries", "x11forwarding"].map((k) => (
            <div key={k} className="rounded-md border border-border/60 px-3 py-2">
              <div className="text-xs text-muted">{k}</div>
              <div className="font-medium text-fg">{eff[k] ?? "—"}</div>
            </div>
          ))}
        </div>
      </Card>

      <Card>
        <h2 className="text-sm font-semibold text-fg">Harden</h2>
        <form
          className="mt-3 space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            harden.mutate(
              {
                port: parseInt(port, 10) || 22,
                permit_root_login: permitRoot,
                password_authentication: passwordAuth === "yes",
              },
              {
                onSuccess: () => toast.success("SSH hardening applied"),
                onError: (err) => toast.error(err.message),
              },
            );
          }}
        >
          <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
            <Field label="Port">
              <Input value={port} onChange={(e) => setPort(e.target.value)} inputMode="numeric" />
            </Field>
            <Field label="Root login">
              <Select value={permitRoot} onChange={(e) => setPermitRoot(e.target.value)}>
                <option value="no">no</option>
                <option value="prohibit-password">prohibit-password (key only)</option>
                <option value="yes">yes</option>
              </Select>
            </Field>
            <Field label="Password authentication">
              <Select value={passwordAuth} onChange={(e) => setPasswordAuth(e.target.value)}>
                <option value="no">no (key-only)</option>
                <option value="yes">yes</option>
              </Select>
            </Field>
          </div>
          <p className="text-xs text-muted">
            Applied through the broker after an <code>sshd -t</code> config test, reloaded (not restarted) so your current
            session survives. Disabling both password and key auth is refused.
          </p>
          <div className="flex justify-end">
            <Button type="submit" loading={harden.isPending}>Apply hardening</Button>
          </div>
        </form>
      </Card>
    </div>
  );
}

// UpdatesTab configures automatic security updates (unattended-upgrades) and
// shows the effective state read back from apt-config.
function UpdatesTab() {
  const upd = useUpdates();
  const configure = useConfigureUpdates();
  const [enabled, setEnabled] = useState(true);
  const [securityOnly, setSecurityOnly] = useState(true);
  const [autoReboot, setAutoReboot] = useState(false);

  if (upd.isLoading) return <Spinner />;
  if (upd.error || !upd.data?.available)
    return <Alert>Automatic updates need the privileged broker — not available on this host.</Alert>;

  const st = upd.data.status ?? {};
  const effectiveOn = st.unattended_effective === "1";

  return (
    <div className="space-y-4">
      <Card>
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold text-fg">Automatic security updates</h2>
          {effectiveOn ? <StatusBadge status="active" /> : <Badge>off</Badge>}
        </div>
        <p className="mt-1 text-xs text-muted">
          Effective apt state: unattended-upgrade = <code>{st.unattended_effective ?? "?"}</code>, panel drop-in{" "}
          {st.dropin_present === "true" ? "present" : "absent"}, tool{" "}
          {st.tool_present === "true" ? "installed" : "not installed"}.
          {st.tool_present !== "true" && " Install the unattended-upgrades package for the policy to run."}
        </p>
      </Card>

      <Card>
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            configure.mutate(
              { enabled, security_only: securityOnly, automatic_reboot: autoReboot },
              { onSuccess: () => toast.success("Update policy applied"), onError: (err) => toast.error(err.message) },
            );
          }}
        >
          <label className="flex items-center gap-2 text-sm text-fg">
            <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            Enable automatic upgrades
          </label>
          <label className="flex items-center gap-2 text-sm text-fg">
            <input type="checkbox" checked={securityOnly} onChange={(e) => setSecurityOnly(e.target.checked)} />
            Security origin only (recommended)
          </label>
          <label className="flex items-center gap-2 text-sm text-fg">
            <input type="checkbox" checked={autoReboot} onChange={(e) => setAutoReboot(e.target.checked)} />
            Reboot automatically when an update requires it
          </label>
          <div className="flex justify-end">
            <Button type="submit" loading={configure.isPending}>Apply policy</Button>
          </div>
        </form>
      </Card>
    </div>
  );
}

// IntegrityTab: file-integrity monitoring (AIDE). Build a baseline of the
// panel's security-critical files, then check for tampering.
function IntegrityTab() {
  const fim = useFIM();
  const init = useFIMInit();
  const check = useFIMCheck();
  const [result, setResult] = useState<FIMResult | null>(null);
  const [scope, setScope] = useState("panel");

  if (fim.isLoading) return <Spinner />;
  if (fim.error || !fim.data?.available)
    return <Alert>File-integrity monitoring needs the privileged broker — not available on this host.</Alert>;
  const st = fim.data.status;

  return (
    <div className="space-y-4">
      <Card>
        <div className="flex items-center justify-between gap-4">
          <div>
            <div className="flex items-center gap-2">
              <h3 className="text-sm font-semibold text-fg">File-integrity monitoring</h3>
              {st.baseline ? <StatusBadge status="active" /> : <Badge>no baseline</Badge>}
              {st.baseline && st.scope && <Badge>{st.scope === "host" ? "host-wide" : "panel"}</Badge>}
            </div>
            <p className="mt-1 text-xs text-muted">
              AIDE hashes files and reports anything added, removed or changed since the baseline. The <em>panel</em>{" "}
              scope watches the panel's own security-critical files (configs, SSH, the daemons); <em>host-wide</em>{" "}
              extends to all of <span className="font-mono">/etc</span> and the system binary/library trees (a longer
              scan).
              {!st.tool_present && " AIDE is not installed on this host."}
            </p>
          </div>
          <div className="flex items-end gap-2">
            <Field label="Scope">
              <Select value={scope} onChange={(e) => setScope(e.target.value)}>
                <option value="panel">panel</option>
                <option value="host">host-wide</option>
              </Select>
            </Field>
            <Button
              variant="ghost"
              loading={init.isPending}
              onClick={() =>
                init.mutate(scope, {
                  onSuccess: () => {
                    setResult(null);
                    toast.success(`Baseline built (${scope === "host" ? "host-wide" : "panel"})`);
                  },
                  onError: (e) => toast.error(e.message),
                })
              }
            >
              {st.baseline ? "Rebuild baseline" : "Build baseline"}
            </Button>
            <Button
              disabled={!st.baseline}
              loading={check.isPending}
              onClick={() =>
                check.mutate(undefined, {
                  onSuccess: (r) => {
                    setResult(r);
                    toast[r.changed ? "error" : "success"](r.changed ? "Changes detected" : "Clean — no changes");
                  },
                  onError: (e) => toast.error(e.message),
                })
              }
            >
              Run check
            </Button>
          </div>
        </div>
      </Card>

      {result && (
        <Card className={result.changed ? "border-danger/40" : ""}>
          <div className="flex items-center gap-3 text-sm">
            {result.changed ? <Badge>changed</Badge> : <StatusBadge status="active" />}
            <span className="text-fg">
              {result.added} added · {result.changed_count} changed · {result.removed} removed
            </span>
          </div>
          {result.changed && result.report && (
            <pre className="mt-3 max-h-72 overflow-auto rounded-md bg-surface-2 p-3 text-xs text-muted">{result.report}</pre>
          )}
        </Card>
      )}

      <AuditScannerCard />
    </div>
  );
}

// AuditScannerCard runs the host audit scanners (rkhunter, lynis) on demand and
// shows the parsed findings. These take minutes; the button reflects that.
function AuditScannerCard() {
  const scan = useAuditScan();
  const [res, setRes] = useState<{ tool: string; warnings: number; suggestions?: number; hardening_index?: number; report: string } | null>(null);

  const run = (tool: string) =>
    scan.mutate(tool, {
      onSuccess: (r) => {
        setRes(r);
        toast.success(`${tool}: ${r.warnings} warning(s)`);
      },
      onError: (e) => toast.error(e.message),
    });

  return (
    <Card>
      <div className="flex items-center justify-between gap-4">
        <div>
          <h3 className="text-sm font-semibold text-fg">Host audit scanners</h3>
          <p className="mt-1 text-xs text-muted">
            <span className="font-medium text-fg">rkhunter</span> hunts for rootkits;{" "}
            <span className="font-medium text-fg">lynis</span> audits the host's configuration and scores its hardening.
            Each takes a few minutes.
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="ghost" loading={scan.isPending} onClick={() => run("rkhunter")}>
            Run rkhunter
          </Button>
          <Button variant="ghost" loading={scan.isPending} onClick={() => run("lynis")}>
            Run lynis
          </Button>
        </div>
      </div>
      {res && (
        <div className="mt-3">
          <div className="flex items-center gap-3 text-sm">
            <Badge>{res.tool}</Badge>
            <span className="text-fg">
              {res.warnings} warning(s)
              {res.hardening_index != null && ` · hardening index ${res.hardening_index}`}
              {res.suggestions ? ` · ${res.suggestions} suggestion(s)` : ""}
            </span>
          </div>
          {res.report && (
            <pre className="mt-3 max-h-72 overflow-auto rounded-md bg-surface-2 p-3 text-xs text-muted">{res.report}</pre>
          )}
        </div>
      )}
    </Card>
  );
}
