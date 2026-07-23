import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

// Data hooks for the Docker module.
//
// Everything here is a read of the host, not of a site, so nothing is keyed by
// site uid. The one thing worth knowing about the shapes: the panel does not
// re-model docker's own output — sizes, uptimes and CPU percentages arrive
// pre-rendered by docker, because re-deriving "Up 3 days" from a timestamp is
// work with no reader and a new way to be wrong.

export interface DockerInfo {
  available: boolean;
  server_version?: string;
  api_version?: string;
  reason?: string;
}

export interface DockerContainer {
  id: string;
  name: string;
  image: string;
  state: string;
  status: string;
  ports: string;
  created: string;
  labels: Record<string, string>;
  /** Whether HeroPanel created it — and therefore whether it can be modified. */
  managed: boolean;
  site_uid?: string;
}

export interface DockerImage {
  id: string;
  repository: string;
  tag: string;
  size: string;
  created: string;
}

export interface DockerStats {
  id: string;
  name: string;
  cpu_perc: string;
  mem_usage: string;
  mem_perc: string;
  net_io: string;
  block_io: string;
  pids: string;
}

export interface ContainerLogs {
  stdout: string;
  stderr: string;
}

export function useDockerInfo() {
  return useQuery({
    queryKey: ["docker", "info"],
    queryFn: () => api.get<DockerInfo>("/docker/info"),
    // The daemon does not appear and disappear; re-probing on every focus would
    // spend a privileged round trip to learn nothing.
    staleTime: 60_000,
  });
}

export function useContainers(siteUID?: string) {
  const qs = siteUID ? `?site=${encodeURIComponent(siteUID)}` : "";
  return useQuery({
    queryKey: ["docker", "containers", siteUID ?? "all"],
    queryFn: () => api.get<DockerContainer[]>(`/docker/containers${qs}`),
    refetchInterval: 10_000,
  });
}

export function useImages() {
  return useQuery({
    queryKey: ["docker", "images"],
    queryFn: () => api.get<DockerImage[]>("/docker/images"),
  });
}

// useStats polls one sample at a time. The API deliberately does not stream:
// a wedged container must never be able to hold a request open, so the client
// asks again instead of waiting.
export function useStats(enabled: boolean) {
  return useQuery({
    queryKey: ["docker", "stats"],
    queryFn: () => api.get<DockerStats[]>("/docker/stats"),
    refetchInterval: 5_000,
    enabled,
  });
}

export function useContainerLogs(id: string | null, tail = 500) {
  return useQuery({
    queryKey: ["docker", "logs", id, tail],
    queryFn: () => api.get<ContainerLogs>(`/docker/containers/${id}/logs?tail=${tail}`),
    enabled: !!id,
    refetchInterval: 5_000,
  });
}

export type ContainerAction = "start" | "stop" | "restart" | "remove";

export function useContainerAction() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, action, force }: { id: string; action: ContainerAction; force?: boolean }) =>
      action === "remove"
        ? api.del(`/docker/containers/${id}${force ? "?force=true" : ""}`)
        : api.post(`/docker/containers/${id}/${action}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["docker"] }),
  });
}

export function usePullImage() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (image: string) => api.post<{ image: string; log: string }>("/docker/images/pull", { image }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["docker", "images"] }),
  });
}

// useRemoveImage deletes an image by id. The server passes docker's "still used
// by a container" refusal straight back, so this surfaces that as the error.
export function useRemoveImage() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, force }: { id: string; force?: boolean }) =>
      api.del(`/docker/images/${encodeURIComponent(id)}${force ? "?force=true" : ""}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["docker", "images"] }),
  });
}

// usePruneImages reclaims disk from unused images. Dangling-only unless all.
export function usePruneImages() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (all: boolean) => api.post<{ log: string }>(`/docker/images/prune${all ? "?all=true" : ""}`, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["docker", "images"] }),
  });
}

/** stateTone maps a container state to the badge colour the rest of the app uses. */
export function stateTone(state: string): string {
  switch (state) {
    case "running":
      return "text-emerald-500";
    case "restarting":
    case "created":
      return "text-amber-500";
    case "exited":
    case "dead":
      return "text-danger";
    default:
      return "text-muted";
  }
}

export interface DockerVolume {
  name: string;
  driver: string;
  labels: Record<string, string>;
  managed: boolean;
  site_uid?: string;
}

