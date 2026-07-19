import { useEffect, useState } from "react";
import { ApiRequestError, type PHPInfo } from "@/lib/api";
import { Alert, Button, Card, Field, Input, Select, Spinner, Toggle } from "@/components/ui";
import { toast } from "@/stores/toast";
import { usePHP, useSetPHP } from "../site-detail";

const VERSIONS = ["8.1", "8.2", "8.3"];
const JIT = ["off", "tracing", "function"];

// PHPTab is the per-site pool editor: version, memory, FPM sizing, an
// allowlisted php.ini editor, and OPcache. It is a full replace (matching the
// backend), so it edits a local copy of the whole envelope and PUTs it.
export function PHPTab({ uid }: { uid: string }) {
  const { data, isLoading, error } = usePHP(uid, true);
  const save = useSetPHP(uid);
  const [form, setForm] = useState<PHPInfo | null>(null);

  useEffect(() => {
    if (data) setForm(structuredClone(data));
  }, [data]);

  if (isLoading || !form) return <Spinner />;
  if (error) return <Alert>Could not load PHP settings.</Alert>;

  const submit = () => {
    save.mutate(form, {
      onSuccess: () => toast.success("PHP settings applied"),
      onError: (e) =>
        toast.error("PHP settings rejected", e instanceof ApiRequestError ? e.message : "The pool config was refused."),
    });
  };

  const setINI = (key: string, value: string) => setForm({ ...form, ini: { ...form.ini, [key]: value } });
  const removeINI = (key: string) => {
    const next = { ...form.ini };
    delete next[key];
    setForm({ ...form, ini: next });
  };
  const unusedAllowed = form.allowed_ini.filter((k) => !(k in form.ini));

  return (
    <div className="space-y-4">
      <Card className="space-y-4 p-5">
        <h3 className="text-sm font-semibold text-fg">Version &amp; memory</h3>
        <div className="grid grid-cols-2 gap-4">
          <Field label="PHP version">
            <Select value={form.version} onChange={(e) => setForm({ ...form, version: e.target.value })}>
              {VERSIONS.map((v) => (
                <option key={v} value={v}>
                  PHP {v}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Memory limit (MB)" hint="× max children is the site's ceiling on the node">
            <Input
              type="number"
              value={form.memory_limit_mb}
              onChange={(e) => setForm({ ...form, memory_limit_mb: Number(e.target.value) })}
            />
          </Field>
        </div>
      </Card>

      <Card className="space-y-4 p-5">
        <h3 className="text-sm font-semibold text-fg">FPM sizing</h3>
        <div className="grid grid-cols-2 gap-4">
          <Field label="Process manager">
            <Select value={form.fpm.pm} onChange={(e) => setForm({ ...form, fpm: { ...form.fpm, pm: e.target.value } })}>
              <option value="ondemand">ondemand</option>
              <option value="dynamic">dynamic</option>
              <option value="static">static</option>
            </Select>
          </Field>
          <Field label="Max children">
            <Input
              type="number"
              value={form.fpm.pm_max_children}
              onChange={(e) => setForm({ ...form, fpm: { ...form.fpm, pm_max_children: Number(e.target.value) } })}
            />
          </Field>
          {form.fpm.pm === "dynamic" && (
            <>
              <Field label="Start servers">
                <Input type="number" value={form.fpm.pm_start_servers} onChange={(e) => setForm({ ...form, fpm: { ...form.fpm, pm_start_servers: Number(e.target.value) } })} />
              </Field>
              <Field label="Min spare">
                <Input type="number" value={form.fpm.pm_min_spare_servers} onChange={(e) => setForm({ ...form, fpm: { ...form.fpm, pm_min_spare_servers: Number(e.target.value) } })} />
              </Field>
              <Field label="Max spare">
                <Input type="number" value={form.fpm.pm_max_spare_servers} onChange={(e) => setForm({ ...form, fpm: { ...form.fpm, pm_max_spare_servers: Number(e.target.value) } })} />
              </Field>
            </>
          )}
          {form.fpm.pm === "ondemand" && (
            <Field label="Idle timeout (s)">
              <Input type="number" value={form.fpm.pm_idle_timeout_sec} onChange={(e) => setForm({ ...form, fpm: { ...form.fpm, pm_idle_timeout_sec: Number(e.target.value) } })} />
            </Field>
          )}
        </div>
      </Card>

      <Card className="space-y-4 p-5">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-sm font-semibold text-fg">OPcache</h3>
            <p className="text-xs text-muted">Bytecode cache. JIT is a workload-dependent bet; off is the safe default.</p>
          </div>
          <Toggle checked={form.opcache.enabled} onChange={(v) => setForm({ ...form, opcache: { ...form.opcache, enabled: v } })} />
        </div>
        {form.opcache.enabled && (
          <Field label="JIT mode">
            <Select value={form.opcache.jit} onChange={(e) => setForm({ ...form, opcache: { ...form.opcache, jit: e.target.value } })}>
              {JIT.map((j) => (
                <option key={j} value={j}>
                  {j}
                </option>
              ))}
            </Select>
          </Field>
        )}
      </Card>

      <Card className="space-y-3 p-5">
        <h3 className="text-sm font-semibold text-fg">php.ini overrides</h3>
        <p className="text-xs text-muted">
          Only allowlisted directives can be set — the ones that confine the site are the panel's, not yours.
        </p>
        {Object.entries(form.ini).map(([k, v]) => (
          <div key={k} className="flex items-center gap-2">
            <code className="w-56 shrink-0 rounded bg-surface px-2 py-1.5 text-xs text-fg">{k}</code>
            <Input value={v} onChange={(e) => setINI(k, e.target.value)} className="h-9" />
            <Button variant="ghost" className="h-9 px-2 text-danger" onClick={() => removeINI(k)}>
              ✕
            </Button>
          </div>
        ))}
        {unusedAllowed.length > 0 && (
          <Select
            className="mt-1"
            value=""
            onChange={(e) => {
              if (e.target.value) setINI(e.target.value, "");
            }}
          >
            <option value="">+ Add a directive…</option>
            {unusedAllowed.map((k) => (
              <option key={k} value={k}>
                {k}
              </option>
            ))}
          </Select>
        )}
      </Card>

      <div className="flex justify-end">
        <Button loading={save.isPending} onClick={submit}>
          Apply PHP settings
        </Button>
      </div>
    </div>
  );
}
