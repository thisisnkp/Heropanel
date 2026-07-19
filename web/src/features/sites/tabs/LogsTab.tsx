import { useState } from "react";
import { Alert, Card, Spinner, cn } from "@/components/ui";
import { useSiteLogs } from "../site-detail";

// LogsTab tails the site's access/error log, auto-refreshing. The logs are
// 0750 and owned by the site user, so this reads them through the broker.
export function LogsTab({ uid }: { uid: string }) {
  const [kind, setKind] = useState<"access" | "error">("access");
  const { data, isLoading, error } = useSiteLogs(uid, kind, true);

  return (
    <div className="space-y-3">
      <div className="flex gap-2">
        {(["access", "error"] as const).map((k) => (
          <button
            key={k}
            onClick={() => setKind(k)}
            className={cn(
              "rounded-lg border px-3 py-1.5 text-sm capitalize transition-colors",
              kind === k ? "border-brand bg-brand/10 text-fg" : "border-border text-muted hover:text-fg",
            )}
          >
            {k} log
          </button>
        ))}
        <span className="ml-auto flex items-center text-xs text-muted">auto-refreshing</span>
      </div>

      {isLoading && <Spinner />}
      {error && <Alert>Could not read the log.</Alert>}
      {data && (
        <Card className="overflow-hidden">
          {data.exists && data.content ? (
            <pre className="max-h-[28rem] overflow-auto bg-surface p-4 text-xs leading-relaxed text-fg">{data.content}</pre>
          ) : (
            <p className="px-4 py-12 text-center text-sm text-muted">
              No {kind} log yet — this site has had no requests.
            </p>
          )}
        </Card>
      )}
    </div>
  );
}
