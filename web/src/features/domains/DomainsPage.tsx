import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Clock, Globe, Link2, Lock, Search, Trash2 } from "lucide-react";
import { api, ApiRequestError, can, type Domain, type ParkedDomain, type Site } from "@/lib/api";
import {
  Alert,
  Badge,
  Button,
  Card,
  CardHeader,
  DataTable,
  EmptyState,
  Field,
  Input,
  Modal,
  PageHeader,
  Select,
  Skeleton,
  Td,
  Tr,
  cn,
} from "@/components/ui";
import { toast } from "@/stores/toast";
import { useMe } from "@/features/auth/auth";
import { useCompleteSetup, useSetup } from "@/features/setup/setup";
import { useSites } from "@/features/sites/sites";
import { useDeleteParked, useParkedDomains, usePark, useVerifyParked } from "./domains";

// DomainsPage has two halves. The top half is the registrar-style parked-
// domain pool: a domain the operator has registered ownership of here, proven
// via a DNS TXT challenge, with no site required — this is what feeds the
// "free domain" picker when creating a site. The bottom half is the existing
// panel-wide view of every domain already attached to a site (primary, alias,
// subdomain, redirect); those are still managed on their site, this is just
// the read-only index.
//
// Both halves share a single entry point: one "Add domain" button opens a
// popup that asks for the hostname and what it's for — park it (no site yet)
// or make it an addon domain on an existing site — rather than each section
// growing its own add flow.

// useAttachDomain adds a domain straight to a site from this page, instead of
// going through the site's own Domains tab. Takes the site uid per call
// (unlike site-detail.ts's useAddDomain) since the target site is chosen
// inside the popup, not fixed when the hook is created.
function useAttachDomain() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ uid, fqdn }: { uid: string; fqdn: string }) =>
      api.post<Domain>(`/sites/${uid}/domains`, { fqdn, kind: "alias" }),
    onSuccess: (_d, vars) => qc.invalidateQueries({ queryKey: ["site", vars.uid, "domains"] }),
  });
}

type Row = Domain & { site: Site };

// mergedDomains flattens each site's domains into one list, guaranteeing the
// site's primary domain appears even if the per-site domains endpoint returned
// only the extras — a domain page that omitted a site's main address would be
// worse than useless.
function mergedDomains(site: Site, domains: Domain[]): Row[] {
  const rows: Row[] = domains.map((d) => ({ ...d, site }));
  if (site.primary_domain && !rows.some((r) => r.fqdn === site.primary_domain)) {
    rows.unshift({
      uid: `primary-${site.uid}`,
      fqdn: site.primary_domain,
      kind: "primary",
      site,
    });
  }
  return rows;
}

