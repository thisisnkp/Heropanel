// The pure core of NexPanel's tiny in-house i18n. No React, no I/O — just the
// two decisions that a lookup makes: choosing the right message for a key (with
// a fallback chain and simple pluralization) and filling its placeholders. These
// are where translation bugs actually live — a missing key that renders blank, a
// plural that reads "1 items", an unclosed `{{token}}` — so they are isolated
// here and unit-tested, while the React glue that consumes them stays thin.

// A message is either a plain string or a one/other pair for pluralization.
export type Message = string | { one: string; other: string };

// A catalog maps flat dotted keys ("auth.signIn.title") to messages.
export type Catalog = Record<string, Message>;

// Vars fill {{placeholders}}. Numbers and strings are both accepted; a `count`
// var additionally drives plural selection.
export type Vars = Record<string, string | number>;

// formatMessage substitutes {{name}} placeholders from vars. An unknown
// placeholder is left intact rather than blanked, so a missing var is visible in
// the UI (and in a test) instead of silently vanishing.
export function formatMessage(template: string, vars?: Vars): string {
  if (!vars) return template;
  return template.replace(/\{\{\s*(\w+)\s*\}\}/g, (whole, name: string) =>
    Object.prototype.hasOwnProperty.call(vars, name) ? String(vars[name]) : whole,
  );
}

// translate resolves a key against the active catalog, falling back to the base
// catalog (the default language) for keys a translation has not covered yet, and
// finally to the key itself so an unknown key is at least identifiable rather
// than blank. When count is given, a plural message picks one (count === 1) or
// other, and count is exposed to placeholders as {{count}}.
export function translate(
  active: Catalog,
  base: Catalog,
  key: string,
  vars?: Vars,
  count?: number,
): string {
  const message = active[key] ?? base[key] ?? key;
  let template: string;
  if (typeof message === "string") {
    template = message;
  } else {
    template = count === 1 ? message.one : message.other;
  }
  const merged: Vars | undefined =
    count === undefined ? vars : { ...(vars ?? {}), count };
  return formatMessage(template, merged);
}
