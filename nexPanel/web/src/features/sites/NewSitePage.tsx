import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, CheckCircle2, Wand2 } from "lucide-react";
import { api, ApiRequestError } from "@/lib/api";
import {
  Alert,
  Badge,
  Button,
  Card,
  CardHeader,
  Combobox,
  Field,
  Input,
  PageHeader,
  cn,
} from "@/components/ui";
import { toast } from "@/stores/toast";
import { useJobs } from "@/stores/jobs";
import { useFreeDomains } from "@/features/domains/domains";
import { classifyDomain, normalizeDomain, siteNameFrom, type DomainVerdict } from "./newsite";
import { isJobResult, useCreateSite } from "./sites";

// The guided path for a plain site — the one most people take. It is a page
// rather than a modal because the domain field needs room: it lists every
// domain the panel already knows the moment it is focused, filters as the
// operator types, and explains what the value they landed on will mean. None of
// that fits a dialog, and none of it works with a native <datalist>.
//
// The deploy-from-Git flows stay in their own modal (DeployAppModal) — they ask
// for a repository, a runtime and a start command, which is a different
// conversation from "what should this be called".

interface TempDomain {
  fqdn: string;
  base: string;
  wildcard: string;
}

// The server mints the name; nothing is reserved, so this is a suggestion the
// operator can still overwrite before submitting.
function useTempDomain() {
  return useMutation({ mutationFn: () => api.post<TempDomain>("/sites/temp-domain") });
}