export function DomainsPage() {
  const { data: me } = useMe();
  const sites = useSites();
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const [adding, setAdding] = useState(false);
  const [viewing, setViewing] = useState<ParkedDomain | null>(null);
  const canPark = can(me, "domain.write");
  const canAttach = can(me, "site.write");

  const siteList = sites.data ?? [];
  // One query per site, in parallel — the per-site domains endpoint is the only
  // source, and there are rarely many sites on a single node.
  const domainQueries = useQueries({
    queries: siteList.map((s) => ({
      queryKey: ["site", s.uid, "domains"],
      queryFn: () => api.get<Domain[]>(`/sites/${s.uid}/domains`),
      staleTime: 30_000,
    })),
  });

  const rows = useMemo(() => {
    const all: Row[] = [];
    siteList.forEach((s, i) => {
      const domains = domainQueries[i]?.data ?? [];
      all.push(...mergedDomains(s, domains));
    });
    const q = query.trim().toLowerCase();
    const filtered = q
      ? all.filter(
        (r) =>
          r.fqdn.toLowerCase().includes(q) ||
          r.kind.toLowerCase().includes(q) ||
          r.site.name.toLowerCase().includes(q),
      )
      : all;
    return filtered.sort((a, b) => a.fqdn.localeCompare(b.fqdn));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [siteList, domainQueries.map((q) => q.dataUpdatedAt).join(","), query]);

  const loadingDomains = domainQueries.some((q) => q.isLoading);
  const canShowAdd = canPark || (canAttach && siteList.length > 0);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Domains"
        description="Every domain you've parked or attached to a website."
        actions={canShowAdd && <Button onClick={() => setAdding(true)}>Add domain</Button>}
      />

      {can(me, "setup.manage") && <PanelDomainCard />}

      {can(me, "domain.read") && <ParkedDomainsSection canWrite={canPark} viewing={viewing} onView={setViewing} />}

      {sites.error && (
        <Alert>
          {sites.error instanceof ApiRequestError && sites.error.status === 403
            ? "You do not have permission to view domains."
            : "Could not load sites."}
        </Alert>
      )}

      <Card className="overflow-hidden">
        <CardHeader
          title="Attached domains"
          description="Every domain already serving a website. Manage each on its site."
          actions={
            siteList.length > 0 && (
              <div className="relative w-56">
                <Search
                  className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted"
                  strokeWidth={2}
                  aria-hidden
                />
                <Input
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="Filter domains…"
                  aria-label="Filter domains"
                  className="h-8 pl-9 text-xs"
                />
              </div>
            )
          }
        />
        {sites.isLoading || (loadingDomains && rows.length === 0) ? (
          <div className="divide-y divide-border">
            {[0, 1, 2].map((i) => (
              <div key={i} className="flex items-center gap-4 px-4 py-3">
                <Skeleton className="h-3.5 w-56" />
                <Skeleton className="h-3.5 w-16" />
                <Skeleton className="h-3.5 w-24" />
              </div>
            ))}
          </div>
        ) : siteList.length === 0 ? (
          <EmptyState icon={Globe} title="No sites yet" hint="Create a website and its domains appear here." />
        ) : rows.length === 0 ? (
          <EmptyState icon={Search} title="No domains match" hint="Clear the filter to see all domains." />
        ) : (
          <DataTable head={["Domain", "Type", "Website", "HTTPS", "Redirect"]}>
            {rows.map((r) => (
              <Tr key={`${r.site.uid}-${r.uid}`}>
                <Td className="font-mono text-xs">{r.fqdn}</Td>
                <Td>
                  <Badge tone={r.kind === "primary" ? "brand" : "neutral"}>{r.kind}</Badge>
                </Td>
                <Td>
                  <button
                    type="button"
                    onClick={() => navigate(`/sites/${r.site.uid}`)}
                    className="font-medium text-brand hover:underline"
                  >
                    {r.site.name}
                  </button>
                </Td>
                <Td>
                  {r.force_https ? (
                    <span className="inline-flex items-center gap-1.5 text-xs font-medium text-success">
                      <Lock className="h-3 w-3" strokeWidth={2.25} aria-hidden />
                      Forced
                    </span>
                  ) : (
                    <span className="text-xs text-muted">—</span>
                  )}
                </Td>
                <Td className="text-xs text-muted">
                  {r.redirect_to ? `${r.redirect_code ?? 301} → ${r.redirect_to}` : "—"}
                </Td>
              </Tr>
            ))}
          </DataTable>
        )}
      </Card>

      {adding && (
        <AddDomainModal
          canPark={canPark}
          canAttach={canAttach}
          sites={siteList}
          onClose={() => setAdding(false)}
          onParked={(pd) => setViewing(pd)}
        />
      )}
    </div>
  );
}

// ── panel domain ─────────────────────────────────────────────────────────────

