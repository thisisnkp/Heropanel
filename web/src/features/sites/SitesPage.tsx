import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQueries, useQueryClient } from "@tanstack/react-query";
import { api, ApiRequestError, can, type Domain, type Site } from "@/lib/api";
import { Alert, Badge, Button, Card, EmptyState, Field, Input, Modal, Spinner, StatusBadge, cn } from "@/components/ui";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { toast } from "@/stores/toast";
import { useJobs } from "@/stores/jobs";
import { useMe } from "@/features/auth/auth";
import { useZones } from "@/features/dns/dns";
import { useDatabases } from "@/features/databases/databases";
import { useFreeDomains } from "@/features/domains/domains";
import { isJobResult, useCreateSite, useSites, type CreateSiteInput } from "./sites";

const FREE_DOMAINS_LIST_ID = "create-site-free-domains";

function CreateSiteModal({ onClose, onSync }: { onClose: () => void; onSync: () => void }) {
  const [form, setForm] = useState<CreateSiteInput>({ name: "", primary_domain: "", type: "static" });
  const create = useCreateSite();
  const track = useJobs((s) => s.track);
  const { data: freeDomains } = useFreeDomains();

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    create.mutate(form, {
      onSuccess: (res) => {
        if (isJobResult(res)) {
          // The job tracker (stores/jobs.ts) fetches the job result on
          // completion and shows the same DNS-not-verified warning itself.
          track(res.job.id, "Provisioning site");
        } else {
          onSync();
          if (res.dns_status === "unverified") {
            toast.info(
              "Website created — DNS not verified yet",
              "Add the site's DNS records and verify it on the Domains page so ownership is proven.",
            );
          }
        }
        toast.info("Creating site…");
        onClose();
      },
    });
  };

  const fieldError = (name: string) =>
    create.error instanceof ApiRequestError ? create.error.fields?.find((f) => f.field === name)?.message : undefined;

  return (
    <Modal title="New website" onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <Field label="Name" hint={fieldError("name")}>
          <Input autoFocus value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Acme" />
        </Field>
        <Field
          label="Primary domain"
          hint={fieldError("primary_domain") ?? (freeDomains?.fqdns.length ? "Pick a verified domain, or type any domain." : undefined)}
        >
          <Input
            list={FREE_DOMAINS_LIST_ID}
            value={form.primary_domain}
            onChange={(e) => setForm({ ...form, primary_domain: e.target.value })}
            placeholder="acme.example.com"
          />
          <datalist id={FREE_DOMAINS_LIST_ID}>
            {(freeDomains?.fqdns ?? []).map((d) => (
              <option key={d} value={d} />
            ))}
          </datalist>
        </Field>
        <Field label="Type">
          <div className="flex gap-2">
            {["static", "php", "proxy"].map((t) => (
              <button
                key={t}
                type="button"
                onClick={() => setForm({ ...form, type: t })}
                className={cn(
                  "flex-1 rounded-lg border px-3 py-2 text-sm capitalize transition-colors",
                  form.type === t ? "border-brand bg-brand/10 text-fg" : "border-border text-muted hover:text-fg",
                )}
              >
                {t}
              </button>
            ))}
          </div>
        </Field>
        {create.error instanceof ApiRequestError && !create.error.fields?.length && <Alert>{create.error.message}</Alert>}
        <div className="flex justify-end gap-2 pt-1">
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

// StatCard is one figure in the summary row (websites / domains / free domains /
// databases). value is a string so a card can show "—" when the caller lacks the
// permission to compute it.
function StatCard({ label, value, hint, loading }: { label: string; value: string; hint?: string; loading?: boolean }) {
  return (
    <Card className="p-4">
      <div className="text-xs font-medium uppercase tracking-wide text-muted">{label}</div>
      <div className="mt-1 flex items-baseline gap-2">
        {loading ? <Spinner className="h-5 w-5 text-brand" /> : <span className="text-2xl font-semibold text-fg">{value}</span>}
      </div>
      {hint && <div className="mt-0.5 text-xs text-muted">{hint}</div>}
    </Card>
  );
}

// ToolsMenu is the per-site "Tools" dropdown: File Manager, Databases, Analytics.
// It closes on outside click and on Escape.
function ToolsMenu({ uid }: { uid: string }) {
  const [open, setOpen] = useState(false);
  const navigate = useNavigate();
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const go = (to: string) => {
    setOpen(false);
    navigate(to);
  };

  const items: { label: string; to: string }[] = [
    { label: "File Manager", to: `/sites/${uid}?tab=files` },
    { label: "Databases", to: `/databases` },
    { label: "Analytics", to: `/sites/${uid}?tab=logs` },
  ];

  return (
    <div className="relative" ref={ref}>
      <Button variant="ghost" className="h-9 px-3" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        Tools
        <svg viewBox="0 0 24 24" className={cn("h-4 w-4 transition-transform", open && "rotate-180")} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M6 9l6 6 6-6" />
        </svg>
      </Button>
      {open && (
        <div role="menu" className="absolute right-0 z-20 mt-1 w-44 overflow-hidden rounded-lg border border-border bg-panel py-1 shadow-lg">
          {items.map((it) => (
            <button
              key={it.label}
              type="button"
              role="menuitem"
              onClick={() => go(it.to)}
              className="block w-full px-3 py-2 text-left text-sm text-fg hover:bg-border/40"
            >
              {it.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function SiteRow({ site }: { site: Site }) {
  const navigate = useNavigate();
  return (
    <div className="flex items-center justify-between gap-3 border-b border-border/60 px-4 py-3 last:border-0">
      <button type="button" onClick={() => navigate(`/sites/${site.uid}`)} className="flex min-w-0 items-center gap-3 text-left">
        <span className="grid h-9 w-9 shrink-0 place-items-center rounded-lg border border-border text-muted">
          <svg viewBox="0 0 24 24" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M8 9l-3 3 3 3M16 9l3 3-3 3M13 6l-2 12" />
          </svg>
        </span>
        <span className="min-w-0">
          <span className="flex items-center gap-2">
            <span className="truncate font-medium text-fg">{site.primary_domain || site.name}</span>
            <Badge>{site.type}</Badge>
          </span>
          <span className="mt-0.5 flex items-center gap-2 text-xs text-muted">
            <span className="truncate">{site.name}</span>
            <StatusBadge status={site.status} />
          </span>
        </span>
      </button>
      <div className="flex shrink-0 items-center gap-2">
        <ToolsMenu uid={site.uid} />
        <Button className="h-9 px-4" onClick={() => navigate(`/sites/${site.uid}`)}>
          Dashboard
        </Button>
      </div>
    </div>
  );
}

// SummaryCards computes the figures shown above the website list: total websites,
// total domains attached across sites, "free" managed domains (DNS zones not
// connected to any site), and databases. Domain and database figures need their
// own read permission — the cards show "—" rather than erroring when the caller
// lacks it.
function SummaryCards({ sites, me }: { sites: Site[]; me: ReturnType<typeof useMe>["data"] }) {
  const canDNS = can(me, "dns.read");
  const canDB = can(me, "database.read");

  // Per-site domains (primary + aliases), in parallel, to count connected domains.
  const domainQueries = useQueries({
    queries: sites.map((s) => ({
      queryKey: ["site", s.uid, "domains"],
      queryFn: () => api.get<Domain[]>(`/sites/${s.uid}/domains`),
      staleTime: 30_000,
    })),
  });
  const zones = useZones();
  const databases = useDatabases();

  const domainsUpdatedAt = domainQueries.map((q) => q.dataUpdatedAt).join(",");
  const { connectedCount, freeDomains } = useMemo(() => {
    const connected = new Set<string>();
    sites.forEach((s, i) => {
      if (s.primary_domain) connected.add(s.primary_domain.toLowerCase());
      (domainQueries[i]?.data ?? []).forEach((d) => connected.add(d.fqdn.toLowerCase()));
    });
    const zoneList = zones.data ?? [];
    const free = zoneList.filter((z) => !connected.has(z.name.toLowerCase())).length;
    return { connectedCount: connected.size, freeDomains: free };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sites, domainsUpdatedAt, zones.dataUpdatedAt]);

  const domainsLoading = domainQueries.some((q) => q.isLoading);

  return (
    <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
      <StatCard label="Websites" value={String(sites.length)} />
      <StatCard label="Domains" value={String(connectedCount)} hint="connected to a website" loading={domainsLoading} />
      <StatCard
        label="Free domains"
        value={canDNS ? String(freeDomains) : "—"}
        hint={canDNS ? "not linked to any website" : "needs DNS access"}
        loading={canDNS && zones.isLoading}
      />
      <StatCard
        label="Databases"
        value={canDB ? String(databases.data?.length ?? 0) : "—"}
        hint={canDB ? undefined : "needs database access"}
        loading={canDB && databases.isLoading}
      />
    </div>
  );
}

export function SitesPage() {
  const qc = useQueryClient();
  const { data, isLoading, error } = useSites();
  const { data: me } = useMe();
  const [modalOpen, setModalOpen] = useState(false);
  const [query, setQuery] = useState("");
  // The command palette's "Create a website" navigates here with ?new=1.
  const [params, setParams] = useSearchParams();

  useEffect(() => {
    if (params.get("new") === "1") {
      setModalOpen(true);
      setParams({}, { replace: true });
    }
  }, [params, setParams]);

  const refresh = () => qc.invalidateQueries({ queryKey: ["sites"] });

  const sites = data ?? [];
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return sites;
    return sites.filter((s) => s.primary_domain.toLowerCase().includes(q) || s.name.toLowerCase().includes(q));
  }, [sites, query]);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-fg">Websites</h1>
          <p className="text-sm text-muted">Isolated sites with a dedicated user, PHP pool, and web server config.</p>
        </div>
        <Button onClick={() => setModalOpen(true)}>Add website</Button>
      </div>

      {isLoading && (
        <div className="flex items-center gap-2 text-muted">
          <Spinner /> Loading…
        </div>
      )}
      {error && <Alert>You do not have permission to view sites, or the request failed.</Alert>}

      {data && (
        <>
          {/* The summary is a widget: if it crashes (a bad metric, a shape
              change), the website list below must still render. */}
          <ErrorBoundary compact title="Summary">
            <SummaryCards sites={sites} me={me} />
          </ErrorBoundary>

          <Card className="overflow-hidden">
            <div className="border-b border-border p-3">
              <Input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Search by domain or name"
                aria-label="Search websites"
              />
            </div>
            {sites.length === 0 ? (
              <EmptyState
                title="No sites yet"
                hint="Create your first website — a dedicated user, isolated files, and a web server vhost."
                action={<Button onClick={() => setModalOpen(true)}>Create a website</Button>}
              />
            ) : filtered.length === 0 ? (
              <EmptyState title="No websites match" hint="Clear the search to see all websites." />
            ) : (
              filtered.map((s) => <SiteRow key={s.uid} site={s} />)
            )}
          </Card>
        </>
      )}

      {modalOpen && <CreateSiteModal onClose={() => setModalOpen(false)} onSync={refresh} />}
    </div>
  );
}
