import { useState } from "react";
import { ApiRequestError } from "@/lib/api";
import { Alert, Badge, Button, Card, EmptyState, Input, Modal, Spinner } from "@/components/ui";
import { toast } from "@/stores/toast";
import {
  stateTone,
  useCreateNetwork,
  useCreateVolume,
  useNetworkDetail,
  useNetworks,
  useRemoveNetwork,
  useRemoveVolume,
  useVolumeDetail,
  useVolumes,
} from "./docker";

// Volumes and networks share a shape — a name, an owner, a delete — so they
// share a component. The one asymmetry is stated in the volume delete: removing
// a network is reversible, removing a volume destroys data.

export function VolumesPanel({ canWrite }: { canWrite: boolean }) {
  const { data, isLoading, error } = useVolumes();
  const create = useCreateVolume();
  const remove = useRemoveVolume();
  const [name, setName] = useState("");
  const [confirm, setConfirm] = useState<string | null>(null);
  const [inspect, setInspect] = useState<string | null>(null);

  if (isLoading) return <Spinner />;
  if (error) return <Alert>Could not list volumes.</Alert>;
  const volumes = data ?? [];

  return (
    <div className="space-y-3">
      {canWrite && (
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (!name.trim()) return;
            create.mutate(name.trim(), {
              onSuccess: () => {
                toast.success("Volume created", name);
                setName("");
              },
              onError: (err) =>
                toast.error("Could not create the volume", err instanceof ApiRequestError ? err.message : undefined),
            });
          }}
        >
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="ghost-content"
            className="max-w-xs"
          />
          <Button type="submit" loading={create.isPending}>
            Create volume
          </Button>
        </form>
      )}

      {volumes.length === 0 ? (
        <EmptyState title="No volumes" hint="A volume is where a container keeps data that must survive it." />
      ) : (
        <Card className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted">
                <th className="px-4 py-2 font-medium">Name</th>
                <th className="px-4 py-2 font-medium">Driver</th>
                <th className="px-4 py-2 font-medium">Owner</th>
                <th className="px-4 py-2" />
              </tr>
            </thead>
            <tbody>
              {volumes.map((v) => (
                <tr key={v.name} className="border-b border-border/60 last:border-0">
                  <td className="px-4 py-2.5 font-mono text-xs text-fg">{v.name}</td>
                  <td className="px-4 py-2.5 text-muted">{v.driver}</td>
                  <td className="px-4 py-2.5 text-xs text-muted">
                    {v.managed ? <Badge>{v.site_uid || "HeroPanel"}</Badge> : "not managed by HeroPanel"}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <Button variant="ghost" className="h-8 px-3" onClick={() => setInspect(v.name)}>
                      Inspect
                    </Button>
                    {canWrite && v.managed && (
                      <Button variant="danger" className="ml-2 h-8 px-3" onClick={() => setConfirm(v.name)}>
                        Delete
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      {confirm && (
        <Modal title={`Delete volume ${confirm}?`} onClose={() => setConfirm(null)}>
          <p className="text-sm text-muted">
            This <strong>destroys everything stored in the volume</strong>. Unlike removing a container, there is
            nothing to restart from afterwards — if a database keeps its data here, it is gone.
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setConfirm(null)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              loading={remove.isPending}
              onClick={() =>
                remove.mutate(confirm, {
                  onSuccess: () => {
                    toast.success("Volume deleted", confirm);
                    setConfirm(null);
                  },
                  onError: (err) =>
                    toast.error(
                      "Could not delete the volume",
                      err instanceof ApiRequestError ? err.message : undefined,
                    ),
                })
              }
            >
              Delete permanently
            </Button>
          </div>
        </Modal>
      )}

      {inspect && <VolumeDetailModal name={inspect} onClose={() => setInspect(null)} />}
    </div>
  );
}

// VolumeDetailModal answers the only question that makes a volume delete safe:
// who is attached to it. The consumer list includes containers the panel does
// not manage, because those are exactly the ones a careless delete would break.
function VolumeDetailModal({ name, onClose }: { name: string; onClose: () => void }) {
  const { data, isLoading, error } = useVolumeDetail(name);

  return (
    <Modal title={`Volume — ${name}`} wide onClose={onClose}>
      {isLoading ? (
        <Spinner />
      ) : error ? (
        <Alert>Could not inspect the volume.</Alert>
      ) : (
        <div className="space-y-4">
          <div>
            <h4 className="mb-1 text-sm font-medium text-fg">Consumers</h4>
            {data && data.consumers.length > 0 ? (
              <Card className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border text-left text-xs text-muted">
                      <th className="px-3 py-2 font-medium">Container</th>
                      <th className="px-3 py-2 font-medium">Image</th>
                      <th className="px-3 py-2 font-medium">State</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.consumers.map((c) => (
                      <tr key={c.id} className="border-b border-border/60 last:border-0">
                        <td className="px-3 py-2 font-mono text-xs text-fg">{c.name}</td>
                        <td className="px-3 py-2 text-muted">{c.image}</td>
                        <td className={`px-3 py-2 ${stateTone(c.state)}`}>{c.state}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </Card>
            ) : (
              <p className="text-sm text-muted">
                No container currently mounts this volume — it is safe to delete without breaking anything running.
              </p>
            )}
          </div>
          <div>
            <h4 className="mb-1 text-sm font-medium text-fg">Detail</h4>
            <pre className="max-h-[40vh] overflow-auto rounded-lg bg-surface p-3 text-xs text-muted">
              {JSON.stringify(data?.volume, null, 2)}
            </pre>
          </div>
        </div>
      )}
    </Modal>
  );
}

export function NetworksPanel({ canWrite }: { canWrite: boolean }) {
  const { data, isLoading, error } = useNetworks();
  const create = useCreateNetwork();
  const remove = useRemoveNetwork();
  const [name, setName] = useState("");
  const [inspect, setInspect] = useState<string | null>(null);

  if (isLoading) return <Spinner />;
  if (error) return <Alert>Could not list networks.</Alert>;
  const networks = data ?? [];

  return (
    <div className="space-y-3">
      {canWrite && (
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (!name.trim()) return;
            create.mutate(name.trim(), {
              onSuccess: () => {
                toast.success("Network created", name);
                setName("");
              },
              onError: (err) =>
                toast.error("Could not create the network", err instanceof ApiRequestError ? err.message : undefined),
            });
          }}
        >
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="ghost-net" className="max-w-xs" />
          <Button type="submit" loading={create.isPending}>
            Create network
          </Button>
          <span className="text-xs text-muted">Always a bridge — a container on the host network is not isolated.</span>
        </form>
      )}

      <Card className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted">
              <th className="px-4 py-2 font-medium">Name</th>
              <th className="px-4 py-2 font-medium">Driver</th>
              <th className="px-4 py-2 font-medium">Scope</th>
              <th className="px-4 py-2 font-medium">Owner</th>
              <th className="px-4 py-2" />
            </tr>
          </thead>
          <tbody>
            {networks.map((n) => (
              <tr key={n.id || n.name} className="border-b border-border/60 last:border-0">
                <td className="px-4 py-2.5 font-mono text-xs text-fg">{n.name}</td>
                <td className="px-4 py-2.5 text-muted">{n.driver}</td>
                <td className="px-4 py-2.5 text-muted">{n.scope}</td>
                <td className="px-4 py-2.5 text-xs text-muted">
                  {n.managed ? <Badge>{n.site_uid || "HeroPanel"}</Badge> : "not managed by HeroPanel"}
                </td>
                <td className="px-4 py-2.5 text-right">
                  <Button variant="ghost" className="h-8 px-3" onClick={() => setInspect(n.name)}>
                    Inspect
                  </Button>
                  {canWrite && n.managed && (
                    <Button
                      variant="danger"
                      className="ml-2 h-8 px-3"
                      onClick={() =>
                        remove.mutate(n.name, {
                          onSuccess: () => toast.success("Network deleted", n.name),
                          onError: (err) =>
                            toast.error(
                              "Could not delete the network",
                              err instanceof ApiRequestError ? err.message : undefined,
                            ),
                        })
                      }
                    >
                      Delete
                    </Button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>

      {inspect && <NetworkDetailModal name={inspect} onClose={() => setInspect(null)} />}
    </div>
  );
}

// NetworkDetailModal shows docker's inspect payload, which already carries the
// map of connected containers under `Containers` — no second lookup needed.
function NetworkDetailModal({ name, onClose }: { name: string; onClose: () => void }) {
  const { data, isLoading, error } = useNetworkDetail(name);

  return (
    <Modal title={`Network — ${name}`} wide onClose={onClose}>
      {isLoading ? (
        <Spinner />
      ) : error ? (
        <Alert>Could not inspect the network.</Alert>
      ) : (
        <pre className="max-h-[60vh] overflow-auto rounded-lg bg-surface p-3 text-xs text-muted">
          {JSON.stringify(data, null, 2)}
        </pre>
      )}
    </Modal>
  );
}
