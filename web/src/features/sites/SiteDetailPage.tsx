import { useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { ArrowLeft, ExternalLink, Globe, Pause, Play } from "lucide-react";
import { ApiRequestError, can } from "@/lib/api";
import { Alert, Badge, Button, Card, Skeleton, StatusBadge, Tabs } from "@/components/ui";
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
  // Seed the active tab from ?tab= so the Websites page's Tools menu can
  // deep-link straight to Files/Logs; it falls back to Overview.
  const [params] = useSearchParams();
  const [tab, setTab] = useState(() => params.get("tab") ?? "overview");
  const suspend = useSuspend(uid);

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="space-y-3">
          <Skeleton className="h-3 w-24" />
          <Skeleton className="h-7 w-72" />
          <Skeleton className="h-3.5 w-56" />
        </div>
        <Skeleton className="h-9 w-full rounded-lg" />
        <Skeleton className="h-64 w-full rounded-xl" />
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
        <Link
          to="/sites"
          className="inline-flex items-center gap-1.5 text-sm text-muted transition-colors hover:text-fg"
        >
          <ArrowLeft className="h-3.5 w-3.5" strokeWidth={2} aria-hidden />
          Websites
        </Link>

        <div className="mt-3 flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
          <div className="flex min-w-0 items-start gap-3">
            <span className="grid h-11 w-11 shrink-0 place-items-center rounded-xl border border-border bg-surface-2 text-muted">
              <Globe className="h-5 w-5" strokeWidth={1.75} aria-hidden />
            </span>
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2.5">
                <h1 className="truncate text-xl font-semibold text-fg">{site.primary_domain}</h1>
                <StatusBadge status={site.status} />
              </div>
              {/* The identifiers an operator needs when they open a shell or read
                  a log: which site this is, how it is served, and whose uid owns
                  the files. Kept as separated chips rather than one dot-joined
                  line so each stays scannable. */}
              <div className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted">
                <span className="truncate">{site.name}</span>
                <span className="text-border-strong">·</span>
                <Badge>{site.type}</Badge>
                {site.system_user && (
                  <>
                    <span className="text-border-strong">·</span>
                    <span className="font-mono">{site.system_user}</span>
                  </>
                )}
              </div>
            </div>
          </div>

          <div className="flex shrink-0 items-center gap-2">
            <a href={`http://${site.primary_domain}`} target="_blank" rel="noreferrer">
              <Button variant="ghost" size="sm">
                <ExternalLink className="h-3.5 w-3.5" strokeWidth={2} aria-hidden />
                Visit
              </Button>
            </a>
            <Button variant="ghost" size="sm" loading={suspend.isPending} onClick={toggleSuspend}>
              {suspended ? (
                <Play className="h-3.5 w-3.5" strokeWidth={2} aria-hidden />
              ) : (
                <Pause className="h-3.5 w-3.5" strokeWidth={2} aria-hidden />
              )}
              {suspended ? "Resume" : "Suspend"}
            </Button>
          </div>
        </div>
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
