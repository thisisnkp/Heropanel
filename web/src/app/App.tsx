import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { Spinner } from "@/components/ui";
import { ApiRequestError } from "@/lib/api";
import { Toaster } from "@/components/Toaster";
import { JobDrawer } from "@/components/JobDrawer";
import { CommandPalette } from "@/components/CommandPalette";
import { useAuthStatus, useMe } from "@/features/auth/auth";
import { LoginPage } from "@/features/auth/LoginPage";
import { BootstrapPage } from "@/features/auth/BootstrapPage";
import { NotConfiguredPage } from "@/features/auth/NotConfiguredPage";
import { AppLayout } from "./layout/AppLayout";
import { DashboardPage } from "@/features/dashboard/DashboardPage";
import { UsersPage } from "@/features/users/UsersPage";
import { SitesPage } from "@/features/sites/SitesPage";
import { SiteDetailPage } from "@/features/sites/SiteDetailPage";
import { DatabasesPage } from "@/features/databases/DatabasesPage";
import { DNSPage } from "@/features/dns/DNSPage";
import { SSLPage } from "@/features/ssl/SSLPage";
import { AuditPage } from "@/features/audit/AuditPage";
import { RecordingsPage } from "@/features/recordings/RecordingsPage";
import { DockerPage } from "@/features/docker/DockerPage";
import { AppsPage } from "@/features/apps/AppsPage";
import { ModulesPage } from "@/features/modules/ModulesPage";

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
  // mounted once, above the routes, so every page shares it.
  return (
    <BrowserRouter>
      <CommandPalette />
      <Routes>
        <Route element={<AppLayout />}>
          <Route path="/" element={<DashboardPage />} />
          <Route path="/sites" element={<SitesPage />} />
          <Route path="/sites/:uid" element={<SiteDetailPage />} />
          <Route path="/databases" element={<DatabasesPage />} />
          <Route path="/dns" element={<DNSPage />} />
          <Route path="/ssl" element={<SSLPage />} />
          <Route path="/audit" element={<AuditPage />} />
          <Route path="/recordings" element={<RecordingsPage />} />
          <Route path="/docker" element={<DockerPage />} />
          <Route path="/apps" element={<AppsPage />} />
          <Route path="/modules" element={<ModulesPage />} />
          <Route path="/users" element={<UsersPage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
      <JobDrawer />
      <Toaster />
    </BrowserRouter>
  );
}
