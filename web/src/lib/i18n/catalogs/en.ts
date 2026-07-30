import type { Catalog } from "../core";

// English is the base catalog: it is complete, ships in the entry bundle (it is
// needed on first paint), and is the fallback every other language resolves
// against. Keys are flat and dotted, grouped by surface.
export const en: Catalog = {
  "app.tagline": "HeroPanel — the fast, modern hosting control panel.",

  "lang.label": "Language",

  "auth.field.email": "Email",
  "auth.field.password": "Password",
  "auth.field.username": "Username",

  "auth.login.title": "Sign in",
  "auth.login.subtitle": "Welcome back to HeroPanel",
  "auth.login.submit": "Sign in",
  "auth.login.or": "or",
  "auth.login.passkey": "Sign in with a passkey",
  "auth.login.enterEmailFirst": "Enter your email first.",
  "auth.login.failed": "Login failed.",
  "auth.login.passkeyFailed": "Passkey sign-in failed.",

  "auth.bootstrap.title": "Welcome to HeroPanel",
  "auth.bootstrap.subtitle": "Create the first administrator account",
  "auth.bootstrap.passwordHint": "At least 8 characters.",
  "auth.bootstrap.submit": "Create administrator",
  "auth.bootstrap.failed": "Setup failed.",
};
