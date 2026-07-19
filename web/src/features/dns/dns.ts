import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type DNSRecord, type DNSZone } from "@/lib/api";

export function useZones() {
  return useQuery({ queryKey: ["dns", "zones"], queryFn: () => api.get<DNSZone[]>("/dns/zones") });
}

export function useZoneRecords(uid: string | null) {
  return useQuery({
    queryKey: ["dns", "zones", uid, "records"],
    queryFn: () => api.get<DNSRecord[]>(`/dns/zones/${uid}/records`),
    enabled: !!uid,
  });
}

export function useCreateZone() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { name: string; primary_ns: string; admin_email: string; ns_ip: string }) =>
      api.post<DNSZone>("/dns/zones", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["dns", "zones"] }),
  });
}

export function useDeleteZone() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.del(`/dns/zones/${uid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["dns", "zones"] }),
  });
}

export function useAddRecord(zoneUid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { name: string; type: string; content: string; ttl: number; priority: number }) =>
      api.post<DNSRecord>(`/dns/zones/${zoneUid}/records`, v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["dns", "zones", zoneUid, "records"] }),
  });
}

export function useDeleteRecord(zoneUid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.del(`/dns/records/${uid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["dns", "zones", zoneUid, "records"] }),
  });
}
