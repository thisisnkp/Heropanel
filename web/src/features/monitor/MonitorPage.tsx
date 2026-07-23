import { useState } from "react";
import { Alert, Card, Spinner } from "@/components/ui";
import { AlertsPanel } from "./AlertsPanel";
import { MetricChart } from "./MetricChart";
import {
  fmtBytes,
  fmtKB,
  fmtUptime,
  useHistory,
  useNodeMetrics,
  useServiceHealth,
  useSiteMetrics,
  usageTone,
  type DiskUsage,
  type HistoryRange,
  type NodeSample,
  type ServiceHealth,
  type SiteSample,
} from "./monitor";

// The Monitoring dashboard.
//
// Every number here is pushed, not polled. The page subscribes to the
// `monitor:node` channel and the server samples only while it is watched, so a
// tab left open in the background stops costing anything the moment it loses the
// subscription. The little "live" dot reflects whether pushes are currently
// arriving; the first paint is a one-shot fetch so the tiles are never empty
// while the socket connects.
export function MonitorPage() {
  const { sample, streaming, isLoading, error } = useNodeMetrics();

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-fg">Monitoring</h1>
          <p className="text-sm text-muted">Live host health — pushed, not polled.</p>
        </div>
        <span className="flex items-center gap-2 text-xs text-muted">
          <span
            className={`h-2 w-2 rounded-full ${streaming ? "animate-pulse bg-emerald-500" : "bg-border"}`}
            aria-hidden
          />
          {streaming ? "live" : "connecting…"}
        </span>
      </div>

      {isLoading ? (
        <Spinner />
      ) : error ? (
        <Alert>Could not read node metrics.</Alert>
      ) : sample ? (
        <NodeTiles sample={sample} />
      ) : null}

      <ServicesPanel />
      <HistoryPanel />
      <AlertsPanel />
      <SitesPanel />
    </div>
  );
}

// HistoryPanel charts node metrics over a chosen range. Each metric is its own
// single-series small multiple — never two scales on one axis.
const RANGES: HistoryRange[] = ["1h", "6h", "24h", "7d", "30d"];

function HistoryPanel() {
  const [range, setRange] = useState<HistoryRange>("24h");
  const { data, isLoading, error } = useHistory(range);
  const points = data?.points ?? [];

  const cpu = points.map((p) => ({ ts: p.ts, value: p.cpu_percent }));
  const mem = points.map((p) => ({
    ts: p.ts,
    value: p.mem_total_kb > 0 ? (p.mem_used_kb / p.mem_total_kb) * 100 : 0,
  }));
  const load = points.map((p) => ({ ts: p.ts, value: p.load1 }));

  return (
    <Card className="p-5">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-medium text-fg">History</h2>
        <div className="flex gap-1">
          {RANGES.map((r) => (
            <button
              key={r}
              onClick={() => setRange(r)}
              className={`rounded px-2 py-1 text-xs ${
                r === range ? "bg-brand/15 text-brand" : "text-muted hover:bg-border/50 hover:text-fg"
              }`}
            >
              {r}
            </button>
          ))}
        </div>
      </div>
      {isLoading ? (
        <Spinner />
      ) : error ? (
        <Alert>Could not load metric history.</Alert>
      ) : (
        <div className="grid gap-4 lg:grid-cols-3">
          <MetricChart title="CPU" points={cpu} color="rgb(var(--brand))" max={100} format={(v) => `${v.toFixed(0)}%`} />
          <MetricChart title="Memory" points={mem} color="rgb(16 185 129)" max={100} format={(v) => `${v.toFixed(0)}%`} />
          <MetricChart title="Load (1m)" points={load} color="rgb(245 158 11)" format={(v) => v.toFixed(2)} />
        </div>
      )}
      <p className="mt-2 text-xs text-muted">
        A sample a minute, kept ~48h, then hourly averages for ~30 days.
      </p>
    </Card>
  );
}

// ServicesPanel shows the up/down state of the services the host depends on.
function ServicesPanel() {
  const { rows, error } = useServiceHealth();
  if (error || rows.length === 0) return null;
  return (
    <Card className="p-5">
      <h2 className="mb-3 text-sm font-medium text-fg">Services</h2>
      <div className="flex flex-wrap gap-3">
        {rows.map((s) => (
          <ServiceChip key={s.service} svc={s} />
        ))}
      </div>
    </Card>
  );
}

function ServiceChip({ svc }: { svc: ServiceHealth }) {
  const tone =
    svc.state === "active"
      ? "bg-emerald-500"
      : svc.state === "unknown"
        ? "bg-border"
        : "bg-danger";
  return (
    <span className="flex items-center gap-2 rounded-lg border border-border bg-surface px-3 py-2 text-sm">
      <span className={`h-2 w-2 rounded-full ${tone}`} aria-hidden />
      <span className="font-medium text-fg">{svc.service}</span>
      <span className="text-xs text-muted">{svc.state}</span>
    </span>
  );
}

