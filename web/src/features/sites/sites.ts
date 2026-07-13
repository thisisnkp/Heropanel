import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type Job, type Site } from "@/lib/api";
import { wsSubscribe } from "@/lib/ws";

export function useSites() {
  return useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
}

// The create endpoint returns either { job } (async) or a Site (synchronous).
export type CreateResult = { job: Job } | Site;

export function isJobResult(r: CreateResult): r is { job: Job } {
  return (r as { job?: Job }).job !== undefined;
}

export interface CreateSiteInput {
  name: string;
  primary_domain: string;
  type: string;
}

export function useCreateSite() {
  return useMutation({
    mutationFn: (v: CreateSiteInput) => api.post<CreateResult>("/sites", v),
  });
}

export function useDeleteSite() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.del<{ job?: Job; ok?: boolean }>(`/sites/${uid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["sites"] }),
  });
}

export interface JobState {
  progress: number;
  step: string;
  status: string;
}

// useJobProgress tracks a job's live progress over WebSocket, seeding from the
// REST endpoint, and invokes onDone once when it reaches a terminal state.
export function useJobProgress(jobUid: string | null, onDone: (status: string) => void): JobState {
  const [state, setState] = useState<JobState>({ progress: 0, step: "queued", status: "queued" });
  const doneRef = useRef(false);

  useEffect(() => {
    if (!jobUid) return;
    doneRef.current = false;
    setState({ progress: 0, step: "queued", status: "queued" });

    const finish = (status: string) => {
      if (doneRef.current) return;
      doneRef.current = true;
      onDone(status);
    };

    // Seed from REST (also the fallback if a WS event was missed).
    api
      .get<Job>(`/jobs/${jobUid}`)
      .then((j) => {
        setState({ progress: j.progress, step: j.status, status: j.status });
        if (j.status === "succeeded" || j.status === "failed") finish(j.status);
      })
      .catch(() => {});

    const unsub = wsSubscribe(`job:${jobUid}`, (data) => {
      const d = data as { progress?: number; step?: string; status?: string };
      setState({ progress: d.progress ?? 0, step: d.step ?? "", status: d.status ?? "running" });
      if (d.status === "succeeded" || d.status === "failed") finish(d.status);
    });

    return unsub;
    // onDone intentionally excluded: it is stable for the job's lifetime.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [jobUid]);

  return state;
}
