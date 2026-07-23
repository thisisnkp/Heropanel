import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiRequestError, can } from "@/lib/api";
import { Alert, Badge, Button, Card, Field, Input, Modal, Spinner } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useMe } from "@/features/auth/auth";
import { useDatabases } from "@/features/databases/databases";

// Backups: full + incremental chains, always sealed before they touch a target.
//
// Restore goes into a NEW site, never over the original — the original keeps
// serving while the restored copy is verified, so a mistaken restore destroys
// nothing. Promoting the copy afterwards is an explicit act.

interface Backup {
  uid: string;
  level: string;
  target: string;
  size_bytes: number;
  db_name?: string;
  created_at: string;
}

interface BackupConfig {
  enabled: boolean;
  interval_hours: number;
  target: string;
  keep_chains: number;
  db_uid: string;
}

interface BackupList {
  backups: Backup[];
  config: BackupConfig;
  available: boolean;
  s3: boolean;
}

function useBackups(uid: string) {
  return useQuery({ queryKey: ["backups", uid], queryFn: () => api.get<BackupList>(`/sites/${uid}/backups`) });
}

function fmtSize(n: number): string {
  if (n <= 0) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  const v = n / 1024 ** i;
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

export function BackupsTab({ uid }: { uid: string }) {
  const { data: me } = useMe();
  const canWrite = can(me, "site.write");
  const qc = useQueryClient();
  const { data, isLoading, error } = useBackups(uid);
  const [restoring, setRestoring] = useState<Backup | null>(null);

  const invalidate = () => qc.invalidateQueries({ queryKey: ["backups", uid] });

  const create = useMutation({
    mutationFn: (level: string) => api.post<Backup>(`/sites/${uid}/backups`, { level }),
    onSuccess: invalidate,
  });
  const del = useMutation({
    mutationFn: (bid: string) => api.del(`/sites/${uid}/backups/${bid}`),
    onSuccess: invalidate,
  });

  if (isLoading) return <Spinner />;
  if (error) return <Alert>Could not load backups.</Alert>;
  if (!data) return null;

  if (!data.available) {
    return (
      <Alert>
        Backups need the privileged broker and a data key (<code className="font-mono text-xs">HP_SECRET_KEY</code>).
        Encryption at rest is not optional — without a key the panel will not store a site&apos;s data at all.
      </Alert>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm text-muted">
          Every archive is compressed and <strong>sealed before it touches any storage</strong> — local disk or S3, a
          stolen copy is ciphertext. Incrementals only store what changed.
        </p>
        {canWrite && (
          <div className="flex gap-2">
            <Button
              loading={create.isPending}
              onClick={() =>
                create.mutate("", {
                  onSuccess: (b) => toast.success("Backup complete", `${b.level} · ${fmtSize(b.size_bytes)}`),
                  onError: (e) => toast.error("Backup failed", e instanceof ApiRequestError ? e.message : undefined),
                })
              }
            >
              Back up now
            </Button>
            <Button
              variant="ghost"
              loading={create.isPending}
              onClick={() =>
                create.mutate("full", {
                  onSuccess: () => toast.success("Full backup complete — a new chain begins"),
                  onError: (e) => toast.error("Backup failed", e instanceof ApiRequestError ? e.message : undefined),
                })
              }
            >
              New full chain
            </Button>
          </div>
        )}
      </div>

      {canWrite && <ConfigForm uid={uid} config={data.config} s3={data.s3} onSaved={invalidate} />}

      {data.backups.length === 0 ? (
        <Card className="p-6 text-center text-sm text-muted">
          No backups yet. The first one is always a full; the ones after only store what changed.
        </Card>
      ) : (
        <Card className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted">
                <th className="px-4 py-2 font-medium">When</th>
                <th className="px-4 py-2 font-medium">Level</th>
                <th className="px-4 py-2 font-medium">Target</th>
                <th className="px-4 py-2 font-medium">Size</th>
                <th className="px-4 py-2" />
              </tr>
            </thead>
            <tbody>
              {data.backups.map((b) => (
                <tr key={b.uid} className="border-b border-border/60 last:border-0">
                  <td className="px-4 py-2.5 text-fg">{new Date(b.created_at + "Z").toLocaleString()}</td>
                  <td className="px-4 py-2.5">
                    <div className="flex items-center gap-1.5">
                      <Badge>{b.level}</Badge>
                      {b.db_name && <Badge>db: {b.db_name}</Badge>}
                    </div>
                  </td>
                  <td className="px-4 py-2.5 text-muted">{b.target}</td>
                  <td className="px-4 py-2.5 text-muted">{fmtSize(b.size_bytes)}</td>
                  <td className="px-4 py-2.5 text-right">
                    {canWrite && (
                      <div className="flex justify-end gap-2">
                        <Button variant="ghost" className="h-8 px-3" onClick={() => setRestoring(b)}>
                          Restore…
                        </Button>
                        <Button
                          variant="danger"
                          className="h-8 px-3"
                          onClick={() =>
                            del.mutate(b.uid, {
                              onSuccess: (r) => {
                                const n = (r as { removed?: string[] }).removed?.length ?? 1;
                                toast.success(n > 1 ? `Deleted ${n} backups (dependents included)` : "Backup deleted");
                              },
                              onError: (e) =>
                                toast.error("Could not delete", e instanceof ApiRequestError ? e.message : undefined),
                            })
                          }
                        >
                          Delete
                        </Button>
                      </div>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}
      <p className="text-xs text-muted">
        Deleting a backup also deletes the later backups that depend on it — a chain never breaks silently.
      </p>

      {restoring && <RestoreWizard uid={uid} backup={restoring} onClose={() => setRestoring(null)} />}
    </div>
  );
}

function ConfigForm({
  uid,
  config,
  s3,
  onSaved,
}: {
  uid: string;
  config: BackupConfig;
  s3: boolean;
  onSaved: () => void;
}) {
  const [c, setC] = useState<BackupConfig>(config);
  const { data: dbs } = useDatabases();
  const save = useMutation({
    mutationFn: () => api.put(`/sites/${uid}/backups/config`, c),
  });

  return (
    <Card className="flex flex-wrap items-end gap-3 p-4">
      <label className="flex items-center gap-2 text-sm text-fg">
        <input type="checkbox" checked={c.enabled} onChange={(e) => setC({ ...c, enabled: e.target.checked })} />
        Scheduled backups
      </label>
      <label className="flex items-center gap-1 text-xs text-muted">
        every
        <Input
          type="number"
          className="max-w-[5rem]"
          value={String(c.interval_hours)}
          onChange={(e) => setC({ ...c, interval_hours: Number(e.target.value) })}
        />
        h
      </label>
      <label className="flex items-center gap-1 text-xs text-muted">
        keep
        <Input
          type="number"
          className="max-w-[4.5rem]"
          value={String(c.keep_chains)}
          onChange={(e) => setC({ ...c, keep_chains: Number(e.target.value) })}
        />
        chains
      </label>
      <select
        className="rounded border border-border bg-panel px-2 py-1.5 text-sm text-fg"
        value={c.target}
        onChange={(e) => setC({ ...c, target: e.target.value })}
      >
        <option value="local">Local disk</option>
        {s3 && <option value="s3">S3</option>}
      </select>
      <label className="flex items-center gap-1.5 text-xs text-muted">
        database
        <select
          className="rounded border border-border bg-panel px-2 py-1.5 text-sm text-fg"
          value={c.db_uid ?? ""}
          onChange={(e) => setC({ ...c, db_uid: e.target.value })}
          title="Include a full dump of this database with every backup, sealed alongside the files"
        >
          <option value="">none (files only)</option>
          {(dbs ?? []).map((d) => (
            <option key={d.uid} value={d.uid}>
              {d.name}
            </option>
          ))}
        </select>
      </label>
      <Button
        variant="ghost"
        loading={save.isPending}
        onClick={() =>
          save.mutate(undefined, {
            onSuccess: () => {
              toast.success("Backup policy saved");
              onSaved();
            },
            onError: (e) => toast.error("Could not save", e instanceof ApiRequestError ? e.message : undefined),
          })
        }
      >
        Save policy
      </Button>
    </Card>
  );
}

function RestoreWizard({ uid, backup, onClose }: { uid: string; backup: Backup; onClose: () => void }) {
  const [name, setName] = useState("");
  const [domain, setDomain] = useState("");
  const [withDb, setWithDb] = useState(Boolean(backup.db_name));
  const [dbName, setDbName] = useState(backup.db_name ? `${backup.db_name}_restored` : "");
  const [done, setDone] = useState<string | null>(null);
  const restore = useMutation({
    mutationFn: () =>
      api.post<{ uid: string; primary_domain: string }>(`/sites/${uid}/backups/${backup.uid}/restore`, {
        name,
        primary_domain: domain,
        db_name: withDb && backup.db_name ? dbName : "",
      }),
  });

  return (
    <Modal title={`Restore backup — ${new Date(backup.created_at + "Z").toLocaleString()}`} onClose={onClose}>
      {done ? (
        <div className="space-y-4">
          <Alert>
            Restored into <strong>{done}</strong>. The original site is untouched — verify the copy, then point your
            domain (or suspend the old site) when you are satisfied.
          </Alert>
          <div className="flex justify-end">
            <Button onClick={onClose}>Done</Button>
          </div>
        </div>
      ) : (
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            restore.mutate(undefined, {
              onSuccess: (site) => {
                setDone(site.primary_domain);
                toast.success("Restore complete", site.primary_domain);
              },
              onError: (err) =>
                toast.error("Restore failed", err instanceof ApiRequestError ? err.message : undefined),
            });
          }}
        >
          <p className="text-sm text-muted">
            This restores the full chain up to this backup into a <strong>new site</strong> — the original keeps serving
            untouched while you verify the copy.
          </p>
          <Field label="New site name">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Acme (restored)" required />
          </Field>
          <Field label="New site domain">
            <Input value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="restore.acme.com" required />
          </Field>
          {backup.db_name && (
            <div className="space-y-2 rounded border border-border p-3">
              <label className="flex items-center gap-2 text-sm text-fg">
                <input type="checkbox" checked={withDb} onChange={(e) => setWithDb(e.target.checked)} />
                Also restore the database dump (<code className="font-mono text-xs">{backup.db_name}</code>)
              </label>
              {withDb && (
                <Field label="New database name">
                  <Input value={dbName} onChange={(e) => setDbName(e.target.value)} required />
                </Field>
              )}
              <p className="text-xs text-muted">
                The dump is imported into a <strong>new</strong> database — the original is never touched.
              </p>
            </div>
          )}
          <div className="flex justify-end gap-2">
            <Button variant="ghost" type="button" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={restore.isPending}>
              Restore
            </Button>
          </div>
        </form>
      )}
    </Modal>
  );
}
