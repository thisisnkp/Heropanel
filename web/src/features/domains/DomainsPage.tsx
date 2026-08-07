import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query";
import { api, ApiRequestError, can, type Domain, type ParkedDomain, type Site } from "@/lib/api";
import { Alert, Badge, Button, Card, EmptyState, Field, Input, Modal, Select, Spinner, StatusBadge } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useMe } from "@/features/auth/auth";
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
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-fg">Domains</h1>
          <p className="text-sm text-muted">Every domain you've parked or attached to a website.</p>
        </div>
        {canShowAdd && <Button onClick={() => setAdding(true)}>Add domain</Button>}
      </div>

      {can(me, "domain.read") && <ParkedDomainsSection canWrite={canPark} viewing={viewing} onView={setViewing} />}

      <div>
        <h2 className="text-lg font-semibold text-fg">Attached domains</h2>
        <p className="text-sm text-muted">Every domain already serving a website. Manage each on its site.</p>
      </div>

      {sites.error && (
        <Alert>
          {sites.error instanceof ApiRequestError && sites.error.status === 403
            ? "You do not have permission to view domains."
            : "Could not load sites."}
        </Alert>
      )}

      {sites.isLoading ? (
        <Spinner />
      ) : (
        <Card className="overflow-hidden">
          <div className="border-b border-border p-3">
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Filter by domain, type, or site…"
              aria-label="Filter domains"
            />
          </div>
          {siteList.length === 0 ? (
            <EmptyState title="No sites yet" hint="Create a website and its domains appear here." />
          ) : rows.length === 0 && !loadingDomains ? (
            <EmptyState title="No domains match" hint="Clear the filter to see all domains." />
          ) : (
            <table className="w-full text-sm">
              <thead className="border-b border-border text-left text-muted">
                <tr>
                  <th className="px-4 py-3 font-medium">Domain</th>
                  <th className="px-4 py-3 font-medium">Type</th>
                  <th className="px-4 py-3 font-medium">Site</th>
                  <th className="px-4 py-3 font-medium">HTTPS</th>
                  <th className="px-4 py-3 font-medium">Redirect</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={`${r.site.uid}-${r.uid}`} className="border-b border-border/60 last:border-0">
                    <td className="px-4 py-3 font-mono text-xs text-fg">{r.fqdn}</td>
                    <td className="px-4 py-3">
                      <Badge>{r.kind}</Badge>
                    </td>
                    <td className="px-4 py-3">
                      <button
                        type="button"
                        onClick={() => navigate(`/sites/${r.site.uid}`)}
                        className="text-brand hover:underline"
                      >
                        {r.site.name}
                      </button>
                    </td>
                    <td className="px-4 py-3">
                      {r.force_https ? <StatusBadge status="active" /> : <span className="text-xs text-muted">—</span>}
                    </td>
                    <td className="px-4 py-3 text-xs text-muted">
                      {r.redirect_to ? `${r.redirect_code ?? 301} → ${r.redirect_to}` : "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          {loadingDomains && (
            <div className="flex items-center gap-2 p-4 text-xs text-muted">
              <Spinner /> Loading domains…
            </div>
          )}
        </Card>
      )}

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
    <Card className="space-y-3 p-5">
      <div>
        <h2 className="text-lg font-semibold text-fg">Parked domains</h2>
        <p className="text-sm text-muted">
          Registered here with no website yet. Verify ownership via DNS and it becomes a free domain you can pick
          when creating a site — no warning, because ownership is already proven.
        </p>
      </div>

      {error && <Alert>Could not load parked domains.</Alert>}
      {isLoading ? (
        <Spinner />
      ) : !data || data.length === 0 ? (
        <EmptyState title="No parked domains" hint="Park a domain to prove ownership before you build a site on it." />
      ) : (
        <table className="w-full text-sm">
          <thead className="border-b border-border text-left text-muted">
            <tr>
              <th className="px-2 py-2 font-medium">Domain</th>
              <th className="px-2 py-2 font-medium">Status</th>
              <th className="px-2 py-2 font-medium">Site</th>
              <th className="px-2 py-2" />
            </tr>
          </thead>
          <tbody>
            {data.map((pd) => (
              <tr key={pd.uid} className="border-b border-border/60 last:border-0">
                <td className="px-2 py-2 font-mono text-xs text-fg">{pd.fqdn}</td>
                <td className="px-2 py-2">
                  <span className={pd.status === "verified" ? "text-xs font-medium text-emerald-500" : "text-xs font-medium text-amber-500"}>
                    {pd.status === "verified" ? "Verified" : "Unverified"}
                  </span>
                </td>
                <td className="px-2 py-2 text-xs text-muted">{pd.attached ? "In use" : "Free"}</td>
                <td className="px-2 py-2 text-right">
                  <div className="flex justify-end gap-2">
                    <Button variant="ghost" className="h-8 px-2" onClick={() => onView(pd)}>
                      {pd.status === "verified" ? "Details" : "Verify"}
                    </Button>
                    {canWrite && (
                      <Button
                        variant="ghost"
                        className="h-8 px-2 text-danger"
                        disabled={pd.attached}
                        title={pd.attached ? "Remove this domain from its site first" : undefined}
                        loading={del.isPending}
                        onClick={() =>
                          del.mutate(pd.uid, {
                            onSuccess: () => toast.success("Domain unparked"),
                            onError: (e) => toast.error("Could not remove", e instanceof ApiRequestError ? e.message : undefined),
                          })
                        }
                      >
                        Remove
                      </Button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
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
            className={`h-1.5 w-1.5 rounded-full ${current.status === "verified" ? "bg-emerald-500" : "bg-amber-500"}`}
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