export function NewSitePage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data: pool } = useFreeDomains();
  const create = useCreateSite();
  const temp = useTempDomain();
  const track = useJobs((s) => s.track);

  const [domain, setDomain] = useState("");
  const [type, setType] = useState("static");
  const [nameOverride, setNameOverride] = useState("");
  const [advanced, setAdvanced] = useState(false);
  const [tempInfo, setTempInfo] = useState<TempDomain | null>(null);

  const free = pool?.fqdns ?? [];
  const trusted = pool?.trusted ?? [];
  const verdict = useMemo(() => classifyDomain(domain, free, trusted), [domain, free, trusted]);

  const derivedName = siteNameFrom(domain);
  const name = nameOverride.trim() || derivedName;
  const canSubmit = normalizeDomain(domain) !== "" && name !== "" && !create.isPending;

  const fieldError = (f: string) =>
    create.error instanceof ApiRequestError ? create.error.fields?.find((x) => x.field === f)?.message : undefined;

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    create.mutate(
      { name, primary_domain: normalizeDomain(domain), type },
      {
        onSuccess: (res) => {
          qc.invalidateQueries({ queryKey: ["sites"] });
          if (isJobResult(res)) {
            // The job drawer follows it from here, and raises the
            // DNS-not-verified warning itself once the result lands.
            track(res.job.id, "Provisioning site");
            toast.info("Creating site…");
            navigate("/sites");
            return;
          }
          if (res.dns_status === "unverified") {
            toast.info(
              "Website created — DNS not verified yet",
              "Add the site's DNS records and verify it on the Domains page so ownership is proven.",
            );
          } else {
            toast.success("Website created");
          }
          navigate(`/sites/${res.uid}`);
        },
      },
    );
  };

  const useTemp = () =>
    temp.mutate(undefined, {
      onSuccess: (t) => {
        setDomain(t.fqdn);
        setTempInfo(t);
      },
      onError: (e) =>
        toast.error(
          "No temporary address available",
          e instanceof ApiRequestError
            ? e.message
            : "An administrator needs to set the panel domain in Setup first.",
        ),
    });

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <PageHeader
        title="New website"
        description="Pick a domain the panel already knows, or type any domain you own. Either way the site is created — an unknown domain just needs its DNS verified afterwards."
      />

      <form onSubmit={submit}>
        <Card>
          <CardHeader title="Address" description="Where this website will be served." />
          <div className="space-y-5 p-5">
            <div className="space-y-1.5">
              <label htmlFor="new-site-domain" className="block text-sm font-medium text-fg">
                Domain
              </label>
              <Combobox
                id="new-site-domain"
                autoFocus
                value={domain}
                onChange={(v) => {
                  setDomain(v);
                  setTempInfo(null);
                }}
                options={free}
                placeholder="acme.com"
                emptyLabel={
                  free.length === 0
                    ? "No verified domains yet — type the domain you want to use."
                    : "No matches — you can still use what you typed."
                }
                renderOption={() => <Badge tone="success">Free</Badge>}
              />
              <DomainVerdictLine verdict={verdict} />
              {fieldError("primary_domain") && <p className="text-xs text-danger">{fieldError("primary_domain")}</p>}
            </div>

            {tempInfo && (
              <Alert tone="info">
                <span className="font-medium">Temporary address.</span> For this to resolve,{" "}
                <code className="font-mono text-xs">{tempInfo.wildcard}</code> must point at this server with an{" "}
                <code className="font-mono text-xs">A</code> record. Swap it for a real domain whenever you have one.
              </Alert>
            )}

            <Field label="Type">
              <div className="flex gap-2">
                {[
                  { id: "static", label: "Static", hint: "HTML, CSS and JS" },
                  { id: "php", label: "PHP", hint: "Dedicated PHP-FPM pool" },
                ].map((t) => (
                  <button
                    key={t.id}
                    type="button"
                    onClick={() => setType(t.id)}
                    className={cn(
                      "flex-1 rounded-lg border px-3 py-2 text-left transition-colors",
                      type === t.id
                        ? "border-brand bg-brand-subtle"
                        : "border-border hover:border-border-strong hover:bg-panel-hover",
                    )}
                  >
                    <span className={cn("block text-sm font-medium", type === t.id ? "text-brand" : "text-fg")}>
                      {t.label}
                    </span>
                    <span className="mt-0.5 block text-xs text-muted">{t.hint}</span>
                  </button>
                ))}
              </div>
            </Field>

            <div>
              <button
                type="button"
                onClick={() => setAdvanced((v) => !v)}
                className="text-xs font-medium text-muted transition-colors hover:text-fg"
              >
                {advanced ? "Hide" : "Show"} advanced
              </button>
              {advanced && (
                <div className="mt-3">
                  <Field
                    label="Site name"
                    hint={
                      fieldError("name") ??
                      `Used in the panel and for the site's system paths. Defaults to "${derivedName || "…"}".`
                    }
                  >
                    <Input
                      value={nameOverride}
                      onChange={(e) => setNameOverride(e.target.value)}
                      placeholder={derivedName}
                    />
                  </Field>
                </div>
              )}
            </div>

            {create.error instanceof ApiRequestError && !create.error.fields?.length && (
              <Alert>{create.error.message}</Alert>
            )}
          </div>

          <div className="flex flex-wrap items-center justify-end gap-2 border-t border-border p-4">
            <Button type="button" variant="ghost" loading={temp.isPending} onClick={useTemp}>
              <Wand2 className="h-3.5 w-3.5" strokeWidth={2} aria-hidden />
              Use temporary domain
            </Button>
            <Button type="button" variant="ghost" onClick={() => navigate("/sites")}>
              Cancel
            </Button>
            <Button type="submit" loading={create.isPending} disabled={!canSubmit}>
              Next
            </Button>
          </div>
        </Card>
      </form>
    </div>
  );
}

// The one line that tells the operator what will happen. Every outcome is
// permitted — this explains, it never blocks — so "unknown" is phrased as work
// to do afterwards rather than as an error.
function DomainVerdictLine({ verdict }: { verdict: DomainVerdict }) {
  if (verdict.kind === "empty") {
    return <p className="text-xs text-muted">Click the field to see every domain available to you.</p>;
  }
  if (verdict.kind === "free") {
    return (
      <p className="flex items-center gap-1.5 text-xs font-medium text-success">
        <CheckCircle2 className="h-3.5 w-3.5 shrink-0" strokeWidth={2} aria-hidden />
        Ownership already proven — this will serve as soon as it's created.
      </p>
    );
  }
  if (verdict.kind === "subdomain") {
    return (
      <p className="flex items-center gap-1.5 text-xs font-medium text-success">
        <CheckCircle2 className="h-3.5 w-3.5 shrink-0" strokeWidth={2} aria-hidden />
        Subdomain of <span className="font-mono">{verdict.parent}</span>, which you already own — no verification
        needed.
      </p>
    );
  }
  return (
    <p className="flex items-start gap-1.5 text-xs font-medium text-warning">
      <AlertTriangle className="mt-px h-3.5 w-3.5 shrink-0" strokeWidth={2} aria-hidden />
      Not known here yet. The site is still created — add its DNS records and verify the domain on the Domains page
      afterwards.
    </p>
  );
}
