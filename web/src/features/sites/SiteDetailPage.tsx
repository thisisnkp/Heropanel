import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ApiRequestError } from "@/lib/api";
import { Alert, Button, Card, Spinner, StatusBadge, Tabs } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useJobs } from "@/stores/jobs";
import { useSite, useSuspend } from "./site-detail";
import { OverviewTab } from "./tabs/OverviewTab";
import { DomainsTab } from "./tabs/DomainsTab";
import { PHPTab } from "./tabs/PHPTab";
import { RuntimeTab } from "./tabs/RuntimeTab";
import { GitTab } from "./tabs/GitTab";
import { LogsTab } from "./tabs/LogsTab";
import { AdvancedTab } from "./tabs/AdvancedTab";

export function SiteDetailPage() {
  const { uid = "" } = useParams();
  const navigate = useNavigate();
  const { data: site, isLoading, error } = useSite(uid);
  const [tab, setTab] = useState("overview");
  const suspend = useSuspend(uid);

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-muted">
        <Spinner /> Loading…
      </div>
    );
  }
  if (error || !site) {
    return <Alert>Site not found, or you do not have permission to view it.</Alert>;
  }

  const isProxy = site.type === "proxy";
  const isPHP = site.type === "php";
  const suspended = site.status === "suspended";

  const tabs = [
    { id: "overview", label: "Overview" },
    { id: "domains", label: "Domains" },
    ...(isPHP ? [{ id: "php", label: "PHP" }] : []),
    ...(isProxy ? [{ id: "runtime", label: "Runtime" }] : []),
    { id: "git", label: "Git" },
    { id: "logs", label: "Logs" },
    { id: "advanced", label: "Advanced" },
  ];

  const toggleSuspend = () => {
    suspend.mutate(!suspended, {
      onSuccess: () => toast.success(suspended ? "Site resumed" : "Site suspended"),
      onError: (e) => toast.error("Could not change status", e instanceof ApiRequestError ? e.message : undefined),
    });
  };

  return (
    <div className="space-y-6">
      <div>
        <Link to="/sites" className="text-sm text-muted hover:text-fg">
          ← Websites
        </Link>
        <div className="mt-2 flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-semibold text-fg">{site.primary_domain}</h1>
            <StatusBadge status={site.status} />
          </div>
          <div className="flex items-center gap-2">
            <a href={`http://${site.primary_domain}`} target="_blank" rel="noreferrer">
              <Button variant="ghost">Visit ↗</Button>
            </a>
            <Button variant="ghost" loading={suspend.isPending} onClick={toggleSuspend}>
              {suspended ? "Resume" : "Suspend"}
            </Button>
          </div>
        </div>
        <p className="mt-1 text-sm text-muted">
          {site.name} · {site.type} · user {site.system_user}
        </p>
      </div>

      <Tabs tabs={tabs} active={tab} onChange={setTab} />

      {tab === "overview" && <OverviewTab site={site} />}
      {tab === "domains" && <DomainsTab uid={uid} />}
      {tab === "php" && <PHPTab uid={uid} />}
      {tab === "runtime" && <RuntimeTab uid={uid} />}
      {tab === "git" && <GitTab uid={uid} />}
      {tab === "logs" && <LogsTab uid={uid} />}
      {tab === "advanced" && (
        <AdvancedTab
          uid={uid}
          domain={site.primary_domain}
          onDeleted={() => navigate("/sites")}
          trackJob={(id, label) => useJobs.getState().track(id, label)}
        />
      )}
    </div>
  );
}

// InfoRow is shared by several tabs for label/value pairs.
export function InfoRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 border-b border-border/60 py-2.5 last:border-0">
      <span className="text-sm text-muted">{label}</span>
      <span className="text-right text-sm text-fg">{children}</span>
    </div>
  );
}

export { Card };
