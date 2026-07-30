import { Select } from "@/components/ui";
import { LANGUAGES, useLang, useT, type Lang } from "@/lib/i18n";

// A compact language chooser. It is labelled for assistive tech and small enough
// to sit in an auth screen footer or an account page. Switching a language that
// is code-split fetches its catalog on demand.
export function LanguageSwitcher({ className }: { className?: string }) {
  const { lang, setLang } = useLang();
  const t = useT();
  return (
    <label className={className}>
      <span className="sr-only">{t("lang.label")}</span>
      <Select
        aria-label={t("lang.label")}
        value={lang}
        onChange={(e) => setLang(e.target.value as Lang)}
        className="h-8 w-auto text-xs"
      >
        {LANGUAGES.map((l) => (
          <option key={l.code} value={l.code}>
            {l.label}
          </option>
        ))}
      </Select>
    </label>
  );
}
