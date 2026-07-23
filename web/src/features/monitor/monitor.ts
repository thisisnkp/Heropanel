import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { wsSubscribe } from "@/lib/ws";

// Monitoring data hooks.
//
// The dashboard does NOT poll. It fetches one sample for the initial paint, then
// subscribes to the `monitor:node` channel and lets the server push updates. The
// server samples only while at least one client is subscribed (subscription-gated
// sampling), so an open-but-unwatched tab costs nothing on the host.

export interface DiskUsage {
  path: string;
  total_bytes: number;
  used_bytes: number;
  used_percent: number;
}

export interface NodeSample {
  cpu_percent: number;
  load1: number;
  load5: number;
  load15: number;
  mem_total_kb: number;
  mem_used_kb: number;
  mem_available_kb: number;
  swap_total_kb: number;
  swap_used_kb: number;
  uptime_sec: number;
  disks: DiskUsage[] | null;
}

// useNodeMetrics returns the latest node sample and whether it is currently
// live (receiving pushes). The one-shot fetch seeds the first paint; the WS
// subscription then drives every update after.
export function useNodeMetrics() {
  const initial = useQuery({
    queryKey: ["monitor", "node"],
    queryFn: () => api.get<NodeSample>("/monitor/node"),
    // No refetchInterval: after the first load the socket owns updates.
    staleTime: Infinity,
  });

  const [live, setLive] = useState<NodeSample | null>(null);
  const [streaming, setStreaming] = useState(false);

  useEffect(() => {
    const off = wsSubscribe("monitor:node", (data) => {
      setLive(data as NodeSample);
      setStreaming(true);
    });
    return () => {
      off();
      setStreaming(false);
    };
  }, []);

  return {
    sample: live ?? initial.data ?? null,
    streaming,
    isLoading: initial.isLoading && !live,
    error: initial.error,
  };
}

export interface SiteSample {
  vhost: string;
  site_uid: string;
  mem_current_bytes: number;
  cpu_percent: number;
  tasks: number;
  present: boolean;
}

export interface ServiceHealth {
  service: string;
  state: string; // active | inactive | failed | unknown
}

// useSiteMetrics returns live per-site usage, seeded by a one-shot read then
// driven by the `monitor:sites` channel.
export function useSiteMetrics() {
  return useChannel<SiteSample>("monitor:sites", "/monitor/sites", "sites");
}

// useServiceHealth returns live service up/down state.
export function useServiceHealth() {
  return useChannel<ServiceHealth>("monitor:services", "/monitor/services", "services");
}

// useChannel is the shared pattern behind the site and service lists: fetch one
// snapshot for the first paint, then let the hub push the rest. `field` is the
// key the payload wraps its array in.
function useChannel<T>(channel: string, path: string, field: string) {
  const initial = useQuery({
    queryKey: ["monitor", field],
    queryFn: () => api.get<Record<string, T[]>>(path),
    staleTime: Infinity,
  });
  const [live, setLive] = useState<T[] | null>(null);

  useEffect(() => {
    const off = wsSubscribe(channel, (data) => {
      const rows = (data as Record<string, T[]>)?.[field];
      if (Array.isArray(rows)) setLive(rows);
    });
    return off;
  }, [channel, field]);

  return {
    rows: live ?? initial.data?.[field] ?? [],
    isLoading: initial.isLoading && !live,
    error: initial.error,
  };
}

export interface HistPoint {
  ts: string;
  cpu_percent: number;
  mem_used_kb: number;
  mem_total_kb: number;
  swap_used_kb: number;
  load1: number;
  root_disk_pct: number;
}

export type HistoryRange = "1h" | "6h" | "24h" | "7d" | "30d";

// useHistory fetches node history for a range. Unlike the live tiles this is a
// plain query — history is at rest, so there is nothing to push; the range
// selector refetches.
export function useHistory(range: HistoryRange) {
  return useQuery({
    queryKey: ["monitor", "history", range],
    queryFn: () => api.get<{ points: HistPoint[] }>(`/monitor/history?range=${range}`),
  });
}

export interface AlertRule {
  uid: string;
  name: string;
  metric: string;
  op: string;
  threshold: number;
  for_sec: number;
  enabled: boolean;
  notify_kind: string;
}

export interface AlertEvent {
  rule_uid: string;
  state: string;
  value: number;
  at: string;
}

export interface AlertRuleInput {
  name: string;
  metric: string;
  op: string;
  threshold: number;
  for_sec: number;
  notify_kind: string;
  notify_target: { webhook_url?: string; telegram_token?: string; telegram_chat?: string };
}

export function useAlertRules() {
  return useQuery({ queryKey: ["monitor", "rules"], queryFn: () => api.get<AlertRule[]>("/monitor/alerts/rules") });
}

export function useAlertEvents() {
  return useQuery({
    queryKey: ["monitor", "events"],
    queryFn: () => api.get<AlertEvent[]>("/monitor/alerts/events"),
    refetchInterval: 15_000,
  });
}

export function useCreateRule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (in_: AlertRuleInput) => api.post<AlertRule>("/monitor/alerts/rules", in_),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["monitor", "rules"] }),
  });
}

export function useToggleRule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ uid, enabled }: { uid: string; enabled: boolean }) =>
      api.put(`/monitor/alerts/rules/${uid}`, { enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["monitor", "rules"] }),
  });
}

export function useDeleteRule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.del(`/monitor/alerts/rules/${uid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["monitor", "rules"] }),
  });
}

// ── formatting helpers ───────────────────────────────────────────────────────

/** fmtBytes renders a byte count as a compact human size. */
export function fmtBytes(n: number): string {
  if (n <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  const v = n / 1024 ** i;
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

/** fmtKB renders a kilobyte count (the unit /proc/meminfo reports in). */
export function fmtKB(kb: number): string {
  return fmtBytes(kb * 1024);
}

/** fmtUptime renders seconds as "3d 4h 12m". */
export function fmtUptime(sec: number): string {
  if (sec <= 0) return "—";
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const parts: string[] = [];
  if (d) parts.push(`${d}d`);
  if (h || d) parts.push(`${h}h`);
  parts.push(`${m}m`);
  return parts.join(" ");
}

/** usageTone maps a 0–100 utilisation to the badge colour the app uses. */
export function usageTone(pct: number): string {
  if (pct >= 90) return "text-danger";
  if (pct >= 75) return "text-amber-500";
  return "text-emerald-500";
}
