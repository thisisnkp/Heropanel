import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type ParkedDomain } from "@/lib/api";

// Data hooks for the parked-domain registry: park a domain with no site, prove
// ownership via a DNS TXT challenge, and see what's free to pick when creating
// a site. Mirrors the per-site data-hook conventions in features/sites/site-detail.ts.

const parkedKey = ["domains", "parked"] as const;

export function useParkedDomains() {
  return useQuery({ queryKey: parkedKey, queryFn: () => api.get<ParkedDomain[]>("/domains/parked") });
}

export function usePark() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (fqdn: string) => api.post<ParkedDomain>("/domains/parked", { fqdn }),
    onSuccess: () => qc.invalidateQueries({ queryKey: parkedKey }),
  });
}

export function useVerifyParked() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.post<ParkedDomain>(`/domains/parked/${uid}/verify`),
    onSuccess: () => qc.invalidateQueries({ queryKey: parkedKey }),
  });
}

export function useDeleteParked() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.del(`/domains/parked/${uid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: parkedKey }),
  });
}

// DomainPool is what the create-site form needs to explain a typed domain.
// `fqdns` is what can be taken outright; `trusted` is every ownership-proven
// domain including ones already serving a site, which is what makes
// "blog.acme.com" recognisable as needing no verification when acme.com is
// already in use.
export interface DomainPool {
  fqdns: string[];
  trusted: string[];
}

// useFreeDomains lists domains available to pick when creating a site: verified
// parked domains and panel-hosted DNS zones not already attached to one.
export function useFreeDomains() {
  return useQuery({
    queryKey: ["domains", "free"],
    queryFn: () => api.get<DomainPool>("/domains/free"),
    staleTime: 30_000,
  });
}