// The one domain that belongs to the installation rather than to a customer:
// the base temporary site addresses are minted under. It lives here because it
// is a domain and this is the domains page — a settings screen for a single
// field would be worse. Admin-only, and it reuses the setup endpoint rather
// than growing a second way to write the same row.
function PanelDomainCard() {
  const { data } = useSetup(true);
  const complete = useCompleteSetup();
  const [editing, setEditing] = useState(false);

  const state = data?.state;
  const base = state?.panel_domain ?? "";
  const ipv4 = state?.panel_ipv4 ?? "";

  return (
    <Card>
      <CardHeader
        title="Panel domain"
        description="The base used for temporary website addresses, so a site can go up before you own a domain."
        actions={
          !editing && (
            <Button variant="ghost" size="sm" onClick={() => setEditing(true)}>
              {base ? "Change" : "Set domain"}
            </Button>
          )
        }
      />
      <div className="p-4">
        {editing ? (
          <PanelDomainForm
            initialDomain={base}
            initialIPv4={ipv4}
            pending={complete.isPending}
            error={complete.error instanceof ApiRequestError ? complete.error : null}
            onCancel={() => setEditing(false)}
            onSave={(panel_domain, panel_ipv4) => {
              // The setup endpoint takes the whole selection, so the current
              // stack choices ride along unchanged — sending a partial would
              // reset the webserver and database engine to empty.
              if (!state) return;
              complete.mutate(
                {
                  webserver: state.webserver!,
                  db_engine: state.db_engine!,
                  manage_dns: state.manage_dns ?? false,
                  create_mail: state.create_mail ?? false,
                  license_key: state.license_key ?? "",
                  panel_domain,
                  panel_ipv4,
                },
                {
                  onSuccess: () => {
                    setEditing(false);
                    toast.success(panel_domain ? "Panel domain saved" : "Panel domain cleared");
                  },
                },
              );
            }}
          />
        ) : base ? (
          <div className="space-y-2 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <code className="font-mono text-xs text-fg">{base}</code>
              <Badge tone="brand">temporary addresses on</Badge>
            </div>
            <p className="text-xs leading-relaxed text-muted">
              New sites can take a <code className="font-mono">site-xxxxxx.{base}</code> address. For those to
              resolve, <code className="font-mono">*.{base}</code> must have an <code className="font-mono">A</code>{" "}
              record pointing at this server
              {ipv4 ? (
                <>
                  {" "}
                  (<code className="font-mono">{ipv4}</code> — created automatically if this domain is a zone managed
                  here).
                </>
              ) : (
                <> — add it wherever this domain's DNS lives.</>
              )}
            </p>
          </div>
        ) : (
          <p className="text-sm text-muted">
            Not set. Without one the panel offers no temporary addresses, and every website needs a domain you own.
          </p>
        )}
      </div>
    </Card>
  );
}

function PanelDomainForm({
  initialDomain,
  initialIPv4,
  pending,
  error,
  onSave,
  onCancel,
}: {
  initialDomain: string;
  initialIPv4: string;
  pending: boolean;
  error: ApiRequestError | null;
  onSave: (domain: string, ipv4: string) => void;
  onCancel: () => void;
}) {
  const [domain, setDomain] = useState(initialDomain);
  const [ipv4, setIPv4] = useState(initialIPv4);
  const fieldError = (f: string) => error?.fields?.find((x) => x.field === f)?.message;

  return (
    <form
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault();
        onSave(domain.trim(), ipv4.trim());
      }}
    >
      <Field label="Domain" hint={fieldError("panel_domain") ?? "Leave empty to turn temporary addresses off."}>
        <Input autoFocus value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="panel.example.com" />
      </Field>
      <Field
        label="This server's IPv4 (optional)"
        hint={fieldError("panel_ipv4") ?? "Only used to create the wildcard record automatically, when the domain is a zone managed here."}
      >
        <Input value={ipv4} onChange={(e) => setIPv4(e.target.value)} placeholder="203.0.113.10" />
      </Field>
      {error && !error.fields?.length && <Alert>{error.message}</Alert>}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" size="sm" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" size="sm" loading={pending}>
          Save
        </Button>
      </div>
    </form>
  );
}

// ── add domain (park or addon) ───────────────────────────────────────────────

