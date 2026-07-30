import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { api, ApiRequestError, can, type SystemInfo } from "@/lib/api";
import { Button, Card, cn, Spinner } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useMe } from "@/features/auth/auth";
import { useNodeMetrics, type DiskUsage } from "@/features/monitor/monitor";

// ── formatting ───────────────────────────────────────────────────────────────

function fmtUptime(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`;
  const m = Math.floor(seconds / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${m % 60}m`;
  const days = Math.floor(h / 24);
  return `${days}d ${h % 24}h`;
}

function fmtFromKB(kb: number): string {
  const gb = kb / 1024 / 1024;
  if (gb >= 1) return `${gb.toFixed(1)} GB`;
  return `${(kb / 1024).toFixed(0)} MB`;
}

function fmtFromBytes(bytes: number): string {
  const gb = bytes / 1024 ** 3;
  if (gb >= 1) return `${gb.toFixed(1)} GB`;
  return `${(bytes / 1024 ** 2).toFixed(0)} MB`;
}

// meterTone shifts a usage bar from brand → amber → danger as it fills, so a box
// running hot reads as a warning at a glance rather than a number to decode.
function meterTone(percent: number): string {
  if (percent >= 90) return "bg-danger";
  if (percent >= 75) return "bg-amber-500";
  return "bg-brand";
}

// ── tiles ────────────────────────────────────────────────────────────────────

function MetricTile({
  label,
  value,
  sub,
  percent,
}: {
  label: string;
  value: string;
  sub?: string;
  percent?: number;
}) {
  return (
    <Card className="p-4">
      <div className="text-xs uppercase tracking-wide text-muted">{label}</div>
      <div className="mt-1 text-2xl font-semibold text-fg">{value}</div>
      {sub && <div className="mt-0.5 text-xs text-muted">{sub}</div>}
      {percent !== undefined && (
        <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-border">
          <div
            className={cn("h-full rounded-full transition-all", meterTone(percent))}
            style={{ width: `${Math.max(0, Math.min(100, percent))}%` }}
          />
        </div>
      )}
    </Card>
  );
}

// rootDisk picks the filesystem the operator cares about most — "/" if present,
// else the first reported — so the disk tile shows one meaningful number rather
// than a list.
function rootDisk(disks: DiskUsage[] | null | undefined): DiskUsage | null {
  if (!disks || disks.length === 0) return null;
  return disks.find((d) => d.path === "/") ?? disks[0];
}

// SystemMetrics is the live hero: CPU, memory, disk and uptime read from the
// monitor module's node sample (world-readable /proc + statfs), seeded by a
// one-shot fetch and then pushed over the realtime hub. Gated by monitor.read.
function SystemMetrics() {
  const { data: me } = useMe();
  const { sample, streaming, isLoading, error } = useNodeMetrics();

  if (!can(me, "monitor.read")) return null;
  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-muted">
        <Spinner /> Loading live metrics…
      </div>
    );
  }
  if (error || !sample) {
    return <p className="text-sm text-muted">Live metrics are unavailable right now.</p>;
  }

  const memPct = sample.mem_total_kb > 0 ? (sample.mem_used_kb / sample.mem_total_kb) * 100 : 0;
  const disk = rootDisk(sample.disks);

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <h2 className="text-sm font-semibold text-fg">System</h2>
        <span className="flex items-center gap-1 text-xs text-muted">
          <span className={cn("h-1.5 w-1.5 rounded-full", streaming ? "bg-success" : "bg-muted")} />
          {streaming ? "live" : "polling"}
        </span>
      </div>
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <MetricTile
          label="CPU"
          value={`${sample.cpu_percent.toFixed(0)}%`}
          sub={`load ${sample.load1.toFixed(2)} · ${sample.load5.toFixed(2)} · ${sample.load15.toFixed(2)}`}
          percent={sample.cpu_percent}
        />
        <MetricTile
          label="Memory"
          value={`${memPct.toFixed(0)}%`}
          sub={`${fmtFromKB(sample.mem_used_kb)} / ${fmtFromKB(sample.mem_total_kb)}`}
          percent={memPct}
        />
        {disk ? (
          <MetricTile
            label="Disk"
            value={`${disk.used_percent.toFixed(0)}%`}
            sub={`${fmtFromBytes(disk.used_bytes)} / ${fmtFromBytes(disk.total_bytes)} · ${disk.path}`}
            percent={disk.used_percent}
          />
        ) : (
          <MetricTile label="Disk" value="—" />
        )}
        <MetricTile label="Uptime" value={fmtUptime(sample.uptime_sec)} sub={`${sample.swap_used_kb > 0 ? `swap ${fmtFromKB(sample.swap_used_kb)}` : "no swap in use"}`} />
      </div>
    </div>
  );
}

