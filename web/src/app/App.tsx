import { lazy, Suspense } from "react";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { Spinner } from "@/components/ui";
import { ApiRequestError } from "@/lib/api";
import { Toaster } from "@/components/Toaster";
import { JobDrawer } from "@/components/JobDrawer";
import { CommandPalette } from "@/components/CommandPalette";
import { useAuthStatus, useMe } from "@/features/auth/auth";
import { ImpersonationBanner } from "@/features/auth/ImpersonationBanner";
import { LoginPage } from "@/features/auth/LoginPage";
import { BootstrapPage } from "@/features/auth/BootstrapPage";
import { NotConfiguredPage } from "@/features/auth/NotConfiguredPage";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { AppLayout } from "./layout/AppLayout";

// Authenticated pages are code-split: each becomes its own chunk fetched on
// first navigation, so the initial load ships the shell and the current route
// rather than every screen at once. The pre-auth pages (login/bootstrap) stay
// eager above — they are the first paint for a logged-out visitor and must not
// flash a spinner. These are named exports, so each import is adapted to the
// default export React.lazy expects.
const page = <T extends Record<string, unknown>, K extends keyof T>(
  loader: () => Promise<T>,
  name: K,
) => lazy(() => loader().then((m) => ({ default: m[name] as React.ComponentType })));

const DashboardPage = page(() => import("@/features/dashboard/DashboardPage"), "DashboardPage");
const UsersPage = page(() => import("@/features/users/UsersPage"), "UsersPage");
const SitesPage = page(() => import("@/features/sites/SitesPage"), "SitesPage");
const SiteDetailPage = page(() => import("@/features/sites/SiteDetailPage"), "SiteDetailPage");
const DatabasesPage = page(() => import("@/features/databases/DatabasesPage"), "DatabasesPage");
const DNSPage = page(() => import("@/features/dns/DNSPage"), "DNSPage");
const DomainsPage = page(() => import("@/features/domains/DomainsPage"), "DomainsPage");
const NameserversPage = page(() => import("@/features/domains/NameserversPage"), "NameserversPage");
const SSLPage = page(() => import("@/features/ssl/SSLPage"), "SSLPage");
const AuditPage = page(() => import("@/features/audit/AuditPage"), "AuditPage");
const RecordingsPage = page(() => import("@/features/recordings/RecordingsPage"), "RecordingsPage");
const DockerPage = page(() => import("@/features/docker/DockerPage"), "DockerPage");
const AppsPage = page(() => import("@/features/apps/AppsPage"), "AppsPage");
const MailPage = page(() => import("@/features/mail/MailPage"), "MailPage");
const MonitorPage = page(() => import("@/features/monitor/MonitorPage"), "MonitorPage");
const SecurityPage = page(() => import("@/features/security/SecurityPage"), "SecurityPage");
const ModulesPage = page(() => import("@/features/modules/ModulesPage"), "ModulesPage");
const MarketplacePage = page(() => import("@/features/marketplace/MarketplacePage"), "MarketplacePage");
const HelpPage = page(() => import("@/features/help/HelpPage"), "HelpPage");
const AccountPage = page(() => import("@/features/account/AccountPage"), "AccountPage");
const SetupWizard = page(() => import("@/features/setup/SetupWizard"), "SetupWizard");

function FullscreenSpinner() {
  return (
    <div className="grid min-h-screen place-items-center bg-surface">
      <Spinner className="h-6 w-6 text-brand" />
    </div>
  );
}

export function App() {
  const me = useMe();
  const status = useAuthStatus();

  if (me.isLoading || status.isLoading) {
    return <FullscreenSpinner />;
  }

  // The panel has no datastore: showing a login form would be a lie, since
  // every submit is guaranteed to fail. Explain what is missing instead.
  //
  // Only a *404* on /auth/status counts as corroborating evidence, because an
  // older server without the `configured` field unmounts the whole auth group
  // and that is what it looks like. Any other failure — a 429 from the rate
  // limiter, a 500, a dropped connection — must not claim the panel is
  // unconfigured: that sends an operator to edit a database setting that was
  // never the problem. Those fall through to the login form, where the real
  // error is shown on submit.
  const statusFailedAsMissing = status.error instanceof ApiRequestError && status.error.status === 404;
  const unconfigured = status.data ? status.data.configured === false : statusFailedAsMissing;
  if (!me.data && unconfigured) {
    return (
      <>
        <NotConfiguredPage />
        <Toaster />
      </>
    );
  }

  // Not authenticated: show bootstrap (first run) or login. The toaster is
  // mounted here too so a failed login can surface a toast.
  if (!me.data) {
    return (
      <>
        {status.data?.needs_bootstrap ? <BootstrapPage /> : <LoginPage />}
        <Toaster />
      </>
    );
  }

  // Authenticated app. The shell — command palette, job drawer, toaster — is
  // mounted once, above the routes, so every page shares it. On a fresh install
  // the first-run setup wizard opens as a popup *over* the dashboard (not a
  // blocking page); only an explicit setup_complete=false shows it, so a server
  // that does not know about setup is never trapped behind it.
  const needsSetup = status.data?.setup_complete === false;
  return (
    <BrowserRouter>
      <ImpersonationBanner />
      <CommandPalette />
      {needsSetup && (
        <ErrorBoundary title="Setup">
          <Suspense fallback={null}>
            <SetupWizard />
          </Suspense>
        </ErrorBoundary>
      )}
      <Suspense fallback={<FullscreenSpinner />}>
        <Routes>
          <Route element={<AppLayout />}>
            <Route path="/" element={<DashboardPage />} />
            <Route path="/sites" element={<SitesPage />} />
            <Route path="/sites/:uid" element={<SiteDetailPage />} />
            <Route path="/databases" element={<DatabasesPage />} />
            <Route path="/domains" element={<DomainsPage />} />
            <Route path="/dns" element={<DNSPage />} />
            <Route path="/nameservers" element={<NameserversPage />} />
            <Route path="/ssl" element={<SSLPage />} />
            <Route path="/audit" element={<AuditPage />} />
            <Route path="/recordings" element={<RecordingsPage />} />
            <Route path="/docker" element={<DockerPage />} />
            <Route path="/apps" element={<AppsPage />} />
            <Route path="/monitor" element={<MonitorPage />} />
            <Route path="/mail" element={<MailPage />} />
            <Route path="/security" element={<SecurityPage />} />
            <Route path="/modules" element={<ModulesPage />} />
            <Route path="/marketplace" element={<MarketplacePage />} />
            <Route path="/help" element={<HelpPage />} />
            <Route path="/account" element={<AccountPage />} />
            <Route path="/users" element={<UsersPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
      </Suspense>
      <JobDrawer />
      <Toaster />
    </BrowserRouter>
  );
}
