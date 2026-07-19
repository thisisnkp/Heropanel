import { useState } from "react";
import { ApiRequestError } from "@/lib/api";
import { Alert, Badge, Button, Card, EmptyState, Field, Input, Modal, Select, Spinner } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useAddRecord, useCreateZone, useDeleteRecord, useDeleteZone, useZoneRecords, useZones } from "./dns";

const TYPES = ["A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV"];

export function DNSPage() {
  const zones = useZones();
  const [selected, setSelected] = useState<string | null>(null);
  const [newZone, setNewZone] = useState(false);
  const delZone = useDeleteZone();

  const activeZone = zones.data?.find((z) => z.uid === selected) ?? null;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-fg">DNS</h1>
          <p className="text-sm text-muted">Authoritative zones on the panel's BIND9 backend.</p>
        </div>
        <Button onClick={() => setNewZone(true)}>New zone</Button>
      </div>

      {zones.error && <Alert>You do not have permission to view DNS.</Alert>}
      {zones.isLoading && <Spinner />}

      {zones.data && zones.data.length === 0 && (
        <Card>
          <EmptyState title="No zones yet" hint="Create an authoritative zone to manage its records here." action={<Button onClick={() => setNewZone(true)}>New zone</Button>} />
        </Card>
      )}

      {zones.data && zones.data.length > 0 && (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-[16rem_1fr]">
          <Card className="h-fit overflow-hidden">
            {zones.data.map((z) => (
              <button
                key={z.uid}
                onClick={() => setSelected(z.uid)}
                className={`flex w-full items-center justify-between border-b border-border/60 px-4 py-3 text-left text-sm last:border-0 ${
                  selected === z.uid ? "bg-brand/10" : "hover:bg-border/20"
                }`}
              >
                <span className="font-medium text-fg">{z.name}</span>
                <Badge>serial {z.serial}</Badge>
              </button>
            ))}
          </Card>

          {activeZone ? (
            <ZoneRecords
              zoneUid={activeZone.uid}
              zoneName={activeZone.name}
              onDeleteZone={() => {
                if (confirm(`Delete zone ${activeZone.name} and all its records?`))
                  delZone.mutate(activeZone.uid, {
                    onSuccess: () => {
                      toast.success("Zone deleted");
                      setSelected(null);
                    },
                  });
              }}
            />
          ) : (
            <Card>
              <EmptyState title="Select a zone" hint="Pick a zone on the left to manage its records." />
            </Card>
          )}
        </div>
      )}

      {newZone && <CreateZoneModal onClose={() => setNewZone(false)} onCreated={(uid) => setSelected(uid)} />}
    </div>
  );
}

function ZoneRecords({ zoneUid, zoneName, onDeleteZone }: { zoneUid: string; zoneName: string; onDeleteZone: () => void }) {
  const records = useZoneRecords(zoneUid);
  const del = useDeleteRecord(zoneUid);
  const [adding, setAdding] = useState(false);

  return (
    <Card className="overflow-hidden">
      <div className="flex items-center justify-between border-b border-border px-4 py-3">
        <span className="text-sm font-medium text-fg">{zoneName}</span>
        <div className="flex gap-2">
          <Button variant="ghost" className="h-8 px-2 text-danger" onClick={onDeleteZone}>
            Delete zone
          </Button>
          <Button className="h-8 px-3" onClick={() => setAdding(true)}>
            Add record
          </Button>
        </div>
      </div>
      {records.isLoading ? (
        <div className="p-4">
          <Spinner />
        </div>
      ) : records.data && records.data.length > 0 ? (
        <table className="w-full text-sm">
          <thead className="border-b border-border text-left text-muted">
            <tr>
              <th className="px-4 py-2.5 font-medium">Name</th>
              <th className="px-4 py-2.5 font-medium">Type</th>
              <th className="px-4 py-2.5 font-medium">Content</th>
              <th className="px-4 py-2.5 font-medium">TTL</th>
              <th className="px-4 py-2.5" />
            </tr>
          </thead>
          <tbody>
            {records.data.map((r) => (
              <tr key={r.uid} className="border-b border-border/60 last:border-0">
                <td className="px-4 py-2.5 font-medium text-fg">{r.name}</td>
                <td className="px-4 py-2.5">
                  <Badge>{r.type}</Badge>
                </td>
                <td className="px-4 py-2.5 font-mono text-xs text-muted">{r.content}</td>
                <td className="px-4 py-2.5 text-muted">{r.ttl}</td>
                <td className="px-4 py-2.5 text-right">
                  <Button
                    variant="ghost"
                    className="h-7 px-2 text-danger"
                    onClick={() => del.mutate(r.uid, { onSuccess: () => toast.success("Record deleted") })}
                  >
                    ✕
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <EmptyState title="No records" hint="Add an A, CNAME, MX, or TXT record." />
      )}

      {adding && <AddRecordModal zoneUid={zoneUid} onClose={() => setAdding(false)} />}
    </Card>
  );
}

function CreateZoneModal({ onClose, onCreated }: { onClose: () => void; onCreated: (uid: string) => void }) {
  const create = useCreateZone();
  const [form, setForm] = useState({ name: "", primary_ns: "", admin_email: "", ns_ip: "" });
  return (
    <Modal title="New zone" onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate(form, {
            onSuccess: (z) => {
              toast.success("Zone created");
              onCreated(z.uid);
              onClose();
            },
          });
        }}
        className="space-y-4"
      >
        <Field label="Zone name">
          <Input autoFocus value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="example.com" />
        </Field>
        <div className="grid grid-cols-2 gap-4">
          <Field label="Primary NS">
            <Input value={form.primary_ns} onChange={(e) => setForm({ ...form, primary_ns: e.target.value })} placeholder="ns1.example.com" />
          </Field>
          <Field label="NS IP">
            <Input value={form.ns_ip} onChange={(e) => setForm({ ...form, ns_ip: e.target.value })} placeholder="203.0.113.10" />
          </Field>
        </div>
        <Field label="Admin email">
          <Input value={form.admin_email} onChange={(e) => setForm({ ...form, admin_email: e.target.value })} placeholder="hostmaster@example.com" />
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

function AddRecordModal({ zoneUid, onClose }: { zoneUid: string; onClose: () => void }) {
  const add = useAddRecord(zoneUid);
  const [form, setForm] = useState({ name: "", type: "A", content: "", ttl: 3600, priority: 0 });
  return (
    <Modal title="Add record" onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          add.mutate(form, {
            onSuccess: () => {
              toast.success("Record added");
              onClose();
            },
          });
        }}
        className="space-y-4"
      >
        <div className="grid grid-cols-[1fr_8rem] gap-4">
          <Field label="Name">
            <Input autoFocus value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="www" />
          </Field>
          <Field label="Type">
            <Select value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value })}>
              {TYPES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </Select>
          </Field>
        </div>
        <Field label="Content">
          <Input value={form.content} onChange={(e) => setForm({ ...form, content: e.target.value })} placeholder="203.0.113.20" />
        </Field>
        <div className="grid grid-cols-2 gap-4">
          <Field label="TTL">
            <Input type="number" value={form.ttl} onChange={(e) => setForm({ ...form, ttl: Number(e.target.value) })} />
          </Field>
          {form.type === "MX" && (
            <Field label="Priority">
              <Input type="number" value={form.priority} onChange={(e) => setForm({ ...form, priority: Number(e.target.value) })} />
            </Field>
          )}
        </div>
        {add.error instanceof ApiRequestError && <Alert>{add.error.message}</Alert>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={add.isPending}>
            Add
          </Button>
        </div>
      </form>
    </Modal>
  );
}
