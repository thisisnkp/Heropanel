import { Alert, Badge, Button, Card, EmptyState, Spinner, StatusBadge } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useMe } from "@/features/auth/auth";
import { useRevokeOtherSessions, useRevokeSession, useSessions } from "./sessions";

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
