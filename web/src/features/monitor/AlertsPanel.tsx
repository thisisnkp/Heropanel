import { useState } from "react";
import { ApiRequestError, can } from "@/lib/api";
import { Alert, Badge, Button, Card, Input } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useMe } from "@/features/auth/auth";
import {
  useAlertEvents,
  useAlertRules,
  useCreateRule,
  useDeleteRule,
  useToggleRule,
  type AlertRuleInput,
} from "./monitor";

// The alerts panel: threshold rules and the events they fire.
//
// A rule fires only after its breach has persisted for its duration, so the UI
// makes that explicit ("for N s"). Notification targets are write-only — the
// server never returns them — so the form collects them but the list never shows
// them; a Telegram token, once saved, is sealed and gone from view.

const METRICS = [
  { v: "cpu", label: "CPU %" },
  { v: "mem", label: "Memory %" },
  { v: "swap", label: "Swap %" },
  { v: "load1", label: "Load (1m)" },
  { v: "disk_root", label: "Disk / %" },
];

export function AlertsPanel() {
  const { data: me } = useMe();
  const canWrite = can(me, "monitor.write");
  const rules = useAlertRules();
  const events = useAlertEvents();
  const toggle = useToggleRule();
  const del = useDeleteRule();
  const [showForm, setShowForm] = useState(false);

  if (rules.error) return null; // no permission or unavailable — hide the section

  const ruleList = rules.data ?? [];
  const eventList = events.data ?? [];

  return (
    <Card className="p-5">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-medium text-fg">Alerts</h2>
        {canWrite && (
          <Button variant="ghost" className="h-8 px-3" onClick={() => setShowForm((v) => !v)}>
            {showForm ? "Cancel" : "New rule"}
          </Button>
        )}
      </div>

      {showForm && <RuleForm onDone={() => setShowForm(false)} />}

      {ruleList.length === 0 ? (
        <p className="text-sm text-muted">No alert rules yet. A rule fires when a metric crosses a threshold and stays there.</p>
      ) : (
        <div className="mt-2 space-y-2">
          {ruleList.map((r) => (
            <div key={r.uid} className="flex items-center justify-between rounded-lg border border-border bg-surface px-3 py-2 text-sm">
              <div className="flex items-center gap-2">
                <span className={`h-2 w-2 rounded-full ${r.enabled ? "bg-emerald-500" : "bg-border"}`} aria-hidden />
                <span className="font-medium text-fg">{r.name}</span>
                <span className="text-xs text-muted">
                  {r.metric} {r.op === "lt" ? "<" : ">"} {r.threshold}
                  {r.for_sec > 0 ? ` for ${r.for_sec}s` : ""}
                </span>
                <Badge>{r.notify_kind}</Badge>
              </div>
              {canWrite && (
                <div className="flex gap-2">
                  <Button variant="ghost" className="h-7 px-2 text-xs" onClick={() => toggle.mutate({ uid: r.uid, enabled: !r.enabled })}>
                    {r.enabled ? "Disable" : "Enable"}
                  </Button>
                  <Button
                    variant="ghost"
                    className="h-7 px-2 text-xs text-danger"
                    onClick={() =>
                      del.mutate(r.uid, {
                        onSuccess: () => toast.success("Rule deleted", r.name),
                      })
                    }
                  >
                    Delete
                  </Button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {eventList.length > 0 && (
        <div className="mt-4">
          <h3 className="mb-2 text-xs font-medium text-muted">Recent events</h3>
          <div className="space-y-1">
            {eventList.slice(0, 8).map((e, i) => (
              <div key={i} className="flex items-center gap-2 text-xs">
                <span className={`h-2 w-2 rounded-full ${e.state === "firing" ? "bg-danger" : "bg-emerald-500"}`} aria-hidden />
                <span className="text-fg">{e.state}</span>
                <span className="text-muted">{e.value.toFixed(1)}</span>
                <span className="ml-auto text-muted">{new Date(e.at + "Z").toLocaleString()}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </Card>
  );
}

function RuleForm({ onDone }: { onDone: () => void }) {
  const create = useCreateRule();
  const [f, setF] = useState<AlertRuleInput>({
    name: "",
    metric: "cpu",
    op: "gt",
    threshold: 90,
    for_sec: 300,
    notify_kind: "log",
    notify_target: {},
  });

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    create.mutate(f, {
      onSuccess: () => {
        toast.success("Alert rule created", f.name);
        onDone();
      },
      onError: (err) => toast.error("Could not create the rule", err instanceof ApiRequestError ? err.message : undefined),
    });
  };

  return (
    <form onSubmit={submit} className="mb-4 space-y-3 rounded-lg border border-border bg-surface p-4">
      <Input placeholder="Rule name" value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} required />
      <div className="flex flex-wrap gap-2">
        <select
          className="rounded border border-border bg-panel px-2 py-1.5 text-sm text-fg"
          value={f.metric}
          onChange={(e) => setF({ ...f, metric: e.target.value })}
        >
          {METRICS.map((m) => (
            <option key={m.v} value={m.v}>
              {m.label}
            </option>
          ))}
        </select>
        <select
          className="rounded border border-border bg-panel px-2 py-1.5 text-sm text-fg"
          value={f.op}
          onChange={(e) => setF({ ...f, op: e.target.value })}
        >
          <option value="gt">&gt;</option>
          <option value="lt">&lt;</option>
        </select>
        <Input
          type="number"
          className="max-w-[8rem]"
          value={String(f.threshold)}
          onChange={(e) => setF({ ...f, threshold: Number(e.target.value) })}
        />
        <label className="flex items-center gap-1 text-xs text-muted">
          for
          <Input
            type="number"
            className="max-w-[6rem]"
            value={String(f.for_sec)}
            onChange={(e) => setF({ ...f, for_sec: Number(e.target.value) })}
          />
          s
        </label>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <select
          className="rounded border border-border bg-panel px-2 py-1.5 text-sm text-fg"
          value={f.notify_kind}
          onChange={(e) => setF({ ...f, notify_kind: e.target.value })}
        >
          <option value="log">Log only</option>
          <option value="webhook">Webhook</option>
          <option value="telegram">Telegram</option>
        </select>
        {f.notify_kind === "webhook" && (
          <Input
            placeholder="https://hooks.example.com/…"
            className="min-w-[16rem] flex-1"
            value={f.notify_target.webhook_url ?? ""}
            onChange={(e) => setF({ ...f, notify_target: { webhook_url: e.target.value } })}
          />
        )}
        {f.notify_kind === "telegram" && (
          <>
            <Input
              placeholder="bot token"
              value={f.notify_target.telegram_token ?? ""}
              onChange={(e) => setF({ ...f, notify_target: { ...f.notify_target, telegram_token: e.target.value } })}
            />
            <Input
              placeholder="chat id"
              value={f.notify_target.telegram_chat ?? ""}
              onChange={(e) => setF({ ...f, notify_target: { ...f.notify_target, telegram_chat: e.target.value } })}
            />
          </>
        )}
      </div>
      {f.notify_kind !== "log" && (
        <Alert>The target is sealed at rest and never shown again — copy it somewhere safe.</Alert>
      )}
      <div className="flex justify-end">
        <Button type="submit" loading={create.isPending}>
          Create rule
        </Button>
      </div>
    </form>
  );
}
