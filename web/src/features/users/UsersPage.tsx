import { useQuery } from "@tanstack/react-query";
import { api, type UserSummary } from "@/lib/api";
import { Badge, Card, Spinner } from "@/components/ui";

export function UsersPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ["users"],
    queryFn: () => api.get<UserSummary[]>("/users"),
  });

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-fg">Users</h1>
        <p className="text-sm text-muted">Panel accounts and their status.</p>
      </div>

      {isLoading && (
        <div className="flex items-center gap-2 text-muted">
          <Spinner /> Loading…
        </div>
      )}
      {error && <p className="text-sm text-danger">You do not have permission to view users, or the request failed.</p>}

      {data && (
        <Card className="overflow-hidden">
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-muted">
              <tr>
                <th className="px-4 py-3 font-medium">User</th>
                <th className="px-4 py-3 font-medium">Email</th>
                <th className="px-4 py-3 font-medium">Status</th>
              </tr>
            </thead>
            <tbody>
              {data.map((u) => (
                <tr key={u.uid} className="border-b border-border/60 last:border-0">
                  <td className="px-4 py-3">
                    <div className="font-medium text-fg">{u.display_name || u.username}</div>
                    <div className="text-xs text-muted">@{u.username}</div>
                  </td>
                  <td className="px-4 py-3 text-muted">{u.email}</td>
                  <td className="px-4 py-3">
                    <Badge>{u.status}</Badge>
                  </td>
                </tr>
              ))}
              {data.length === 0 && (
                <tr>
                  <td colSpan={3} className="px-4 py-8 text-center text-muted">
                    No users yet.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </Card>
      )}
    </div>
  );
}
