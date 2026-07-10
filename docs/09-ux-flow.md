# 09 — UX Flow & Frontend Architecture

**Inspiration, not imitation.** We borrow hPanel's *clarity, speed, and task-oriented flow* — nothing else. Zero copied markup, CSS, icons, illustrations, assets, layout code, or branding. HeroPanel has its own visual identity, component library, and iconography.

## 1. Design principles
- **Task-first, not menu-first.** The operator's job ("deploy a site", "issue a cert") is the unit of navigation, not the technology.
- **Speed as UX.** Instant nav (SPA + prefetch), optimistic updates where safe, skeletons over spinners, realtime instead of manual refresh.
- **Progressive disclosure.** Simple defaults up front; advanced controls (php.ini, vhost, cgroup limits) tucked behind "Advanced".
- **Never leave the user guessing.** Every async action shows live progress, and every failure gives a plain-language reason + next step.
- **Keyboard-native.** Command palette + shortcuts for power users; full mouse/touch parity.
- **Accessible + responsive + themable.** WCAG-AA contrast, focus states, reduced-motion support, mobile-usable, light/dark.

## 2. Frontend stack (per spec)
| Concern | Choice |
|---------|--------|
| Framework | **React 18 + TypeScript**, **Vite** |
| Styling | **TailwindCSS** + design tokens (CSS vars) for theming |
| Components | **shadcn/ui** (Radix primitives) — generated then owned; original styling |
| Motion | **Framer Motion** (purposeful, reduced-motion aware) |
| Data | **TanStack Query** (server cache) + **Zustand** (UI/client state) |
| Realtime | WS client → React Query cache invalidation |
| Editor | **Monaco** (files, php.ini, vhost, compose, DNS zone) |
| Terminal | **xterm.js** (web terminal, per-user isolation) |
| Charts | Lightweight, dependency-checked lib per the dataviz system (accessible, theme-aware) |
| Icons | Original/opensource icon set referenced by *key* (never copied brand assets) |
| Forms | React Hook Form + Zod (schemas shared conceptually with API contract) |
| i18n | `locale`/`timezone` from user profile; message catalogs |

## 3. Information Architecture

```
┌ Top bar ───────────────────────────────────────────────────────────────┐
│ ⬢ HeroPanel   [ ⌘K Global Search ]        ◑ theme  🔔 notifs  ⚙  ◕ user │
├ Sidebar (collapsible) ─────────────┬ Content ─────────────────────────── │
│ Dashboard                          │                                     │
│ Sites            ▸ per-site tabs   │   context-aware, task-oriented views │
│ Databases                          │                                     │
│ Domains & DNS                      │                                     │
│ SSL                                │                                     │
│ Email                              │                                     │
│ Docker & Apps                      │                                     │
│ Files*  Terminal                   │                                     │
│ Backups                            │                                     │
│ Scheduler (Cron)                   │                                     │
│ Security                           │                                     │
│ Monitoring                         │                                     │
│ ── admin ──                        │                                     │
│ Users & Roles   Modules   Settings │                                     │
│ System   Updates   Audit Log       │                                     │
└────────────────────────────────────┴─────────────────────────────────── ┘
* Files visible only for baremetal sites; features gate on installed modules & RBAC scope
```

Navigation adapts to **role** (a client sees only their sites/tools; reseller sees their tenant; admin sees everything) and to **installed modules** (Docker greyed with an "Install module" CTA if absent).

## 4. Per-site workspace (the heart of the panel)
Selecting a site opens a focused workspace with tabs:
```
Overview | Domains | PHP/Runtime | Database | SSL | Files* | Git/Deploys |
Logs | Cron | Backups | Metrics | Security | Advanced
```
- **Overview**: status, primary domain, runtime + version, quick actions (restart, open, clone, suspend), live health/metrics tiles.
- **PHP/Runtime**: PHP **selector** (version per site), FPM sizing, memory/upload limits, OPcache/JIT toggles, extension manager, php.ini editor (Monaco). For node/python/go: entrypoint, build/start commands, env, instances, health path.
- **Advanced**: raw web-server vhost editor (validated before save), cgroup limits, disable_functions, open_basedir.

