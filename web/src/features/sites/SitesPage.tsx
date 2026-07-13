import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiRequestError, type Site } from "@/lib/api";
import { Alert, Badge, Button, Card, Field, Input, Spinner, cn } from "@/components/ui";
import {
  isJobResult,
  useCreateSite,
  useDeleteSite,
  useJobProgress,
  useSites,
  type CreateSiteInput,
} from "./sites";

const statusTone: Record<string, string> = {
  active: "text-emerald-500",
  provisioning: "text-amber-500",
  error: "text-danger",
  disabled: "text-muted",
  suspended: "text-amber-500",
};

function StatusBadge({ status }: { status: string }) {
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-xs font-medium", statusTone[status] ?? "text-muted")}>
      <span className="h-1.5 w-1.5 rounded-full bg-current" />
      {status}
    </span>
  );
}

// JobProgress renders a live progress bar for an in-flight provisioning job.
function JobProgress({
  jobUid,
  label,
  onDone,
}: {
  jobUid: string;
  label: string;
  onDone: (status: string) => void;
}) {
  const { progress, step, status } = useJobProgress(jobUid, onDone);
  const failed = status === "failed";
  return (
    <Card className="p-4">
      <div className="mb-2 flex items-center justify-between text-sm">
        <span className="font-medium text-fg">
          {label} — <span className="text-muted">{failed ? "failed" : step || status}</span>
        </span>
        <span className="text-muted">{progress}%</span>
      </div>
      <div className="h-2 overflow-hidden rounded-full bg-border">
        <div
          className={cn("h-full rounded-full transition-all duration-300", failed ? "bg-danger" : "bg-brand")}
          style={{ width: `${Math.max(4, progress)}%` }}
        />
      </div>
      {failed && <p className="mt-2 text-xs text-danger">Provisioning failed. See the site's status for details.</p>}
    </Card>
  );
}

function CreateSiteModal({ onClose, onJob, onSync }: { onClose: () => void; onJob: (uid: string) => void; onSync: () => void }) {
  const [form, setForm] = useState<CreateSiteInput>({ name: "", primary_domain: "", type: "static" });
  const create = useCreateSite();

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    create.mutate(form, {
      onSuccess: (res) => {
        if (isJobResult(res)) onJob(res.job.id);
        else onSync();
        onClose();
      },
    });
  };

  const fieldError = (name: string) =>
    create.error instanceof ApiRequestError ? create.error.fields?.find((f) => f.field === name)?.message : undefined;

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/40 p-4" onClick={onClose}>
      <Card className="w-full max-w-md p-6" >
        <form onClick={(e) => e.stopPropagation()} onSubmit={submit} className="space-y-4">
          <h2 className="text-lg font-semibold text-fg">New website</h2>

          <Field label="Name" hint={fieldError("name")}>
            <Input
              autoFocus
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="Acme"
            />
          </Field>

          <Field label="Primary domain" hint={fieldError("primary_domain")}>
            <Input
              value={form.primary_domain}
              onChange={(e) => setForm({ ...form, primary_domain: e.target.value })}
              placeholder="acme.example.com"
            />
          </Field>

          <Field label="Type">
            <div className="flex gap-2">
              {["static", "php"].map((t) => (
                <button
                  key={t}
                  type="button"
                  onClick={() => setForm({ ...form, type: t })}
                  className={cn(
                    "flex-1 rounded-lg border px-3 py-2 text-sm capitalize transition-colors",
                    form.type === t ? "border-brand bg-brand/10 text-fg" : "border-border text-muted hover:text-fg",
                  )}
                >
                  {t}
                </button>
              ))}
            </div>
          </Field>

          {create.error instanceof ApiRequestError && !create.error.fields?.length && (
            <Alert>{create.error.message}</Alert>
          )}

          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={create.isPending}>
              Create
            </Button>
          </div>
        </form>
      </Card>
    </div>
  );
}

export function SitesPage() {
  const qc = useQueryClient();
  const { data, isLoading, error } = useSites();
  const del = useDeleteSite();
  const [modalOpen, setModalOpen] = useState(false);
  const [activeJob, setActiveJob] = useState<{ uid: string; label: string } | null>(null);

  const refresh = () => qc.invalidateQueries({ queryKey: ["sites"] });

  const onJobDone = () => {
    refresh();
    // Keep the bar briefly so the user sees 100%, then clear.
    setTimeout(() => setActiveJob(null), 1200);
  };

  const remove = (s: Site) => {
    if (!confirm(`Delete ${s.primary_domain}? This removes the site, its user, and files.`)) return;
    del.mutate(s.uid, {
      onSuccess: (res) => {
        if (res.job) setActiveJob({ uid: res.job.id, label: `Deleting ${s.primary_domain}` });
      },
    });
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-fg">Websites</h1>
          <p className="text-sm text-muted">Isolated sites with a dedicated user, PHP pool, and web server config.</p>
        </div>
        <Button onClick={() => setModalOpen(true)}>New site</Button>
      </div>

      {activeJob && <JobProgress jobUid={activeJob.uid} label={activeJob.label} onDone={onJobDone} />}

      {isLoading && (
        <div className="flex items-center gap-2 text-muted">
          <Spinner /> Loading…
        </div>
      )}
      {error && <Alert>You do not have permission to view sites, or the request failed.</Alert>}

      {data && (
        <Card className="overflow-hidden">
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-muted">
              <tr>
                <th className="px-4 py-3 font-medium">Site</th>
                <th className="px-4 py-3 font-medium">Type</th>
                <th className="px-4 py-3 font-medium">Status</th>
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody>
              {data.map((s) => (
                <tr key={s.uid} className="border-b border-border/60 last:border-0">
                  <td className="px-4 py-3">
                    <div className="font-medium text-fg">{s.primary_domain}</div>
                    <div className="text-xs text-muted">{s.name}</div>
                  </td>
                  <td className="px-4 py-3">
                    <Badge>{s.type}</Badge>
                  </td>
                  <td className="px-4 py-3">
                    <StatusBadge status={s.status} />
                  </td>
                  <td className="px-4 py-3 text-right">
                    <Button variant="ghost" className="h-8 px-2 text-danger" onClick={() => remove(s)}>
                      Delete
                    </Button>
                  </td>
                </tr>
              ))}
              {data.length === 0 && !activeJob && (
                <tr>
                  <td colSpan={4} className="px-4 py-10 text-center text-muted">
                    No sites yet. Create your first website.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </Card>
      )}

      {modalOpen && (
        <CreateSiteModal
          onClose={() => setModalOpen(false)}
          onJob={(uid) => setActiveJob({ uid, label: "Provisioning site" })}
          onSync={refresh}
        />
      )}
    </div>
  );
}
