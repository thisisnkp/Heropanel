import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { Spinner } from "@/components/ui";
import { Toaster } from "@/components/Toaster";
import { JobDrawer } from "@/components/JobDrawer";
import { CommandPalette } from "@/components/CommandPalette";
import { useAuthStatus, useMe } from "@/features/auth/auth";
import { LoginPage } from "@/features/auth/LoginPage";
import { BootstrapPage } from "@/features/auth/BootstrapPage";
import { AppLayout } from "./layout/AppLayout";
import { DashboardPage } from "@/features/dashboard/DashboardPage";
import { UsersPage } from "@/features/users/UsersPage";
import { SitesPage } from "@/features/sites/SitesPage";
import { SiteDetailPage } from "@/features/sites/SiteDetailPage";
import { DatabasesPage } from "@/features/databases/DatabasesPage";
import { DNSPage } from "@/features/dns/DNSPage";
import { SSLPage } from "@/features/ssl/SSLPage";
import { AuditPage } from "@/features/audit/AuditPage";
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
