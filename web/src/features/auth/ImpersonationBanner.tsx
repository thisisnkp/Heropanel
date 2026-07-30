import { Button } from "@/components/ui";
import { toast } from "@/stores/toast";
import { useMe, useStopImpersonation } from "./auth";

// ImpersonationBanner is a persistent, high-contrast bar shown whenever the
// current session is an administrator acting as another user. It keeps the fact
// that "you are not yourself right now" impossible to miss, and offers the one
// control that matters — stepping back to your own identity. Rendered above the
// routed app so it is present on every page during impersonation.
export function ImpersonationBanner() {
  const me = useMe();
  const stop = useStopImpersonation();
  const p = me.data;
  if (!p?.impersonator_email) return null;

  return (
    <div
      role="alert"
      className="flex items-center justify-center gap-3 bg-amber-500 px-4 py-2 text-sm font-medium text-amber-950"
    >
      <span>
        Acting as <strong>{p.email}</strong> — signed in as {p.impersonator_email}. Every action is audited as you.
      </span>
      <Button
        variant="ghost"
        className="h-7 bg-amber-950/10 px-2 text-amber-950 hover:bg-amber-950/20"
        loading={stop.isPending}
        onClick={() =>
          stop.mutate(undefined, {
            onSuccess: () => toast.success("Back to your own account"),
            onError: (e) => toast.error(e.message),
          })
        }
      >
        Stop impersonating
      </Button>
    </div>
  );
}
