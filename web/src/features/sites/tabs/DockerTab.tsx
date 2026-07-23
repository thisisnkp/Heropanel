import { can } from "@/lib/api";
import { Alert, EmptyState, Spinner } from "@/components/ui";
import { useMe } from "@/features/auth/auth";
import { Containers } from "@/features/docker/DockerPage";
import { useDockerInfo } from "@/features/docker/docker";

// A site's own containers.
//
// This tab exists because the host-wide Docker page answers "what is running on
// this machine", which is not the question someone looking at one site is
// asking. The API has always supported the site filter; without this there was
// no way to reach it, and a site deployed in docker mode had no view of its own
// workload at all.
export function DockerTab({ uid }: { uid: string }) {
  const { data: me } = useMe();
  const canWrite = can(me, "docker.write");
  const info = useDockerInfo();

  if (info.isLoading) return <Spinner />;
  if (info.error) return <Alert>Could not reach the Docker module.</Alert>;
  if (!info.data?.available) {
    return (
      <EmptyState
        title="Docker is not available on this host"
        hint={info.data?.reason || "No Docker daemon answered, so this site's containers cannot be managed here."}
      />
    );
  }

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted">
        Containers attributed to this site. Anything created here is labelled with the site, so it appears in both this
        tab and the host-wide Docker page.
      </p>
      <Containers canWrite={canWrite} siteUID={uid} />
    </div>
  );
}
