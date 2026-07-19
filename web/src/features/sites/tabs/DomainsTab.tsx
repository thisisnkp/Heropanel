import { useState } from "react";
import { ApiRequestError, type Domain } from "@/lib/api";
import { Alert, Badge, Button, Card, EmptyState, Field, Input, Modal, Select, Spinner, Toggle } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useAddDomain, useDeleteDomain, useDomains, useSetForceHTTPS } from "../site-detail";

export function DomainsTab({ uid }: { uid: string }) {
  const { data, isLoading, error } = useDomains(uid);
  const del = useDeleteDomain(uid);
  const forceHTTPS = useSetForceHTTPS(uid);
  const [adding, setAdding] = useState(false);

  const primary = data?.find((d) => d.kind === "primary");

  if (isLoading) return <Spinner />;
  if (error) return <Alert>Could not load domains.</Alert>;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted">Aliases point extra hostnames here; redirects 301 elsewhere.</p>
        <Button onClick={() => setAdding(true)}>Add domain</Button>
      </div>

      {primary && (
        <Card className="flex items-center justify-between p-4">
          <div>
            <p className="text-sm font-medium text-fg">Force HTTPS</p>
            <p className="text-xs text-muted">
              Redirect plain HTTP to HTTPS on the primary domain. Enable only once a certificate exists.
            </p>
          </div>
          <Toggle
            checked={!!primary.force_https}
            disabled={forceHTTPS.isPending}
            onChange={(v) =>
              forceHTTPS.mutate(
                { enabled: v },
                {
                  onSuccess: () => toast.success(v ? "Force HTTPS on" : "Force HTTPS off"),
                  onError: (e) => toast.error("Could not change", e instanceof ApiRequestError ? e.message : undefined),
                },
              )
            }
          />
        </Card>
      )}

      <Card className="overflow-hidden">
        {data && data.length > 0 ? (
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-muted">
              <tr>
                <th className="px-4 py-3 font-medium">Domain</th>
                <th className="px-4 py-3 font-medium">Kind</th>
                <th className="px-4 py-3 font-medium">Target</th>
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody>
              {data.map((d) => (
                <tr key={d.uid} className="border-b border-border/60 last:border-0">
                  <td className="px-4 py-3 font-medium text-fg">{d.fqdn}</td>
                  <td className="px-4 py-3">
                    <Badge>{d.kind}</Badge>
                  </td>
                  <td className="px-4 py-3 text-muted">
                    {d.kind === "redirect" ? `${d.redirect_code || 301} → ${d.redirect_to}` : "—"}
                  </td>
                  <td className="px-4 py-3 text-right">
                    {d.kind !== "primary" && (
                      <Button
                        variant="ghost"
                        className="h-8 px-2 text-danger"
                        loading={del.isPending}
                        onClick={() => del.mutate(d.uid, { onSuccess: () => toast.success("Domain removed") })}
                      >
                        Remove
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <EmptyState title="No extra domains" hint="The primary domain always serves; add aliases or redirects here." />
        )}
      </Card>

      {adding && <AddDomainModal uid={uid} onClose={() => setAdding(false)} />}
    </div>
  );
}

function AddDomainModal({ uid, onClose }: { uid: string; onClose: () => void }) {
  const add = useAddDomain(uid);
  const [form, setForm] = useState({ fqdn: "", kind: "alias", redirect_to: "", redirect_code: 301 });

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const payload: Partial<Domain> & { redirect_code?: number } = { fqdn: form.fqdn, kind: form.kind };
    if (form.kind === "redirect") {
      payload.redirect_to = form.redirect_to;
      payload.redirect_code = form.redirect_code;
    }
    add.mutate(payload as never, {
      onSuccess: () => {
        toast.success("Domain added");
        onClose();
      },
    });
  };

  return (
    <Modal title="Add domain" onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <Field label="Hostname">
          <Input autoFocus value={form.fqdn} onChange={(e) => setForm({ ...form, fqdn: e.target.value })} placeholder="www.acme.com" />
        </Field>
        <Field label="Kind">
          <Select value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value })}>
            <option value="alias">Alias (serves this site)</option>
            <option value="redirect">Redirect (301 elsewhere)</option>
          </Select>
        </Field>
        {form.kind === "redirect" && (
          <>
            <Field label="Redirect to">
              <Input value={form.redirect_to} onChange={(e) => setForm({ ...form, redirect_to: e.target.value })} placeholder="https://acme.com/" />
            </Field>
            <Field label="Status code">
              <Select value={String(form.redirect_code)} onChange={(e) => setForm({ ...form, redirect_code: Number(e.target.value) })}>
                <option value="301">301 (permanent)</option>
                <option value="302">302 (temporary)</option>
              </Select>
            </Field>
          </>
        )}
        {add.error instanceof ApiRequestError && <Alert>{add.error.message}</Alert>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={add.isPending}>
            Add
          </Button>
        </div>
      </form>
    </Modal>
  );
}
