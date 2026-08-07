import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { App } from "./app/App";
import { queryClient } from "./lib/queryClient";
import { initTheme } from "./stores/theme";
import { I18nProvider } from "./lib/i18n";
import { ErrorBoundary } from "./components/ErrorBoundary";
import "./styles/index.css";

initTheme();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    {/* Root boundary: the last-resort net so an error anywhere shows a recover
        screen instead of a blank page. */}
    <ErrorBoundary title="HeroPanel">
      <I18nProvider>
        <QueryClientProvider client={queryClient}>
          <App />
        </QueryClientProvider>
      </I18nProvider>
    </ErrorBoundary>
  </StrictMode>,
);
