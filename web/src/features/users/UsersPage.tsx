import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { ApiRequestError, can } from "@/lib/api";
import { Alert, Badge, Button, Card, Field, Input, Modal, Spinner, StatusBadge, Tabs, Toggle } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useMe } from "@/features/auth/auth";
import {
  useUsers,
  useCreateUser,
  useSetUserStatus,
  useSetUserRoles,
  useSetUserPassword,
  useDeleteUser,
  useImpersonate,
  useRoles,
  usePermissions,
  useCreateRole,
  useUpdateRole,
  useDeleteRole,
  grantableRoles,
  type User,
  type Role,
} from "./users";

// The multi-user administration page: managing accounts and their roles, and the
// custom-roles/permissions catalog. Gated by user.read (view) / user.write (edit)
// server-side; a viewer without user.write simply sees the actions error.
export function UsersPage() {
  const [tab, setTab] = useState("users");
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-fg">Users &amp; roles</h1>
        <p className="text-sm text-muted">Panel accounts, their roles, and the permission catalog.</p>
      </div>
      <Tabs
        tabs={[
          { id: "users", label: "Users" },
          { id: "roles", label: "Roles" },
        ]}
        active={tab}
        onChange={setTab}
      />
      {tab === "users" ? <UsersTab /> : <RolesTab />}
    </div>
  );
}

function UsersTab() {
  const { data, isLoading, error } = useUsers();
  const me = useMe();
  const roles = useRoles();
  const canImpersonate = can(me.data, "user.impersonate");
  const currentUID = me.data?.user_uid ?? "";
  const [creating, setCreating] = useState(false);
  const [rolesFor, setRolesFor] = useState<User | null>(null);
  const [pwFor, setPwFor] = useState<User | null>(null);

  if (error)
    return (
      <Alert>
        {error instanceof ApiRequestError && error.status === 403
          ? "You do not have permission to view users."
          : "Could not load users."}
      </Alert>
    );

  const users = data?.users ?? [];
  // Only offer roles the caller can actually assign — a reseller who lacks a
  // permission cannot grant a role that holds it (the server would 403), so
  // showing it would be a dead option. The Roles catalog tab still lists all.
  const assignable = grantableRoles(me.data, roles.data?.roles ?? []);
  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <Button onClick={() => setCreating(true)}>Add user</Button>
      </div>
      {isLoading ? (
        <Spinner />
      ) : (
        <Card className="overflow-hidden">
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-muted">
              <tr>
                <th className="px-4 py-3 font-medium">User</th>
                <th className="px-4 py-3 font-medium">Roles</th>
                <th className="px-4 py-3 font-medium">Status</th>
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <UserRow
                  key={u.uid}
                  user={u}
                  canImpersonate={canImpersonate && u.status === "active" && !u.superuser && u.uid !== currentUID}
                  onRoles={() => setRolesFor(u)}
                  onPassword={() => setPwFor(u)}
                />
              ))}
              {users.length === 0 && (
                <tr>
                  <td colSpan={4} className="px-4 py-8 text-center text-muted">
                    No users yet.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </Card>
      )}
      {creating && <CreateUserModal roles={assignable} onClose={() => setCreating(false)} />}
      {rolesFor && <RolesModal user={rolesFor} roles={assignable} onClose={() => setRolesFor(null)} />}
      {pwFor && <PasswordModal user={pwFor} onClose={() => setPwFor(null)} />}
    </div>
  );
}

function UserRow({
  user,
  canImpersonate,
  onRoles,
  onPassword,
}: {
  user: User;
  canImpersonate: boolean;
  onRoles: () => void;
  onPassword: () => void;
}) {
  const setStatus = useSetUserStatus();
  const del = useDeleteUser();
  const impersonate = useImpersonate();
  const navigate = useNavigate();
  return (
    <tr className="border-b border-border/60 last:border-0">
      <td className="px-4 py-3">
        <div className="flex items-center gap-2">
          <span className="font-medium text-fg">{user.display_name || user.username}</span>
          {user.superuser && <Badge>admin</Badge>}
        </div>
        <div className="text-xs text-muted">
          @{user.username} · {user.email}
        </div>
      </td>
      <td className="px-4 py-3">
        <div className="flex flex-wrap gap-1">
          {user.roles.length === 0 ? <span className="text-xs text-muted">none</span> : user.roles.map((r) => <Badge key={r}>{r}</Badge>)}
        </div>
      </td>
      <td className="px-4 py-3">
        <StatusBadge status={user.status} />
      </td>
      <td className="px-4 py-3">
        <div className="flex justify-end gap-2">
          {canImpersonate && (
            <Button
              variant="ghost"
              className="h-7 px-2"
              loading={impersonate.isPending}
              onClick={() => {
                if (!confirm(`Act as ${user.email}? Everything you do will be audited as you, acting as them.`)) return;
                impersonate.mutate(user.uid, {
                  onSuccess: () => {
                    toast.success(`Now acting as ${user.email}`);
                    navigate("/");
                  },
                  onError: (e) => toast.error(e.message),
                });
              }}
            >
              Impersonate
            </Button>
          )}
          <Button variant="ghost" className="h-7 px-2" onClick={onRoles}>
            Roles
          </Button>
          <Button variant="ghost" className="h-7 px-2" onClick={onPassword}>
            Password
          </Button>
          <Button
            variant="ghost"
            className="h-7 px-2"
            loading={setStatus.isPending && setStatus.variables?.uid === user.uid}
            onClick={() =>
              setStatus.mutate(
                { uid: user.uid, status: user.status === "active" ? "suspended" : "active" },
                { onError: (e) => toast.error(e.message) },
              )
            }
          >
            {user.status === "active" ? "Suspend" : "Activate"}
          </Button>
          <Button
            variant="ghost"
            className="h-7 px-2 text-danger"
            onClick={() => {
              if (!confirm(`Delete ${user.email}? Their sessions end immediately.`)) return;
              del.mutate(user.uid, { onError: (e) => toast.error(e.message) });
            }}
          >
            Delete
          </Button>
        </div>
      </td>
    </tr>
  );
}

