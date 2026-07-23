import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiRequestError, can, type SystemInfo } from "@/lib/api";
import { Button, Card, Spinner } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useMe } from "@/features/auth/auth";

function Stat({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <Card className="p-4">
      <div className="text-xs uppercase tracking-wide text-muted">{label}</div>
      <div className="mt-1 text-2xl font-semibold text-fg">{value}</div>
      {sub && <div className="mt-0.5 text-xs text-muted">{sub}</div>}
    </Card>
  );
}

function fmtUptime(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  const m = Math.floor(seconds / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  return `${h}h ${m % 60}m`;
}

export function DashboardPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ["system-info"],
    queryFn: () => api.get<SystemInfo>("/system/info"),
    refetchInterval: 10_000,
  });

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-fg">Dashboard</h1>
        <p className="text-sm text-muted">Control-plane overview.</p>
      </div>

      {isLoading && (
        <div className="flex items-center gap-2 text-muted">
          <Spinner /> Loading system info…
        </div>
      )}
      {error && <p className="text-sm text-danger">Failed to load system info.</p>}

      {data && (
        <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
          <Stat label="Version" value={data.version} sub={`${data.product}`} />
          <Stat label="Platform" value={`${data.os}/${data.arch}`} sub={data.go} />
          <Stat label="CPUs" value={String(data.cpus)} />
          <Stat label="Uptime" value={fmtUptime(data.uptime_seconds)} sub={`since ${new Date(data.started_at).toLocaleString()}`} />
        </div>
      )}

      <PanelBackupsCard />

      <Card className="p-6">
        <h2 className="text-sm font-semibold text-fg">Getting started</h2>
        <ul className="mt-3 space-y-2 text-sm text-muted">
          <li>• Auth &amp; RBAC are live — you are signed in as an administrator.</li>
          <li>• Next milestones: sites, DNS, SSL, Docker, monitoring.</li>
          <li>• The control plane is running on ~10&nbsp;MB of RAM.</li>
        </ul>
      </Card>
    </div>
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
