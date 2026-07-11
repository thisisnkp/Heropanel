import { useQuery } from "@tanstack/react-query";
import { api, type SystemInfo } from "@/lib/api";
import { Card, Spinner } from "@/components/ui";

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