function CreateUserModal({ roles, onClose }: { roles: Role[]; onClose: () => void }) {
  const create = useCreateUser();
  const [email, setEmail] = useState("");
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [selected, setSelected] = useState<string[]>([]);

  return (
    <Modal title="Add user" onClose={onClose}>
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate(
            { email: email.trim(), username: username.trim(), display_name: displayName.trim(), password, roles: selected },
            {
              onSuccess: (u) => {
                toast.success(`${u.email} created`);
                onClose();
              },
              onError: (err) => toast.error(err.message),
            },
          );
        }}
      >
        <Field label="Email">
          <Input value={email} onChange={(e) => setEmail(e.target.value)} type="email" required />
        </Field>
        <Field label="Username">
          <Input value={username} onChange={(e) => setUsername(e.target.value)} required />
        </Field>
        <Field label="Display name (optional)">
          <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        </Field>
        <Field label="Password">
          <Input value={password} onChange={(e) => setPassword(e.target.value)} type="password" required minLength={8} />
        </Field>
        <Field label="Roles">
          <RolePicker roles={roles} selected={selected} onChange={setSelected} />
        </Field>
        <div className="flex justify-end gap-2 pt-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={create.isPending}>
            Create
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function RolesModal({ user, roles, onClose }: { user: User; roles: Role[]; onClose: () => void }) {
  const setRoles = useSetUserRoles();
  const [selected, setSelected] = useState<string[]>(user.roles);
  return (
    <Modal title={`Roles — ${user.username}`} onClose={onClose}>
      <div className="space-y-3">
        <RolePicker roles={roles} selected={selected} onChange={setSelected} />
        <div className="flex justify-end gap-2 pt-2">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            loading={setRoles.isPending}
            onClick={() =>
              setRoles.mutate(
                { uid: user.uid, roles: selected },
                {
                  onSuccess: () => {
                    toast.success("Roles updated");
                    onClose();
                  },
                  onError: (e) => toast.error(e.message),
                },
              )
            }
          >
            Save
          </Button>
        </div>
      </div>
    </Modal>
  );
}

function PasswordModal({ user, onClose }: { user: User; onClose: () => void }) {
  const setPw = useSetUserPassword();
  const [password, setPassword] = useState("");
  return (
    <Modal title={`Reset password — ${user.username}`} onClose={onClose}>
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          setPw.mutate(
            { uid: user.uid, password },
            {
              onSuccess: () => {
                toast.success("Password reset — their sessions have ended.");
                onClose();
              },
              onError: (err) => toast.error(err.message),
            },
          );
        }}
      >
        <p className="text-xs text-muted">Setting a new password signs the user out of all sessions.</p>
        <Field label="New password">
          <Input value={password} onChange={(e) => setPassword(e.target.value)} type="password" required minLength={8} />
        </Field>
        <div className="flex justify-end gap-2 pt-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={setPw.isPending}>
            Reset
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function RolePicker({ roles, selected, onChange }: { roles: Role[]; selected: string[]; onChange: (r: string[]) => void }) {
  const toggle = (slug: string) => onChange(selected.includes(slug) ? selected.filter((s) => s !== slug) : [...selected, slug]);
  if (roles.length === 0) return <p className="text-xs text-muted">No roles defined.</p>;
  return (
    <div className="space-y-2 rounded-md border border-border/60 p-3">
      {roles.map((r) => (
        <div key={r.slug} className="flex items-center justify-between gap-3">
          <span className="text-sm text-fg">
            {r.name} <span className="text-xs text-muted">({r.slug})</span>
          </span>
          <Toggle checked={selected.includes(r.slug)} onChange={() => toggle(r.slug)} label={r.name} />
        </div>
      ))}
    </div>
  );
}

// ── roles tab ────────────────────────────────────────────────────────────────

