import { useState } from "react";
import { Button, Modal, Select } from "@/components/ui";
import { WebTerminal } from "@/components/WebTerminal";
import type { DockerContainer } from "./docker";

// A shell inside a container.
//
// It reuses the site terminal's component outright — the wire protocol is
// identical, so the alternative was a second xterm integration that would drift
// in its copy/paste, resize and key handling. Only the endpoint differs.
//
// Two things it says out loud, because both surprise people: the shell runs as
// whatever user the *image* runs as (usually root inside the container, which is
// not root on the host), and nothing typed here is transcribed the way a site
// terminal session is.
export function ExecModal({ container, onClose }: { container: DockerContainer; onClose: () => void }) {
  const [shell, setShell] = useState("/bin/sh");
  const [reconnect, setReconnect] = useState(0);

  const ref = container.name || container.id;
  const params = new URLSearchParams({ shell });

  return (
    <Modal title={`Shell — ${container.name}`} wide onClose={onClose}>
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Select
          value={shell}
          onChange={(e) => {
            setShell(e.target.value);
            setReconnect((n) => n + 1);
          }}
          className="w-40"
        >
          <option value="/bin/sh">/bin/sh</option>
          <option value="/bin/bash">/bin/bash</option>
          <option value="/bin/ash">/bin/ash</option>
        </Select>
        <Button variant="ghost" onClick={() => setReconnect((n) => n + 1)}>
          Reconnect
        </Button>
        <span className="text-xs text-muted">
          Runs as the image&apos;s own user — often root <em>inside</em> the container, which is not root on the host.
        </span>
      </div>

      <div className="h-[60vh]">
        <WebTerminal
          uid={ref}
          endpoint={`/docker/containers/${encodeURIComponent(ref)}/exec`}
          reconnectKey={reconnect}
          key={`${ref}-${params}`}
        />
      </div>

      <p className="mt-2 text-xs text-muted">
        Opening this shell is recorded in the audit log. Unlike a site terminal, the session itself is not transcribed.
      </p>
    </Modal>
  );
}
