import { useState } from "react";
import { ApiRequestError, can } from "@/lib/api";
import { Alert, Badge, Button, Card, EmptyState, Spinner } from "@/components/ui";
import { useMe } from "@/features/auth/auth";
import { useDockerInfo } from "@/features/docker/docker";
import { DeployWizard } from "./DeployWizard";
import { useAppTemplates, type AppTemplate } from "./apps";

// The one-click app catalog.
//
// The organising decision here is feasibility. Every card knows whether the host
// has the memory the app needs, and an app that will not fit is shown *disabled
// with the reason* rather than hidden — hiding it would leave an operator
// wondering why the app they read about is missing; showing "needs 512 MB, you
// have 240 MB" tells them exactly what to change.
export function AppsPage() {
  const { data: me } = useMe();
  const canDeploy = can(me, "docker.write");
  const info = useDockerInfo();
  const templates = useAppTemplates();
  const [deploying, setDeploying] = useState<AppTemplate | null>(null);

  if (info.isLoading) return <Spinner />;

  if (!info.data?.available) {
    return (
      <div className="space-y-6">
        <Header />
        <EmptyState
          title="Docker is not available on this host"
          hint={info.data?.reason || "One-click apps deploy as Docker containers. Install Docker and restart HeroPanel."}
        />
      </div>
    );
  }

  if (templates.isLoading) return <Spinner />;
  if (templates.error) {
    return (
      <div className="space-y-6">
        <Header />
        <Alert>
          {templates.error instanceof ApiRequestError && templates.error.status === 403
            ? "You do not have permission to view the app catalog."
            : "Could not load the app catalog."}
        </Alert>
      </div>
    );
  }

  const apps = templates.data ?? [];

  return (
    <div className="space-y-6">
      <Header />
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {apps.map((t) => (
          <Card key={t.slug} className="flex flex-col gap-3 p-5">
            <div className="flex items-start justify-between gap-2">
              <div>
                <h3 className="font-semibold text-fg">{t.name}</h3>
                <Badge>{t.category}</Badge>
              </div>
              <span className="text-xs text-muted">{t.min_memory_mb} MB+</span>
            </div>
            <p className="flex-1 text-sm text-muted">{t.description}</p>

            {t.feasible ? (
              <Button disabled={!canDeploy} onClick={() => setDeploying(t)}>
                {canDeploy ? "Deploy" : "Deploy (needs permission)"}
              </Button>
            ) : (
              <div>
                <Button disabled className="w-full">
                  Not enough memory
                </Button>
                <p className="mt-1 text-xs text-danger">
                  Needs {t.min_memory_mb} MB; {t.available_mb} MB available.
                </p>
              </div>
            )}
          </Card>
        ))}
      </div>

      {apps.length === 0 && <EmptyState title="No app templates" hint="The catalog is empty." />}

      {deploying && <DeployWizard template={deploying} onClose={() => setDeploying(null)} />}
    </div>
  );
}

function Header() {
  return (
    <div>
      <h1 className="text-2xl font-semibold text-fg">One-click apps</h1>
      <p className="text-sm text-muted">
        Curated applications deployed as Docker stacks — secrets generated for you, ports bound to loopback so you front
        them with a reverse proxy, and data kept in volumes that survive a redeploy.
      </p>
    </div>
  );
}
