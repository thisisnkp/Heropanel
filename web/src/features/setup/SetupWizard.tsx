import { useMemo, useState } from "react";
import { Alert, Button, Field, Input, Modal, Select } from "@/components/ui";
import { ApiRequestError } from "@/lib/api";
import {
  useCompleteSetup,
  useSetup,
  type DBEngine,
  type SetupOption,
  type SetupSelection,
  type Webserver,
} from "./setup";

// SetupWizard is the first-run infrastructure dialog. The panel serves itself
// over net/http on SQLite, so it is reachable immediately after install — the
// dashboard renders behind this popup. The wizard captures the four choices that
// shape the host (webserver, database engine, DNS, mail) as dropdowns; on finish
// the panel provisions itself to match. It reappears on reload until completed,
// but can be dismissed for the session ("change this later").

function firstSupported(opts: SetupOption[], fallback: string): string {
  return opts.find((o) => o.supported)?.id ?? fallback;
}

// optionLabel appends the note (e.g. "coming soon") so a disabled dropdown entry
// still explains why it cannot be picked.
function optionLabel(o: SetupOption): string {
  return o.note ? `${o.label} — ${o.note}` : o.label;
}

export function SetupWizard() {
  const info = useSetup();
  const complete = useCompleteSetup();
  const [open, setOpen] = useState(true);

  const webservers = info.data?.webservers ?? [];
  const dbEngines = info.data?.db_engines ?? [];
  const state = info.data?.state;

  // Seed from any previously-saved selection (a re-run), else the first
  // supported option. Recomputed once the catalogs arrive.
  const initial = useMemo<SetupSelection>(
    () => ({
      webserver: (state?.webserver ?? firstSupported(webservers, "openlitespeed")) as Webserver,
      db_engine: (state?.db_engine ?? firstSupported(dbEngines, "mariadb")) as DBEngine,
      manage_dns: state?.manage_dns ?? false,
      create_mail: state?.create_mail ?? false,
      license_key: state?.license_key ?? "",
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [webservers.length, dbEngines.length, state?.webserver, state?.db_engine],
  );

  const [sel, setSel] = useState<SetupSelection | null>(null);
  const current = sel ?? initial;

  if (!open) return null;

  const errorMessage =
    complete.error instanceof ApiRequestError
      ? complete.error.message
      : complete.error
        ? "Could not complete setup. Please try again."
        : null;

  const submit = () => {
    complete.mutate(current, { onSuccess: () => setOpen(false) });
  };

  return (
    <Modal title="Set up your server" onClose={() => setOpen(false)} wide>
      <div className="space-y-5">
        <p className="text-sm text-muted">
          Choose the hosting stack HeroPanel will install and manage. You can change this later.
        </p>

        {errorMessage && <Alert>{errorMessage}</Alert>}

        <Field label="Web server" hint="Serves your websites; HeroPanel renders and reloads its config for you.">
          <Select
            value={current.webserver}
            onChange={(e) => setSel({ ...current, webserver: e.target.value as Webserver })}
          >
            {webservers.map((o) => (
              <option key={o.id} value={o.id} disabled={!o.supported}>
                {optionLabel(o)}
              </option>
            ))}
          </Select>
        </Field>

        {current.webserver === "litespeed_enterprise" && (
          <Field
            label="LiteSpeed license serial"
            hint="Required for a paid LiteSpeed Enterprise license; leave empty to use a trial."
          >
            <Input
              value={current.license_key ?? ""}
              onChange={(e) => setSel({ ...current, license_key: e.target.value })}
              placeholder="XXXX-XXXX-XXXX-XXXX"
            />
          </Field>
        )}

        <Field label="Database" hint="Engine for your websites' databases. The panel itself always uses SQLite.">
          <Select
            value={current.db_engine}
            onChange={(e) => setSel({ ...current, db_engine: e.target.value as DBEngine })}
          >
            {dbEngines.map((o) => (
              <option key={o.id} value={o.id} disabled={!o.supported}>
                {optionLabel(o)}
              </option>
            ))}
          </Select>
        </Field>

        <Field label="DNS" hint="Manage authoritative DNS zones (BIND) from this panel?">
          <Select
            value={current.manage_dns ? "yes" : "no"}
            onChange={(e) => setSel({ ...current, manage_dns: e.target.value === "yes" })}
          >
            <option value="no">No — I use external DNS</option>
            <option value="yes">Yes — manage DNS here</option>
          </Select>
        </Field>

        <Field label="Mail server" hint="Set up a mail server (Postfix + Dovecot) for mailboxes on your domains?">
          <Select
            value={current.create_mail ? "yes" : "no"}
            onChange={(e) => setSel({ ...current, create_mail: e.target.value === "yes" })}
          >
            <option value="no">No mail server</option>
            <option value="yes">Yes — create a mail server</option>
          </Select>
        </Field>

        <div className="flex items-center justify-end gap-3 border-t border-border pt-4">
          <Button variant="ghost" onClick={() => setOpen(false)}>
            Later
          </Button>
          <Button onClick={submit} loading={complete.isPending}>
            Finish setup
          </Button>
        </div>
      </div>
    </Modal>
  );
}
