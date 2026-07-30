import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { translate, type Catalog, type Vars } from "./core";
import { en } from "./catalogs/en";

// The React layer over the pure core: a provider that holds the active language
// and its (possibly lazily-loaded) catalog, a `t` that closes over both, and the
// hooks screens use. English is bundled and is the fallback; other languages are
// code-split and fetched only when selected, so translating more of the app
// never weighs down the entry bundle the perf budget guards.

export type Lang = "en" | "es";

export const LANGUAGES: { code: Lang; label: string }[] = [
  { code: "en", label: "English" },
  { code: "es", label: "Español" },
];

const STORAGE_KEY = "hp_lang";

const loaders: Record<Lang, () => Promise<Catalog>> = {
  en: () => Promise.resolve(en),
  es: () => import("./catalogs/es").then((m) => m.es),
};

function initialLang(): Lang {
  const stored = typeof localStorage !== "undefined" ? localStorage.getItem(STORAGE_KEY) : null;
  return stored && stored in loaders ? (stored as Lang) : "en";
}

export type TFunc = (key: string, vars?: Vars, count?: number) => string;

const I18nContext = createContext<{ t: TFunc; lang: Lang; setLang: (l: Lang) => void } | null>(null);

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(initialLang);
  const [catalog, setCatalog] = useState<Catalog>(en);

  useEffect(() => {
    let cancelled = false;
    void loaders[lang]().then((c) => {
      if (!cancelled) setCatalog(c);
    });
    return () => {
      cancelled = true;
    };
  }, [lang]);

  const setLang = useCallback((l: Lang) => {
    try {
      localStorage.setItem(STORAGE_KEY, l);
    } catch {
      // Private-mode / disabled storage: the choice just does not persist.
    }
    setLangState(l);
  }, []);

  // t falls back through the active catalog → English → the key itself, so a
  // partial translation renders English rather than blanks.
  const t = useMemo<TFunc>(
    () => (key, vars, count) => translate(catalog, en, key, vars, count),
    [catalog],
  );

  const value = useMemo(() => ({ t, lang, setLang }), [t, lang, setLang]);
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useT(): TFunc {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useT must be used within an I18nProvider");
  return ctx.t;
}

export function useLang() {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useLang must be used within an I18nProvider");
  return { lang: ctx.lang, setLang: ctx.setLang };
}
