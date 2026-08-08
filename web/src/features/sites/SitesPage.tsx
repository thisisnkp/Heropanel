import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQueries, useQueryClient } from "@tanstack/react-query";
import {
  Binary,
  ChevronDown,
  Database as DatabaseIcon,
  FileCode,
  GitBranch,
  Globe,
  LayoutTemplate,
  Link2,
  type LucideIcon,
  Search,
  Sparkles,
} from "lucide-react";
import { api, ApiRequestError, can, type Domain, type Site } from "@/lib/api";
import {
  Alert,
  Badge,
  Button,
  Card,
  DataTable,
  EmptyState,
  Field,
  Input,
  Modal,
  PageHeader,
  Skeleton,
  Stat,
  StatusBadge,
  Td,
  Tr,
  cn,
} from "@/components/ui";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { toast } from "@/stores/toast";
import { useJobs } from "@/stores/jobs";
import { useMe } from "@/features/auth/auth";
import { useZones } from "@/features/dns/dns";
import { useDatabases } from "@/features/databases/databases";
import { useFreeDomains } from "@/features/domains/domains";
import { DeployAppModal } from "./DeployAppModal";
import { isJobResult, useCreateSite, useSites, type CreateSiteInput } from "./sites";

type ActiveModal = "static" | "git-web" | "git-binary" | null;

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
                  form.type === t ? "border-brand bg-brand-subtle text-fg" : "border-border text-muted hover:text-fg",
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
      <Button variant="ghost" size="sm" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        Tools
        <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", open && "rotate-180")} strokeWidth={2} aria-hidden />
      </Button>
      {open && (
        <div
          role="menu"
          className="hp-pop-in absolute right-0 z-20 mt-1 w-44 overflow-hidden rounded-lg border border-border bg-panel py-1 shadow-lg"
        >
          {items.map((it) => (
            <button
              key={it.label}
              type="button"
              role="menuitem"
              onClick={() => go(it.to)}
              className="block w-full px-3 py-1.5 text-left text-[13px] text-fg transition-colors hover:bg-panel-hover"
            >
              {it.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// AddWebsiteMenu is the single entry point for every way to get a new
// website onto the panel — a plain site, or a guided deploy that chains
// site-create with the existing Git/Runtime setup (see DeployAppModal).
// "Coming soon" entries are visible (matching the full intended menu) but
// inert, since those two paths don't exist yet.
function AddWebsiteMenu({ onSelect }: { onSelect: (m: ActiveModal) => void }) {
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

  // A `to` opens a guided page; a `modal` opens a dialog in place. The plain
  // site path earns a page because its domain field needs the room (see
  // NewSitePage); the Git flows stay dialogs.
  const items: { label: string; hint: string; icon: LucideIcon; modal?: ActiveModal; to?: string }[] = [
    { label: "Custom PHP/Static Website", hint: "A plain site — files you manage yourself.", icon: FileCode, to: "/sites/new" },
    { label: "Deploy App", hint: "Node.js, Python, or PHP from GitHub.", icon: GitBranch, modal: "git-web" },
    { label: "Deploy Single Binary Proxy App", hint: "Go, C++, or Rust from GitHub.", icon: Binary, modal: "git-binary" },
    { label: "Build App From Hero Web Builder", hint: "Visual page builder.", icon: LayoutTemplate },
    { label: "Create WordPress Website", hint: "One-click WordPress stack.", icon: Sparkles },
  ];

  return (
    <div className="relative" ref={ref}>
      <Button aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        Add website
        <ChevronDown className={cn("h-4 w-4 transition-transform", open && "rotate-180")} strokeWidth={2} aria-hidden />
      </Button>
      {open && (
        <div
          role="menu"
          className="hp-pop-in absolute right-0 z-20 mt-1.5 w-[20rem] overflow-hidden rounded-xl border border-border bg-panel p-1 shadow-lg"
        >
          {items.map((it) => {
            const Icon = it.icon;
            const enabled = !!it.modal || !!it.to;
            return (
              <button
                key={it.label}
                type="button"
                role="menuitem"
                disabled={!enabled}
                onClick={() => {
                  if (!enabled) return;
                  setOpen(false);
                  if (it.to) navigate(it.to);
                  else if (it.modal) onSelect(it.modal);
                }}
                className={cn(
                  "flex w-full items-start gap-3 rounded-lg px-2.5 py-2 text-left transition-colors",
                  enabled ? "hover:bg-panel-hover" : "cursor-not-allowed opacity-55",
                )}
              >
                <span
                  className={cn(
                    "mt-0.5 grid h-8 w-8 shrink-0 place-items-center rounded-lg border",
                    enabled ? "border-brand-border bg-brand-subtle text-brand" : "border-border bg-surface-2 text-muted",
                  )}
                >
                  <Icon className="h-4 w-4" strokeWidth={1.75} aria-hidden />
                </span>
                <span className="min-w-0">
                  <span className="flex flex-wrap items-center gap-1.5">
                    <span className="text-[13px] font-medium text-fg">{it.label}</span>
                    {!enabled && <Badge>Coming soon</Badge>}
                  </span>
                  <span className="mt-0.5 block text-xs leading-relaxed text-muted">{it.hint}</span>
                </span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

// SiteRow is one row of the websites table. The domain leads because it is what
// an operator recognises a site by; the human name sits under it as the
// secondary identifier rather than competing for the same line.
function SiteRow({ site }: { site: Site }) {
  const navigate = useNavigate();
  const open = () => navigate(`/sites/${site.uid}`);
  return (
    <Tr>
      <Td className="max-w-0">
        <button type="button" onClick={open} className="flex min-w-0 items-center gap-3 text-left">
          <span className="grid h-9 w-9 shrink-0 place-items-center rounded-lg border border-border bg-surface-2 text-muted">
            <Globe className="h-4 w-4" strokeWidth={1.75} aria-hidden />
          </span>
          <span className="min-w-0">
            <span className="block truncate font-medium text-fg hover:text-brand">
              {site.primary_domain || site.name}
            </span>
            <span className="mt-0.5 block truncate text-xs text-muted">{site.name}</span>
          </span>
        </button>
      </Td>
      <Td>
        <Badge>{site.type}</Badge>
      </Td>
      <Td>
        <StatusBadge status={site.status} />
      </Td>
      <Td align="right">
        <div className="flex justify-end gap-2">
          <ToolsMenu uid={site.uid} />
          <Button size="sm" onClick={open}>
            Manage
          </Button>
        </div>
      </Td>
    </Tr>
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
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <Stat label="Websites" value={sites.length} icon={Globe} tone="brand" />
      <Stat
        label="Domains"
        value={connectedCount}
        hint="connected to a website"
        icon={Link2}
        loading={domainsLoading}
      />
      <Stat
        label="Free domains"
        value={canDNS ? freeDomains : "—"}
        hint={canDNS ? "not linked to any website" : "needs DNS access"}
        icon={Sparkles}
        tone={canDNS && freeDomains > 0 ? "success" : "neutral"}
        loading={canDNS && zones.isLoading}
      />
      <Stat
        label="Databases"
        value={canDB ? (databases.data?.length ?? 0) : "—"}
        hint={canDB ? undefined : "needs database access"}
        icon={DatabaseIcon}
        loading={canDB && databases.isLoading}
      />
    </div>
  );
}

export function SitesPage() {
  const qc = useQueryClient();
  const { data, isLoading, error } = useSites();
  const { data: me } = useMe();
  const [activeModal, setActiveModal] = useState<ActiveModal>(null);
  const [query, setQuery] = useState("");
  // The command palette's "Create a website" navigates here with ?new=1 —
  // it opens the plain static/PHP form directly, not the full menu.
  const [params, setParams] = useSearchParams();

  useEffect(() => {
    if (params.get("new") === "1") {
      setActiveModal("static");
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
      <PageHeader
        title="Websites"
        description="Isolated sites with a dedicated user, PHP pool, and web server config."
        actions={<AddWebsiteMenu onSelect={setActiveModal} />}
      />

      {error && <Alert>You do not have permission to view sites, or the request failed.</Alert>}

      {isLoading ? (
        <SitesSkeleton />
      ) : (
        data && (
          <>
            {/* The summary is a widget: if it crashes (a bad metric, a shape
                change), the website list below must still render. */}
            <ErrorBoundary compact title="Summary">
              <SummaryCards sites={sites} me={me} />
            </ErrorBoundary>

            <Card className="overflow-hidden">
              <div className="border-b border-border p-3">
                <div className="relative max-w-sm">
                  <Search
                    className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted"
                    strokeWidth={2}
                    aria-hidden
                  />
                  <Input
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    placeholder="Search by domain or name"
                    aria-label="Search websites"
                    className="pl-9"
                  />
                </div>
              </div>
              {sites.length === 0 ? (
                <EmptyState
                  icon={Globe}
                  title="No websites yet"
                  hint="Create your first website — a dedicated user, isolated files, and a web server vhost."
                  action={<AddWebsiteMenu onSelect={setActiveModal} />}
                />
              ) : filtered.length === 0 ? (
                <EmptyState icon={Search} title="No websites match" hint="Clear the search to see all websites." />
              ) : (
                <DataTable head={["Website", "Type", "Status", { label: "", align: "right" }]}>
                  {filtered.map((s) => (
                    <SiteRow key={s.uid} site={s} />
                  ))}
                </DataTable>
              )}
            </Card>
          </>
        )
      )}

      {activeModal === "static" && <CreateSiteModal onClose={() => setActiveModal(null)} onSync={refresh} />}
      {(activeModal === "git-web" || activeModal === "git-binary") && (
        <DeployAppModal flavor={activeModal} onClose={() => setActiveModal(null)} />
      )}
    </div>
  );
}

// The loading view mirrors the loaded one — four stat tiles over a table — so
// the page does not jump when the data lands. A centred spinner would reserve
// none of that space and every element would shift.
function SitesSkeleton() {
  return (
    <div className="space-y-6">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <Card key={i} className="flex items-start gap-3 p-4">
            <Skeleton className="h-8 w-8 shrink-0 rounded-lg" />
            <div className="min-w-0 flex-1 space-y-2">
              <Skeleton className="h-2.5 w-16" />
              <Skeleton className="h-5 w-10" />
            </div>
          </Card>
        ))}
      </div>
      <Card className="divide-y divide-border">
        {[0, 1, 2].map((i) => (
          <div key={i} className="flex items-center gap-3 p-4">
            <Skeleton className="h-9 w-9 shrink-0 rounded-lg" />
            <div className="flex-1 space-y-2">
              <Skeleton className="h-3.5 w-48" />
              <Skeleton className="h-2.5 w-24" />
            </div>
            <Skeleton className="h-8 w-20 shrink-0 rounded-md" />
          </div>
        ))}
      </Card>
    </div>
  );
}
