import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQueries } from "@tanstack/react-query";
import { api, ApiRequestError, type Domain, type Site } from "@/lib/api";
import { Alert, Badge, Card, EmptyState, Input, Spinner, StatusBadge } from "@/components/ui";
import { useSites } from "@/features/sites/sites";

// DomainsPage is the panel-wide view of every domain — primary, alias, subdomain
// and redirect — across all of the caller's sites in one place, rather than one
// site at a time in each site's workspace. Domains are owned by sites (a domain
// only means anything as an entry point to a site), so each row links back to
// the site that serves it, where it is actually managed.

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
  const sites = useSites();
  const navigate = useNavigate();
  const [query, setQuery] = useState("");

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

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-fg">Domain management</h1>
        <p className="text-sm text-muted">Every domain pointing at a site on this panel. Manage each on its site.</p>
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
    </div>
  );
}