// AddDomainModal is the single entry point for getting a new domain into the
// panel: a hostname, and what it's for. "Park it" proves ownership via DNS
// with no site required (feeds the free-domain picker at site creation);
// picking a site attaches it there immediately as an addon domain (an alias
// serving that site, added via the same endpoint as the site's own Domains
// tab). One popup instead of two separate add flows.
function AddDomainModal({
  canPark,
  canAttach,
  sites,
  onClose,
  onParked,
}: {
  canPark: boolean;
  canAttach: boolean;
  sites: Site[];
  onClose: () => void;
  onParked: (pd: ParkedDomain) => void;
}) {
  const park = usePark();
  const attach = useAttachDomain();
  const [fqdn, setFqdn] = useState("");
  const [target, setTarget] = useState(canPark ? "park" : (sites[0]?.uid ?? ""));

  const busy = park.isPending || attach.isPending;
  const err = park.error ?? attach.error;

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (target === "park") {
      park.mutate(fqdn, {
        onSuccess: (pd) => {
          onClose();
          onParked(pd);
          toast.success("Domain parked — verify ownership to finish.");
        },
      });
    } else {
      attach.mutate(
        { uid: target, fqdn },
        {
          onSuccess: () => {
            onClose();
            toast.success("Domain added to site");
          },
        },
      );
    }
  };

  return (
    <Modal title="Add domain" onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <Field label="Domain">
          <Input autoFocus value={fqdn} onChange={(e) => setFqdn(e.target.value)} placeholder="acme.com" />
        </Field>
        <Field
          label="What is this domain for?"
          hint={
            target === "park"
              ? "Prove ownership via DNS now; use it for a website whenever you're ready."
              : "Serves this website immediately, as an addon domain."
          }
        >
          <Select value={target} onChange={(e) => setTarget(e.target.value)}>
            {canPark && <option value="park">Park it (no website yet)</option>}
            {canAttach &&
              sites.map((s) => (
                <option key={s.uid} value={s.uid}>
                  Addon domain for {s.name}
                </option>
              ))}
          </Select>
        </Field>
        {err instanceof ApiRequestError && <Alert>{err.message}</Alert>}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={busy} disabled={!fqdn.trim() || !target}>
            {target === "park" ? "Park domain" : "Add domain"}
          </Button>
        </div>
      </form>
    </Modal>
  );
}

// ── parked domains ───────────────────────────────────────────────────────────

