import { useState } from "react";
import { ApiRequestError, can } from "@/lib/api";
import { Alert, Button, Card, Field, Input, Modal } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useMe } from "@/features/auth/auth";
import { useDeployApp, useExposeApp, type AppTemplate, type DeployResult } from "./apps";

// The deploy wizard: collect the template's non-secret fields, deploy, then show
// what was created and offer to expose it on a domain.
//
// Secret fields are deliberately *not* collected. The server generates them and
// returns them once — so the wizard has two states, a form and a result, and the
// result screen is the only place those secrets will ever be shown. That is
// stated plainly there, because an operator who closes it expecting to find the
// password later will not. The result screen also carries the reverse-proxy
// wiring: an app is deployed on loopback and is not reachable from the internet
// until it is given a domain, which is the ExposeStep below.
export function DeployWizard({ template, onClose }: { template: AppTemplate; onClose: () => void }) {
  const deploy = useDeployApp();
  const { data: me } = useMe();
  const [name, setName] = useState(template.slug);
  const [values, setValues] = useState<Record<string, string>>({});
  const [result, setResult] = useState<DeployResult | null>(null);

  const editable = (template.fields ?? []).filter((f) => !f.secret);
  const generated = (template.fields ?? []).filter((f) => f.secret);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    deploy.mutate(
      { slug: template.slug, name: name.trim(), values },
      {
        onSuccess: (res) => {
          setResult(res);
          toast.success(`${template.name} deployed`, name);
        },
        onError: (err) => toast.error("Deploy failed", err instanceof ApiRequestError ? err.message : undefined),
      },
    );
  };

  if (result) {
    return (
      <Modal title={`${template.name} is deploying`} onClose={onClose}>
        <div className="space-y-4">
          <Alert>
            The stack is starting. It may take a minute or two while images are pulled and containers come up — the
            Docker page shows live status.
          </Alert>

          <div className="text-sm text-muted">
            Published on <code className="font-mono text-fg">127.0.0.1:{result.port}</code> — bound to loopback, so it is
            not reachable from the internet until you give it a domain below.
          </div>

          {Object.keys(result.secrets).length > 0 && (
            <Card className="space-y-2 p-4">
              <p className="text-sm font-medium text-fg">Generated credentials</p>
              <p className="text-xs text-danger">
                Copy these now. They were generated for this deploy and are <strong>shown only once</strong> — the panel
                does not store them in a form it can show you again.
              </p>
              {Object.entries(result.secrets).map(([k, v]) => (
                <div key={k} className="flex items-center justify-between gap-2 rounded bg-surface px-3 py-2">
                  <span className="text-xs text-muted">{k}</span>
                  <code className="font-mono text-xs text-fg">{v}</code>
                  <Button
                    variant="ghost"
                    className="h-7 px-2 text-xs"
                    onClick={() => void navigator.clipboard.writeText(v).then(() => toast.success("Copied"))}
                  >
                    Copy
                  </Button>
                </div>
              ))}
            </Card>
          )}

          <ExposeStep project={result.project} canExpose={can(me, "site.write")} />

          <div className="flex justify-end">
            <Button onClick={onClose}>Done</Button>
          </div>
        </div>
      </Modal>
    );
  }

  return (
    <Modal title={`Deploy ${template.name}`} onClose={onClose}>
      <form className="space-y-4" onSubmit={submit}>
        <Field label="Name" hint="Lowercase letters, digits, dash or underscore. Used to name the stack.">
          <Input value={name} onChange={(e) => setName(e.target.value)} required />
        </Field>

        {editable.map((f) => (
          <Field key={f.key} label={f.label + (f.required ? "" : " (optional)")} hint={f.help}>
            <Input
              value={values[f.key] ?? ""}
              onChange={(e) => setValues((v) => ({ ...v, [f.key]: e.target.value }))}
              placeholder={f.placeholder}
              required={f.required}
            />
          </Field>
        ))}

        {generated.length > 0 && (
          <p className="text-xs text-muted">
            {generated.length === 1 ? "A secret" : `${generated.length} secrets`} will be generated for you and shown
            once after deploy — you do not choose {generated.length === 1 ? "it" : "them"}, so{" "}
            {generated.length === 1 ? "it" : "they"} cannot be weak or reused.
          </p>
        )}

        <div className="flex items-center justify-between">
          <span className="text-xs text-muted">Needs {template.min_memory_mb} MB.</span>
          <div className="flex gap-2">
            <Button variant="ghost" type="button" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={deploy.isPending}>
              Deploy
            </Button>
          </div>
        </div>
        {deploy.isPending && (
          <p className="text-right text-xs text-muted">Pulling images — this can take a few minutes.</p>
        )}
      </form>
    </Modal>
  );
}

// ExposeStep gives a freshly deployed app a public domain. Behind it the server
// creates a proxy site pointing at the app's live loopback port — so from here on
// the app is a real site, and its TLS, aliases and suspension are managed on the
// Sites page like any other.
function ExposeStep({ project, canExpose }: { project: string; canExpose: boolean }) {
  const expose = useExposeApp();
  const [domain, setDomain] = useState("");
  const [exposedAt, setExposedAt] = useState<string | null>(null);

  if (exposedAt) {
    return (
      <Alert>
        Now serving at{" "}
        <a className="font-mono text-brand underline" href={`https://${exposedAt}`} target="_blank" rel="noreferrer">
          {exposedAt}
        </a>
        . Manage its certificate and domains on the Sites page.
      </Alert>
    );
  }

  if (!canExpose) {
    return (
      <p className="text-xs text-muted">
        Point a site&apos;s reverse proxy at the port above to expose it — you do not have permission to create a site
        here.
      </p>
    );
  }

  return (
    <form
      className="flex flex-wrap items-end gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        const d = domain.trim().toLowerCase();
        if (!d) return;
        expose.mutate(
          { project, domain: d },
          {
            onSuccess: () => {
              setExposedAt(d);
              toast.success("App exposed", d);
            },
            onError: (err) =>
              toast.error("Could not expose the app", err instanceof ApiRequestError ? err.message : undefined),
          },
        );
      }}
    >
      <Field label="Expose on a domain" hint="Creates a proxy site that fronts this app.">
        <Input value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="blog.example.com" />
      </Field>
      <Button type="submit" loading={expose.isPending}>
        Expose
      </Button>
    </form>
  );
}
