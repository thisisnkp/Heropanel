import type { Catalog } from "../core";

// Spanish translation of the sign-in surface. It is loaded on demand (not in the
// entry bundle) and is deliberately partial: any key it does not carry falls
// back to English, which is exactly the path the core's fallback chain exists to
// serve as more of the app is translated over time.
export const es: Catalog = {
  "app.tagline": "HeroPanel — el panel de control de hosting rápido y moderno.",

  "lang.label": "Idioma",

  "auth.field.email": "Correo electrónico",
  "auth.field.password": "Contraseña",
  "auth.field.username": "Nombre de usuario",

  "auth.login.title": "Iniciar sesión",
  "auth.login.subtitle": "Bienvenido de nuevo a HeroPanel",
  "auth.login.submit": "Iniciar sesión",
  "auth.login.or": "o",
  "auth.login.passkey": "Iniciar sesión con una clave de acceso",
  "auth.login.enterEmailFirst": "Introduce tu correo primero.",
  "auth.login.failed": "Error al iniciar sesión.",
  "auth.login.passkeyFailed": "Error al iniciar sesión con la clave de acceso.",

  "auth.bootstrap.title": "Bienvenido a HeroPanel",
  "auth.bootstrap.subtitle": "Crea la primera cuenta de administrador",
  "auth.bootstrap.passwordHint": "Al menos 8 caracteres.",
  "auth.bootstrap.submit": "Crear administrador",
  "auth.bootstrap.failed": "Error en la configuración.",
};
