import { forwardRef, useEffect, useId, useMemo, useRef, useState } from "react";
import type {
  ButtonHTMLAttributes,
  InputHTMLAttributes,
  ReactNode,
  SelectHTMLAttributes,
  TextareaHTMLAttributes,
} from "react";
import type { LucideIcon } from "lucide-react";
import { FOCUSABLE_SELECTOR, nextFocusIndex } from "@/lib/focustrap";

export function cn(...parts: (string | false | null | undefined)[]): string {
  return parts.filter(Boolean).join(" ");
}

// Every interactive control shares one focus treatment: a solid brand ring
// offset from the element against the page colour. Consistency is the point —
// a keyboard user should never have to work out what "focused" looks like on
// this particular control.
const focusRing =
  "focus:outline-none focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2";

// Inputs get a softer treatment than buttons: the border itself turns brand and
// a low-opacity halo grows around it, which reads as "typing here" rather than
// "you tabbed onto this".
const fieldFocus =
  "focus:outline-none focus-visible:border-brand focus-visible:ring-2 focus-visible:ring-brand/25";

const fieldBase =
  "w-full rounded-lg border border-border-strong bg-surface text-sm text-fg placeholder:text-muted/80 " +
  "transition-[color,background-color,border-color,box-shadow] hover:border-muted/50 " +
  "disabled:cursor-not-allowed disabled:opacity-60 disabled:hover:border-border-strong";

// The micro-label: 11px, semibold, wide-tracked, uppercase. Used for table
// headers, nav section dividers and stat captions. It is the typographic device
// that lets a dense screen have a second level of heading without spending any
// vertical space on it.
export const microLabel = "text-2xs font-semibold uppercase tracking-wider text-muted";

// ── buttons ─────────────────────────────────────────────────────────────────

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "ghost" | "danger";
  /** md (default) is the standard control height; sm is for table-row actions. */
  size?: "sm" | "md" | "lg";
  loading?: boolean;
}

const buttonSizes = {
  sm: "h-8 gap-1.5 rounded-md px-2.5 text-xs",
  md: "h-9 gap-2 rounded-lg px-3.5 text-sm",
  lg: "h-10 gap-2 rounded-lg px-4 text-sm",
} as const;

export function Button({
  variant = "primary",
  size = "md",
  loading,
  className,
  children,
  disabled,
  ...rest
}: ButtonProps) {
  const base = cn(
    "inline-flex select-none items-center justify-center whitespace-nowrap font-medium",
    "transition-[color,background-color,border-color,box-shadow]",
    "disabled:cursor-not-allowed disabled:opacity-50 disabled:shadow-none",
    focusRing,
  );
  // Hover shifts the fill to a real second colour rather than fading the whole
  // control: opacity hover washes out the label too, which is the single most
  // common tell of an unfinished interface.
  const variants = {
    primary: "bg-brand text-brand-fg shadow-sm hover:bg-brand-hover active:bg-brand-active",
    ghost: "border border-border-strong bg-panel text-fg shadow-sm hover:bg-panel-hover active:bg-surface-2",
    danger: "bg-danger text-danger-fg shadow-sm hover:bg-danger/90 active:bg-danger/80",
  } as const;
  return (
    <button className={cn(base, buttonSizes[size], variants[variant], className)} disabled={disabled || loading} {...rest}>
      {loading && <Spinner />}
      {children}
    </button>
  );
}

// IconButton is a square, label-less control for toolbars. It takes the label
// as a prop rather than as children so the accessible name can never be
// forgotten — an icon-only button without one is invisible to a screen reader.
export function IconButton({
  label,
  className,
  children,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { label: string }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      className={cn(
        "grid h-9 w-9 shrink-0 place-items-center rounded-lg text-muted transition-colors",
        "hover:bg-panel-hover hover:text-fg disabled:cursor-not-allowed disabled:opacity-50",
        focusRing,
        className,
      )}
      {...rest}
    >
      {children}
    </button>
  );
}

// ── form controls ───────────────────────────────────────────────────────────

export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  function Input({ className, ...rest }, ref) {
    return <input ref={ref} className={cn(fieldBase, fieldFocus, "h-9 px-3", className)} {...rest} />;
  },
);

export const Select = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement>>(
  function Select({ className, children, ...rest }, ref) {
    // np-select (index.css) suppresses the OS dropdown arrow and draws a themed
    // chevron — the native one cannot be styled and dates the whole page.
    return (
      <select ref={ref} className={cn(fieldBase, fieldFocus, "np-select h-9 px-3", className)} {...rest}>
        {children}
      </select>
    );
  },
);

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(
  function Textarea({ className, ...rest }, ref) {
    return <textarea ref={ref} className={cn(fieldBase, fieldFocus, "px-3 py-2 font-mono text-xs", className)} {...rest} />;
  },
);

