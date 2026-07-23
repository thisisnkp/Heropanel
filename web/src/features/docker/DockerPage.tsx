import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { ApiRequestError, can } from "@/lib/api";
import { Alert, Badge, Button, Card, EmptyState, Input, Modal, Spinner, Tabs, cn } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useMe } from "@/features/auth/auth";
import { WebTerminal } from "@/components/WebTerminal";
import { CreateContainerModal } from "./CreateContainerModal";
import { ExecModal } from "./ExecModal";
import { NetworksPanel, VolumesPanel } from "./ResourcesTab";
import {
  stateTone,
  useContainerAction,
  useContainerLogs,
  useContainers,
  useDockerInfo,
  useImages,
  usePruneImages,
  usePullImage,
  useRemoveImage,
  useStats,
  type ContainerAction,
  type DockerContainer,
  type DockerImage,
} from "./docker";

// The Docker module's UI.
//
// The organising idea is the `managed` flag. The panel shows every container on
// the host — an admin whose machine is out of memory needs to see the one eating
// it, whoever started it — but only offers actions on the containers HeroPanel
// created. The broker enforces that regardless; the UI matches it so the buttons
// do not lie about what will happen.
export function DockerPage() {
  const { data: me } = useMe();
  const canWrite = can(me, "docker.write");
  const info = useDockerInfo();
  const [tab, setTab] = useState("containers");

  if (info.isLoading) return <Spinner />;

  if (info.error) {
    return (
      <div className="space-y-6">
        <Header />
        <Alert>
          {info.error instanceof ApiRequestError && info.error.status === 403
            ? "You do not have permission to view containers."
            : "Could not reach the Docker module."}
        </Alert>
      </div>
    );
  }

  // A host without Docker is a state, not a failure — and the daemon's own
  // reason is shown, because "not installed" and "permission denied" need
  // completely different fixes and only the daemon can tell them apart.
  if (!info.data?.available) {
    return (
      <div className="space-y-6">
        <Header />
        <EmptyState
          title="Docker is not available on this host"
          hint={
            info.data?.reason ||
            "No Docker daemon answered. Install Docker and restart HeroPanel, and this page will populate itself."
          }
        />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <Header version={info.data.server_version} />
      <Tabs
        tabs={[
          { id: "containers", label: "Containers" },
          { id: "images", label: "Images" },
          { id: "volumes", label: "Volumes" },
          { id: "networks", label: "Networks" },
        ]}
        active={tab}
        onChange={setTab}
      />
      {tab === "containers" && <Containers canWrite={canWrite} />}
      {tab === "images" && <Images canWrite={canWrite} />}
      {tab === "volumes" && <VolumesPanel canWrite={canWrite} />}
      {tab === "networks" && <NetworksPanel canWrite={canWrite} />}
    </div>
  );
}

function Header({ version }: { version?: string }) {
  return (
    <div>
      <h1 className="text-2xl font-semibold text-fg">Docker</h1>
      <p className="text-sm text-muted">
        Containers on this host{version ? ` · Engine ${version}` : ""}. HeroPanel acts only on containers it created;
        everything else is shown read-only.
      </p>
    </div>
  );
}

// Containers is shared by the host-wide page and a site's own Docker tab. The
// only difference is the scope: with a siteUID it lists just that site's
// containers, and anything it creates is attributed to the site.
export function Containers({ canWrite, siteUID }: { canWrite: boolean; siteUID?: string }) {
  const { data, isLoading, error } = useContainers(siteUID);
  const [creating, setCreating] = useState(false);
  const [showStats, setShowStats] = useState(false);
  const stats = useStats(showStats);
  const [logsFor, setLogsFor] = useState<DockerContainer | null>(null);
  const [execIn, setExecIn] = useState<DockerContainer | null>(null);
  const [confirmRemove, setConfirmRemove] = useState<DockerContainer | null>(null);
  const act = useContainerAction();

  const statsByID = useMemo(() => {
    const m = new Map<string, string>();
    for (const s of stats.data ?? []) m.set(s.name, `${s.cpu_perc} · ${s.mem_usage}`);
    return m;
  }, [stats.data]);

  const run = (c: DockerContainer, action: ContainerAction, force?: boolean) => {
    act.mutate(
      { id: c.name || c.id, action, force },
      {
        onSuccess: () => {
          toast.success(`Container ${action === "remove" ? "removed" : action + "ed"}`, c.name);
          setConfirmRemove(null);
        },
        onError: (e) =>
          toast.error(
            `Could not ${action} the container`,
            e instanceof ApiRequestError ? e.message : undefined,
          ),
      },
    );
  };

  if (isLoading) return <Spinner />;
  if (error) return <Alert>Could not list containers.</Alert>;

  const containers = data ?? [];
  if (containers.length === 0) {
    return (
      <>
        <EmptyState
          title="No containers"
          hint={siteUID ? "This site has no containers yet." : "Nothing is running on this host yet."}
          action={canWrite ? <Button onClick={() => setCreating(true)}>Create a container</Button> : undefined}
        />
        {creating && <CreateContainerModal siteUID={siteUID} onClose={() => setCreating(false)} />}
      </>
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <span className="text-xs text-muted">
          {containers.length} container{containers.length === 1 ? "" : "s"}
        </span>
        <div className="flex gap-2">
          <Button variant="ghost" onClick={() => setShowStats((s) => !s)}>
            {showStats ? "Hide usage" : "Show usage"}
          </Button>
          {canWrite && <Button onClick={() => setCreating(true)}>Create</Button>}
        </div>
      </div>
      {creating && <CreateContainerModal siteUID={siteUID} onClose={() => setCreating(false)} />}

      <Card className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-2 font-medium">Name</th>
              <th className="px-4 py-2 font-medium">Image</th>
              <th className="px-4 py-2 font-medium">State</th>
              {showStats && <th className="px-4 py-2 font-medium">CPU · Memory</th>}
              <th className="px-4 py-2 font-medium">Ports</th>
              <th className="px-4 py-2" />
            </tr>
          </thead>
          <tbody>
            {containers.map((c) => (
              <tr key={c.id} className="border-b border-border/60 last:border-0 hover:bg-surface/60">
                <td className="px-4 py-2.5">
                  <div className="text-fg">{c.name}</div>
                  {c.site_uid ? (
                    <Link to={`/sites/${c.site_uid}`} className="text-xs text-brand hover:underline">
                      {c.site_uid}
                    </Link>
                  ) : (
                    !c.managed && (
                      <span
                        className="text-xs text-muted"
                        title="HeroPanel did not create this container, so it will not start, stop or remove it"
                      >
                        not managed by HeroPanel
                      </span>
                    )
                  )}
                </td>
                <td className="px-4 py-2.5 font-mono text-xs text-muted">{c.image}</td>
                <td className="px-4 py-2.5">
                  <span className={cn("text-xs font-medium", stateTone(c.state))}>{c.state}</span>
                  <div className="text-xs text-muted">{c.status}</div>
                </td>
                {showStats && (
                  <td className="whitespace-nowrap px-4 py-2.5 text-xs text-muted">
                    {statsByID.get(c.name) ?? (stats.isLoading ? "…" : "—")}
                  </td>
                )}
                <td className="px-4 py-2.5 font-mono text-xs text-muted">{c.ports || "—"}</td>
                <td className="px-4 py-2.5">
                  <div className="flex items-center justify-end gap-2">
                    <Button variant="ghost" className="h-8 px-3" onClick={() => setLogsFor(c)}>
                      Logs
                    </Button>
                    {canWrite && c.managed && (
                      <>
                        {c.state === "running" && (
                          <Button variant="ghost" className="h-8 px-3" onClick={() => setExecIn(c)}>
                            Shell
                          </Button>
                        )}
                        {c.state === "running" ? (
                          <>
                            <Button variant="ghost" className="h-8 px-3" onClick={() => run(c, "restart")}>
                              Restart
                            </Button>
                            <Button variant="ghost" className="h-8 px-3" onClick={() => run(c, "stop")}>
                              Stop
                            </Button>
                          </>
                        ) : (
                          <Button variant="ghost" className="h-8 px-3" onClick={() => run(c, "start")}>
                            Start
                          </Button>
                        )}
                        <Button variant="danger" className="h-8 px-3" onClick={() => setConfirmRemove(c)}>
                          Remove
                        </Button>
                      </>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>

      {logsFor && <LogsModal container={logsFor} onClose={() => setLogsFor(null)} />}
      {execIn && <ExecModal container={execIn} onClose={() => setExecIn(null)} />}

      {confirmRemove && (
        <Modal title={`Remove ${confirmRemove.name}?`} onClose={() => setConfirmRemove(null)}>
          <p className="text-sm text-muted">
            This removes the container. Its <strong>volumes are kept</strong> — deleting a container is routine,
            deleting its data is a separate act — so anything stored in a volume survives and can be reattached.
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setConfirmRemove(null)}>
              Cancel
            </Button>
            <Button variant="danger" loading={act.isPending} onClick={() => run(confirmRemove, "remove", true)}>
              Remove
            </Button>
          </div>
        </Modal>
      )}
    </div>
  );
}

// LogsModal shows a container's output two ways. The default is a polled tail
// (last 500 lines, refreshed) — cheap and enough for a glance. "Follow live"
// switches to a WebSocket that streams `docker logs --follow` as the container
// writes it, reusing the terminal's emulator (colours, scrollback, search) in a
// read-only mode. stdout and stderr are interleaved as the program wrote them.
function LogsModal({ container, onClose }: { container: DockerContainer; onClose: () => void }) {
  const [live, setLive] = useState(false);
  const ref = container.name || container.id;

  return (
    <Modal title={`Logs — ${container.name}`} wide onClose={onClose}>
      <div className="mb-2 flex items-center justify-between">
        <p className="text-xs text-muted">
          {live
            ? "Streaming live — output appears as the container writes it."
            : "Last 500 lines, refreshed every 5 seconds."}
        </p>
        <Button variant="ghost" className="h-8 px-3" onClick={() => setLive((v) => !v)}>
          {live ? "Stop following" : "Follow live"}
        </Button>
      </div>

      {live ? (
        <div className="h-[65vh]">
          <WebTerminal
            uid={ref}
            endpoint={`/docker/containers/${encodeURIComponent(ref)}/logs/stream`}
            wsQuery={{ tail: "1000" }}
            readOnly
          />
        </div>
      ) : (
        <PolledLogs id={ref} />
      )}

      <p className="mt-2 text-xs text-muted">
        Reading container logs is recorded in the audit log — they routinely carry connection strings and customer data.
      </p>
    </Modal>
  );
}

// PolledLogs is the non-live view: a bounded tail refreshed on an interval.
function PolledLogs({ id }: { id: string }) {
  const { data, isLoading, error } = useContainerLogs(id);
  const text = [data?.stdout ?? "", data?.stderr ?? ""].filter(Boolean).join("");

  return (
    <div className="h-[65vh] overflow-auto rounded-lg bg-surface p-3">
      {isLoading ? (
        <Spinner />
      ) : error ? (
        <Alert>Could not read this container&apos;s logs.</Alert>
      ) : text ? (
        <pre className="whitespace-pre-wrap break-all font-mono text-xs text-fg">{text}</pre>
      ) : (
        <p className="text-sm text-muted">This container has written nothing yet.</p>
      )}
    </div>
  );
}

function Images({ canWrite }: { canWrite: boolean }) {
  const { data, isLoading, error } = useImages();
  const pull = usePullImage();
  const remove = useRemoveImage();
  const prune = usePruneImages();
  const [image, setImage] = useState("");
  const [confirmRemove, setConfirmRemove] = useState<DockerImage | null>(null);
  const [confirmPrune, setConfirmPrune] = useState(false);

  if (isLoading) return <Spinner />;
  if (error) return <Alert>Could not list images.</Alert>;

  const images = data ?? [];

  const runRemove = (img: DockerImage) =>
    remove.mutate(
      { id: img.id },
      {
        onSuccess: () => {
          toast.success("Image removed", `${img.repository}:${img.tag}`);
          setConfirmRemove(null);
        },
        // Docker's "still used by a container" refusal arrives here as the message.
        onError: (e) =>
          toast.error("Could not remove the image", e instanceof ApiRequestError ? e.message : undefined),
      },
    );

  return (
    <div className="space-y-3">
      {canWrite && (
        <div className="flex flex-wrap items-center gap-2">
          <form
            className="flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (!image.trim()) return;
              pull.mutate(image.trim(), {
                onSuccess: () => {
                  toast.success("Image pulled", image);
                  setImage("");
                },
                onError: (e) =>
                  toast.error("Could not pull the image", e instanceof ApiRequestError ? e.message : undefined),
              });
            }}
          >
            <Input
              value={image}
              onChange={(e) => setImage(e.target.value)}
              placeholder="ghost:5-alpine"
              className="max-w-xs"
            />
            <Button type="submit" loading={pull.isPending}>
              Pull image
            </Button>
          </form>
          <Button variant="ghost" onClick={() => setConfirmPrune(true)} loading={prune.isPending}>
            Prune unused
          </Button>
          <span className="text-xs text-muted">A large image can take several minutes.</span>
        </div>
      )}

      {images.length === 0 ? (
        <EmptyState title="No images" hint="Pull one to get started." />
      ) : (
        <Card className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted">
                <th className="px-4 py-2 font-medium">Repository</th>
                <th className="px-4 py-2 font-medium">Tag</th>
                <th className="px-4 py-2 font-medium">Size</th>
                <th className="px-4 py-2 font-medium">Created</th>
                {canWrite && <th className="px-4 py-2" />}
              </tr>
            </thead>
            <tbody>
              {images.map((img) => (
                <tr key={img.id + img.tag} className="border-b border-border/60 last:border-0">
                  <td className="px-4 py-2.5 text-fg">{img.repository}</td>
                  <td className="px-4 py-2.5">
                    <Badge>{img.tag}</Badge>
                  </td>
                  <td className="px-4 py-2.5 text-muted">{img.size}</td>
                  <td className="px-4 py-2.5 text-muted">{img.created}</td>
                  {canWrite && (
                    <td className="px-4 py-2.5 text-right">
                      <Button variant="ghost" className="h-8 px-3" onClick={() => setConfirmRemove(img)}>
                        Remove
                      </Button>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      {confirmRemove && (
        <Modal title={`Remove ${confirmRemove.repository}:${confirmRemove.tag}?`} onClose={() => setConfirmRemove(null)}>
          <p className="text-sm text-muted">
            This deletes the image. If a container — running or stopped — still uses it, Docker refuses and the
            container is left untouched; remove the container first.
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setConfirmRemove(null)}>
              Cancel
            </Button>
            <Button variant="danger" loading={remove.isPending} onClick={() => runRemove(confirmRemove)}>
              Remove
            </Button>
          </div>
        </Modal>
      )}

      {confirmPrune && (
        <Modal title="Prune unused images?" onClose={() => setConfirmPrune(false)}>
          <p className="text-sm text-muted">
            This removes <strong>dangling</strong> images — untagged layers left behind by rebuilds, which nothing
            references by name. Images a container uses are kept.
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setConfirmPrune(false)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              loading={prune.isPending}
              onClick={() =>
                prune.mutate(false, {
                  onSuccess: () => {
                    toast.success("Pruned unused images");
                    setConfirmPrune(false);
                  },
                  onError: (e) =>
                    toast.error("Could not prune images", e instanceof ApiRequestError ? e.message : undefined),
                })
              }
            >
              Prune
            </Button>
          </div>
        </Modal>
      )}
    </div>
  );
}
