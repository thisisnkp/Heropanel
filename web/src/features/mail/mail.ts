import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

// Types mirror the API views (internal/mail). Passwords are write-only and
// never appear here.

export type MailDomain = {
  uid: string;
  domain: string;
  dkim_selector: string;
  dkim_public?: string;
  status: string;
  created_at: string;
};

export type Mailbox = {
  uid: string;
  address: string;
  local_part: string;
  quota_mb: number;
  status: string;
  created_at: string;
};

export type MailAlias = {
  uid: string;
  source: string;
  destination: string;
  created_at: string;
};

export type MailDNSCheck = {
  label: string;
  type: string;
  value: string;
  priority?: number;
  found: boolean;
  observed?: string[];
};

export type MailUsage = { known: boolean; used_kb: number; limit_kb: number };

export type QueueMessage = {
  id: string;
  queue: string;
  sender: string;
  size_bytes: number;
  arrival: string;
  recipients: { address: string; delay_reason?: string }[];
};

export function useMailDomains() {
  return useQuery({
    queryKey: ["mail", "domains"],
    queryFn: () => api.get<{ domains: MailDomain[]; available: boolean }>("/mail/domains"),
  });
}

export function useMailDomain(uid: string | null) {
  return useQuery({
    queryKey: ["mail", "domains", uid],
    queryFn: () =>
      api.get<{ domain: MailDomain; accounts: Mailbox[]; aliases: MailAlias[] }>(`/mail/domains/${uid}`),
    enabled: !!uid,
  });
}

export function useCreateMailDomain() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { domain: string }) => api.post<MailDomain>("/mail/domains", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["mail", "domains"] }),
  });
}

export function useDeleteMailDomain() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { uid: string; purge: boolean }) =>
      api.del(`/mail/domains/${v.uid}?purge=${v.purge}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["mail", "domains"] }),
  });
}

export function useMailDNS(uid: string | null) {
  return useQuery({
    queryKey: ["mail", "domains", uid, "dns"],
    queryFn: () => api.get<{ records: MailDNSCheck[] }>(`/mail/domains/${uid}/dns`),
    enabled: !!uid,
  });
}

export function useMailUsage(uid: string | null) {
  return useQuery({
    queryKey: ["mail", "domains", uid, "usage"],
    queryFn: () => api.get<{ usage: Record<string, MailUsage> }>(`/mail/domains/${uid}/usage`),
    enabled: !!uid,
  });
}

export function useCreateMailbox(domainUid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { local_part: string; password: string; quota_mb?: number }) =>
      api.post<Mailbox>(`/mail/domains/${domainUid}/accounts`, v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["mail", "domains", domainUid] }),
  });
}

export function useDeleteMailbox(domainUid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { uid: string; purge: boolean }) =>
      api.del(`/mail/domains/${domainUid}/accounts/${v.uid}?purge=${v.purge}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["mail", "domains", domainUid] }),
  });
}

export function useSetMailboxPassword() {
  return useMutation({
    mutationFn: (v: { uid: string; password: string }) =>
      api.put(`/mail/accounts/${v.uid}/password`, { password: v.password }),
  });
}

export function useSetMailboxQuota(domainUid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { uid: string; quota_mb: number }) =>
      api.put(`/mail/accounts/${v.uid}/quota`, { quota_mb: v.quota_mb }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["mail", "domains", domainUid] }),
  });
}

export function useSetMailboxStatus(domainUid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { uid: string; status: "active" | "suspended" }) =>
      api.put(`/mail/accounts/${v.uid}/status`, { status: v.status }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["mail", "domains", domainUid] }),
  });
}

export function useCreateMailAlias(domainUid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { source: string; destination: string }) =>
      api.post<MailAlias>(`/mail/domains/${domainUid}/aliases`, v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["mail", "domains", domainUid] }),
  });
}

export function useDeleteMailAlias(domainUid: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.del(`/mail/domains/${domainUid}/aliases/${uid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["mail", "domains", domainUid] }),
  });
}

export function useMailQueue() {
  return useQuery({
    queryKey: ["mail", "queue"],
    queryFn: () => api.get<{ messages: QueueMessage[]; running: boolean }>("/mail/queue"),
    refetchInterval: 15000,
  });
}

export function useFlushMailQueue() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post("/mail/queue/flush", {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["mail", "queue"] }),
  });
}

export function useDeleteQueued() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ids: string[]) => api.post("/mail/queue/delete", { ids }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["mail", "queue"] }),
  });
}

export function fmtKB(kb: number): string {
  if (kb >= 1024 * 1024) return `${(kb / 1024 / 1024).toFixed(1)} GB`;
  if (kb >= 1024) return `${(kb / 1024).toFixed(1)} MB`;
  return `${kb} KB`;
}

export function fmtBytes(b: number): string {
  if (b >= 1024 * 1024) return `${(b / 1024 / 1024).toFixed(1)} MB`;
  if (b >= 1024) return `${(b / 1024).toFixed(1)} KB`;
  return `${b} B`;
}
