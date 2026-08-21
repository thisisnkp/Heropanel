/** Mail fixtures — mailboxes, forwarders and the deliverability records. */

export interface Mailbox {
  readonly address: string;
  readonly used: string;
  readonly pct: number;
}

export interface Forwarder {
  readonly from: string;
  readonly to: string;
}

export interface MailRecord {
  readonly label: string;
  readonly value: string;
  readonly state: "Valid" | "Weak" | "Missing";
}

export const MAIL_STATS = [
  { label: "Mailboxes", value: "4", sub: "across 2 domains" },
  { label: "Storage", value: "3.2 GB", sub: "of 25 GB included" },
  { label: "Forwarders", value: "3", sub: "no loops detected" },
  { label: "Spam blocked", value: "1,842", sub: "last 30 days" },
];

export const MAILBOXES: readonly Mailbox[] = [
  { address: "aarav@novaretail.in", used: "1.9 GB of 10 GB", pct: 19 },
  { address: "orders@novaretail.in", used: "820 MB of 5 GB", pct: 16 },
  { address: "support@novaretail.in", used: "430 MB of 5 GB", pct: 9 },
  { address: "billing@billing-portal.co", used: "96 MB of 5 GB", pct: 2 },
];

export const FORWARDERS: readonly Forwarder[] = [
  { from: "hello@novaretail.in", to: "aarav@novaretail.in" },
  { from: "invoices@novaretail.in", to: "billing@billing-portal.co" },
  { from: "careers@brightlabs.dev", to: "aarav@novaretail.in" },
];

export const MAIL_DNS: readonly MailRecord[] = [
  { label: "MX", value: "mx1.nexpanel.mail · priority 10", state: "Valid" },
  { label: "SPF", value: "v=spf1 include:nexpanel.mail -all", state: "Valid" },
  { label: "DKIM", value: "nexp._domainkey · 2048-bit", state: "Valid" },
  { label: "DMARC", value: "p=none · reports to aarav@", state: "Weak" },
];
