import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, rawFetch, type DNSRecord, type DNSZone } from "@/lib/api";

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

// exportZone fetches the zone as a master file and triggers a download.
export async function exportZone(zoneUid: string, zoneName: string) {
  const res = await rawFetch("GET", `/dns/zones/${zoneUid}/export`);
  const text = await res.text();
  const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = `db.${zoneName}`;
  a.click();
  URL.revokeObjectURL(url);
}

export interface ImportResult {
  imported: number;
  skipped: string[];
}

export function useImportZone(zoneUid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (zoneFile: string) => api.post<ImportResult>(`/dns/zones/${zoneUid}/import`, { zone_file: zoneFile }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["dns", "zones", zoneUid, "records"] });
      qc.invalidateQueries({ queryKey: ["dns", "zones"] });
    },
  });
}

export interface DNSSECStatus {
  zone: string;
  enabled: boolean;
  signed: boolean;
  ds: string[];
  dnskey: string[];
}

export function useDNSSEC(zoneUid: string | null) {
  return useQuery({
    queryKey: ["dns", "zones", zoneUid, "dnssec"],
    queryFn: () => api.get<DNSSECStatus>(`/dns/zones/${zoneUid}/dnssec`),
    enabled: !!zoneUid,
    // Signing runs asynchronously in BIND after enabling; poll until the DS shows.
    refetchInterval: (q) => (q.state.data && q.state.data.enabled && !q.state.data.signed ? 3000 : false),
  });
}

export function useSetDNSSEC(zoneUid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (enabled: boolean) => api.put<DNSZone>(`/dns/zones/${zoneUid}/dnssec`, { enabled }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["dns", "zones", zoneUid, "dnssec"] });
      qc.invalidateQueries({ queryKey: ["dns", "zones"] });
    },
  });
}
