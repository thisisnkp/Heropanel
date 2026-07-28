import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

// A user's own active sessions (one per login/device). No token is ever
// returned; the current session is flagged so the UI never revokes the branch
// the user is sitting on by accident.
export type Session = {
  uid: string;
  ip: string;
  user_agent: string;
  created_at: string;
  expires_at: string;
  current: boolean;
};

export function useSessions() {
  return useQuery({
    queryKey: ["account", "sessions"],
    queryFn: () => api.get<{ sessions: Session[] }>("/auth/sessions"),
  });
}

export function useRevokeSession() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.del(`/auth/sessions/${uid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["account", "sessions"] }),
  });
}

export function useRevokeOtherSessions() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<{ revoked: number }>("/auth/sessions/revoke-others", {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["account", "sessions"] }),
  });
}
