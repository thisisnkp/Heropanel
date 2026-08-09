import { useState } from "react";
import { ApiRequestError } from "@/lib/api";
import { Button, Field, Input, Modal, Select, Textarea } from "@/components/ui";
import { toast } from "@/stores/toast";
import { parseEnv, parseMounts, parsePorts, useCreateContainer, useVolumes } from "./docker";

// The create-container form.
//
// What it does *not* offer is the design: there is no bind-address field for a
// port and no host-path field for a mount, because the server accepts neither.
// A form that collected them would be collecting inputs the broker refuses, and
// the operator would learn that only after filling the form in. The two notes
// below say why, in the places where someone would otherwise go looking for the
// missing field.
export function CreateContainerModal({ siteUID, onClose }: { siteUID?: string; onClose: () => void }) {
  const create = useCreateContainer();
  const volumes = useVolumes();

  const [name, setName] = useState("");
  const [image, setImage] = useState("");
  const [ports, setPorts] = useState("");
  const [mounts, setMounts] = useState("");
  const [env, setEnv] = useState("");
  const [restart, setRestart] = useState("unless-stopped");
  const [memory, setMemory] = useState("");

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    create.mutate(
      {
        name: name.trim(),
        image: image.trim(),
        site: siteUID,
        ports: parsePorts(ports),
        volumes: parseMounts(mounts),
        env: parseEnv(env),
        restart,
        memory_mb: memory ? Number(memory) : undefined,
      },
      {
        onSuccess: () => {
          toast.success("Container created", name);
          onClose();
        },
        onError: (err) =>
          toast.error("Could not create the container", err instanceof ApiRequestError ? err.message : undefined),
      },
    );
  };

  const known = (volumes.data ?? []).filter((v) => v.managed).map((v) => v.name);

  return (
    <Modal title="Create a container" wide onClose={onClose}>
      <form className="space-y-4" onSubmit={submit}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Name">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="ghost" required />
          </Field>
          <Field label="Image" hint="Pulled before the container starts.">
            <Input value={image} onChange={(e) => setImage(e.target.value)} placeholder="ghost:5-alpine" required />
          </Field>
        </div>

        <Field
          label="Published ports"
          hint="One per line, host:container — e.g. 2368:2368. Always bound to 127.0.0.1: Docker's firewall rules are applied ahead of the host's, so a container published on all interfaces stays reachable from the internet even when the firewall denies that port. Put the reverse proxy in front to expose it."
        >
          <Textarea value={ports} onChange={(e) => setPorts(e.target.value)} rows={2} placeholder="2368:2368" />
        </Field>

        <Field
          label="Volumes"
          hint={
            "One per line, volume-name:/path/in/container (add :ro for read-only). Named volumes only — a host path cannot be mounted, because a container holding /var/run/docker.sock or / is a complete escape to host root." +
            (known.length ? ` Available: ${known.join(", ")}.` : "")
          }
        >
          <Textarea
            value={mounts}
            onChange={(e) => setMounts(e.target.value)}
            rows={2}
            placeholder="ghost-content:/var/lib/ghost/content"
          />
        </Field>

        <Field
          label="Environment"
          hint="One KEY=value per line. Sent to Docker through stdin, never as command arguments — arguments are readable by any user on the host through /proc."
        >
          <Textarea value={env} onChange={(e) => setEnv(e.target.value)} rows={3} placeholder="NODE_ENV=production" />
        </Field>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Restart policy">
            <Select value={restart} onChange={(e) => setRestart(e.target.value)}>
              <option value="unless-stopped">unless-stopped (recommended)</option>
              <option value="on-failure">on-failure</option>
              <option value="always">always</option>
              <option value="no">no</option>
            </Select>
          </Field>
          <Field label="Memory limit (MB)" hint="Optional. Without one, a runaway container can take the host down.">
            <Input
              type="number"
              value={memory}
              onChange={(e) => setMemory(e.target.value)}
              placeholder="512"
              min={16}
            />
          </Field>
        </div>

        <div className="flex justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={create.isPending}>
            Create
          </Button>
        </div>
        {create.isPending && (
          <p className="text-right text-xs text-muted">Pulling the image first — this can take a few minutes.</p>
        )}
      </form>
    </Modal>
  );
}
