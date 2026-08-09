import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

// A module offered by the catalog, plus this panel's trust verdict on it.
export type CatalogEntry = {
  slug: string;
  name: string;
  version: string;
  category: string;
  description: string;
  icon: string;
  capabilities: string[];
  requires_broker: string[];
  verified: boolean;
  publisher_key?: string;
  verify_error?: string;
  installed: boolean;
  state?: string;
  /** What this panel has, where `version` is what the catalog offers. */
  installed_version?: string;
  /** Only true when Update would actually succeed: verified, same publisher, strictly newer. */
  update_available?: boolean;
};

export type Catalog = {
  modules: CatalogEntry[];
  trust_anchored: boolean;
};

export function useCatalog() {
  return useQuery({
    queryKey: ["marketplace", "catalog"],
    queryFn: () => api.get<Catalog>("/marketplace/catalog"),
  });
}

function invalidate(qc: ReturnType<typeof useQueryClient>) {
  void qc.invalidateQueries({ queryKey: ["marketplace"] });
}

export function useInstallModule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (slug: string) => api.post(`/marketplace/modules/${slug}/install`, {}),
    onSuccess: () => invalidate(qc),
  });
}

export function useUpdateModule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (slug: string) => api.post(`/marketplace/modules/${slug}/update`, {}),
    onSuccess: () => invalidate(qc),
  });
}

export function useSetModuleEnabled() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { slug: string; enabled: boolean }) =>
      api.post(`/marketplace/modules/${v.slug}/${v.enabled ? "enable" : "disable"}`, {}),
    onSuccess: () => invalidate(qc),
  });
}

export function useUninstallModule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (slug: string) => api.del(`/marketplace/modules/${slug}`),
    onSuccess: () => invalidate(qc),
  });
}
