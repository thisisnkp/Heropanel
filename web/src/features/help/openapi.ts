// Turning the live OpenAPI document into something a help page can render. The
// spec is served (unauthenticated) at /api/v1/openapi.json and is built by
// walking hpd's real routing tree, so this reference cannot drift from what the
// server actually exposes. These transforms are pure — grouping by tag, pulling
// out the required permission, filtering by a search box — so the parsing that
// is easy to get subtly wrong (a `parameters` key mistaken for a method, an
// empty group left dangling after a filter) is isolated and unit-tested.

export type OpenApiOperation = {
  summary?: string;
  tags?: string[];
  description?: string;
  deprecated?: boolean;
  // hpd stamps the required RBAC permission here (openapi.go).
  "x-required-permission"?: string;
};

export type OpenApiSpec = {
  info?: { title?: string; version?: string };
  paths?: Record<string, Record<string, OpenApiOperation>>;
};

export type ApiEndpoint = {
  method: string;
  path: string;
  summary: string;
  permission?: string;
  deprecated: boolean;
  tag: string;
};

export type ApiGroup = {
  tag: string;
  endpoints: ApiEndpoint[];
};

// The HTTP methods an operation object may key. Anything else under a path item
// (notably `parameters`) is not an operation and must be skipped.
const HTTP_METHODS = ["get", "post", "put", "patch", "delete"];

// groupEndpoints flattens the spec's paths into endpoints grouped by their first
// tag, each group and its endpoints sorted for a stable render.
export function groupEndpoints(spec: OpenApiSpec | null | undefined): ApiGroup[] {
  const byTag = new Map<string, ApiEndpoint[]>();
  const paths = spec?.paths ?? {};
  for (const [path, item] of Object.entries(paths)) {
    for (const method of HTTP_METHODS) {
      const op = item[method];
      if (!op) continue;
      const tag = op.tags?.[0] ?? "Other";
      const list = byTag.get(tag) ?? [];
      list.push({
        method: method.toUpperCase(),
        path,
        summary: op.summary ?? "",
        permission: op["x-required-permission"],
        deprecated: op.deprecated === true,
        tag,
      });
      byTag.set(tag, list);
    }
  }
  const groups = Array.from(byTag, ([tag, endpoints]) => ({
    tag,
    endpoints: endpoints.sort((a, b) =>
      a.path === b.path ? a.method.localeCompare(b.method) : a.path.localeCompare(b.path),
    ),
  }));
  return groups.sort((a, b) => a.tag.localeCompare(b.tag));
}

// filterEndpoints keeps endpoints matching the query across method, path,
// summary, permission, or tag, and drops groups left empty so the UI never shows
// a heading with nothing under it. An empty query returns the groups unchanged.
export function filterEndpoints(groups: ApiGroup[], query: string): ApiGroup[] {
  const q = query.trim().toLowerCase();
  if (!q) return groups;
  return groups
    .map((g) => ({
      tag: g.tag,
      endpoints: g.endpoints.filter(
        (e) =>
          e.path.toLowerCase().includes(q) ||
          e.summary.toLowerCase().includes(q) ||
          e.method.toLowerCase().includes(q) ||
          (e.permission ?? "").toLowerCase().includes(q) ||
          g.tag.toLowerCase().includes(q),
      ),
    }))
    .filter((g) => g.endpoints.length > 0);
}

// countEndpoints totals the endpoints across groups, for the "N endpoints" label.
export function countEndpoints(groups: ApiGroup[]): number {
  return groups.reduce((n, g) => n + g.endpoints.length, 0);
}