// ── resource counts ──────────────────────────────────────────────────────────

// ResourceCard shows how many of a resource the caller can see, linking to its
// page. It runs only when the caller holds the read permission, so a client with
// no databases never fires a request that would 403 — the card simply is not there.
function ResourceCard({
  label,
  perm,
  endpoint,
  count,
  to,
}: {
  label: string;
  perm: string;
  endpoint: string;
  count: (data: unknown) => number;
  to: string;
}) {
  const { data: me } = useMe();
  const navigate = useNavigate();
  const enabled = can(me, perm);
  const { data, isLoading } = useQuery({
    queryKey: ["dashboard", endpoint],
    queryFn: () => api.get<unknown>(endpoint),
    enabled,
    staleTime: 30_000,
  });
  if (!enabled) return null;
  return (
    <button type="button" onClick={() => navigate(to)} className="text-left">
      <Card className="p-4 transition-colors hover:border-brand/50">
        <div className="text-xs uppercase tracking-wide text-muted">{label}</div>
        <div className="mt-1 text-2xl font-semibold text-fg">{isLoading ? "…" : count(data)}</div>
        <div className="mt-0.5 text-xs text-brand">Manage →</div>
      </Card>
    </button>
  );
}

const arrayLen = (d: unknown): number => (Array.isArray(d) ? d.length : 0);

function ResourceCounts() {
  const { data: me } = useMe();
  // Render nothing (not an empty heading) when the caller can see none of these.
  const anyVisible =
    can(me, "site.read") ||
    can(me, "database.read") ||
    can(me, "dns.read") ||
    can(me, "ssl.read") ||
    can(me, "mail.read");
  if (!anyVisible) return null;

  return (
    <div className="space-y-2">
      <h2 className="text-sm font-semibold text-fg">Resources</h2>
      <div className="grid grid-cols-2 gap-4 md:grid-cols-3 lg:grid-cols-5">
        <ResourceCard label="Websites" perm="site.read" endpoint="/sites" count={arrayLen} to="/sites" />
        <ResourceCard label="Databases" perm="database.read" endpoint="/databases" count={arrayLen} to="/databases" />
        <ResourceCard label="DNS zones" perm="dns.read" endpoint="/dns/zones" count={arrayLen} to="/dns" />
        <ResourceCard label="Certificates" perm="ssl.read" endpoint="/ssl/certificates" count={arrayLen} to="/ssl" />
        <ResourceCard
          label="Mail domains"
          perm="mail.read"
          endpoint="/mail/domains"
          count={(d) => {
            const v = d as { domains?: unknown[] } | null;
            return Array.isArray(v?.domains) ? v!.domains!.length : 0;
          }}
          to="/mail"
        />
      </div>
    </div>
  );
}

// ── page ─────────────────────────────────────────────────────────────────────

export function DashboardPage() {
  const { data: me } = useMe();
  const { data } = useQuery({
    queryKey: ["system-info"],
    queryFn: () => api.get<SystemInfo>("/system/info"),
    refetchInterval: 30_000,
  });

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-fg">Dashboard</h1>
        <p className="text-sm text-muted">
          {me?.display_name ?? me?.username ? `Welcome, ${me?.display_name ?? me?.username}. ` : ""}
          Live overview of this node.
        </p>
      </div>

      <SystemMetrics />
      <ResourceCounts />

      <PanelBackupsCard />
      <KeyringCard />

      {data && (
        <p className="text-xs text-muted">
          {data.product} {data.version} · {data.os}/{data.arch} · {data.cpus} CPU(s) · Go {data.go}
        </p>
      )}
    </div>
  );
}