// filterOptions is the combobox's matching rule, exported so it can be tested
// without a DOM. Substring rather than prefix: a user hunting for a domain
// types the memorable part ("acme"), which is rarely at the front of
// "staging.acme.co.uk".
export function filterOptions(options: string[], query: string): string[] {
  const q = query.trim().toLowerCase();
  if (!q) return options;
  return options.filter((o) => o.toLowerCase().includes(q));
}

// Combobox is a text field with a suggestion list — *not* a select. The typed
// value is always the value, so a domain that appears in no list is still a
// legal answer; the list only saves typing for the ones the panel already
// knows. A native <datalist> cannot do this: it will not open on click, cannot
// be styled, and gives no way to annotate a row.
export function Combobox({
  value,
  onChange,
  options,
  placeholder,
  renderOption,
  emptyLabel = "No matches — you can still use what you typed.",
  id,
  autoFocus,
}: {
  value: string;
  onChange: (v: string) => void;
  options: string[];
  placeholder?: string;
  /** Optional trailing annotation for a row (a badge, a note). */
  renderOption?: (option: string) => ReactNode;
  emptyLabel?: string;
  id?: string;
  autoFocus?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const wrapRef = useRef<HTMLDivElement>(null);
  const listId = useId();
  const generatedId = useId();
  const inputId = id ?? generatedId;

  const matches = useMemo(() => filterOptions(options, value), [options, value]);

  // Any click outside closes. Escape is handled on the input itself so it does
  // not steal the key from a dialog that contains this field.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  const commit = (option: string) => {
    onChange(option);
    setOpen(false);
    setActive(-1);
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      if (open) {
        e.stopPropagation();
        setOpen(false);
        setActive(-1);
      }
      return;
    }
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (!open) {
        setOpen(true);
        return;
      }
      if (matches.length === 0) return;
      const delta = e.key === "ArrowDown" ? 1 : -1;
      setActive((i) => (i + delta + matches.length) % matches.length);
      return;
    }
    // Enter only commits a highlighted row. With nothing highlighted the typed
    // text stands and the form submits — the whole point of a combobox over a
    // select is that an unlisted value is valid.
    if (e.key === "Enter" && open && active >= 0 && matches[active]) {
      e.preventDefault();
      commit(matches[active]);
    }
  };

  return (
    <div className="relative" ref={wrapRef}>
      <input
        id={inputId}
        role="combobox"
        aria-expanded={open}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={open && active >= 0 ? `${listId}-${active}` : undefined}
        autoComplete="off"
        autoFocus={autoFocus}
        value={value}
        placeholder={placeholder}
        onChange={(e) => {
          onChange(e.target.value);
          setOpen(true);
          setActive(-1);
        }}
        onFocus={() => setOpen(true)}
        onClick={() => setOpen(true)}
        onKeyDown={onKeyDown}
        className={cn(fieldBase, fieldFocus, "h-9 px-3")}
      />
      {open && (
        <ul
          id={listId}
          role="listbox"
          className="np-pop-in absolute z-30 mt-1 max-h-64 w-full overflow-auto rounded-lg border border-border bg-panel p-1 shadow-lg"
        >
          {matches.length === 0 ? (
            <li className="px-2.5 py-2 text-xs text-muted">{emptyLabel}</li>
          ) : (
            matches.map((o, i) => (
              <li key={o}>
                <button
                  type="button"
                  id={`${listId}-${i}`}
                  role="option"
                  aria-selected={i === active}
                  // mousedown, not click: the input's blur would otherwise close
                  // the list before the click ever lands.
                  onMouseDown={(e) => {
                    e.preventDefault();
                    commit(o);
                  }}
                  onMouseEnter={() => setActive(i)}
                  className={cn(
                    "flex w-full items-center justify-between gap-3 rounded-md px-2.5 py-1.5 text-left text-[13px]",
                    i === active ? "bg-brand-subtle text-brand" : "text-fg hover:bg-panel-hover",
                  )}
                >
                  <span className="truncate font-mono text-xs">{o}</span>
                  {renderOption?.(o)}
                </button>
              </li>
            ))
          )}
        </ul>
      )}
    </div>
  );
}