function ParkedDomainsSection({
  canWrite,
  viewing,
  onView,
}: {
  canWrite: boolean;
  viewing: ParkedDomain | null;
  onView: (pd: ParkedDomain | null) => void;
}) {
  const { data, isLoading, error } = useParkedDomains();
  const del = useDeleteParked();

  return (
    <Card className="overflow-hidden">
      <CardHeader
        title="Parked domains"
        description="Registered here with no website yet. Verify ownership via DNS and it becomes a free domain you can pick when creating a site — no warning, because ownership is already proven."
      />

      {error ? (
        <div className="p-4">
          <Alert>Could not load parked domains.</Alert>
        </div>
      ) : isLoading ? (
        <div className="divide-y divide-border">
          {[0, 1].map((i) => (
            <div key={i} className="flex items-center gap-4 px-4 py-3">
              <Skeleton className="h-3.5 w-48" />
              <Skeleton className="h-3.5 w-20" />
            </div>
          ))}
        </div>
      ) : !data || data.length === 0 ? (
        <EmptyState
          icon={Link2}
          title="No parked domains"
          hint="Park a domain to prove ownership before you build a site on it."
        />
      ) : (
        <DataTable head={["Domain", "Ownership", "Availability", { label: "", align: "right" }]}>
          {data.map((pd) => {
            const verified = pd.status === "verified";
            return (
              <Tr key={pd.uid}>
                <Td className="font-mono text-xs">{pd.fqdn}</Td>
                <Td>
                  <span
                    className={cn(
                      "inline-flex items-center gap-1.5 text-xs font-medium",
                      verified ? "text-success" : "text-warning",
                    )}
                  >
                    {verified ? (
                      <CheckCircle2 className="h-3.5 w-3.5" strokeWidth={2} aria-hidden />
                    ) : (
                      <Clock className="h-3.5 w-3.5" strokeWidth={2} aria-hidden />
                    )}
                    {verified ? "Verified" : "Unverified"}
                  </span>
                </Td>
                <Td>
                  <Badge tone={pd.attached ? "neutral" : "success"}>{pd.attached ? "In use" : "Free"}</Badge>
                </Td>
                <Td align="right">
                  <div className="flex justify-end gap-2">
                    <Button variant="ghost" size="sm" onClick={() => onView(pd)}>
                      {verified ? "Details" : "Verify"}
                    </Button>
                    {canWrite && (
                      <Button
                        variant="ghost"
                        size="sm"
                        className="text-danger"
                        disabled={pd.attached}
                        title={pd.attached ? "Remove this domain from its site first" : "Unpark this domain"}
                        loading={del.isPending}
                        onClick={() =>
                          del.mutate(pd.uid, {
                            onSuccess: () => toast.success("Domain unparked"),
                            onError: (e) =>
                              toast.error("Could not remove", e instanceof ApiRequestError ? e.message : undefined),
                          })
                        }
                      >
                        <Trash2 className="h-3.5 w-3.5" strokeWidth={2} aria-hidden />
                        Remove
                      </Button>
                    )}
                  </div>
                </Td>
              </Tr>
            );
          })}
        </DataTable>
      )}

      {viewing && <ParkedDomainDetailModal domain={viewing} canWrite={canWrite} onClose={() => onView(null)} />}
    </Card>
  );
}

// ParkedDomainDetailModal shows the DNS challenge and lets the operator
// re-check it. Used both right after parking and from the list's Verify/
// Details action.
function ParkedDomainDetailModal({
  domain,
  canWrite,
  onClose,
}: {
  domain: ParkedDomain;
  canWrite: boolean;
  onClose: () => void;
}) {
  const verify = useVerifyParked();
  const [current, setCurrent] = useState(domain);

  return (
    <Modal title={current.fqdn} onClose={onClose}>
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <span
            className={`h-1.5 w-1.5 rounded-full ${current.status === "verified" ? "bg-success" : "bg-warning"}`}
          />
          <span className="text-sm text-muted">{current.status === "verified" ? "Ownership verified" : "Not verified yet"}</span>
        </div>

        {current.status !== "verified" && (
          <>
            <p className="text-sm text-muted">
              Add this TXT record wherever this domain's DNS is managed, then verify:
            </p>
            <div className="space-y-2 rounded-lg border border-border bg-surface p-3 text-xs">
              <div>
                <div className="text-muted">Name</div>
                <code className="break-all text-fg">{current.challenge_name}</code>
              </div>
              <div>
                <div className="text-muted">Type</div>
                <code className="text-fg">TXT</code>
              </div>
              <div>
                <div className="text-muted">Value</div>
                <code className="break-all text-fg">{current.challenge_value}</code>
              </div>
            </div>
          </>
        )}

        <p className="text-xs text-muted">{current.wildcard_hint}</p>

        {verify.error instanceof ApiRequestError && <Alert>{verify.error.message}</Alert>}

        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            Close
          </Button>
          {canWrite && current.status !== "verified" && (
            <Button
              loading={verify.isPending}
              onClick={() =>
                verify.mutate(current.uid, {
                  onSuccess: (pd) => {
                    setCurrent(pd);
                    if (pd.status === "verified") toast.success("Domain verified");
                  },
                  onError: (e) =>
                    toast.error("Not verified yet", e instanceof ApiRequestError ? e.message : "The DNS record was not found."),
                })
              }
            >
              Verify now
            </Button>
          )}
        </div>
      </div>
    </Modal>
  );
}
