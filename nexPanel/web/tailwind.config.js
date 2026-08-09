/** @type {import('tailwindcss').Config} */
export default {
  // Theme is toggled by adding/removing the `dark` class on <html>.
  darkMode: "class",
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // Semantic tokens mapped to CSS variables so both themes share names.
        // See src/styles/index.css for what each step is for and the contrast
        // each pair is held to.
        surface: "rgb(var(--surface) / <alpha-value>)",
        "surface-2": "rgb(var(--surface-2) / <alpha-value>)",
        panel: "rgb(var(--panel) / <alpha-value>)",
        "panel-hover": "rgb(var(--panel-hover) / <alpha-value>)",
        border: "rgb(var(--border) / <alpha-value>)",
        "border-strong": "rgb(var(--border-strong) / <alpha-value>)",
        fg: "rgb(var(--fg) / <alpha-value>)",
        muted: "rgb(var(--muted) / <alpha-value>)",
        brand: "rgb(var(--brand) / <alpha-value>)",
        "brand-hover": "rgb(var(--brand-hover) / <alpha-value>)",
        "brand-active": "rgb(var(--brand-active) / <alpha-value>)",
        "brand-fg": "rgb(var(--brand-fg) / <alpha-value>)",
        "brand-subtle": "rgb(var(--brand-subtle) / <alpha-value>)",
        "brand-border": "rgb(var(--brand-border) / <alpha-value>)",
        danger: "rgb(var(--danger) / <alpha-value>)",
        "danger-fg": "rgb(var(--danger-fg) / <alpha-value>)",
        "danger-subtle": "rgb(var(--danger-subtle) / <alpha-value>)",
        success: "rgb(var(--success) / <alpha-value>)",
        "success-subtle": "rgb(var(--success-subtle) / <alpha-value>)",
        warning: "rgb(var(--warning) / <alpha-value>)",
        "warning-subtle": "rgb(var(--warning-subtle) / <alpha-value>)",
      },
      // Elevation comes from the theme so a shadow is tinted correctly in both
      // (a black shadow on a dark panel is invisible; a heavy one on white is
      // dirt). `shadow-sm` is overridden deliberately — it is already used
      // across the app and should pick the tuned value up everywhere.
      boxShadow: {
        sm: "var(--shadow-sm)",
        DEFAULT: "var(--shadow-md)",
        md: "var(--shadow-md)",
        lg: "var(--shadow-lg)",
      },
      // Focus rings are drawn against the page, so the offset must be the page
      // colour or it punches a white hole in the dark theme.
      ringOffsetColor: {
        DEFAULT: "rgb(var(--surface))",
      },
      fontFamily: {
        // "Inter Variable" is the family name the bundled @font-face declares
        // (see styles/index.css). The rest is the fallback chain used only
        // before it loads, or if the asset is missing — each platform's
        // *current* UI face, newest first.
        sans: [
          "Inter Variable",
          "Inter",
          "-apple-system",
          "BlinkMacSystemFont",
          "Segoe UI Variable Text",
          "Segoe UI",
          "Roboto",
          "Helvetica Neue",
          "Arial",
          "sans-serif",
          "Apple Color Emoji",
          "Segoe UI Emoji",
        ],
        mono: ["ui-monospace", "SFMono-Regular", "Menlo", "Cascadia Mono", "Consolas", "monospace"],
      },
      // Sizes carry their own leading and tracking so a heading is correct
      // wherever it is used, instead of every page re-tuning `tracking-tight`
      // by hand. Large text is pulled in progressively — untracked 24px is the
      // most common tell of an untypeset interface. Only `2xs` is new; the
      // familiar steps keep their sizes so existing pages do not shift.
      fontSize: {
        "2xs": ["0.6875rem", { lineHeight: "1rem", letterSpacing: "0.02em" }],
        xs: ["0.75rem", { lineHeight: "1.125rem" }],
        sm: ["0.875rem", { lineHeight: "1.25rem" }],
        base: ["1rem", { lineHeight: "1.5rem", letterSpacing: "-0.006em" }],
        lg: ["1.125rem", { lineHeight: "1.5rem", letterSpacing: "-0.012em" }],
        xl: ["1.25rem", { lineHeight: "1.625rem", letterSpacing: "-0.018em" }],
        "2xl": ["1.5rem", { lineHeight: "1.875rem", letterSpacing: "-0.022em" }],
        "3xl": ["1.875rem", { lineHeight: "2.25rem", letterSpacing: "-0.026em" }],
      },
    },
  },
  plugins: [],
};