function RolesTab() {
  const { data, isLoading, error } = useRoles();
  const perms = usePermissions();
  const [editing, setEditing] = useState<Role | null>(null);
  const [creating, setCreating] = useState(false);

  if (error) return <Alert>Could not load roles.</Alert>;
  const roles = data?.roles ?? [];
  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <Button onClick={() => setCreating(true)}>Add role</Button>
      </div>
      {isLoading ? (
        <Spinner />
      ) : (
        <div className="grid gap-3 sm:grid-cols-2">
          {roles.map((r) => (
            <Card key={r.slug} className="p-4">
              <div className="flex items-start justify-between">
                <div>
                  <div className="flex items-center gap-2">
                    <h3 className="text-sm font-semibold text-fg">{r.name}</h3>
                    {r.system ? <Badge>system</Badge> : <Badge>custom</Badge>}
                  </div>
                  <p className="mt-0.5 text-xs text-muted">{r.description || r.slug}</p>
                </div>
                <Button variant="ghost" className="h-7 px-2" onClick={() => setEditing(r)}>
                  {r.system ? "View" : "Edit"}
                </Button>
              </div>
              <div className="mt-3 flex flex-wrap gap-1">
                {r.permissions.includes("*") ? (
                  <Badge>full access</Badge>
                ) : r.permissions.length === 0 ? (
                  <span className="text-xs text-muted">no permissions</span>
                ) : (
                  r.permissions.slice(0, 8).map((p) => <Badge key={p}>{p}</Badge>)
                )}
                {r.permissions.length > 8 && <span className="text-xs text-muted">+{r.permissions.length - 8} more</span>}
              </div>
            </Card>
          ))}
        </div>
      )}
      {creating && <RoleEditor perms={perms.data?.permissions ?? []} onClose={() => setCreating(false)} />}
      {editing && <RoleEditor role={editing} perms={perms.data?.permissions ?? []} onClose={() => setEditing(null)} />}
    </div>
  );
}

function RoleEditor({ role, perms, onClose }: { role?: Role; perms: { slug: string; description: string }[]; onClose: () => void }) {
  const create = useCreateRole();
  const update = useUpdateRole();
  const del = useDeleteRole();
  const editing = !!role;
  const locked = role?.system ?? false;
  const [slug, setSlug] = useState(role?.slug ?? "");
  const [name, setName] = useState(role?.name ?? "");
  const [description, setDescription] = useState(role?.description ?? "");
  const [selected, setSelected] = useState<string[]>(role?.permissions ?? []);
  const togglePerm = (p: string) => setSelected(selected.includes(p) ? selected.filter((s) => s !== p) : [...selected, p]);

  const save = () => {
    if (editing) {
      update.mutate(
        { slug: role!.slug, name, description, permissions: locked ? undefined : selected },
        {
          onSuccess: () => {
            toast.success("Role updated");
            onClose();
          },
          onError: (e) => toast.error(e.message),
        },
      );
    } else {
      create.mutate(
        { slug: slug.trim(), name: name.trim(), description, permissions: selected },
        {
          onSuccess: () => {
            toast.success("Role created");
            onClose();
          },
          onError: (e) => toast.error(e.message),
        },
      );
    }
  };

  return (
    <Modal title={editing ? `Role — ${role!.name}` : "Add role"} onClose={onClose} wide>
      <div className="space-y-3">
        {!editing && (
          <Field label="Slug" hint="Lowercase id, e.g. support">
            <Input value={slug} onChange={(e) => setSlug(e.target.value.toLowerCase())} />
          </Field>
        )}
        <Field label="Name">
          <Input value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Description">
          <Input value={description} onChange={(e) => setDescription(e.target.value)} />
        </Field>
        <Field label="Permissions">
          {locked ? (
            <p className="text-xs text-muted">
              This is a system role — its permissions are fixed. Create a custom role to define your own set.
            </p>
          ) : (
            <div className="max-h-64 space-y-1.5 overflow-auto rounded-md border border-border/60 p-3">
              {perms
                .filter((p) => p.slug !== "*")
                .map((p) => (
                  <div key={p.slug} className="flex items-center justify-between gap-3">
                    <span className="text-sm text-fg">
                      <span className="font-mono text-xs">{p.slug}</span>
                      <span className="ml-2 text-xs text-muted">{p.description}</span>
                    </span>
                    <Toggle checked={selected.includes(p.slug)} onChange={() => togglePerm(p.slug)} label={p.slug} />
                  </div>
                ))}
            </div>
          )}
        </Field>
        <div className="flex items-center justify-between pt-2">
          <div>
            {editing && !locked && (
              <Button
                variant="ghost"
                className="text-danger"
                onClick={() => {
                  if (!confirm(`Delete the ${role!.name} role?`)) return;
                  del.mutate(role!.slug, {
                    onSuccess: () => {
                      toast.success("Role deleted");
                      onClose();
                    },
                    onError: (e) => toast.error(e.message),
                  });
                }}
              >
                Delete role
              </Button>
            )}
          </div>
          <div className="flex gap-2">
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button loading={create.isPending || update.isPending} onClick={save}>
              {editing ? "Save" : "Create"}
            </Button>
          </div>
        </div>
      </div>
    </Modal>
  );
}