## 5. Signature flows (all async → live progress)

### Create Site (wizard)
```
Type (PHP/Laravel/WordPress/Node/Python/Go/Static/Docker/Proxy)
  → Domain (validate + optional DNS/SSL auto-setup)
  → Runtime & version (arch-aware options)
  → Deploy mode (Bare-metal | Git | Docker)  ← this decides File Manager availability
  → Resources (plan/limits; sensible defaults from detected RAM)
  → Review → Create
       │
       └─ 202 job: [ provisioning user → dir → FPM pool/runtime → vhost →
                     DNS → SSL → health ]  live steps in a progress drawer
```

### Git deployment
```
Connect source (GitHub/GitLab/Bitbucket via PAT/deploy key/OAuth)
  → pick repo + branch → build/install/output config → enable auto-deploy (webhook)
  → Deploy: live build log (streamed) → health → success
  → History list with per-run logs + one-click Rollback
```

### One-click app (Docker)
```
Browse catalog (n8n, Supabase, Ghost, Uptime Kuma, …) → RAM/feasibility check
  → fill template variables (with secret masking) → Deploy
  → live compose pull/up logs → URL + credentials surfaced securely
```

### Issue SSL
```
Pick domain(s) → challenge (HTTP-01 / DNS-01 for wildcard) → provider (LE/ZeroSSL/custom)
  → 202 job with live ACME steps → auto-renewal scheduled → status chip on domain
```

## 6. Cross-cutting UX systems

| System | Behavior |
|--------|----------|
| **Command palette (⌘K / Ctrl-K)** | Fuzzy actions + navigation + resource jump ("go to site acme", "issue cert", "restart php"). Backed by `/search` + a local action registry |
| **Global search** | Cross-resource (sites, domains, databases, containers, users) with typed results |
| **Notifications** | Realtime bell (WS `notifications:{user}`), toast for foreground events, history panel; levels info/success/warning/error |
| **Job/Progress drawer** | Any 202 action drops a live progress card (steps, %, streamed log link); persists in a "Tasks" tray until dismissed |
| **Realtime everywhere** | Metrics tiles, container states, deploy logs, cert status update without refresh (subscription-gated so idle tabs cost nothing) |
| **Keyboard shortcuts** | `g s` sites, `g d` databases, `/` search, `c` create-in-context, `?` shortcut cheatsheet |
| **Theme** | Light/Dark via `data-theme`; tokens drive both; respects OS preference; per-user override |
| **Glassmorphism** | Applied tastefully (overlays, command palette, drawers) — never at the cost of contrast/readability |
| **Empty & error states** | Every list has a helpful empty state with a primary action; errors show cause + remedy + correlation id |
| **Confirvmations** | Destructive actions (delete site, drop DB) require typed confirmation + show blast radius |

## 7. Realtime data-flow (frontend)
```
Mutation → 202 {job, ws_channel} → optimistic UI + progress card
        → WS events (job.progress/completed) → React Query invalidate → refetch canonical
Resource views subscribe to `resource:{id}` channels while mounted; unmount → unsubscribe
Metrics views subscribe to metrics channels → monitor module samples only while watched
```
This gives live UX with **zero idle polling** and a single source of truth (server), reconciled through the query cache.

## 8. Performance budget (frontend)
- Route-level **code-splitting** + prefetch on hover/idle; heavy tools (Monaco, xterm, charts) lazy-loaded only where used.
- Virtualized long lists (files, logs, containers, audit).
- Initial shell interactive **< 1.5 s** on a mid-range connection; interactions feel instant via optimistic + skeleton patterns.
- Bundle discipline: analyze on CI, budget-fail on regressions.

## 9. Onboarding
First login (bootstrap token) → create admin → optional MFA setup → a short **setup checklist** (point a domain, create your first site, secure the panel with a real cert) that teaches the mental model without blocking power users.

---
Next: [10 — Development Roadmap](10-roadmap.md)
