import { Alert, Badge, Button, Card, EmptyState, Spinner, StatusBadge } from "@/components/ui";
import { toast } from "@/stores/toast";
import { can } from "@/lib/api";
import { useMe } from "@/features/auth/auth";
import {
  useCatalog,
  useInstallModule,
  useSetModuleEnabled,
  useUninstallModule,
  type CatalogEntry,
} from "./marketplace";

// MarketplacePage browses the signed module catalog. Every entry shows a trust
// verdict — a module is installable only when a pinned publisher key vouched for
// its manifest; the rest are shown with the reason they cannot be trusted, never
// hidden, so an operator can tell a safe module from a tampered or unknown one.
export function MarketplacePage() {
  const me = useMe();
  const canManage = can(me.data, "module.manage");
  const catalog = useCatalog();

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-fg">Marketplace</h1>
        <p className="text-sm text-muted">
          Install modules signed by a trusted publisher. A module runs only after a pinned key has verified its
          manifest.
        </p>
      </div>

      {catalog.error && <Alert>You do not have permission to view the marketplace.</Alert>}
      {catalog.isLoading && <Spinner />}

      {catalog.data && !catalog.data.trust_anchored && (
        <Alert>
          No publisher key is pinned, so nothing can be installed. Set <span className="font-mono">marketplace.keys</span>{" "}
          (or <span className="font-mono">HP_MARKETPLACE_KEYS</span>) to the ed25519 public key of a publisher you trust.
        </Alert>
      )}

      {catalog.data &&
        (catalog.data.modules.length === 0 ? (
          <EmptyState
            title="No modules offered"
            hint="Point marketplace.catalog at a module feed to populate this list."
          />
        ) : (
          <div className="grid gap-4 sm:grid-cols-2">
            {catalog.data.modules.map((m) => (
              <ModuleCard key={m.slug} m={m} canManage={canManage} />
            ))}
          </div>
        ))}
    </div>
  );
}

function ModuleCard({ m, canManage }: { m: CatalogEntry; canManage: boolean }) {
  const install = useInstallModule();
  const setEnabled = useSetModuleEnabled();
  const uninstall = useUninstallModule();
  const busy = install.isPending || setEnabled.isPending || uninstall.isPending;

  return (
    <Card className="flex flex-col gap-3 p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <h2 className="truncate text-sm font-semibold text-fg">{m.name || m.slug}</h2>
            <span className="text-xs text-muted">{m.version}</span>
          </div>
          <p className="mt-0.5 text-xs text-muted">{m.description || m.slug}</p>
        </div>
        <span
          className={
            "shrink-0 rounded-full px-2 py-0.5 text-xs font-medium " +
            (m.verified
              ? "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400"
              : "bg-amber-500/15 text-amber-700 dark:text-amber-400")
          }
        >
          {m.verified ? "Verified" : "Unverified"}
        </span>
      </div>

      <div className="flex flex-wrap gap-1">
        {m.category && <Badge>{m.category}</Badge>}
        {m.capabilities.map((c) => (
          <Badge key={c}>{c}</Badge>
        ))}
        {m.installed && <StatusBadge status={m.state || "installed"} />}
      </div>

      {m.verified ? (
        <p className="text-xs text-muted">
          Signed by publisher <span className="font-mono">{m.publisher_key}</span>.
        </p>
      ) : (
        <p className="text-xs text-amber-700 dark:text-amber-400">{m.verify_error || "Not signed by a trusted key."}</p>
      )}

      {m.requires_broker.length > 0 && (
        <p className="text-xs text-muted">
          Requests privileged operations: <span className="font-mono">{m.requires_broker.join(", ")}</span>
        </p>
      )}

      {canManage && (
        <div className="mt-auto flex flex-wrap gap-2 pt-1">
          {!m.installed ? (
            <Button
              className="h-8 px-3"
              disabled={!m.verified || busy}
              loading={install.isPending}
              onClick={() =>
                install.mutate(m.slug, {
                  onSuccess: () => toast.success(`Installed ${m.name || m.slug}`),
                  onError: (e) => toast.error(e.message),
                })
              }
            >
              Install
            </Button>
          ) : (
            <>
              <Button
                variant="ghost"
                className="h-8 px-3"
                disabled={busy}
                loading={setEnabled.isPending}
                onClick={() =>
                  setEnabled.mutate(
                    { slug: m.slug, enabled: m.state !== "enabled" },
                    { onError: (e) => toast.error(e.message) },
                  )
                }
              >
                {m.state === "enabled" ? "Disable" : "Enable"}
              </Button>
              <Button
                variant="ghost"
                className="h-8 px-3 text-danger"
                disabled={busy}
                onClick={() => {
                  if (!confirm(`Uninstall ${m.name || m.slug}?`)) return;
                  uninstall.mutate(m.slug, {
                    onSuccess: () => toast.success("Uninstalled"),
                    onError: (e) => toast.error(e.message),
                  });
                }}
              >
                Uninstall
              </Button>
            </>
          )}
        </div>
      )}
    </Card>
  );
}
