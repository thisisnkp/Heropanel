/**
 * The site fixtures the design prototype ships with.
 *
 * These live in `data/` rather than inside the store because they are a
 * stand-in for `GET /api/v1/sites`, not part of the store's contract. When the
 * real endpoint is wired up, `useSitesStore().load()` changes and every screen
 * that reads the store keeps working — which is the point of the split.
 */
import type { Site } from "@/stores/sites";

export const SEED_SITES: readonly Site[] = [
  { id: 1, name: "novaretail.in", domain: "novaretail.in", stackKey: "wp", deploy: "Manual", status: "live", lastDeploy: "2 hours ago", branch: "—", repo: "—" },
  { id: 2, name: "api.novaretail.in", domain: "api.novaretail.in", stackKey: "node", deploy: "GitHub", status: "live", lastDeploy: "18 minutes ago", branch: "production", repo: "aaravrao/nova-api" },
  { id: 3, name: "billing-portal.co", domain: "billing-portal.co", stackKey: "php", deploy: "Manual", status: "live", lastDeploy: "6 days ago", branch: "—", repo: "—" },
  { id: 4, name: "brightlabs.dev", domain: "brightlabs.dev", stackKey: "static", deploy: "Manual", status: "live", lastDeploy: "3 weeks ago", branch: "—", repo: "—" },
  { id: 5, name: "queue.novaretail.in", domain: "queue.novaretail.in", stackKey: "node", deploy: "GitHub", status: "building", lastDeploy: "deploying…", branch: "main", repo: "aaravrao/nova-queue" },
];
