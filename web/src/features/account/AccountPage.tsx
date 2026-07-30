import { useState } from "react";
import { Alert, Badge, Button, Card, EmptyState, Field, Input, Modal, Spinner, StatusBadge } from "@/components/ui";
import { toast } from "@/stores/toast";
import { can } from "@/lib/api";
import { useMe } from "@/features/auth/auth";
import { useRevokeOtherSessions, useRevokeSession, useSessions } from "./sessions";
import {
  useWebhooks,
  useCreateWebhook,
  useDeleteWebhook,
  useWebhookDeliveries,
} from "@/features/webhooks/webhooks";

export function AccountPage() {
  const me = useMe();
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-fg">Account</h1>
        <p className="text-sm text-muted">
          {me.data?.display_name ?? me.data?.username} · {me.data?.email}
        </p>
      </div>
      <SessionsCard />
      {can(me.data, "webhook.read") && <WebhooksCard canWrite={can(me.data, "webhook.write")} />}
    </div>
  );
}

function SessionsCard() {
  const sessions = useSessions();
  const revoke = useRevokeSession();
  const revokeOthers = useRevokeOtherSessions();

  if (sessions.isLoading) return <Spinner />;
  if (sessions.error) return <Alert>Could not load your sessions.</Alert>;
  const list = sessions.data?.sessions ?? [];
  const others = list.filter((s) => !s.current).length;

  return (
    <Card className="overflow-hidden">
      <div className="flex items-center justify-between border-b border-border px-4 py-3">
        <div>
          <h2 className="text-sm font-semibold text-fg">Active sessions</h2>
          <p className="text-xs text-muted">Every device currently signed in to your account.</p>
        </div>
        {others > 0 && (
          <Button
            variant="ghost"
            className="h-8 px-3"
            loading={revokeOthers.isPending}
            onClick={() =>
              revokeOthers.mutate(undefined, {
                onSuccess: (r) => toast.success(`Signed out ${r.revoked} other session(s)`),
                onError: (e) => toast.error(e.message),
              })
            }
          >
            Sign out everywhere else
          </Button>
        )}
      </div>
      {list.length === 0 ? (
        <div className="p-4">
          <EmptyState title="No active sessions" />
        </div>
      ) : (
        <div className="divide-y divide-border/60">
          {list.map((s) => (
            <div key={s.uid} className="flex items-center justify-between px-4 py-3 text-sm">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-xs text-fg">{s.ip || "unknown IP"}</span>
                  {s.current ? <StatusBadge status="active" /> : <Badge>other</Badge>}
                </div>
                <div className="truncate text-xs text-muted" title={s.user_agent}>
                  {s.user_agent || "unknown device"} · since {s.created_at}
                </div>
              </div>
              {!s.current && (
                <Button
                  variant="ghost"
                  className="h-7 px-2 text-danger"
                  onClick={() =>
                    revoke.mutate(s.uid, {
                      onSuccess: () => toast.success("Session revoked"),
                      onError: (e) => toast.error(e.message),
                    })
                  }
                >
                  Revoke
                </Button>
              )}
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

// ── webhooks ─────────────────────────────────────────────────────────────────

function WebhooksCard({ canWrite }: { canWrite: boolean }) {
  const { data, isLoading } = useWebhooks();
  const del = useDeleteWebhook();
  const [creating, setCreating] = useState(false);
  const [secret, setSecret] = useState<{ url: string; secret: string } | null>(null);
  const [openDeliveries, setOpenDeliveries] = useState<string | null>(null);
  const hooks = data?.webhooks ?? [];

  return (
    <Card className="overflow-hidden">
      <div className="flex items-center justify-between border-b border-border px-4 py-3">
        <div>
          <h2 className="text-sm font-semibold text-fg">Webhooks</h2>
          <p className="text-xs text-muted">Receive HMAC-signed HTTP callbacks when things happen.</p>
        </div>
        {canWrite && (
          <Button className="h-8 px-3" onClick={() => setCreating(true)}>
            Add webhook
          </Button>
        )}
      </div>
      {isLoading ? (
        <div className="p-4">
          <Spinner />
        </div>
      ) : hooks.length === 0 ? (
        <EmptyState title="No webhooks" hint="Add one to start receiving events." />
      ) : (
        <ul className="divide-y divide-border/60">
          {hooks.map((h) => (
            <li key={h.uid} className="px-4 py-3">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="truncate font-mono text-xs text-fg">{h.url}</div>
                  <div className="mt-1 flex flex-wrap gap-1">
                    {h.events.map((e) => (
                      <Badge key={e}>{e}</Badge>
                    ))}
                    <StatusBadge status={h.active ? "active" : "disabled"} />
                  </div>
                </div>
                <div className="flex shrink-0 gap-2">
                  <Button variant="ghost" className="h-7 px-2" onClick={() => setOpenDeliveries(openDeliveries === h.uid ? null : h.uid)}>
                    Deliveries
                  </Button>
                  {canWrite && (
                    <Button
                      variant="ghost"
                      className="h-7 px-2 text-danger"
                      onClick={() => {
                        if (!confirm("Delete this webhook? Its delivery history is removed too.")) return;
                        del.mutate(h.uid, { onError: (e) => toast.error(e.message) });
                      }}
                    >
                      Delete
                    </Button>
                  )}
                </div>
              </div>
              {openDeliveries === h.uid && <DeliveriesList uid={h.uid} />}
            </li>
          ))}
        </ul>
      )}
      {creating && (
        <CreateWebhookModal
          onClose={() => setCreating(false)}
          onCreated={(url, s) => {
            setCreating(false);
            setSecret({ url, secret: s });
          }}
        />
      )}
      {secret && <SecretModal url={secret.url} secret={secret.secret} onClose={() => setSecret(null)} />}
    </Card>
  );
}

function DeliveriesList({ uid }: { uid: string }) {
  const { data, isLoading } = useWebhookDeliveries(uid, true);
  const deliveries = data?.deliveries ?? [];
  if (isLoading) return <div className="mt-3"><Spinner /></div>;
  if (deliveries.length === 0) return <p className="mt-3 text-xs text-muted">No deliveries yet.</p>;
  return (
    <table className="mt-3 w-full text-xs">
      <thead className="text-left text-muted">
        <tr>
          <th className="py-1 font-medium">Event</th>
          <th className="py-1 font-medium">Status</th>
          <th className="py-1 font-medium">Code</th>
          <th className="py-1 font-medium">Attempts</th>
        </tr>
      </thead>
      <tbody>
        {deliveries.map((d) => (
          <tr key={d.uid} className="border-t border-border/40">
            <td className="py-1 font-mono">{d.event}</td>
            <td className="py-1">
              <StatusBadge status={d.status} />
            </td>
            <td className="py-1">{d.response_code || "—"}</td>
            <td className="py-1">{d.attempts}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function CreateWebhookModal({ onClose, onCreated }: { onClose: () => void; onCreated: (url: string, secret: string) => void }) {
  const create = useCreateWebhook();
  const [url, setUrl] = useState("");
  const [events, setEvents] = useState("*");
  return (
    <Modal title="Add webhook" onClose={onClose}>
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          const evList = events
            .split(",")
            .map((s) => s.trim())
            .filter(Boolean);
          create.mutate(
            { url: url.trim(), events: evList.length ? evList : ["*"] },
            {
              onSuccess: (w) => onCreated(w.url, w.secret),
              onError: (err) => toast.error(err.message),
            },
          );
        }}
      >
        <Field label="Endpoint URL">
          <Input value={url} onChange={(e) => setUrl(e.target.value)} type="url" placeholder="https://example.com/hook" required />
        </Field>
        <Field label="Events" hint='Comma-separated resource types, or "*" for all (e.g. sites, dns, ssl).'>
          <Input value={events} onChange={(e) => setEvents(e.target.value)} />
        </Field>
        <div className="flex justify-end gap-2 pt-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={create.isPending}>
            Create
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function SecretModal({ url, secret, onClose }: { url: string; secret: string; onClose: () => void }) {
  return (
    <Modal title="Signing secret" onClose={onClose}>
      <div className="space-y-3">
        <Alert>
          This secret is shown once. Store it now — it signs every delivery to <span className="font-mono">{url}</span> and cannot be
          retrieved again.
        </Alert>
        <div className="break-all rounded-md border border-border/60 bg-surface p-3 font-mono text-xs text-fg">{secret}</div>
        <p className="text-xs text-muted">
          Verify a delivery: <span className="font-mono">X-HeroPanel-Signature</span> = "sha256=" + HMAC-SHA256(secret,{" "}
          <span className="font-mono">X-HeroPanel-Timestamp</span> + "." + body).
        </p>
        <div className="flex justify-end gap-2">
          <Button
            variant="ghost"
            onClick={() => {
              void navigator.clipboard?.writeText(secret);
              toast.success("Secret copied");
            }}
          >
            Copy
          </Button>
          <Button onClick={onClose}>Done</Button>
        </div>
      </div>
    </Modal>
  );
}
