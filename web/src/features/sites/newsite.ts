// Pure helpers for the create-site form. They live apart from the component so
// the rules that decide what a typed domain *means* can be tested without a
// DOM — that classification is the only real logic on that page.

export type DomainVerdict =
  | { kind: "empty" }
  /** Exactly a domain the panel knows and nothing is using. */
  | { kind: "free"; fqdn: string }
  /** A subdomain of a domain whose ownership is already proven here. */
  | { kind: "subdomain"; parent: string }
  /** Not known here. Creation still works; the operator must verify it after. */
  | { kind: "unknown" };

/** isSubdomainOf mirrors the server's isSameOrSubdomain (internal/domain/parked.go). */
function isSubdomainOf(fqdn: string, parent: string): boolean {
  return fqdn.endsWith("." + parent);
}

// classifyDomain explains a typed hostname against the two lists the panel
// returns. Ordering matters: an exact match against the free pool is the
// strongest statement, then inheritance from a trusted parent, then "unknown".
//
// `trusted` deliberately includes domains that already have a site, which is
// why a subdomain of an in-use domain is not reported as unknown — the server's
// Classify accepts it for exactly that reason, and a warning here would
// contradict what actually happens on submit.
export function classifyDomain(input: string, free: string[], trusted: string[]): DomainVerdict {
  const fqdn = normalizeDomain(input);
  if (!fqdn) return { kind: "empty" };
  if (free.includes(fqdn)) return { kind: "free", fqdn };

  // Longest parent wins, so "a.b.example.com" reports b.example.com rather than
  // example.com when both are trusted — the closer one is the useful answer.
  let best = "";
  for (const t of trusted) {
    if (isSubdomainOf(fqdn, t) && t.length > best.length) best = t;
  }
  if (best) return { kind: "subdomain", parent: best };
  return { kind: "unknown" };
}

/** normalizeDomain matches the server's NormalizeFQDN (internal/domain/domain.go). */
export function normalizeDomain(input: string): string {
  return input.trim().toLowerCase().replace(/\.$/, "");
}

// siteNameFrom derives a default site name from a domain: the first label,
// unless that is a throwaway "www", in which case the one after it is what the
// operator actually means. Keeps the form to a single field in the common case.
export function siteNameFrom(domain: string): string {
  const labels = normalizeDomain(domain).split(".").filter(Boolean);
  if (labels.length === 0) return "";
  if (labels[0] === "www" && labels.length > 1) return labels[1];
  return labels[0];
}
