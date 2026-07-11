import { create } from "zustand";
import { persist } from "zustand/middleware";

type Theme = "light" | "dark";

interface ThemeState {
  theme: Theme;
  toggle: () => void;
  set: (t: Theme) => void;
}

function apply(theme: Theme) {
  const root = document.documentElement;
  root.classList.toggle("dark", theme === "dark");
  root.dataset.theme = theme;
}

export const useTheme = create<ThemeState>()(
  persist(
    (set, get) => ({
      theme: "dark",
      toggle: () => {
        const next = get().theme === "dark" ? "light" : "dark";
        apply(next);
        set({ theme: next });
      },
      set: (t) => {
        apply(t);
        set({ theme: t });
      },
    }),
    {
      name: "hp-theme",
      onRehydrateStorage: () => (state) => {
        apply(state?.theme ?? "dark");
      },
    },
  ),
);

// Ensure the initial paint matches the persisted theme.
export function initTheme() {
  const raw = localStorage.getItem("hp-theme");
  let theme: Theme = "dark";
  if (raw) {
    try {
      theme = JSON.parse(raw).state.theme ?? "dark";
    } catch {
      /* ignore */
    }
  }
  apply(theme);
}
