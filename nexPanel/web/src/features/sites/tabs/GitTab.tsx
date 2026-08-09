import { useState } from "react";
import { ApiRequestError } from "@/lib/api";
import { Badge, Button, Card, EmptyState, Field, Input, Select, Spinner, StatusBadge, Toggle } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useJobs } from "@/stores/jobs";
import { useDeploy, useDeployments, useGitSource, useRollback, useSetGitSource } from "../site-detail";

// GitTab configures the site's Git source, shows deploy history, and offers
// deploy + rollback (both async, tracked in the global job drawer).
export function GitTab({ uid }: { uid: string }) {
  const { data: source, isLoading } = useGitSource(uid);
  const deployments = useDeployments(uid);
  const deploy = useDeploy(uid);
  const rollback = useRollback(uid);
  const [editing, setEditing] = useState(false);

  if (isLoading) return <Spinner />;

  const track = (id: string, label: string) => useJobs.getState().track(id, label);

  const runDeploy = () =>
    deploy.mutate(undefined, {
      onSuccess: (res) => {
        if (res.job) track(res.job.id, "Deploy");
        else toast.success("Deployed");
        deployments.refetch();
      },
      onError: (e) => toast.error("Deploy failed", e instanceof ApiRequestError ? e.message : undefined),
    });

  const runRollback = (dep: string) =>
    rollback.mutate(dep, {
      onSuccess: (res) => {
        if (res.job) track(res.job.id, "Rollback");
        else toast.success("Rolled back");
        deployments.refetch();
      },
    });

  if (!source || editing) {
    return <GitSourceForm uid={uid} onDone={() => setEditing(false)} hasSource={!!source} />;
  }

  return (
    <div className="space-y-4">
      <Card className="space-y-3 p-5">
        <div className="flex items-start justify-between">
          <div>
            <h3 className="text-sm font-semibold text-fg">Source</h3>
            <p className="mt-1 break-all font-mono text-xs text-muted">{source.repo_url}</p>
            <div className="mt-2 flex flex-wrap gap-2">
              <Badge>branch {source.branch}</Badge>
              <Badge>auth {source.auth_kind}</Badge>
              {source.auto_composer && <Badge>composer</Badge>}
            </div>
          </div>
          <div className="flex gap-2">
            <Button variant="ghost" onClick={() => setEditing(true)}>
              Edit
            </Button>
            <Button loading={deploy.isPending} onClick={runDeploy}>
              Deploy
            </Button>
          </div>
        </div>

        {source.public_key && (
          <div className="rounded-lg border border-border bg-surface p-3">
            <p className="mb-1 text-xs font-medium text-fg">Deploy key — register this on the repository</p>
            <code className="block break-all text-[11px] text-muted">{source.public_key}</code>
          </div>
        )}
        {source.webhook_url && (
          <div className="rounded-lg border border-border bg-surface p-3">
            <p className="mb-1 text-xs font-medium text-fg">Webhook URL — add this as a push webhook</p>
            <code className="block break-all text-[11px] text-muted">{source.webhook_url}</code>
          </div>
        )}
      </Card>

      <Card className="overflow-hidden">
        <div className="border-b border-border px-4 py-3 text-sm font-medium text-fg">Deploy history</div>
        {deployments.data && deployments.data.length > 0 ? (
          <table className="w-full text-sm">
            <tbody>
              {deployments.data.map((d) => (
                <tr key={d.uid} className="border-b border-border/60 last:border-0">
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">
                      <StatusBadge status={d.status} />
                      <Badge>{d.trigger}</Badge>
                    </div>
                    <div className="mt-1 text-xs text-muted">{new Date(d.created_at).toLocaleString()}</div>
                  </td>
                  <td className="px-4 py-3 text-right">
                    {d.status === "succeeded" && (
                      <Button variant="ghost" className="h-8 px-2" onClick={() => runRollback(d.uid)}>
                        Rollback here
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <EmptyState title="No deployments yet" hint="Deploy to pull, build, and swap the release atomically." />
        )}
      </Card>
    </div>
  );
}

function GitSourceForm({ uid, onDone, hasSource }: { uid: string; onDone: () => void; hasSource: boolean }) {
  const save = useSetGitSource(uid);
  const [form, setForm] = useState({
    repo_url: "",
    branch: "main",
    build_command: "",
    web_root: "",
    auth_kind: "none",
    auth_username: "",
    token: "",
    host_key: "",
    auto_composer: true,
  });

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    save.mutate(form, {
      onSuccess: () => {
        toast.success("Git source saved");
        onDone();
      },
      onError: (e2) => toast.error("Could not save source", e2 instanceof ApiRequestError ? e2.message : undefined),
    });
  };

  return (
    <Card className="space-y-4 p-5">
      <h3 className="text-sm font-semibold text-fg">{hasSource ? "Edit Git source" : "Connect a Git repository"}</h3>
      <form onSubmit={submit} className="space-y-4">
        <Field label="Repository URL">
          <Input autoFocus value={form.repo_url} onChange={(e) => setForm({ ...form, repo_url: e.target.value })} placeholder="https://github.com/acme/app.git" />
        </Field>
        <div className="grid grid-cols-2 gap-4">
          <Field label="Branch">
            <Input value={form.branch} onChange={(e) => setForm({ ...form, branch: e.target.value })} />
          </Field>
          <Field label="Auth">
            <Select value={form.auth_kind} onChange={(e) => setForm({ ...form, auth_kind: e.target.value })}>
              <option value="none">Public (none)</option>
              <option value="token">HTTPS token (PAT)</option>
              <option value="ssh_key">SSH deploy key</option>
            </Select>
          </Field>
        </div>
        {form.auth_kind === "token" && (
          <div className="grid grid-cols-2 gap-4">
            <Field label="Username" hint="blank picks the provider default">
              <Input value={form.auth_username} onChange={(e) => setForm({ ...form, auth_username: e.target.value })} />
            </Field>
            <Field label="Token" hint="stored sealed; never shown again">
              <Input type="password" value={form.token} onChange={(e) => setForm({ ...form, token: e.target.value })} />
            </Field>
          </div>
        )}
        {form.auth_kind === "ssh_key" && (
          <>
            <p className="rounded-lg border border-border bg-surface p-3 text-xs text-muted">
              The panel generates an ed25519 deploy key and shows the public half after saving — register it on the repo.
            </p>
            <Field
              label="Pinned host key"
              hint="optional but recommended; paste `ssh-keyscan <host>` output. When set, the first clone is verified too (strict), not trust-on-first-use."
            >
              <textarea
                value={form.host_key}
                onChange={(e) => setForm({ ...form, host_key: e.target.value })}
                rows={3}
                className="w-full rounded-lg border border-border bg-surface px-3 py-2 font-mono text-[11px] text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-brand"
                placeholder="github.com ssh-ed25519 AAAAC3NzaC1lZDI1..."
              />
            </Field>
          </>
        )}
        <Field label="Build command" hint="optional; runs after clone, before the swap">
          <Input value={form.build_command} onChange={(e) => setForm({ ...form, build_command: e.target.value })} placeholder="npm ci && npm run build" />
        </Field>
        <Field label="Web root" hint="optional subdirectory to serve (e.g. dist, public)">
          <Input value={form.web_root} onChange={(e) => setForm({ ...form, web_root: e.target.value })} />
        </Field>
        <div className="flex items-center justify-between rounded-lg border border-border p-3">
          <div>
            <p className="text-sm font-medium text-fg">Composer auto-install</p>
            <p className="text-xs text-muted">Run composer install when a release has composer.json.</p>
          </div>
          <Toggle checked={form.auto_composer} onChange={(v) => setForm({ ...form, auto_composer: v })} />
        </div>
        <div className="flex justify-end gap-2">
          {hasSource && (
            <Button type="button" variant="ghost" onClick={onDone}>
              Cancel
            </Button>
          )}
          <Button type="submit" loading={save.isPending}>
            Save source
          </Button>
        </div>
      </form>
    </Card>
  );
}
