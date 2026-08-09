import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

// Data hooks for panel self-update (docs/26).

export interface UpdateStatus {
  current: string;
  channel: string;
  available?: string;
  notes?: string;
  up_to_date: boolean;
  /** False when no release source or no pinned key is configured; `reason` says which. */
  configured: boolean;
  reason?: string;
  last_state?: string;
  last_target?: string;
  last_error?: string;
}

const updateKey = ["system", "update"] as const;

export function useUpdateStatus() {
  return useQuery({
    queryKey: updateKey,
    queryFn: () => api.get<UpdateStatus>("/system/update"),
    // The check reaches an external release server, so it is not free — but it
    // is also the kind of thing an operator expects to be current when they
    // look at it. A minute is the compromise.
    staleTime: 60_000,
  });
}

export function useCheckUpdate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<UpdateStatus>("/system/update/check"),
    onSuccess: (st) => qc.setQueryData(updateKey, st),
  });
}

// useApplyUpdate starts an update. It resolves as soon as the installer has
// been handed the release — the panel is then restarted underneath this very
// browser session, so there is no success to wait for here.
export function useApplyUpdate() {
  return useMutation({
    mutationFn: (version?: string) =>
      api.post<{ started: boolean; uid: string; from: string; to: string; note: string }>(
        "/system/update/apply",
        version ? { version } : {},
      ),
  });
}
