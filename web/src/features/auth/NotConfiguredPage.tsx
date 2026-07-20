import { AuthShell } from "./AuthShell";
import { Card } from "@/components/ui";

// NotConfiguredPage is what a fresh `hpd` shows when it has no datastore.
//
// Without this the login form renders, every submit fails, and the only clue is
// a generic "not found" — because with no database the auth routes do not exist.
// A panel that cannot work should say what is missing and how to supply it,
// rather than making an operator read the server log to find out.
export function NotConfiguredPage() {
  return (
    <AuthShell title="HeroPanel is not configured" subtitle="No database is set, so sign-in is unavailable.">
      <Card className="space-y-4 p-5">
        <p className="text-sm text-muted">
          hpd started without a datastore. Accounts, sites and every other feature live in the database, so the panel
          cannot sign anyone in until one is configured.
        </p>
        <div className="space-y-2">
          <p className="text-sm font-medium text-fg">Set a datastore, then restart hpd</p>
          <p className="text-xs text-muted">
            Either add <code className="rounded bg-surface px-1 py-0.5 font-mono">database.dsn</code> to the config file,
            or set the environment variables:
          </p>
          <pre className="overflow-x-auto rounded-lg border border-border bg-surface p-3 font-mono text-[11px] leading-relaxed text-fg">
{`# SQLite (single-node, no server to run)
HP_DATABASE_DRIVER=sqlite
HP_DATABASE_DSN=/var/lib/heropanel/hp.db

# or MariaDB
HP_DATABASE_DRIVER=mariadb
HP_DATABASE_DSN="user:pass@tcp(127.0.0.1:3306)/heropanel?parseTime=true"`}
          </pre>
        </div>
        <p className="text-xs text-muted">
          The server log prints the same guidance at startup. Once a datastore is reachable this screen is replaced by
          the first-run setup.
        </p>
      </Card>
    </AuthShell>
  );
}
