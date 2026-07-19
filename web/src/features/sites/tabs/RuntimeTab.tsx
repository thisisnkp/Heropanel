import { useEffect, useState } from "react";
import { ApiRequestError, type Runtime } from "@/lib/api";
import { Button, Card, Field, Input, Select, Spinner, StatusBadge } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useRuntime, useRuntimeControl, useRuntimeHealth, useSetRuntime } from "../site-detail";

const RUNTIMES = ["node", "python", "go", "generic"];

// RuntimeTab configures a proxy site's supervised app process, shows a live
// health probe, and offers start/stop/restart.
export function RuntimeTab({ uid }: { uid: string }) {
  const { data, isLoading } = useRuntime(uid, true);
  const configured = !!data;
  const health = useRuntimeHealth(uid, configured);
  const save = useSetRuntime(uid);
  const control = useRuntimeControl(uid);

  const [form, setForm] = useState<Partial<Runtime>>({ runtime: "node", command: "", port: 3000, health_path: "" });
  const [envText, setEnvText] = useState("");

  useEffect(() => {
    if (data) {
      setForm(data);
      setEnvText(
        Object.entries(data.env || {})
          .map(([k, v]) => `${k}=${v}`)
          .join("\n"),
      );
    }
  }, [data]);

  if (isLoading) return <Spinner />;

  const submit = () => {
    const env: Record<string, string> = {};
    for (const line of envText.split("\n")) {
      const [k, ...rest] = line.split("=");
      if (k.trim()) env[k.trim()] = rest.join("=");
    }
    save.mutate(
      { ...form, env },
      {
        onSuccess: () => toast.success("Runtime applied"),
        onError: (e) => toast.error("Runtime rejected", e instanceof ApiRequestError ? e.message : undefined),
      },
    );
  };

  const doControl = (action: "start" | "stop" | "restart") =>
    control.mutate(action, { onSuccess: () => toast.success(`App ${action}ed`) });

  return (
    <div className="space-y-4">
      {configured && (
        <Card className="flex flex-wrap items-center justify-between gap-3 p-4">
          <div className="flex items-center gap-3">
            <StatusBadge status={data!.status} />
            {health.data?.configured && (
              <span className="text-xs text-muted">
                health: {health.data.healthy ? `${health.data.status_code} ok` : health.data.error || "unhealthy"}
              </span>
            )}
          </div>
          <div className="flex gap-2">
            <Button variant="ghost" className="h-9" loading={control.isPending} onClick={() => doControl("start")}>
              Start
            </Button>
            <Button variant="ghost" className="h-9" loading={control.isPending} onClick={() => doControl("stop")}>
              Stop
            </Button>
            <Button variant="ghost" className="h-9" loading={control.isPending} onClick={() => doControl("restart")}>
              Restart
            </Button>
          </div>
        </Card>
      )}

      <Card className="space-y-4 p-5">
        <h3 className="text-sm font-semibold text-fg">{configured ? "Edit runtime" : "Configure runtime"}</h3>
        <div className="grid grid-cols-2 gap-4">
          <Field label="Runtime">
            <Select value={form.runtime} onChange={(e) => setForm({ ...form, runtime: e.target.value })}>
              {RUNTIMES.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Port" hint="the local port the app listens on">
            <Input type="number" value={form.port} onChange={(e) => setForm({ ...form, port: Number(e.target.value) })} />
          </Field>
        </div>
        <Field label="Start command" hint="e.g. node server.js — runs as the site user in current/">
          <Input value={form.command} onChange={(e) => setForm({ ...form, command: e.target.value })} placeholder="node server.js" />
        </Field>
        <Field label="Health path" hint="optional; e.g. /healthz — makes 'running' mean the app actually answers">
          <Input value={form.health_path || ""} onChange={(e) => setForm({ ...form, health_path: e.target.value })} placeholder="/healthz" />
        </Field>
        <Field label="Environment" hint="KEY=value per line; PORT is injected automatically">
          <textarea
            value={envText}
            onChange={(e) => setEnvText(e.target.value)}
            rows={4}
            className="w-full rounded-lg border border-border bg-surface px-3 py-2 font-mono text-xs text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-brand"
            placeholder="NODE_ENV=production"
          />
        </Field>
        <div className="flex justify-end">
          <Button loading={save.isPending} onClick={submit}>
            {configured ? "Apply" : "Create runtime"}
          </Button>
        </div>
      </Card>
    </div>
  );
}