// SitesPanel shows live per-site resource usage from cgroup accounting.
function SitesPanel() {
  const { rows, error } = useSiteMetrics();
  if (error || rows.length === 0) return null;
  return (
    <Card className="overflow-x-auto p-0">
      <h2 className="px-5 pt-5 text-sm font-medium text-fg">Sites</h2>
      <table className="mt-3 w-full text-sm">
        <thead>
          <tr className="border-b border-border text-left text-xs text-muted">
            <th className="px-5 py-2 font-medium">Site</th>
            <th className="px-5 py-2 font-medium">CPU</th>
            <th className="px-5 py-2 font-medium">Memory</th>
            <th className="px-5 py-2 font-medium">Tasks</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((s: SiteSample) => (
            <tr key={s.vhost} className="border-b border-border/60 last:border-0">
              <td className="px-5 py-2.5 font-mono text-xs text-fg">{s.vhost}</td>
              {s.present ? (
                <>
                  <td className={`px-5 py-2.5 ${usageTone(s.cpu_percent)}`}>{s.cpu_percent.toFixed(1)}%</td>
                  <td className="px-5 py-2.5 text-muted">{fmtBytes(s.mem_current_bytes)}</td>
                  <td className="px-5 py-2.5 text-muted">{s.tasks}</td>
                </>
              ) : (
                <td className="px-5 py-2.5 text-muted" colSpan={3}>
                  idle — nothing running in this site&apos;s slice yet
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </Card>
  );
}

function NodeTiles({ sample }: { sample: NodeSample }) {
  const memPct = sample.mem_total_kb > 0 ? (sample.mem_used_kb / sample.mem_total_kb) * 100 : 0;
  const swapPct = sample.swap_total_kb > 0 ? (sample.swap_used_kb / sample.swap_total_kb) * 100 : 0;
  const disks = sample.disks ?? [];

  return (
    <div className="space-y-6">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Meter label="CPU" pct={sample.cpu_percent} value={`${sample.cpu_percent.toFixed(0)}%`} />
        <Meter
          label="Memory"
          pct={memPct}
          value={`${fmtKB(sample.mem_used_kb)} / ${fmtKB(sample.mem_total_kb)}`}
        />
        <Stat label="Load (1 / 5 / 15m)" value={`${sample.load1.toFixed(2)} / ${sample.load5.toFixed(2)} / ${sample.load15.toFixed(2)}`} />
        <Stat label="Uptime" value={fmtUptime(sample.uptime_sec)} />
      </div>

      {sample.swap_total_kb > 0 && (
        <Card className="p-5">
          <Meter
            label="Swap"
            pct={swapPct}
            value={`${fmtKB(sample.swap_used_kb)} / ${fmtKB(sample.swap_total_kb)}`}
            flat
          />
        </Card>
      )}

      <Card className="p-5">
        <h2 className="mb-3 text-sm font-medium text-fg">Disks</h2>
        {disks.length === 0 ? (
          <p className="text-sm text-muted">No mounted filesystems reported.</p>
        ) : (
          <div className="space-y-4">
            {disks.map((d) => (
              <DiskRow key={d.path} disk={d} />
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

function DiskRow({ disk }: { disk: DiskUsage }) {
  return (
    <div>
      <div className="mb-1 flex items-center justify-between text-sm">
        <span className="font-mono text-xs text-fg">{disk.path}</span>
        <span className="text-muted">
          {fmtKB(disk.used_bytes / 1024)} / {fmtKB(disk.total_bytes / 1024)}{" "}
          <span className={usageTone(disk.used_percent)}>({disk.used_percent.toFixed(0)}%)</span>
        </span>
      </div>
      <Bar pct={disk.used_percent} />
    </div>
  );
}

// Meter is a labelled tile with a utilisation bar.
function Meter({ label, pct, value, flat }: { label: string; pct: number; value: string; flat?: boolean }) {
  const inner = (
    <>
      <div className="flex items-baseline justify-between">
        <span className="text-sm text-muted">{label}</span>
        <span className={`text-sm font-medium ${usageTone(pct)}`}>{pct.toFixed(0)}%</span>
      </div>
      <div className="mt-2">
        <Bar pct={pct} />
      </div>
      <div className="mt-2 text-xs text-muted">{value}</div>
    </>
  );
  return flat ? <div>{inner}</div> : <Card className="p-5">{inner}</Card>;
}

// Stat is a plain labelled value (no bar).
function Stat({ label, value }: { label: string; value: string }) {
  return (
    <Card className="p-5">
      <div className="text-sm text-muted">{label}</div>
      <div className="mt-2 text-lg font-semibold text-fg">{value}</div>
    </Card>
  );
}

// Bar renders a utilisation track. The fill colour follows the same 75/90 % tone
// thresholds the numbers use, so the eye and the figure agree.
function Bar({ pct }: { pct: number }) {
  const clamped = Math.max(0, Math.min(100, pct));
  const color = pct >= 90 ? "bg-danger" : pct >= 75 ? "bg-amber-500" : "bg-emerald-500";
  return (
    <div className="h-2 w-full overflow-hidden rounded-full bg-border/60">
      <div className={`h-full rounded-full ${color} transition-[width] duration-500`} style={{ width: `${clamped}%` }} />
    </div>
  );
}
