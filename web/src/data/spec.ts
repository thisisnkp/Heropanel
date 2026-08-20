/**
 * The shared screen spec.
 *
 * Thirty-one of this panel's screens are the same layout with different
 * content: a stat row, an optional choice group, a toggle panel beside a side
 * panel, one or two tables, and an optional log tail. The design expressed that
 * as a single template fed by a spec object, and that is worth keeping — the
 * alternative is thirty-one files that drift apart the first time the table
 * header padding changes.
 *
 * A screen that genuinely differs (the dashboard, the file manager, PHP
 * settings) is a component of its own. That is a decision someone makes, not
 * something that happens by accident when a copy gets edited.
 */
import type { FlagKey } from "@/stores/flags";

export interface SpecStat {
  readonly label: string;
  readonly value: string;
  readonly sub: string;
}

export interface SpecToggle {
  readonly label: string;
  readonly sub: string;
  readonly flag: FlagKey;
  /** Marks the switch as a paid add-on rather than a plain setting. */
  readonly paid?: boolean;
  /** Shows a "Risky" badge while the switch is on. */
  readonly warn?: boolean;
}

export interface SpecChoice {
  readonly label: string;
  readonly sub?: string;
}

export interface SpecField {
  readonly label: string;
  readonly value: string;
}

export interface SpecAction {
  readonly label: string;
  readonly primary?: boolean;
}

export interface SpecRow {
  readonly a: string;
  readonly b: string;
  readonly c: string;
  readonly action?: string;
  /** Second, destructive action. */
  readonly danger?: string;
}

export interface SpecTable {
  readonly title: string;
  readonly action: string;
  readonly columns: readonly [string, string, string];
  readonly rows: readonly SpecRow[];
}

export interface SpecLogLine {
  readonly time: string;
  readonly text: string;
  readonly tone?: "default" | "success" | "warning" | "danger";
}

/** The score panel the security overview leads with. */
export interface SpecHero {
  readonly score: number;
  readonly grade: string;
  readonly note: string;
  readonly critical: string;
  readonly warning: string;
  readonly healthy: string;
}

/** A one-click remediation with a severity. */
export interface SpecFix {
  readonly label: string;
  readonly sub: string;
  readonly severity: "critical" | "warning";
  /** Route name the Fix button goes to. */
  readonly to: string;
}

export interface Spec {
  readonly kicker: string;
  readonly title: string;
  readonly sub: string;
  readonly hero?: SpecHero;
  readonly quickFixes?: readonly SpecFix[];
  readonly stats?: readonly SpecStat[];
  readonly choiceTitle?: string;
  readonly choices?: readonly SpecChoice[];
  /** Which choice is selected by default. */
  readonly choiceDefault?: string;
  readonly toggleTitle?: string;
  readonly toggles?: readonly SpecToggle[];
  readonly sideTitle?: string;
  readonly sideNote?: string;
  readonly fields?: readonly SpecField[];
  readonly sideActions?: readonly SpecAction[];
  readonly table1?: SpecTable;
  readonly table2?: SpecTable;
  readonly logs?: readonly SpecLogLine[];
  readonly logName?: string;
}

export const row = (a: string, b: string, c: string, action?: string, danger?: string): SpecRow => ({
  a, b, c, action, danger,
});