export function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: string }) {
  return (
    <label className="block space-y-1.5">
      <span className="text-sm font-medium text-fg">{label}</span>
      {children}
      {hint && <span className="block text-xs leading-relaxed text-muted">{hint}</span>}
    </label>
  );
}

// Toggle is a controlled switch. Used for OPcache, force-HTTPS, extensions —
// anything that is genuinely on/off.
export function Toggle({
  checked,
  onChange,
  label,
  disabled,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label?: string;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cn(
        "inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors disabled:cursor-not-allowed disabled:opacity-50",
        focusRing,
        // The off state uses the strong border rather than the hairline: at
        // hairline weight the track vanishes and the switch looks broken.
        checked ? "bg-brand" : "bg-border-strong",
      )}
      aria-label={label}
    >
      <span
        className={cn(
          "h-5 w-5 rounded-full bg-white shadow-sm transition-transform",
          checked ? "translate-x-5" : "translate-x-0.5",
        )}
      />
    </button>
  );
}

// ── surfaces ────────────────────────────────────────────────────────────────

export function Card({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn("rounded-xl border border-border bg-panel shadow-sm", className)}>{children}</div>;
}

// CardHeader is the titled strip at the top of a card. It is a separate export
// rather than a Card prop so a card can have a header, a header plus toolbar,
// or neither, without Card growing a matrix of optional props.
export function CardHeader({
  title,
  description,
  actions,
  className,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex items-start justify-between gap-4 border-b border-border px-4 py-3", className)}>
      <div className="min-w-0">
        <h3 className="text-sm font-semibold text-fg">{title}</h3>
        {description && <p className="mt-0.5 text-xs leading-relaxed text-muted">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}

// ── page structure ──────────────────────────────────────────────────────────

// PageHeader is the one title block every page uses: an optional breadcrumb
// trail, the page name, a line explaining what the page is for, and the page's
// primary actions on the right. Pages that hand-roll this drift apart in title
// size, spacing and where the buttons sit, which is what makes a multi-page app
// feel like several apps.
export function PageHeader({
  title,
  description,
  actions,
  breadcrumb,
  className,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  breadcrumb?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-wrap items-start justify-between gap-x-6 gap-y-3", className)}>
      <div className="min-w-0">
        {breadcrumb && <div className="mb-1.5">{breadcrumb}</div>}
        <h1 className="text-xl font-semibold text-fg">{title}</h1>
        {description && <p className="mt-1 max-w-2xl text-sm leading-relaxed text-muted">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}

// SectionHeader separates regions *within* a page. It is deliberately quieter
// than PageHeader — same structure, one step down in weight — so the hierarchy
// between "this page" and "this part of the page" is never ambiguous.
export function SectionHeader({
  title,
  description,
  actions,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
      <div className="min-w-0">
        <h2 className="text-base font-semibold text-fg">{title}</h2>
        {description && <p className="mt-0.5 max-w-2xl text-sm leading-relaxed text-muted">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}

// Stat is one figure in a summary row. The icon is what turns a row of numbers
// into something scannable — the eye finds the shape before it reads the label.
export function Stat({
  label,
  value,
  hint,
  icon: Icon,
  loading,
  tone = "neutral",
}: {
  label: string;
  value: ReactNode;
  hint?: string;
  icon?: LucideIcon;
  loading?: boolean;
  tone?: "neutral" | "brand" | "success" | "warning" | "danger";
}) {
  const tones = {
    neutral: "bg-surface-2 text-muted",
    brand: "bg-brand-subtle text-brand",
    success: "bg-success-subtle text-success",
    warning: "bg-warning-subtle text-warning",
    danger: "bg-danger-subtle text-danger",
  } as const;
  return (
    <Card className="flex items-start gap-3 p-4">
      {Icon && (
        <span className={cn("grid h-8 w-8 shrink-0 place-items-center rounded-lg", tones[tone])}>
          <Icon className="h-4 w-4" strokeWidth={2} aria-hidden />
        </span>
      )}
      <div className="min-w-0">
        <div className={microLabel}>{label}</div>
        <div className="mt-1 text-2xl font-semibold leading-none text-fg tabular">
          {loading ? <Skeleton className="h-6 w-12 rounded" /> : value}
        </div>
        {hint && <div className="mt-1.5 text-xs text-muted">{hint}</div>}
      </div>
    </Card>
  );
}

// ── data tables ─────────────────────────────────────────────────────────────

type Column = string | { label: ReactNode; align?: "left" | "right" | "center"; className?: string };

const alignClass = { left: "text-left", right: "text-right", center: "text-center" } as const;

// DataTable owns the parts of a table that must not vary: the micro-label
// header row, the hairline between rows, the cell rhythm, and the horizontal
// scroll container that keeps a wide table from widening the whole page.
export function DataTable({ head, children, className }: { head: Column[]; children: ReactNode; className?: string }) {
  return (
    <div className={cn("overflow-x-auto", className)}>
      <table className="w-full border-collapse">
        <thead>
          <tr className="border-b border-border bg-surface-2/50">
            {head.map((c, i) => {
              const col = typeof c === "string" ? { label: c } : c;
              return (
                <th
                  key={i}
                  scope="col"
                  className={cn(
                    "px-4 py-2.5 first:pl-4 last:pr-4",
                    microLabel,
                    alignClass[(typeof c === "string" ? undefined : c.align) ?? "left"],
                    typeof c === "string" ? undefined : c.className,
                  )}
                >
                  {col.label}
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  );
}

export function Tr({
  children,
  onClick,
  className,
}: {
  children: ReactNode;
  onClick?: () => void;
  className?: string;
}) {
  return (
    <tr
      onClick={onClick}
      className={cn(
        "border-b border-border/70 transition-colors last:border-0",
        onClick && "cursor-pointer hover:bg-panel-hover",
        !onClick && "hover:bg-panel-hover/60",
        className,
      )}
    >
      {children}
    </tr>
  );
}

export function Td({
  children,
  align = "left",
  className,
}: {
  children: ReactNode;
  align?: "left" | "right" | "center";
  className?: string;
}) {
  return <td className={cn("px-4 py-2.5 text-sm text-fg", alignClass[align], className)}>{children}</td>;
}

// ── feedback ────────────────────────────────────────────────────────────────

export function Spinner({ className }: { className?: string }) {
  return (
    <span
      className={cn("inline-block h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent", className)}
      aria-hidden
    />
  );
}

// Skeleton stands in for content whose shape is already known. Preferred over a
// spinner wherever the layout is predictable: it shows how much is coming and
// stops the page reflowing when it lands.
export function Skeleton({ className }: { className?: string }) {
  return <span className={cn("np-skeleton block h-4 w-full rounded-md", className)} aria-hidden />;
}

// Alert defaults to the danger tone because that is what every existing caller
// means by it; the other tones are for callers that want to say something
// without making it look like a failure.
const alertTones = {
  danger: "border-danger/30 bg-danger-subtle text-danger",
  warning: "border-warning/30 bg-warning-subtle text-warning",
  success: "border-success/30 bg-success-subtle text-success",
  info: "border-brand-border bg-brand-subtle text-fg",
} as const;

export function Alert({ children, tone = "danger" }: { children: ReactNode; tone?: keyof typeof alertTones }) {
  return <div className={cn("rounded-lg border px-3 py-2 text-sm leading-relaxed", alertTones[tone])}>{children}</div>;
}

// Badge labels a thing (a site's type, a domain's kind, a branch name). It is
// a squared-off tag, not a pill — pills read as status, and status has its own
// component below.
const badgeTones = {
  neutral: "border-border bg-surface-2 text-muted",
  brand: "border-brand-border bg-brand-subtle text-brand",
  success: "border-success/25 bg-success-subtle text-success",
  warning: "border-warning/25 bg-warning-subtle text-warning",
  danger: "border-danger/25 bg-danger-subtle text-danger",
} as const;

export function Badge({ children, tone = "neutral" }: { children: ReactNode; tone?: keyof typeof badgeTones }) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-md border px-1.5 py-0.5 text-2xs font-medium",
        badgeTones[tone],
      )}
    >
      {children}
    </span>
  );
}

// One status vocabulary for the whole panel, in semantic tokens rather than
// raw palette colours — so "degraded" is the same amber on a site, a runtime,
// and a job, and both themes get a value tuned for their own background.
const dotTone: Record<string, string> = {
  active: "text-success",
  running: "text-success",
  ready: "text-success",
  succeeded: "text-success",
  provisioning: "text-warning",
  suspended: "text-warning",
  degraded: "text-warning",
  queued: "text-warning",
  error: "text-danger",
  failed: "text-danger",
  stopped: "text-muted",
  disabled: "text-muted",
};

// Statuses that mean "work is happening" get a pulsing halo, so a page that is
// mid-provision looks alive rather than stuck.
const busyStatus = new Set(["provisioning", "queued", "running"]);

// StatusBadge is the dot+label status shown across sites, runtimes, jobs. One
// component so a status colour means the same thing everywhere.
export function StatusBadge({ status }: { status: string }) {
  const tone = dotTone[status] ?? "text-muted";
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-xs font-medium capitalize", tone)}>
      <span className="relative flex h-1.5 w-1.5 shrink-0">
        {busyStatus.has(status) && (
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-current opacity-60" />
        )}
        <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-current" />
      </span>
      {status}
    </span>
  );
}

// EmptyState is the "nothing here yet" panel with an optional call to action.
// The icon is not decoration: an empty region with only two lines of grey text
// reads as a failed load, and a framed glyph is what distinguishes "nothing
// here yet" from "something went wrong".
export function EmptyState({
  title,
  hint,
  action,
  icon: Icon,
}: {
  title: string;
  hint?: string;
  action?: ReactNode;
  icon?: LucideIcon;
}) {
  return (
    <div className="grid place-items-center gap-1.5 px-4 py-14 text-center">
      {Icon && (
        <span className="mb-1.5 grid h-11 w-11 place-items-center rounded-xl border border-border bg-surface-2 text-muted">
          <Icon className="h-5 w-5" strokeWidth={1.75} aria-hidden />
        </span>
      )}
      <p className="text-sm font-semibold text-fg">{title}</p>
      {hint && <p className="max-w-sm text-sm leading-relaxed text-muted">{hint}</p>}
      {action && <div className="mt-3">{action}</div>}
    </div>
  );
}

// ── overlays ────────────────────────────────────────────────────────────────

// Modal is the shared dialog: a backdrop that closes on click/Escape, and a
// card that does not. Every create/confirm flow uses this instead of the ad-hoc
// version SitesPage grew.
export function Modal({
  title,
  onClose,
  children,
  wide,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
}) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = useId();

  // Dialog focus management: on open, move focus into the dialog and remember
  // where it came from; trap Tab within the dialog so keyboard focus cannot
  // wander to the page behind it; on close, hand focus back to whatever opened
  // it. Escape closes. Without this a keyboard or screen-reader user is left
  // stranded behind the overlay — the defining failure of an inaccessible modal.
  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    const node = dialogRef.current;
    const focusables = () =>
      node ? Array.from(node.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)) : [];
    (focusables()[0] ?? node)?.focus();

    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onClose();
        return;
      }
      if (e.key !== "Tab") return;
      const els = focusables();
      e.preventDefault();
      if (els.length === 0) return;
      const idx = els.indexOf(document.activeElement as HTMLElement);
      els[nextFocusIndex(els.length, idx, e.shiftKey)]?.focus();
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      opener?.focus?.();
    };
  }, [onClose]);

  return (
    <div
      className="np-fade-in fixed inset-0 z-50 grid place-items-center bg-fg/40 p-4 backdrop-blur-sm"
      onClick={onClose}
    >
      <Card className={cn("np-pop-in w-full p-6 shadow-lg", wide ? "max-w-2xl" : "max-w-md")}>
        <div
          ref={dialogRef}
          role="dialog"
          aria-modal="true"
          aria-labelledby={titleId}
          tabIndex={-1}
          onClick={(e) => e.stopPropagation()}
          className="focus:outline-none"
        >
          <div className="mb-5 flex items-start justify-between gap-4">
            <h2 id={titleId} className="text-base font-semibold text-fg">
              {title}
            </h2>
            <button
              type="button"
              onClick={onClose}
              aria-label="Close dialog"
              className={cn("-m-1 rounded-md p-1 text-muted transition-colors hover:bg-panel-hover hover:text-fg", focusRing)}
            >
              <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round">
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </button>
          </div>
          {children}
        </div>
      </Card>
    </div>
  );
}

// Tabs is a simple controlled tab strip for the site workspace.
export function Tabs({
  tabs,
  active,
  onChange,
}: {
  tabs: { id: string; label: string }[];
  active: string;
  onChange: (id: string) => void;
}) {
  return (
    <div className="flex gap-1 overflow-x-auto border-b border-border">
      {tabs.map((t) => (
        <button
          key={t.id}
          onClick={() => onChange(t.id)}
          className={cn(
            "-mb-px whitespace-nowrap rounded-t-md border-b-2 px-3 py-2 text-sm font-medium transition-colors",
            "focus:outline-none focus-visible:bg-panel-hover focus-visible:text-fg",
            active === t.id
              ? "border-brand text-fg"
              : "border-transparent text-muted hover:border-border-strong hover:text-fg",
          )}
        >
          {t.label}
        </button>
      ))}
    </div>
  );
}