export interface DockerNetwork {
  id: string;
  name: string;
  driver: string;
  scope: string;
  labels: Record<string, string>;
  managed: boolean;
  site_uid?: string;
}

export interface PortMapping {
  host: number;
  container: number;
  proto?: string;
}

export interface VolumeMount {
  volume: string;
  path: string;
  read_only?: boolean;
}

/**
 * ContainerSpec has no bind-address field for ports and no host-path field for
 * mounts — not as an omission but as the contract. The broker binds every
 * published port to 127.0.0.1 and accepts named volumes only, so a form that
 * offered those inputs would be offering something the server will refuse.
 */
export interface ContainerSpec {
  name: string;
  image: string;
  site?: string;
  env?: Record<string, string>;
  ports?: PortMapping[];
  volumes?: VolumeMount[];
  restart?: string;
  network?: string;
  memory_mb?: number;
  command?: string[];
}

export function useVolumes() {
  return useQuery({ queryKey: ["docker", "volumes"], queryFn: () => api.get<DockerVolume[]>("/docker/volumes") });
}

export function useNetworks() {
  return useQuery({ queryKey: ["docker", "networks"], queryFn: () => api.get<DockerNetwork[]>("/docker/networks") });
}

// VolumeConsumer is one container mounting a volume. Unmanaged ones are included:
// the point of the detail view is to answer "is this safe to delete?".
export interface VolumeConsumer {
  id: string;
  name: string;
  image: string;
  state: string;
}

// VolumeDetail is what the inspect drawer renders: docker's own record plus the
// containers attached. `volume` is passed through as opaque JSON.
export interface VolumeDetail {
  volume: unknown;
  consumers: VolumeConsumer[];
}

export function useVolumeDetail(name: string | null) {
  return useQuery({
    queryKey: ["docker", "volume", name],
    queryFn: () => api.get<VolumeDetail>(`/docker/volumes/${encodeURIComponent(name!)}`),
    enabled: !!name,
  });
}

export function useNetworkDetail(name: string | null) {
  return useQuery({
    queryKey: ["docker", "network", name],
    queryFn: () => api.get<unknown>(`/docker/networks/${encodeURIComponent(name!)}`),
    enabled: !!name,
  });
}

export function useCreateContainer() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (spec: ContainerSpec) => api.post<{ name: string; id: string }>("/docker/containers", spec),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["docker"] }),
  });
}

export function useCreateVolume() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.post("/docker/volumes", { name }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["docker", "volumes"] }),
  });
}

export function useRemoveVolume() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.del(`/docker/volumes/${encodeURIComponent(name)}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["docker", "volumes"] }),
  });
}

export function useCreateNetwork() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.post("/docker/networks", { name }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["docker", "networks"] }),
  });
}

export function useRemoveNetwork() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.del(`/docker/networks/${encodeURIComponent(name)}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["docker", "networks"] }),
  });
}

/**
 * parseEnv reads the KEY=value textarea. Values keep everything after the first
 * "=", because a connection string is full of them; a blank line or a line
 * without "=" is skipped rather than becoming a variable named after itself.
 */
export function parseEnv(text: string): Record<string, string> {
  const env: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    const eq = trimmed.indexOf("=");
    if (eq <= 0) continue;
    env[trimmed.slice(0, eq).trim()] = trimmed.slice(eq + 1);
  }
  return env;
}

/** parsePorts reads "8080:80" or "8080:80/udp" lines. */
export function parsePorts(text: string): PortMapping[] {
  const ports: PortMapping[] = [];
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const [spec, proto] = trimmed.split("/");
    const [host, container] = spec.split(":");
    const h = Number(host);
    const c = Number(container ?? host);
    if (!Number.isInteger(h) || !Number.isInteger(c)) continue;
    ports.push({ host: h, container: c, proto: proto === "udp" ? "udp" : "tcp" });
  }
  return ports;
}

/** parseMounts reads "volume-name:/path" lines, optionally ":ro". */
export function parseMounts(text: string): VolumeMount[] {
  const mounts: VolumeMount[] = [];
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const parts = trimmed.split(":");
    if (parts.length < 2) continue;
    mounts.push({ volume: parts[0], path: parts[1], read_only: parts[2] === "ro" });
  }
  return mounts;
}
