import { useState } from "react";
import { ApiRequestError } from "@/lib/api";
import { Alert, Badge, Button, Card, EmptyState, Field, Input, Modal, Select, Spinner, StatusBadge, Textarea } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useCertificates, useIssueCert, useSelfSigned, useUploadCert } from "./ssl";

export function SSLPage() {
  const certs = useCertificates();
  const [issuing, setIssuing] = useState(false);

  const expiresIn = (iso: string) => {
    const days = Math.round((new Date(iso).getTime() - Date.now()) / 86400000);
    return days < 0 ? "expired" : `${days}d`;
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-fg">SSL certificates</h1>
          <p className="text-sm text-muted">Let's Encrypt (HTTP-01 &amp; DNS-01/wildcard), self-signed, or custom upload.</p>
        </div>
        <Button onClick={() => setIssuing(true)}>Issue certificate</Button>
      </div>

      {certs.error && <Alert>You do not have permission to view certificates.</Alert>}
      {certs.isLoading && <Spinner />}

      {certs.data && (
        <Card className="overflow-hidden">
          {certs.data.length > 0 ? (
            <table className="w-full text-sm">
              <thead className="border-b border-border text-left text-muted">
                <tr>
                  <th className="px-4 py-3 font-medium">Common name</th>
                  <th className="px-4 py-3 font-medium">Provider</th>
                  <th className="px-4 py-3 font-medium">Status</th>
                  <th className="px-4 py-3 font-medium">Expires</th>
                  <th className="px-4 py-3 font-medium">Auto-renew</th>
                </tr>
              </thead>
              <tbody>
                {certs.data.map((c) => (
                  <tr key={c.uid} className="border-b border-border/60 last:border-0">
                    <td className="px-4 py-3">
                      <div className="font-medium text-fg">{c.common_name}</div>
                      {c.sans.length > 1 && <div className="text-xs text-muted">+{c.sans.length - 1} SAN</div>}
                    </td>
                    <td className="px-4 py-3">
                      <Badge>{c.provider}</Badge>
                    </td>
                    <td className="px-4 py-3">
                      <StatusBadge status={c.status} />
                    </td>
                    <td className="px-4 py-3 text-muted">{c.expires_at ? expiresIn(c.expires_at) : "—"}</td>
                    <td className="px-4 py-3 text-muted">{c.auto_renew ? "yes" : "no"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <EmptyState title="No certificates" hint="Issue a Let's Encrypt certificate or upload your own." action={<Button onClick={() => setIssuing(true)}>Issue certificate</Button>} />
          )}
        </Card>
      )}

      {issuing && <IssueModal onClose={() => setIssuing(false)} />}
    </div>
  );
}

function IssueModal({ onClose }: { onClose: () => void }) {
  const [mode, setMode] = useState<"le" | "self" | "upload">("le");
  const issue = useIssueCert();
  const self = useSelfSigned();
  const upload = useUploadCert();
  const [domain, setDomain] = useState("");
  const [method, setMethod] = useState("http");
  const [webroot, setWebroot] = useState("");
  const [certPem, setCertPem] = useState("");
  const [keyPem, setKeyPem] = useState("");

  const done = () => {
    toast.success("Certificate issued");
    onClose();
  };
  const fail = (e: unknown) => toast.error("Issue failed", e instanceof ApiRequestError ? e.message : undefined);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (mode === "le") issue.mutate({ domain, method, webroot: method === "http" ? webroot : undefined }, { onSuccess: done, onError: fail });
    else if (mode === "self") self.mutate({ domain }, { onSuccess: done, onError: fail });
    else upload.mutate({ cert_pem: certPem, key_pem: keyPem }, { onSuccess: done, onError: fail });
  };

  const pending = issue.isPending || self.isPending || upload.isPending;

  return (
    <Modal title="Issue certificate" onClose={onClose} wide={mode === "upload"}>
      <div className="mb-4 flex gap-2">
        {([
          ["le", "Let's Encrypt"],
          ["self", "Self-signed"],
          ["upload", "Upload"],
        ] as const).map(([id, label]) => (
          <button
            key={id}
            onClick={() => setMode(id)}
            className={`flex-1 rounded-lg border px-3 py-2 text-sm transition-colors ${
              mode === id ? "border-brand bg-brand/10 text-fg" : "border-border text-muted hover:text-fg"
            }`}
          >
            {label}
          </button>
        ))}
      </div>

      <form onSubmit={submit} className="space-y-4">
        {mode === "le" && (
          <>
            <Field label="Domain" hint="use *.example.com for a wildcard (forces DNS-01)">
              <Input autoFocus value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="acme.example.com" />
            </Field>
            <Field label="Validation">
              <Select value={method} onChange={(e) => setMethod(e.target.value)}>
                <option value="http">HTTP-01 (webroot)</option>
                <option value="dns">DNS-01 (needs a managed zone; required for wildcard)</option>
              </Select>
            </Field>
            {method === "http" && (
              <Field label="Webroot" hint="the site's public directory">
                <Input value={webroot} onChange={(e) => setWebroot(e.target.value)} placeholder="/srv/heropanel/sites/1/public" />
              </Field>
            )}
          </>
        )}
        {mode === "self" && (
          <Field label="Domain">
            <Input autoFocus value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="acme.example.com" />
          </Field>
        )}
        {mode === "upload" && (
          <>
            <Field label="Certificate (PEM)">
              <Textarea rows={5} value={certPem} onChange={(e) => setCertPem(e.target.value)} placeholder="-----BEGIN CERTIFICATE-----" />
            </Field>
            <Field label="Private key (PEM)">
              <Textarea rows={5} value={keyPem} onChange={(e) => setKeyPem(e.target.value)} placeholder="-----BEGIN PRIVATE KEY-----" />
            </Field>
          </>
        )}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={pending}>
            {mode === "upload" ? "Upload" : "Issue"}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
