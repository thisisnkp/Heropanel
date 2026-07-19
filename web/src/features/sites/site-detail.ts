import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  api,
  type Deployment,
  type Domain,
  type GitSource,
  type Job,
  type PHPInfo,
  type Runtime,
  type RuntimeHealth,
  type Site,
  type SiteLimits,
  type SiteLog,
} from "@/lib/api";

// Data hooks for the per-site workspace. Each facet (domains, PHP, runtime, git,
// logs, limits) is its own query keyed by the site uid, so a change to one tab
// invalidates only that tab.

export function useSite(uid: string) {
  return useQuery({ queryKey: ["site", uid], queryFn: () => api.get<Site>(`/sites/${uid}`) });
}

// ── domains ─────────────────────────────────────────────────────────────────

export function useDomains(uid: string) {
  return useQuery({ queryKey: ["site", uid, "domains"], queryFn: () => api.get<Domain[]>(`/sites/${uid}/domains`) });
}

export function useAddDomain(uid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { fqdn: string; kind: string; redirect_to?: string; redirect_code?: number }) =>
      api.post<Domain>(`/sites/${uid}/domains`, v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["site", uid, "domains"] }),
  });
}

export function useDeleteDomain(uid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (did: string) => api.del(`/sites/${uid}/domains/${did}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["site", uid, "domains"] }),
  });
}

export function useSetForceHTTPS(uid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { enabled: boolean }) => api.put(`/sites/${uid}/force-https`, v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["site", uid, "domains"] }),
  });
}

// ── PHP ─────────────────────────────────────────────────────────────────────

export function usePHP(uid: string, enabled: boolean) {
  return useQuery({
    queryKey: ["site", uid, "php"],
    queryFn: () => api.get<PHPInfo>(`/sites/${uid}/php`),
    enabled,
  });
}

export function useSetPHP(uid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: Partial<PHPInfo>) => api.put<PHPInfo>(`/sites/${uid}/php`, v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["site", uid, "php"] }),
  });
}

// ── runtime (proxy sites) ────────────────────────────────────────────────────

export function useRuntime(uid: string, enabled: boolean) {
  return useQuery({
    queryKey: ["site", uid, "runtime"],
    queryFn: () => api.get<Runtime>(`/sites/${uid}/runtime`).catch(() => null),
    enabled,
  });
}

export function useRuntimeHealth(uid: string, enabled: boolean) {
  return useQuery({
    queryKey: ["site", uid, "runtime", "health"],
    queryFn: () => api.get<RuntimeHealth>(`/sites/${uid}/runtime/health`),
    enabled,
    refetchInterval: 10000,
  });
}

export function useSetRuntime(uid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: Partial<Runtime>) => api.put<Runtime>(`/sites/${uid}/runtime`, v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["site", uid, "runtime"] }),
  });
}

export function useRuntimeControl(uid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (action: "start" | "stop" | "restart") => api.post<Runtime>(`/sites/${uid}/runtime/${action}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["site", uid, "runtime"] }),
  });
}

// ── git ──────────────────────────────────────────────────────────────────────

export function useGitSource(uid: string) {
  return useQuery({
    queryKey: ["site", uid, "git"],
    queryFn: () => api.get<GitSource>(`/sites/${uid}/git`).catch(() => null),
  });
}

export function useDeployments(uid: string) {
  return useQuery({
    queryKey: ["site", uid, "git", "deployments"],
    queryFn: () => api.get<Deployment[]>(`/sites/${uid}/git/deployments`),
  });
}

export function useSetGitSource(uid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: Record<string, unknown>) => api.put<GitSource>(`/sites/${uid}/git`, v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["site", uid, "git"] }),
  });
}

export function useDeploy(uid: string) {
  return useMutation({ mutationFn: () => api.post<{ job?: Job } & Deployment>(`/sites/${uid}/git/deploy`) });
}

export function useRollback(uid: string) {
  return useMutation({
    mutationFn: (dep: string) => api.post<{ job?: Job } & Deployment>(`/sites/${uid}/git/rollback/${dep}`),
  });
}

// ── logs ─────────────────────────────────────────────────────────────────────

export function useSiteLogs(uid: string, kind: string, enabled: boolean) {
  return useQuery({
    queryKey: ["site", uid, "logs", kind],
    queryFn: () => api.get<SiteLog>(`/sites/${uid}/logs?kind=${kind}&lines=200`),
    enabled,
    refetchInterval: 5000,
  });
}

// ── limits ───────────────────────────────────────────────────────────────────

export function useLimits(uid: string) {
  return useQuery({ queryKey: ["site", uid, "limits"], queryFn: () => api.get<SiteLimits>(`/sites/${uid}/limits`) });
}

export function useSetLimits(uid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: SiteLimits) => api.put<SiteLimits>(`/sites/${uid}/limits`, v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["site", uid, "limits"] }),
  });
}

// ── lifecycle ────────────────────────────────────────────────────────────────

export function useSuspend(uid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (suspend: boolean) => api.post<Site>(`/sites/${uid}/${suspend ? "suspend" : "resume"}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["site", uid] }),
  });
}

export function useClone(uid: string) {
  return useMutation({
    mutationFn: (v: { name: string; primary_domain: string }) =>
      api.post<{ job?: Job } & Site>(`/sites/${uid}/clone`, v),
  });
}
