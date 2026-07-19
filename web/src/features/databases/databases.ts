import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type AdminerSSO, type Database, type DatabaseUser } from "@/lib/api";

export function useDatabases() {
  return useQuery({ queryKey: ["databases"], queryFn: () => api.get<Database[]>("/databases") });
}

export function useDBUsers() {
  return useQuery({ queryKey: ["database-users"], queryFn: () => api.get<DatabaseUser[]>("/database-users") });
}

export function useCreateDatabase() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { name: string }) => api.post<Database>("/databases", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["databases"] }),
  });
}

export function useDeleteDatabase() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.del(`/databases/${uid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["databases"] }),
  });
}

export function useCreateDBUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { username: string; host: string; password: string }) =>
      api.post<DatabaseUser>("/database-users", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["database-users"] }),
  });
}

export function useDeleteDBUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.del(`/database-users/${uid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["database-users"] }),
  });
}

export function useGrant() {
  return useMutation({
    mutationFn: (v: { dbUid: string; user_uid: string; privileges: string[] }) =>
      api.post(`/databases/${v.dbUid}/grant`, { user_uid: v.user_uid, privileges: v.privileges }),
  });
}

export function useAdminerSSO() {
  return useMutation({ mutationFn: (uid: string) => api.post<AdminerSSO>(`/databases/${uid}/adminer-sso`) });
}

// The export is a plain browser navigation to the streamed download; there is no
// JSON body. Import is a multipart-ish POST the backend accepts as a job.
export function databaseExportURL(uid: string): string {
  return `/api/v1/databases/${uid}/export`;
}
