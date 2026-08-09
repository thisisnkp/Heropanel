import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type FirewallRule = {
  uid: string;
  position: number;
  action: string;
  protocol: string;
  port: number;
  port_end: number;
  source: string;
  comment: string;
};

export type FirewallList = {
  rules: FirewallRule[];
  pending: boolean;
  deadline: string;
  available: boolean;
};

export type ApplyResult = { token: string; deadline: string; window_sec: number };

export function useFirewall() {
  return useQuery({
    queryKey: ["security", "firewall"],
    queryFn: () => api.get<FirewallList>("/firewall"),
    // While a change is pending, poll so the countdown and the auto-revert
    // outcome are reflected without a manual refresh.
    refetchInterval: (q) => (q.state.data?.pending ? 2000 : false),
  });
}

export function useAddFirewallRule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { action: string; protocol: string; port: number; port_end: number; source: string; comment: string }) =>
      api.post<FirewallRule>("/firewall/rules", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["security", "firewall"] }),
  });
}

export type FIMStatus = { baseline: boolean; tool_present: boolean; scope: string };
export type FIMResult = {
  changed: boolean;
  added: number;
  removed: number;
  changed_count: number;
  scope: string;
  report: string;
};

export function useFIM() {
  return useQuery({
    queryKey: ["security", "fim"],
    queryFn: () => api.get<{ status: FIMStatus; available: boolean }>("/security/fim"),
  });
}

export function useFIMInit() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (scope: string) => api.post("/security/fim/init", { scope }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["security", "fim"] }),
  });
}

export function useFIMCheck() {
  return useMutation({
    mutationFn: () => api.post<FIMResult>("/security/fim/check", {}),
  });
}

export type AuditResult = {
  tool: string;
  warnings: number;
  suggestions?: number;
  hardening_index?: number;
  report: string;
};

export function useAuditScan() {
  return useMutation({
    mutationFn: (tool: string) => api.post<AuditResult>(`/security/audit/${tool}`, {}),
  });
}

export type IPListEntry = { uid: string; cidr: string; mode: string; comment: string; country: string };

export function useFirewallIPList() {
  return useQuery({
    queryKey: ["security", "firewall", "iplist"],
    queryFn: () => api.get<{ entries: IPListEntry[] }>("/firewall/iplist"),
  });
}

export type CountrySummary = { country: string; mode: string; count: number };

export function useFirewallCountries() {
  return useQuery({
    queryKey: ["security", "firewall", "countries"],
    queryFn: () => api.get<{ countries: CountrySummary[]; available: boolean }>("/firewall/countries"),
  });
}

export function useImportFirewallCountry() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { country: string; mode: string }) =>
      api.post<{ country: string; mode: string; count: number }>("/firewall/countries", v),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["security", "firewall", "countries"] });
      qc.invalidateQueries({ queryKey: ["security", "firewall", "iplist"] });
    },
  });
}

export function useRemoveFirewallCountry() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (cc: string) => api.del(`/firewall/countries/${cc}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["security", "firewall", "countries"] });
      qc.invalidateQueries({ queryKey: ["security", "firewall", "iplist"] });
    },
  });
}

export function useAddFirewallIPEntry() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { cidr: string; mode: string; comment: string }) =>
      api.post<IPListEntry>("/firewall/iplist", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["security", "firewall", "iplist"] }),
  });
}

export function useDeleteFirewallIPEntry() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.del(`/firewall/iplist/${uid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["security", "firewall", "iplist"] }),
  });
}

export function useDeleteFirewallRule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.del(`/firewall/rules/${uid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["security", "firewall"] }),
  });
}

export function useApplyFirewall() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<ApplyResult>("/firewall/apply", {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["security", "firewall"] }),
  });
}

export function useConfirmFirewall() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (token: string) => api.post("/firewall/confirm", { token }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["security", "firewall"] }),
  });
}

export function useRollbackFirewall() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post("/firewall/rollback", {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["security", "firewall"] }),
  });
}

// ── malware ──────────────────────────────────────────────────────────────────

export type Finding = { path: string; signature: string };
export type ScanResult = { scan: { uid: string; infected: number }; findings: Finding[] };
export type QuarantineItem = {
  uid: string;
  site_uid: string;
  original_path: string;
  signature: string;
  status: string;
  created_at: string;
};

export function useQuarantine() {
  return useQuery({
    queryKey: ["security", "quarantine"],
    queryFn: () => api.get<{ items: QuarantineItem[]; available: boolean }>("/security/quarantine"),
  });
}

export function useScanSite() {
  return useMutation({
    mutationFn: (siteUid: string) => api.post<ScanResult>(`/sites/${siteUid}/scan`, {}),
  });
}

export function useQuarantineFile() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { site_uid: string; path: string; signature: string }) =>
      api.post<QuarantineItem>("/security/quarantine", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["security", "quarantine"] }),
  });
}

export function useRestoreQuarantine() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.post(`/security/quarantine/${uid}/restore`, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["security", "quarantine"] }),
  });
}

export function useDeleteQuarantine() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.del(`/security/quarantine/${uid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["security", "quarantine"] }),
  });
}

// ── passkeys (WebAuthn) ──────────────────────────────────────────────────────

export type Passkey = { uid: string; name: string; created_at: string };

export function usePasskeys() {
  return useQuery({
    queryKey: ["account", "passkeys"],
    queryFn: () => api.get<{ passkeys: Passkey[]; enabled: boolean }>("/account/passkeys"),
  });
}

export function useRegisterPasskey() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (name: string) => {
      const { registerPasskey } = await import("@/lib/webauthn");
      return registerPasskey(name);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["account", "passkeys"] }),
  });
}

export function useDeletePasskey() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.del(`/account/passkeys/${uid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["account", "passkeys"] }),
  });
}

// ── fail2ban ─────────────────────────────────────────────────────────────────

export type Jail = { name: string; banned: string[] };

export function useFail2Ban() {
  return useQuery({
    queryKey: ["security", "fail2ban"],
    queryFn: () => api.get<{ jails: Jail[]; running: boolean; available: boolean }>("/security/fail2ban"),
  });
}

export type SSHOptions = {
  port: number;
  permit_root_login: string;
  password_authentication: boolean;
  pubkey_authentication: boolean;
  max_auth_tries: number;
};

export function useSSH() {
  return useQuery({
    queryKey: ["security", "ssh"],
    queryFn: () =>
      api.get<{ effective: Record<string, string>; available: boolean }>("/security/ssh"),
  });
}

export function useHardenSSH() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: Partial<SSHOptions>) => api.post("/security/ssh", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["security", "ssh"] }),
  });
}

export type UpdatesOptions = {
  enabled: boolean;
  security_only: boolean;
  automatic_reboot: boolean;
  reboot_time: string;
};

export function useUpdates() {
  return useQuery({
    queryKey: ["security", "updates"],
    queryFn: () =>
      api.get<{ status: Record<string, string>; available: boolean }>("/security/updates"),
  });
}

export function useConfigureUpdates() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: Partial<UpdatesOptions>) => api.post("/security/updates", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["security", "updates"] }),
  });
}

export function useUnban() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { jail: string; ip: string }) => api.post("/security/fail2ban/unban", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["security", "fail2ban"] }),
  });
}

export function useBan() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { jail: string; ip: string }) => api.post("/security/fail2ban/ban", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["security", "fail2ban"] }),
  });
}
