import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { Spinner } from "@/components/ui";
import { useAuthStatus, useMe } from "@/features/auth/auth";
import { LoginPage } from "@/features/auth/LoginPage";
import { BootstrapPage } from "@/features/auth/BootstrapPage";
import { AppLayout } from "./layout/AppLayout";
import { DashboardPage } from "@/features/dashboard/DashboardPage";
import { UsersPage } from "@/features/users/UsersPage";
import { SitesPage } from "@/features/sites/SitesPage";

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

  // Not authenticated: show bootstrap (first run) or login.
  if (!me.data) {
    if (status.data?.needs_bootstrap) {
      return <BootstrapPage />;
    }
    return <LoginPage />;
  }

  // Authenticated app.
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<AppLayout />}>
          <Route path="/" element={<DashboardPage />} />
          <Route path="/sites" element={<SitesPage />} />
          <Route path="/users" element={<UsersPage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
