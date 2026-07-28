import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiRequestError, api, can, type PHPInfo } from "@/lib/api";
import { Alert, Button, Card, Field, Input, Select, Spinner, Toggle } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useMe } from "@/features/auth/auth";
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

      <VersionOpcacheCard version={data?.version ?? form.version} />
    </div>
  );
}

interface VersionOpcache {
  version: string;
  memory_consumption_mb: number;
  interned_strings_buffer_mb: number;
  max_accelerated_files: number;
  validate_timestamps: boolean;
  revalidate_freq_sec: number;
  jit_buffer_size_mb: number;
}

// VersionOpcacheCard tunes the PHP_INI_SYSTEM OPcache directives — the
// shared-memory sizes the FPM master allocates once at startup. These are
// per-**version**, not per-site: applying them restarts FPM for every site on
// the version, so it is gated by system.write and says so plainly.
function VersionOpcacheCard({ version }: { version: string }) {
  const { data: me } = useMe();
  const canWrite = can(me, "system.write");
  const qc = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ["php-opcache", version],
    queryFn: () => api.get<VersionOpcache>(`/php/opcache?version=${encodeURIComponent(version)}`),
    enabled: can(me, "system.read"),
  });
  const [form, setForm] = useState<VersionOpcache | null>(null);
  useEffect(() => {
    if (data) setForm(data);
  }, [data]);

  const save = useMutation({
    mutationFn: (v: VersionOpcache) => api.put<VersionOpcache>("/php/opcache", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["php-opcache", version] }),
  });

  if (!can(me, "system.read")) return null;
  if (isLoading || !form) return null;
  if (error) return null;

  const set = (patch: Partial<VersionOpcache>) => setForm({ ...form, ...patch });

  return (
    <Card className="space-y-4 p-5">
      <div>
        <h3 className="text-sm font-semibold text-fg">OPcache shared memory — PHP {version} (all sites)</h3>
        <p className="text-xs text-muted">
          These sizes are allocated once by the FPM master at startup, so they belong to the whole PHP version, not one
          site. Applying restarts FPM for <strong>every site on PHP {version}</strong>.
        </p>
      </div>
      <div className="grid grid-cols-2 gap-4 md:grid-cols-3">
        <Field label="Memory (MB)" hint="opcache.memory_consumption">
          <Input type="number" value={form.memory_consumption_mb} onChange={(e) => set({ memory_consumption_mb: Number(e.target.value) })} />
        </Field>
        <Field label="Interned strings (MB)">
          <Input type="number" value={form.interned_strings_buffer_mb} onChange={(e) => set({ interned_strings_buffer_mb: Number(e.target.value) })} />
        </Field>
        <Field label="Max files">
          <Input type="number" value={form.max_accelerated_files} onChange={(e) => set({ max_accelerated_files: Number(e.target.value) })} />
        </Field>
        <Field label="Revalidate freq (s)">
          <Input type="number" value={form.revalidate_freq_sec} onChange={(e) => set({ revalidate_freq_sec: Number(e.target.value) })} />
        </Field>
        <Field label="JIT buffer (MB)" hint="0 disables the JIT buffer">
          <Input type="number" value={form.jit_buffer_size_mb} onChange={(e) => set({ jit_buffer_size_mb: Number(e.target.value) })} />
        </Field>
        <Field label="Validate timestamps">
          <div className="pt-1.5">
            <Toggle checked={form.validate_timestamps} onChange={(v) => set({ validate_timestamps: v })} />
          </div>
        </Field>
      </div>
      {canWrite && (
        <div className="flex justify-end">
          <Button
            variant="ghost"
            loading={save.isPending}
            onClick={() =>
              save.mutate(form, {
                onSuccess: () => toast.success(`OPcache tuned for PHP ${version} (FPM restarted)`),
                onError: (e) => toast.error("OPcache rejected", e instanceof ApiRequestError ? e.message : undefined),
              })
            }
          >
            Apply to PHP {version}
          </Button>
        </div>
      )}
    </Card>
  );
}
