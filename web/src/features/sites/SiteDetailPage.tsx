import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ApiRequestError, can } from "@/lib/api";
import { Alert, Button, Card, Spinner, StatusBadge, Tabs } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useJobs } from "@/stores/jobs";
import { useMe } from "@/features/auth/auth";
import { useSite, useSuspend } from "./site-detail";
import { OverviewTab } from "./tabs/OverviewTab";
import { DomainsTab } from "./tabs/DomainsTab";
import { PHPTab } from "./tabs/PHPTab";
import { RuntimeTab } from "./tabs/RuntimeTab";
import { GitTab } from "./tabs/GitTab";
import { FilesTab } from "./tabs/FilesTab";
import { TerminalTab } from "./tabs/TerminalTab";
import { BackupsTab } from "./tabs/BackupsTab";
import { CronTab } from "./tabs/CronTab";
import { DockerTab } from "./tabs/DockerTab";
import { LogsTab } from "./tabs/LogsTab";
import { AdvancedTab } from "./tabs/AdvancedTab";

export function SiteDetailPage() {
  const { uid = "" } = useParams();
  const navigate = useNavigate();
  const { data: site, isLoading, error } = useSite(uid);
  const { data: me } = useMe();
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
  // The File Manager is baremetal-only (git/docker content is owned by the
  // deploy pipeline) and needs file.read; hide the tab otherwise rather than
  // letting it 403 on click.
  const showFiles = site.deploy_mode === "baremetal" && can(me, "file.read");
  // A terminal needs a real Linux account to attach to, and its own permission —
  // running arbitrary commands is a much larger grant than editing a file.
  const showTerminal = !!site.system_user && can(me, "terminal.use");
  // Containers are host-level, so this tab needs only docker.read rather than
  // the site permissions. Without it a site deployed in docker mode had no view
  // of its own workload at all, even though the API had always supported the
  // site filter.
  const showDocker = can(me, "docker.read");

  const tabs = [
    { id: "overview", label: "Overview" },
    { id: "domains", label: "Domains" },
    ...(isPHP ? [{ id: "php", label: "PHP" }] : []),
    ...(isProxy ? [{ id: "runtime", label: "Runtime" }] : []),
    ...(showFiles ? [{ id: "files", label: "Files" }] : []),
    ...(showTerminal ? [{ id: "terminal", label: "Terminal" }] : []),
    ...(showDocker ? [{ id: "docker", label: "Docker" }] : []),
    { id: "git", label: "Git" },
    { id: "cron", label: "Cron" },
    { id: "backups", label: "Backups" },
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
      {tab === "files" && showFiles && <FilesTab uid={uid} />}
      {tab === "terminal" && showTerminal && <TerminalTab uid={uid} systemUser={site.system_user} />}
      {tab === "docker" && showDocker && <DockerTab uid={uid} />}
      {tab === "git" && <GitTab uid={uid} />}
      {tab === "cron" && <CronTab uid={uid} />}
      {tab === "backups" && <BackupsTab uid={uid} />}
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
