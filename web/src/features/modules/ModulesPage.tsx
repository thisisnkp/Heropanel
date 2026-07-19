import { useQuery } from "@tanstack/react-query";
import { api, type ModuleInfo } from "@/lib/api";
import { Alert, Badge, Card, EmptyState, Spinner, StatusBadge } from "@/components/ui";

function useModules() {
  return useQuery({
    queryKey: ["modules"],
    queryFn: () => api.get<{ modules: ModuleInfo[] }>("/modules").then((r) => r.modules),
  });
}

function useCapabilities() {
  return useQuery({
    queryKey: ["capabilities"],
    queryFn: () => api.get<{ capabilities: string[] }>("/capabilities").then((r) => r.capabilities),
  });
}

// ModulesPage shows the module registry: what is enabled and which capabilities
// are currently available. Satellite modules (install/enable/disable) arrive
// with the gRPC transport in a later phase; today this reflects the in-core
// features the registry advertises.
export function ModulesPage() {
  const modules = useModules();
  const caps = useCapabilities();

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-fg">Modules</h1>
        <p className="text-sm text-muted">
          Registered modules and the capabilities they provide. Installable satellite modules land with the module
          transport in a later phase.
        </p>
      </div>

      {modules.error && <Alert>You do not have permission to view modules.</Alert>}
      {(modules.isLoading || caps.isLoading) && <Spinner />}

      {modules.data && (
        <Card className="overflow-hidden">
          {modules.data.length > 0 ? (
            <table className="w-full text-sm">
              <thead className="border-b border-border text-left text-muted">
                <tr>
                  <th className="px-4 py-3 font-medium">Module</th>
                  <th className="px-4 py-3 font-medium">State</th>
                  <th className="px-4 py-3 font-medium">Capabilities</th>
                </tr>
              </thead>
              <tbody>
                {modules.data.map((m) => (
                  <tr key={m.slug} className="border-b border-border/60 last:border-0 align-top">
                    <td className="px-4 py-3 font-medium text-fg">{m.slug}</td>
                    <td className="px-4 py-3">
                      <StatusBadge status={m.state} />
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex flex-wrap gap-1.5">
                        {m.capabilities.map((c) => (
                          <Badge key={c}>{c}</Badge>
                        ))}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <EmptyState title="No modules registered" hint="In-core features register here as the datastore is wired." />
          )}
        </Card>
      )}
    </div>
  );
}
