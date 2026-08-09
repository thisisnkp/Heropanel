import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

// The first-run setup wizard's data layer. It mirrors the backend's setup module
// (internal/setup): the option catalogs and the persisted selection. The wizard
// gates the whole panel until a selection is submitted, which flips
// setup_complete in /auth/status.

export type Webserver = "openlitespeed" | "nginx" | "apache" | "litespeed_enterprise";
export type DBEngine = "mysql" | "mariadb" | "postgresql";

export interface SetupOption {
  id: string;
  label: string;
  note?: string;
  supported: boolean;
}

export interface SetupSelection {
  webserver: Webserver;
  db_engine: DBEngine;
  manage_dns: boolean;
  create_mail: boolean;
  /** LiteSpeed Enterprise serial; only meaningful when webserver is
   *  litespeed_enterprise. Empty = trial. */
  license_key?: string;
  /** This installation's own base domain — the parent temporary site
   *  addresses are minted under (site-k3f9a2.<panel_domain>). Optional;
   *  empty simply means no temporary addresses are offered. */
  panel_domain?: string;
  /** This host's public IPv4, used only so the panel can create the
   *  `*.<panel_domain>` A record itself when that domain is a zone it hosts.
   *  Optional; the panel never infers its own address. */
  panel_ipv4?: string;
}

export interface SetupState extends Partial<SetupSelection> {
  completed: boolean;
  completed_at?: string;
}

export interface SetupInfo {
  state: SetupState;
  webservers: SetupOption[];
  db_engines: SetupOption[];
}

// enabled lets a caller that is not the wizard itself — the Domains page's
// panel-domain card — skip the request for someone without setup.manage,
// rather than firing one that is certain to 403.
export function useSetup(enabled = true) {
  return useQuery({
    queryKey: ["setup"],
    queryFn: () => api.get<SetupInfo>("/setup"),
    enabled,
  });
}

// useCompleteSetup submits the operator's selection. On success the panel is
// configured, so both the setup and auth-status caches are invalidated to lift
// the wizard and reveal the app.
export function useCompleteSetup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (sel: SetupSelection) => api.post<SetupState>("/setup", sel),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["setup"] });
      qc.invalidateQueries({ queryKey: ["auth-status"] });
    },
  });
}
