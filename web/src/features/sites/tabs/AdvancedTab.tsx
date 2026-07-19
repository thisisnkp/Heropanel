import { useEffect, useState } from "react";
import { ApiRequestError, type SiteLimits } from "@/lib/api";
import { Alert, Button, Card, Field, Input, Modal, Spinner } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useDeleteSite } from "../sites";
import { useClone, useLimits, useSetLimits } from "../site-detail";

const MB = 1024 * 1024;

// AdvancedTab holds the resource limits (the cgroup slice), clone, and delete.
export function AdvancedTab({
  uid,
  domain,
  onDeleted,
  trackJob,
}: {
  uid: string;
  domain: string;
  onDeleted: () => void;
  trackJob: (id: string, label: string) => void;
}) {
  const limits = useLimits(uid);
  const saveLimits = useSetLimits(uid);
  const del = useDeleteSite();
  const clone = useClone(uid);
  const [cloning, setCloning] = useState(false);
  const [form, setForm] = useState<SiteLimits>({ cpu_quota_pct: 0, mem_limit_bytes: 0, pids_max: 0 });

  useEffect(() => {
    if (limits.data) setForm(limits.data);
  }, [limits.data]);

  const submitLimits = () =>
    saveLimits.mutate(form, {
      onSuccess: () => toast.success("Limits applied to the slice"),
      onError: (e) => toast.error("Limits rejected", e instanceof ApiRequestError ? e.message : undefined),
    });

  const remove = () => {
    if (!confirm(`Delete ${domain}? This removes the site, its user, and all files. This cannot be undone.`)) return;
    del.mutate(uid, {
      onSuccess: (res) => {
        if (res.job) trackJob(res.job.id, `Deleting ${domain}`);
        toast.info("Deleting site…");
        onDeleted();
      },
    });
  };

  return (
    <div className="space-y-4">
      <Card className="space-y-4 p-5">
        <div>
          <h3 className="text-sm font-semibold text-fg">Resource limits</h3>
          <p className="text-xs text-muted">Enforced by the site's cgroup slice. 0 means unlimited.</p>
        </div>
        {limits.isLoading ? (
          <Spinner />
        ) : (
          <>
            <div className="grid grid-cols-3 gap-4">
              <Field label="CPU quota (%)" hint="% of one core; 200 = two cores">
                <Input type="number" value={form.cpu_quota_pct} onChange={(e) => setForm({ ...form, cpu_quota_pct: Number(e.target.value) })} />
              </Field>
              <Field label="Memory (MB)">
                <Input
                  type="number"
                  value={form.mem_limit_bytes ? Math.round(form.mem_limit_bytes / MB) : 0}
                  onChange={(e) => setForm({ ...form, mem_limit_bytes: Number(e.target.value) * MB })}
                />
              </Field>
              <Field label="Max processes">
                <Input type="number" value={form.pids_max} onChange={(e) => setForm({ ...form, pids_max: Number(e.target.value) })} />
              </Field>
            </div>
            <div className="flex justify-end">
              <Button loading={saveLimits.isPending} onClick={submitLimits}>
                Apply limits
              </Button>
            </div>
          </>
        )}
      </Card>

      <Card className="flex items-center justify-between p-5">
        <div>
          <h3 className="text-sm font-semibold text-fg">Clone</h3>
          <p className="text-xs text-muted">Copy this site's files into a brand-new site (its own user, no database).</p>
        </div>
        <Button variant="ghost" onClick={() => setCloning(true)}>
          Clone site
        </Button>
      </Card>

      <Card className="flex items-center justify-between border-danger/40 p-5">
        <div>
          <h3 className="text-sm font-semibold text-danger">Delete site</h3>
          <p className="text-xs text-muted">Removes the site, its Linux user, and all files. Irreversible.</p>
        </div>
        <Button variant="danger" loading={del.isPending} onClick={remove}>
          Delete
        </Button>
      </Card>

      {cloning && (
        <CloneModal
          onClose={() => setCloning(false)}
          pending={clone.isPending}
          error={clone.error instanceof ApiRequestError ? clone.error.message : undefined}
          onSubmit={(v) =>
            clone.mutate(v, {
              onSuccess: (res) => {
                if (res.job) trackJob(res.job.id, "Cloning site");
                toast.info("Cloning site…");
                setCloning(false);
              },
            })
          }
        />
      )}
    </div>
  );
}

function CloneModal({
  onClose,
  onSubmit,
  pending,
  error,
}: {
  onClose: () => void;
  onSubmit: (v: { name: string; primary_domain: string }) => void;
  pending: boolean;
  error?: string;
}) {
  const [form, setForm] = useState({ name: "", primary_domain: "" });
  return (
    <Modal title="Clone site" onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          onSubmit(form);
        }}
        className="space-y-4"
      >
        <Field label="New site name">
          <Input autoFocus value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Acme Staging" />
        </Field>
        <Field label="New primary domain" hint="a clone needs its own hostname">
          <Input value={form.primary_domain} onChange={(e) => setForm({ ...form, primary_domain: e.target.value })} placeholder="staging.acme.com" />
        </Field>
        {error && <Alert>{error}</Alert>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={pending}>
            Clone
          </Button>
        </div>
      </form>
    </Modal>
  );
}
