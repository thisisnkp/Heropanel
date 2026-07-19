import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { ApiRequestError } from "@/lib/api";
import { Alert, Badge, Button, Card, EmptyState, Field, Input, Modal, Spinner, StatusBadge, cn } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useJobs } from "@/stores/jobs";
import { isJobResult, useCreateSite, useSites, type CreateSiteInput } from "./sites";

function CreateSiteModal({ onClose, onSync }: { onClose: () => void; onSync: () => void }) {
  const [form, setForm] = useState<CreateSiteInput>({ name: "", primary_domain: "", type: "static" });
  const create = useCreateSite();
  const track = useJobs((s) => s.track);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    create.mutate(form, {
      onSuccess: (res) => {
        if (isJobResult(res)) track(res.job.id, "Provisioning site");
        else onSync();
        toast.info("Creating site…");
        onClose();
      },
    });
  };

  const fieldError = (name: string) =>
    create.error instanceof ApiRequestError ? create.error.fields?.find((f) => f.field === name)?.message : undefined;

  return (
    <Modal title="New website" onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <Field label="Name" hint={fieldError("name")}>
          <Input autoFocus value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Acme" />
        </Field>
        <Field label="Primary domain" hint={fieldError("primary_domain")}>
          <Input value={form.primary_domain} onChange={(e) => setForm({ ...form, primary_domain: e.target.value })} placeholder="acme.example.com" />
        </Field>
        <Field label="Type">
          <div className="flex gap-2">
            {["static", "php", "proxy"].map((t) => (
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
        {create.error instanceof ApiRequestError && !create.error.fields?.length && <Alert>{create.error.message}</Alert>}
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={create.isPending}>
            Create
          </Button>
        </div>
      </form>
    </Modal>
  );
}

export function SitesPage() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { data, isLoading, error } = useSites();
  const [modalOpen, setModalOpen] = useState(false);
  // The command palette's "Create a website" navigates here with ?new=1.
  const [params, setParams] = useSearchParams();

  useEffect(() => {
    if (params.get("new") === "1") {
      setModalOpen(true);
      setParams({}, { replace: true });
    }
  }, [params, setParams]);

  const refresh = () => qc.invalidateQueries({ queryKey: ["sites"] });

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-fg">Websites</h1>
          <p className="text-sm text-muted">Isolated sites with a dedicated user, PHP pool, and web server config.</p>
        </div>
        <Button onClick={() => setModalOpen(true)}>New site</Button>
      </div>

      {isLoading && (
        <div className="flex items-center gap-2 text-muted">
          <Spinner /> Loading…
        </div>
      )}
      {error && <Alert>You do not have permission to view sites, or the request failed.</Alert>}

      {data && (
        <Card className="overflow-hidden">
          {data.length > 0 ? (
            <table className="w-full text-sm">
              <thead className="border-b border-border text-left text-muted">
                <tr>
                  <th className="px-4 py-3 font-medium">Site</th>
                  <th className="px-4 py-3 font-medium">Type</th>
                  <th className="px-4 py-3 font-medium">Status</th>
                </tr>
              </thead>
              <tbody>
                {data.map((s) => (
                  <tr
                    key={s.uid}
                    onClick={() => navigate(`/sites/${s.uid}`)}
                    className="cursor-pointer border-b border-border/60 last:border-0 hover:bg-border/20"
                  >
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
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <EmptyState
              title="No sites yet"
              hint="Create your first website — a dedicated user, isolated files, and a web server vhost."
              action={<Button onClick={() => setModalOpen(true)}>Create a website</Button>}
            />
          )}
        </Card>
      )}

      {modalOpen && <CreateSiteModal onClose={() => setModalOpen(false)} onSync={refresh} />}
    </div>
  );
}
