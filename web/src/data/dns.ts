/**
 * DNS fixtures: the zone, the record types the editor accepts, and the fields
 * each type needs.
 *
 * The per-type field list is data rather than a switch in the template because
 * the record form is the one place in the panel where the *shape* of the form
 * changes with a picker — an MX record has a priority, a SRV record has four
 * numbers, a CNAME has neither.
 */

export const DNS_DOMAINS = ["novaretail.in", "api.novaretail.in", "billing-portal.co", "brightlabs.dev"] as const;

export const RECORD_TYPES = ["A", "AAAA", "CNAME", "MX", "TXT", "SRV", "CAA", "NS"] as const;
export type RecordType = (typeof RECORD_TYPES)[number];

export interface RecordField {
  readonly label: string;
  readonly placeholder: string;
  /** A grid track, so numeric fields stay narrow beside a long value field. */
  readonly width: string;
}

export function recordFields(type: RecordType, domain: string): readonly RecordField[] {
  const ttl = (value: string, width = "minmax(0, 0.6fr)") => ({ label: "TTL", placeholder: value, width });

  switch (type) {
    case "A":
      return [
        { label: "Name", placeholder: "@ or subdomain", width: "minmax(0, 1.1fr)" },
        { label: "IPv4 address", placeholder: "203.0.113.24", width: "minmax(0, 1.6fr)" },
        ttl("300"),
      ];
    case "AAAA":
      return [
        { label: "Name", placeholder: "@ or subdomain", width: "minmax(0, 1.1fr)" },
        { label: "IPv6 address", placeholder: "2001:db8::1", width: "minmax(0, 1.6fr)" },
        ttl("300"),
      ];
    case "CNAME":
      return [
        { label: "Name", placeholder: "www", width: "minmax(0, 1.1fr)" },
        { label: "Points to", placeholder: domain || "example.com", width: "minmax(0, 1.6fr)" },
        ttl("300"),
      ];
    case "MX":
      return [
        { label: "Name", placeholder: "@", width: "minmax(0, 0.9fr)" },
        { label: "Mail server", placeholder: "mx1.nexpanel.mail", width: "minmax(0, 1.6fr)" },
        { label: "Priority", placeholder: "10", width: "minmax(0, 0.6fr)" },
        ttl("3600"),
      ];
    case "TXT":
      return [
        { label: "Name", placeholder: "@ or _dmarc", width: "minmax(0, 1.1fr)" },
        { label: "Content", placeholder: "v=spf1 include:nexpanel.mail -all", width: "minmax(0, 2.2fr)" },
        ttl("3600"),
      ];
    case "SRV":
      return [
        { label: "Name", placeholder: "_sip._tcp", width: "minmax(0, 1.1fr)" },
        { label: "Target", placeholder: "sip.example.com", width: "minmax(0, 1.4fr)" },
        { label: "Port", placeholder: "5060", width: "minmax(66px, 0.5fr)" },
        { label: "Priority", placeholder: "10", width: "minmax(66px, 0.5fr)" },
        { label: "Weight", placeholder: "5", width: "minmax(66px, 0.5fr)" },
        ttl("3600", "minmax(66px, 0.5fr)"),
      ];
    case "CAA":
      return [
        { label: "Name", placeholder: "@", width: "minmax(0, 0.9fr)" },
        { label: "Flag", placeholder: "0", width: "minmax(0, 0.5fr)" },
        { label: "Tag", placeholder: "issue", width: "minmax(0, 0.8fr)" },
        { label: "Value", placeholder: "letsencrypt.org", width: "minmax(0, 1.6fr)" },
        ttl("3600"),
      ];
    case "NS":
      return [
        { label: "Name", placeholder: "subdomain", width: "minmax(0, 1.1fr)" },
        { label: "Nameserver", placeholder: "ns1.nexpanel.net", width: "minmax(0, 1.6fr)" },
        ttl("3600"),
      ];
  }
}

export interface DnsRecord {
  readonly id: string;
  readonly name: string;
  readonly type: RecordType;
  readonly ttl: string;
  readonly value: string;
}

export function zoneRecords(domain: string): readonly DnsRecord[] {
  return [
    { id: "r1", name: "@", type: "A", ttl: "300", value: "203.0.113.24" },
    { id: "r2", name: "www", type: "CNAME", ttl: "300", value: domain },
    { id: "r3", name: "@", type: "MX", ttl: "3600", value: "mx1.nexpanel.mail (priority 10)" },
    { id: "r4", name: "@", type: "TXT", ttl: "3600", value: "v=spf1 include:nexpanel.mail -all" },
    { id: "r5", name: "nexp._domainkey", type: "TXT", ttl: "3600", value: "v=DKIM1; k=rsa; p=MIIBIjAN…" },
    { id: "r6", name: "_dmarc", type: "TXT", ttl: "3600", value: "v=DMARC1; p=none; rua=mailto:aarav@" },
    { id: "r7", name: "staging", type: "A", ttl: "300", value: "203.0.113.24" },
  ];
}

export const ZONE_STATS = [
  { label: "Status", value: "Active", sub: "resolving worldwide" },
  { label: "Records", value: "7", sub: "zone serial 2026081501" },
  { label: "SSL", value: "Valid", sub: "expires in 74 days" },
  { label: "Registrar", value: "External", sub: "renews 12 Mar 2027" },
];

export const ZONE_TARGETS = [
  { icon: "language", label: "Website", value: "203.0.113.24" },
  { icon: "mail", label: "Mail", value: "mx1.nexpanel.mail" },
  { icon: "account-tree", label: "Subdomains", value: "3 active" },
];

export const ZONE_SUBDOMAINS = [
  { name: "staging", root: "/staging/public", ssl: "Active" },
  { name: "cdn", root: "/public/assets", ssl: "Active" },
  { name: "docs", root: "/docs/site", ssl: "Active" },
];

export const ZONE_REDIRECTS = [
  { from: "/old-shop/*", to: "/products/$1", type: "301" },
  { from: "/sale", to: "/products?tag=sale", type: "302" },
  { from: "/team.html", to: "/about#team", type: "301" },
];

export const NAMESERVERS = ["ns1.nexpanel.net", "ns2.nexpanel.net"];

export const EXPORT_FORMATS = ["BIND zone file", "JSON", "CSV"] as const;

export function exportPreview(domain: string) {
  return [
    "@\t300\tIN\tA\t203.0.113.24",
    "www\t300\tIN\tCNAME\t" + domain + ".",
    "@\t3600\tIN\tMX\t10 mx1.nexpanel.mail.",
    '@\t3600\tIN\tTXT\t"v=spf1 include:nexpanel.mail -all"',
  ];
}