interface KeyringStatus {
  available: boolean;
  active_generation: number;
  key_count: number;
  legacy_key_in_use: boolean;
}

// Data-key rotation: the envelope that seals credentials at rest. Rotating mints
// a new active data key; existing values keep opening under their own generation.
function KeyringCard() {
  const { data: me } = useMe();
  const canRead = can(me, "system.read");
  const qc = useQueryClient();
  const { data } = useQuery({
    queryKey: ["keyring"],
    queryFn: () => api.get<KeyringStatus>("/system/keyring"),
    enabled: canRead,
  });
  const rotate = useMutation({
    mutationFn: () => api.post<KeyringStatus>("/system/keyring/rotate", {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["keyring"] }),
  });
  if (!canRead || !data) return null;

  return (
    <Card className="flex flex-wrap items-center justify-between gap-3 p-4">
      <div>
        <h2 className="text-sm font-semibold text-fg">Data-key rotation</h2>
        <p className="mt-0.5 text-xs text-muted">
          {data.available
            ? data.legacy_key_in_use
              ? "Sealed credentials use the master-derived key (generation 0). Rotate to a wrapped data key so future rotations re-wrap keys instead of re-encrypting every row."
              : `Active data-key generation ${data.active_generation} · ${data.key_count} key(s). New sealed values use it; older values still open under their own generation.`
            : "Needs the broker's master key (HP_SECRET_KEY) and a datastore."}
        </p>
      </div>
      {data.available && can(me, "system.write") && (
        <Button
          variant="ghost"
          loading={rotate.isPending}
          onClick={() =>
            rotate.mutate(undefined, {
              onSuccess: (s) => toast.success(`Rotated to data-key generation ${s.active_generation}`),
              onError: (e) => toast.error("Rotation failed", e instanceof ApiRequestError ? e.message : undefined),
            })
          }
        >
          Rotate data key
        </Button>
      )}
    </Card>
  );
}

interface PanelBackup {
  uid: string;
  target: string;
  size_bytes: number;
  created_at: string;
}

interface PanelBackupList {
  backups: PanelBackup[];
  available: boolean;
  policy: { target: string; interval_hours: number; keep: number };
}

// Panel self-backup: sealed snapshots of the panel's own database. Restore is
// deliberately out-of-band (`hpd decrypt` + docs) — a panel that needs its
// database back cannot be trusted to serve that request.
function PanelBackupsCard() {
  const { data: me } = useMe();
  const canRead = can(me, "system.read");
  const qc = useQueryClient();
  const { data } = useQuery({
    queryKey: ["panel-backups"],
    queryFn: () => api.get<PanelBackupList>("/system/backups"),
    enabled: canRead,
  });
  const snap = useMutation({
    mutationFn: () => api.post<PanelBackup>("/system/backups", {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["panel-backups"] }),
  });

  if (!canRead || !data) return null;
  const latest = data.backups[0];

  return (
    <Card className="flex flex-wrap items-center justify-between gap-3 p-4">
      <div>
        <h2 className="text-sm font-semibold text-fg">Panel self-backup</h2>
        <p className="mt-0.5 text-xs text-muted">
          {data.available
            ? latest
              ? `Last sealed snapshot ${new Date(latest.created_at + "Z").toLocaleString()} · every ${data.policy.interval_hours}h, keeping ${data.policy.keep} · restore via hpd decrypt`
              : `No snapshot yet — the scheduler takes one every ${data.policy.interval_hours}h.`
            : "Needs the broker and HP_SECRET_KEY — sealed-at-rest is not optional."}
        </p>
      </div>
      {data.available && can(me, "system.write") && (
        <Button
          variant="ghost"
          loading={snap.isPending}
          onClick={() =>
            snap.mutate(undefined, {
              onSuccess: () => toast.success("Panel snapshot sealed and stored"),
              onError: (e) => toast.error("Snapshot failed", e instanceof ApiRequestError ? e.message : undefined),
            })
          }
        >
          Snapshot now
        </Button>
      )}
    </Card>
  );
}
