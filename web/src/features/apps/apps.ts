import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

// Data hooks for the one-click Apps catalog.
//
// An app is a labelled compose stack, so these all reduce to Docker operations
// on the server — nothing here is a new kind of privilege. The one thing worth
// knowing about the shapes: a deploy returns any generated secrets exactly once
// (they are not stored in a form the panel can return later), so the caller must
// show them at that moment and not expect to fetch them again.

export interface AppField {
  key: string;
  label: string;
  placeholder?: string;
  secret: boolean;
  required: boolean;
  help?: string;
}

export interface AppTemplate {
  slug: string;
  name: string;
  description: string;
  category: string;
  icon: string;
  min_memory_mb: number;
  http_port: number;
  fields: AppField[] | null;
  /** Whether the host has the memory this template needs. */
  feasible: boolean;
  available_mb: number;
}

export interface DeployResult {
  project: string;
  port: number;
  /** Generated secrets, shown once. Empty for templates with no secret fields. */
  secrets: Record<string, string>;
}

export interface AppService {
  name: string;
  service: string;
  image: string;
  state: string;
  status: string;
  ports: string;
}

export function useAppTemplates() {
  return useQuery({
    queryKey: ["apps", "templates"],
    queryFn: () => api.get<AppTemplate[]>("/apps/templates"),
  });
}

export function useDeployApp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { slug: string; name: string; site?: string; values: Record<string, string> }) =>
      api.post<DeployResult>("/apps", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["docker"] }),
  });
}

export function useAppStatus(project: string) {
  return useQuery({
    queryKey: ["apps", "status", project],
    queryFn: () => api.get<AppService[]>(`/apps/${encodeURIComponent(project)}`),
    enabled: !!project,
    refetchInterval: 5_000,
  });
}

export function useRemoveApp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (project: string) => api.del(`/apps/${encodeURIComponent(project)}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["docker"] }),
  });
}

// AppExposure reports whether an app is fronted by a domain. When it is, the app
// is a real proxy site, managed from then on through the Sites page.
export interface AppExposure {
  exposed: boolean;
  domain?: string;
  site_uid?: string;
  status?: string;
}

export function useAppExposure(project: string) {
  return useQuery({
    queryKey: ["apps", "exposure", project],
    queryFn: () => api.get<AppExposure>(`/apps/${encodeURIComponent(project)}/exposure`),
    enabled: !!project,
  });
}

// useExposeApp fronts a deployed app with a domain. On the server this creates a
// proxy site whose vhost reverse-proxies to the app's live loopback port, so the
// app is reachable from the internet without exposing the container directly.
export function useExposeApp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ project, domain }: { project: string; domain: string }) =>
      api.post(`/apps/${encodeURIComponent(project)}/expose`, { domain }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["apps"] });
      qc.invalidateQueries({ queryKey: ["sites"] });
    },
  });
}

export function useUnexposeApp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (project: string) => api.del(`/apps/${encodeURIComponent(project)}/expose`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["apps"] });
      qc.invalidateQueries({ queryKey: ["sites"] });
    },
  });
}
