import { useState } from "react";
import { ApiRequestError, type Database, type DatabaseUser } from "@/lib/api";
import { Alert, Button, Card, EmptyState, Field, Input, Modal, Spinner } from "@/components/ui";
import { toast } from "@/stores/toast";
import {
  databaseExportURL,
  useAdminerSSO,
  useCreateDBUser,
  useCreateDatabase,
  useDBUsers,
  useDatabases,
  useDeleteDBUser,
  useDeleteDatabase,
} from "./databases";

function bytes(n?: number): string {
  if (!n) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${units[i]}`;
}

export function DatabasesPage() {
  const dbs = useDatabases();
  const users = useDBUsers();
  const del = useDeleteDatabase();
  const delUser = useDeleteDBUser();
  const sso = useAdminerSSO();
  const [newDB, setNewDB] = useState(false);
  const [newUser, setNewUser] = useState(false);

  const openAdminer = (db: Database) => {
    sso.mutate(db.uid, {
      onSuccess: (s) => {
        // Post the throwaway credential to Adminer's login form in a new tab.
        const form = document.createElement("form");
        form.method = "POST";
        form.action = s.url;
        form.target = "_blank";
        const fields: Record<string, string> = {
          "auth[driver]": s.driver,
          "auth[server]": s.server,
          "auth[username]": s.username,
          "auth[password]": s.password,
          "auth[db]": s.database,
        };
        for (const [k, v] of Object.entries(fields)) {
          const input = document.createElement("input");
          input.type = "hidden";
          input.name = k;
          input.value = v;
          form.appendChild(input);
        }
        document.body.appendChild(form);
        form.submit();
        form.remove();
        toast.success("Opened Adminer", "A throwaway account was created; it expires shortly.");
      },
      onError: (e) => toast.error("Adminer hand-off failed", e instanceof ApiRequestError ? e.message : undefined),
    });
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-fg">Databases</h1>
          <p className="text-sm text-muted">MariaDB databases and users, with grants, export/import, and Adminer.</p>
        </div>
        <div className="flex gap-2">
          <Button variant="ghost" onClick={() => setNewUser(true)}>
            New user
          </Button>
          <Button onClick={() => setNewDB(true)}>New database</Button>
        </div>
      </div>

      {dbs.error && <Alert>You do not have permission to view databases.</Alert>}
      {dbs.isLoading && <Spinner />}

      {dbs.data && (
        <Card className="overflow-hidden">
          {dbs.data.length > 0 ? (
            <table className="w-full text-sm">
              <thead className="border-b border-border text-left text-muted">
                <tr>
                  <th className="px-4 py-3 font-medium">Database</th>
                  <th className="px-4 py-3 font-medium">Size</th>
                  <th className="px-4 py-3" />
                </tr>
              </thead>
              <tbody>
                {dbs.data.map((db) => (
                  <tr key={db.uid} className="border-b border-border/60 last:border-0">
                    <td className="px-4 py-3 font-medium text-fg">{db.name}</td>
                    <td className="px-4 py-3 text-muted">{bytes(db.size_bytes)}</td>
                    <td className="px-4 py-3">
                      <div className="flex justify-end gap-1">
                        <Button variant="ghost" className="h-8 px-2" onClick={() => openAdminer(db)} loading={sso.isPending}>
                          Adminer
                        </Button>
                        <a href={databaseExportURL(db.uid)} download>
                          <Button variant="ghost" className="h-8 px-2">
                            Export
                          </Button>
                        </a>
                        <Button
                          variant="ghost"
                          className="h-8 px-2 text-danger"
                          onClick={() => {
                            if (confirm(`Drop database ${db.name}? This is irreversible.`))
                              del.mutate(db.uid, { onSuccess: () => toast.success("Database dropped") });
                          }}
                        >
                          Drop
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <EmptyState title="No databases yet" hint="Create a MariaDB database to back a site." action={<Button onClick={() => setNewDB(true)}>New database</Button>} />
          )}
        </Card>
      )}

      {users.data && users.data.length > 0 && (
        <Card className="overflow-hidden">
          <div className="border-b border-border px-4 py-3 text-sm font-medium text-fg">Database users</div>
          <table className="w-full text-sm">
            <tbody>
              {users.data.map((u: DatabaseUser) => (
                <tr key={u.uid} className="border-b border-border/60 last:border-0">
                  <td className="px-4 py-3 font-medium text-fg">
                    {u.username}
                    <span className="ml-2 text-xs text-muted">@ {u.host}</span>
                  </td>
                  <td className="px-4 py-3 text-right">
                    <Button
                      variant="ghost"
                      className="h-8 px-2 text-danger"
                      onClick={() => {
                        if (confirm(`Drop user ${u.username}?`))
                          delUser.mutate(u.uid, { onSuccess: () => toast.success("User dropped") });
                      }}
                    >
                      Drop
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      {newDB && <CreateDBModal onClose={() => setNewDB(false)} />}
      {newUser && <CreateUserModal onClose={() => setNewUser(false)} />}
    </div>
  );
}

function CreateDBModal({ onClose }: { onClose: () => void }) {
  const create = useCreateDatabase();
  const [name, setName] = useState("");
  return (
    <Modal title="New database" onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate(
            { name },
            {
              onSuccess: () => {
                toast.success("Database created");
                onClose();
              },
            },
          );
        }}
        className="space-y-4"
      >
        <Field label="Name">
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="acme_app" />
        </Field>
        {create.error instanceof ApiRequestError && <Alert>{create.error.message}</Alert>}
        <div className="flex justify-end gap-2">
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

function CreateUserModal({ onClose }: { onClose: () => void }) {
  const create = useCreateDBUser();
  const [form, setForm] = useState({ username: "", host: "localhost", password: "" });
  return (
    <Modal title="New database user" onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate(form, {
            onSuccess: () => {
              toast.success("User created");
              onClose();
            },
          });
        }}
        className="space-y-4"
      >
        <Field label="Username">
          <Input autoFocus value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} />
        </Field>
        <Field label="Host" hint="localhost, %, or a specific address">
          <Input value={form.host} onChange={(e) => setForm({ ...form, host: e.target.value })} />
        </Field>
        <Field label="Password">
          <Input type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} />
        </Field>
        {create.error instanceof ApiRequestError && <Alert>{create.error.message}</Alert>}
        <div className="flex justify-end gap-2">
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
