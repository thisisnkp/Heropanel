import { useState } from "react";
import { ApiRequestError } from "@/lib/api";
import { Alert, Badge, Button, Card, EmptyState, Spinner, StatusBadge } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useDNSSEC, useZoneRecords, useZones } from "@/features/dns/dns";
import type { DNSZone } from "@/lib/api";

// NameserversPage answers the question a registrar asks: which nameservers serve
// this zone, and — when DNSSEC is on — what DS record proves it. Everything here
// is read from the panel's own authoritative zones (the NS records at the apex
// and the DNSSEC status), so it is the delegation information to paste into a
// registrar, gathered in one place instead of dug out of each zone's records.

function copy(text: string) {
  void navigator.clipboard?.writeText(text);
  toast.success("Copied");
}

export function NameserversPage() {
  const zones = useZones();
  const list = zones.data ?? [];

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold text-fg">Nameserver management</h1>
        <p className="text-sm text-muted">
          The nameservers serving each authoritative zone, and the DS record to hand your registrar when DNSSEC is on.
        </p>
      </div>

      {zones.error && (
        <Alert>
          {zones.error instanceof ApiRequestError && zones.error.status === 403
            ? "You do not have permission to view DNS zones."
            : "Could not load DNS zones."}
        </Alert>
      )}

      {zones.isLoading ? (
        <Spinner />
      ) : list.length === 0 ? (
        <Card className="overflow-hidden">
          <EmptyState title="No zones yet" hint="Create a DNS zone and its nameservers appear here." />
        </Card>
      ) : (
        <div className="space-y-3">
          {list.map((z) => (
            <ZoneCard key={z.uid} zone={z} />
          ))}
        </div>
      )}
    </div>
  );
}

function ZoneCard({ zone }: { zone: DNSZone }) {
  const [open, setOpen] = useState(false);
  return (
    <Card className="overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left"
      >
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="truncate font-medium text-fg">{zone.name}</span>
            <StatusBadge status={zone.status} />
            {zone.dnssec_enabled && <Badge>DNSSEC</Badge>}
          </div>
          <div className="mt-0.5 truncate font-mono text-xs text-muted">primary ns: {zone.primary_ns}</div>
        </div>
        <span className="shrink-0 text-xs text-brand">{open ? "Hide" : "View delegation"}</span>
      </button>
      {open && <ZoneDelegation zone={zone} />}
    </Card>
  );
}

function ZoneDelegation({ zone }: { zone: DNSZone }) {
  const records = useZoneRecords(zone.uid);
  const dnssec = useDNSSEC(zone.dnssec_enabled ? zone.uid : null);

  // The nameservers are the NS records at the zone apex ("@" or the bare zone
  // name); records deeper in the tree are delegations of sub-zones, not this
  // zone's own nameservers.
  const nsRecords = (records.data ?? []).filter(
    (r) => r.type === "NS" && (r.name === "@" || r.name === "" || r.name === zone.name || r.name === zone.name + "."),
  );

  return (
    <div className="space-y-4 border-t border-border/60 px-4 py-3">
      <section>
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted">Nameservers</h3>
        {records.isLoading ? (
          <div className="mt-2"><Spinner /></div>
        ) : nsRecords.length === 0 ? (
          <p className="mt-1 text-xs text-muted">
            No apex NS records found. The zone's primary nameserver is{" "}
            <span className="font-mono">{zone.primary_ns}</span>.
          </p>
        ) : (
          <ul className="mt-2 space-y-1">
            {nsRecords.map((r) => (
              <li key={r.uid} className="flex items-center justify-between gap-2">
                <span className="font-mono text-xs text-fg">{r.content}</span>
                <Button variant="ghost" className="h-6 px-2 text-xs" onClick={() => copy(r.content)}>
                  Copy
                </Button>
              </li>
            ))}
          </ul>
        )}
        <p className="mt-2 text-xs text-muted">Set these as the domain's nameservers at your registrar.</p>
      </section>

      {zone.dnssec_enabled && (
        <section>
          <h3 className="text-xs font-semibold uppercase tracking-wide text-muted">DS record (for the registrar)</h3>
          {dnssec.isLoading ? (
            <div className="mt-2"><Spinner /></div>
          ) : !dnssec.data?.signed ? (
            <p className="mt-1 text-xs text-muted">Signing in progress — the DS record appears once BIND finishes.</p>
          ) : dnssec.data.ds.length === 0 ? (
            <p className="mt-1 text-xs text-muted">No DS record available.</p>
          ) : (
            <ul className="mt-2 space-y-1">
              {dnssec.data.ds.map((ds) => (
                <li key={ds} className="flex items-start justify-between gap-2">
                  <span className="break-all font-mono text-xs text-fg">{ds}</span>
                  <Button variant="ghost" className="h-6 shrink-0 px-2 text-xs" onClick={() => copy(ds)}>
                    Copy
                  </Button>
                </li>
              ))}
            </ul>
          )}
          <p className="mt-2 text-xs text-muted">
            Add this DS record at your registrar to complete the DNSSEC chain of trust.
          </p>
        </section>
      )}
    </div>
  );
}
