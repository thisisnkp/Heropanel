import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type Principal } from "@/lib/api";

export type User = {
  uid: string;
  email: string;
  username: string;
  display_name: string;
  status: string;
  roles: string[];
  superuser: boolean;
};

export type Role = {
  uid: string;
  slug: string;
  name: string;
  description: string;
  system: boolean;
  permissions: string[];
};

// canGrantRole mirrors the server's role-escalation guard (assertRolesWithinActor):
// you may assign a role only if you hold every permission it grants, and never the
// full-access "*" role. It is the same rule the server enforces — the point of
// computing it in the UI is to not offer a reseller a role the server would then
// 403, not to be the enforcement (which stays on the server).
export function canGrantRole(me: Principal | null | undefined, role: Role): boolean {
  if (!me) return false;
  if (me.permissions.includes("*")) return true; // superuser grants anything
  if (role.permissions.includes("*")) return false; // nobody but a superuser grants "*"
  return role.permissions.every((p) => me.permissions.includes(p));
}

// grantableRoles is the subset of roles the actor may actually assign.
export function grantableRoles(me: Principal | null | undefined, roles: Role[]): Role[] {
  return roles.filter((r) => canGrantRole(me, r));
}

export type Permission = {
  slug: string;
  resource: string;
  action: string;
  description: string;
};

export function useUsers() {
  return useQuery({
    queryKey: ["users"],
    queryFn: () => api.get<{ users: User[] }>("/users"),
  });
}

export function useCreateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { email: string; username: string; display_name?: string; password: string; roles: string[] }) =>
      api.post<User>("/users", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}

export function useSetUserStatus() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ uid, status }: { uid: string; status: string }) =>
      api.post<User>(`/users/${uid}/status`, { status }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}

export function useSetUserRoles() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ uid, roles }: { uid: string; roles: string[] }) =>
      api.put<User>(`/users/${uid}/roles`, { roles }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}

export function useSetUserPassword() {
  return useMutation({
    mutationFn: ({ uid, password }: { uid: string; password: string }) =>
      api.put(`/users/${uid}/password`, { password }),
  });
}

export function useDeleteUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.del(`/users/${uid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}

// useImpersonate starts an audited session acting as the target user. The
// backend swaps the session cookie server-side; on success we drop every cached
// query and reseat the identity so the whole app re-renders as the target.
export function useImpersonate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.post<Principal>(`/users/${uid}/impersonate`),
    onSuccess: (target) => {
      qc.clear();
      qc.setQueryData(["me"], target);
    },
  });
}

export function useRoles() {
  return useQuery({
    queryKey: ["roles"],
    queryFn: () => api.get<{ roles: Role[] }>("/roles"),
  });
}

export function usePermissions() {
  return useQuery({
    queryKey: ["permissions"],
    queryFn: () => api.get<{ permissions: Permission[] }>("/permissions"),
  });
}

export function useCreateRole() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { slug: string; name: string; description?: string; permissions: string[] }) =>
      api.post<Role>("/roles", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["roles"] }),
  });
}

export function useUpdateRole() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ slug, ...v }: { slug: string; name?: string; description?: string; permissions?: string[] }) =>
      api.put<Role>(`/roles/${slug}`, v),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["roles"] }),
  });
}

export function useDeleteRole() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (slug: string) => api.del(`/roles/${slug}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["roles"] }),
  });
}
