import { Component, type ErrorInfo, type ReactNode } from "react";
import { Button } from "./ui";

// ErrorBoundary contains a render-time crash to its own subtree instead of
// letting it unmount the whole app (React's default: any uncaught render error
// tears down the entire tree — a blank screen). This is the frontend half of
// fault isolation: one broken page or widget must not take the rest of the panel
// down with it. Boundaries are layered — root (last resort), per-route (keeps the
// shell alive), and per-widget (a broken card leaves its neighbours working).

// resetKeysChanged reports whether two resetKeys arrays differ (shallow). A
// boundary clears its caught error when its resetKeys change — e.g. the route
// path — so navigating away from a crashed page recovers automatically instead
// of showing the fallback forever.
export function resetKeysChanged(a: unknown[] | undefined, b: unknown[] | undefined): boolean {
  if (a === b) return false;
  if (!a || !b || a.length !== b.length) return true;
  for (let i = 0; i < a.length; i++) {
    if (!Object.is(a[i], b[i])) return true;
  }
  return false;
}

interface Props {
  children: ReactNode;
  /** Shown in the fallback, e.g. "This page" or "Summary". */
  title?: string;
  /** Compact inline fallback for widgets (vs. the fuller page/section fallback). */
  compact?: boolean;
  /** When any of these change, a caught error is cleared and children re-render. */
  resetKeys?: unknown[];
}

interface State {
  error: Error | null;
}

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // Contain and record; never rethrow (that would defeat the isolation).
    // eslint-disable-next-line no-console
    console.error(`[ErrorBoundary] ${this.props.title ?? "section"} crashed:`, error, info.componentStack);
  }

  componentDidUpdate(prev: Props) {
    if (this.state.error && resetKeysChanged(prev.resetKeys, this.props.resetKeys)) {
      this.setState({ error: null });
    }
  }

  private reset = () => this.setState({ error: null });

  render() {
    if (!this.state.error) return this.props.children;

    const title = this.props.title ?? "This section";
    const detail = import.meta.env.DEV ? this.state.error.message : null;

    if (this.props.compact) {
      return (
        <div className="rounded-lg border border-danger/40 bg-danger/5 p-4 text-sm">
          <p className="font-medium text-fg">{title} failed to load</p>
          {detail && <p className="mt-1 break-words font-mono text-xs text-danger">{detail}</p>}
          <button
            type="button"
            onClick={this.reset}
            className="mt-2 text-xs font-medium text-brand hover:underline focus:outline-none"
          >
            Retry
          </button>
        </div>
      );
    }

    return (
      <div className="grid min-h-[50vh] place-items-center p-6">
        <div className="w-full max-w-md rounded-xl border border-border bg-panel p-6 text-center shadow-sm">
          <div className="mx-auto grid h-10 w-10 place-items-center rounded-full bg-danger/10 text-danger">
            <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M12 9v4M12 17h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z" />
            </svg>
          </div>
          <h2 className="mt-3 text-base font-semibold text-fg">{title} ran into a problem</h2>
          <p className="mt-1 text-sm text-muted">
            The rest of the panel is still working. You can retry this section or reload the page.
          </p>
          {detail && (
            <p className="mt-3 break-words rounded-md bg-danger/5 p-2 text-left font-mono text-xs text-danger">{detail}</p>
          )}
          <div className="mt-4 flex justify-center gap-2">
            <Button variant="ghost" onClick={() => window.location.reload()}>
              Reload page
            </Button>
            <Button onClick={this.reset}>Retry</Button>
          </div>
        </div>
      </div>
    );
  }
}
