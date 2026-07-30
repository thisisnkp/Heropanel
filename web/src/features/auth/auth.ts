import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type AuthStatus, type Principal } from "@/lib/api";

// useMe resolves the current principal. A 401 resolves to null (not an error)
// so the UI can render the login screen.
export function useMe() {
  return useQuery({
    queryKey: ["me"],
    queryFn: async (): Promise<Principal | null> => {
      try {
        return await api.get<Principal>("/auth/me");
      } catch (e: unknown) {
        if (e && typeof e === "object" && "status" in e && (e as { status: number }).status === 401) {
          return null;
        }
        throw e;
      }
    },
  });
}

export function useAuthStatus() {
  return useQuery({
    queryKey: ["auth-status"],
    queryFn: () => api.get<AuthStatus>("/auth/status"),
  });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { email: string; password: string }) => api.post<Principal>("/auth/login", v),
    onSuccess: (principal) => {
      qc.setQueryData(["me"], principal);
      qc.invalidateQueries({ queryKey: ["auth-status"] });
    },
  });
}

export function useBootstrap() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { email: string; username: string; password: string }) =>
      api.post<Principal>("/auth/bootstrap", v),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["auth-status"] });
    },
  });
}

// useStopImpersonation ends the current impersonation and restores the
// administrator's own session (swapped server-side). We reset the cache and
// reseat the returned admin principal so the app returns to the admin's view.
export function useStopImpersonation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<Principal>("/auth/impersonation/stop"),
    onSuccess: (admin) => {
      qc.clear();
      qc.setQueryData(["me"], admin);
    },
  });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post("/auth/logout"),
    onSuccess: () => {
      qc.setQueryData(["me"], null);
      qc.clear();
    },
  });
}
