import { useQuery } from "@tanstack/react-query";
import { api, type AuditEntry } from "@/lib/api";
import { Alert, Badge, Button, Card, EmptyState, Spinner, cn } from "@/components/ui";
import { toast } from "@/stores/toast";

function useAudit() {
  return useQuery({ queryKey: ["audit"], queryFn: () => api.get<AuditEntry[]>("/audit?limit=100") });
}

const outcomeTone: Record<string, string> = {
  success: "text-emerald-500",
  failure: "text-danger",
  denied: "text-amber-500",
};

// AuditPage shows the tamper-evident log and lets an operator verify the chain.
export function AuditPage() {
  const audit = useAudit();

  const verify = async () => {
    try {
      const res = await api.get<{ intact: boolean; error?: string }>("/audit/verify");
      if (res.intact) toast.success("Audit chain intact", "Every entry hashes back to its predecessor.");
      else toast.error("Audit chain broken", res.error || "A row does not match its hash.");
    } catch {
      toast.error("Verification failed");
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-fg">Audit log</h1>
          <p className="text-sm text-muted">Hash-chained record of every mutation: who, what, from where, and the outcome.</p>
        </div>
        <Button variant="ghost" onClick={verify}>
          Verify chain
        </Button>
      </div>

      {audit.error && <Alert>You do not have permission to view the audit log.</Alert>}
      {audit.isLoading && <Spinner />}

      {audit.data && (
        <Card className="overflow-hidden">
          {audit.data.length > 0 ? (
            <table className="w-full text-sm">
              <thead className="border-b border-border text-left text-muted">
                <tr>
                  <th className="px-4 py-3 font-medium">When</th>
                  <th className="px-4 py-3 font-medium">Actor</th>
                  <th className="px-4 py-3 font-medium">Action</th>
                  <th className="px-4 py-3 font-medium">Resource</th>
                  <th className="px-4 py-3 font-medium">Outcome</th>
                </tr>
              </thead>
              <tbody>
                {audit.data.map((e) => (
                  <tr key={e.uid} className="border-b border-border/60 last:border-0 align-top">
                    <td className="whitespace-nowrap px-4 py-3 text-xs text-muted">{new Date(e.created_at).toLocaleString()}</td>
                    <td className="px-4 py-3">
                      <Badge>{e.actor_kind}</Badge>
                      <div className="mt-1 text-xs text-muted">{e.actor_ip}</div>
                    </td>
                    <td className="px-4 py-3 font-mono text-xs text-fg">{e.action}</td>
                    <td className="px-4 py-3 text-xs text-muted">
                      {e.resource_type}
                      {e.resource_id && <div className="font-mono">{e.resource_id}</div>}
                    </td>
                    <td className={cn("px-4 py-3 text-xs font-medium", outcomeTone[e.outcome] ?? "text-muted")}>
                      {e.outcome}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <EmptyState title="No audit entries yet" hint="Mutations you make will appear here." />
          )}
        </Card>
      )}
    </div>
  );
}
