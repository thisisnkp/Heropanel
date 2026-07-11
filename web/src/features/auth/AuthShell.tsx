import type { ReactNode } from "react";
import { Logo } from "@/components/Logo";

// AuthShell is the centered, glass-accented frame around login / bootstrap.
export function AuthShell({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) {
  return (
    <div className="relative grid min-h-screen place-items-center overflow-hidden px-4">
      <div className="pointer-events-none absolute -top-32 left-1/2 h-96 w-96 -translate-x-1/2 rounded-full bg-brand/20 blur-3xl" />
      <div className="w-full max-w-sm space-y-6">
        <div className="flex flex-col items-center gap-3 text-center">
          <Logo className="h-10 w-10" />
          <div>
            <h1 className="text-xl font-semibold text-fg">{title}</h1>
            <p className="text-sm text-muted">{subtitle}</p>
          </div>
        </div>
        {children}
        <p className="text-center text-xs text-muted">HeroPanel — the fast, modern hosting control panel.</p>
      </div>
    </div>
  );
}
