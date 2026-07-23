package apps

import "fmt"

// The built-in templates.
//
// Each is a small, real compose file rather than a link to an upstream one, so
// what the panel deploys is exactly what is reviewed here — no fetch of remote
// YAML that could change under the operator. Three conventions hold across all
// of them:
//
//   - the web port is published to **127.0.0.1** only, never 0.0.0.0, so an app
//     is reachable through a reverse proxy and not directly from the internet
//     (the same reason the container-create capability binds loopback);
//   - persistent data lives in a **named volume**, so a tear-down and redeploy
//     does not lose it, and `compose down` (which never removes volumes) is safe;
//   - every secret is a generated field, never a default in the YAML.

func init() {
	register(ghost)
	register(uptimeKuma)
	register(nocodb)
	register(vaultwarden)
	register(gitea)
	register(redisApp)
	register(postgresApp)
	register(nginxDemo)
}

// ghost — a publishing platform. It needs a database password it never shows the
// operator, which is exactly what a generated secret is for.
var ghost = Template{
	Slug: "ghost", Name: "Ghost", Category: "CMS", Icon: "ghost",
	Description: "Modern publishing platform for blogs and newsletters.",
	MinMemoryMB: 512, HTTPPort: 2368,
	Fields: []Field{
		{Key: "url", Label: "Public URL", Placeholder: "https://blog.example.com", Required: true,
			Help: "The address the site will be served on. Ghost bakes this into links, so it must be the real one."},
		{Key: "db_password", Label: "Database password", Secret: true},
	},
	compose: func(in RenderInput) string {
		return fmt.Sprintf(`services:
  ghost:
    image: ghost:5-alpine
    restart: unless-stopped
    ports: ["127.0.0.1:%d:2368"]
    environment:
      url: %q
      database__client: mysql
      database__connection__host: db
      database__connection__user: ghost
      database__connection__password: %q
      database__connection__database: ghost
    volumes: ["ghost-content:/var/lib/ghost/content"]
    depends_on: [db]
  db:
    image: mysql:8
    restart: unless-stopped
    environment:
      MYSQL_ROOT_PASSWORD: %q
      MYSQL_DATABASE: ghost
      MYSQL_USER: ghost
      MYSQL_PASSWORD: %q
    volumes: ["ghost-db:/var/lib/mysql"]
volumes:
  ghost-content:
  ghost-db:
`, in.HostPort, in.Values["url"], in.Values["db_password"], in.Values["db_password"], in.Values["db_password"])
	},
}

// uptimeKuma — a status/uptime monitor. No secret, no database: the smallest
// useful one-click, and half of the phase's exit criteria.
var uptimeKuma = Template{
	Slug: "uptime-kuma", Name: "Uptime Kuma", Category: "Monitoring", Icon: "activity",
	Description: "Self-hosted uptime monitoring with a clean dashboard.",
	MinMemoryMB: 256, HTTPPort: 3001,
	compose: func(in RenderInput) string {
		return fmt.Sprintf(`services:
  uptime-kuma:
    image: louislam/uptime-kuma:1
    restart: unless-stopped
    ports: ["127.0.0.1:%d:3001"]
    volumes: ["uptime-kuma:/app/data"]
volumes:
  uptime-kuma:
`, in.HostPort)
	},
}

// nocodb — a spreadsheet-database. Ships with SQLite in a volume, so it is a
// single container with a generated admin/JWT secret.
var nocodb = Template{
	Slug: "nocodb", Name: "NocoDB", Category: "Database", Icon: "table",
	Description: "Turn any database into a smart spreadsheet.",
	MinMemoryMB: 512, HTTPPort: 8080,
	Fields: []Field{{Key: "jwt_secret", Label: "JWT secret", Secret: true}},
	compose: func(in RenderInput) string {
		return fmt.Sprintf(`services:
  nocodb:
    image: nocodb/nocodb:latest
    restart: unless-stopped
    ports: ["127.0.0.1:%d:8080"]
    environment:
      NC_AUTH_JWT_SECRET: %q
    volumes: ["nocodb:/usr/app/data"]
volumes:
  nocodb:
`, in.HostPort, in.Values["jwt_secret"])
	},
}

// vaultwarden — a Bitwarden-compatible password manager. The admin token is
// generated; defaulting it would be handing out the admin panel.
var vaultwarden = Template{
	Slug: "vaultwarden", Name: "Vaultwarden", Category: "Security", Icon: "shield",
	Description: "Lightweight Bitwarden-compatible password manager.",
	MinMemoryMB: 256, HTTPPort: 80,
	Fields: []Field{
		{Key: "domain", Label: "Public URL", Placeholder: "https://vault.example.com", Required: true},
		{Key: "admin_token", Label: "Admin token", Secret: true},
	},
	compose: func(in RenderInput) string {
		return fmt.Sprintf(`services:
  vaultwarden:
    image: vaultwarden/server:latest
    restart: unless-stopped
    ports: ["127.0.0.1:%d:80"]
    environment:
      DOMAIN: %q
      ADMIN_TOKEN: %q
    volumes: ["vaultwarden:/data"]
volumes:
  vaultwarden:
`, in.HostPort, in.Values["domain"], in.Values["admin_token"])
	},
}

// gitea — a self-hosted git service.
var gitea = Template{
	Slug: "gitea", Name: "Gitea", Category: "Developer", Icon: "git-branch",
	Description: "Lightweight self-hosted Git service.",
	MinMemoryMB: 512, HTTPPort: 3000,
	compose: func(in RenderInput) string {
		return fmt.Sprintf(`services:
  gitea:
    image: gitea/gitea:1
    restart: unless-stopped
    ports: ["127.0.0.1:%d:3000"]
    environment:
      USER_UID: "1000"
      USER_GID: "1000"
    volumes: ["gitea:/data"]
volumes:
  gitea:
`, in.HostPort)
	},
}

// redisApp — a plain Redis, for an app that needs a cache. No HTTP port: it is
// reached over the docker network, not published, so HTTPPort is zero and no
// loopback publish is emitted.
var redisApp = Template{
	Slug: "redis", Name: "Redis", Category: "Database", Icon: "database",
	Description: "In-memory data store for caching and queues.",
	MinMemoryMB: 128, HTTPPort: 0,
	Fields: []Field{{Key: "password", Label: "Password", Secret: true}},
	compose: func(in RenderInput) string {
		return fmt.Sprintf(`services:
  redis:
    image: redis:7-alpine
    restart: unless-stopped
    command: ["redis-server", "--requirepass", %q]
    ports: ["127.0.0.1:%d:6379"]
    volumes: ["redis:/data"]
volumes:
  redis:
`, in.Values["password"], in.HostPort)
	},
}

// postgresApp — a plain PostgreSQL.
var postgresApp = Template{
	Slug: "postgres", Name: "PostgreSQL", Category: "Database", Icon: "database",
	Description: "The world's most advanced open-source relational database.",
	MinMemoryMB: 256, HTTPPort: 5432,
	Fields: []Field{
		{Key: "db_name", Label: "Database name", Placeholder: "appdb", Required: true},
		{Key: "password", Label: "Password", Secret: true},
	},
	compose: func(in RenderInput) string {
		return fmt.Sprintf(`services:
  postgres:
    image: postgres:16-alpine
    restart: unless-stopped
    ports: ["127.0.0.1:%d:5432"]
    environment:
      POSTGRES_DB: %q
      POSTGRES_PASSWORD: %q
    volumes: ["postgres:/var/lib/postgresql/data"]
volumes:
  postgres:
`, in.HostPort, in.Values["db_name"], in.Values["password"])
	},
}

// nginxDemo — a tiny static server, the cheapest thing to prove the pipeline
// end to end in a test without pulling a database.
var nginxDemo = Template{
	Slug: "nginx-demo", Name: "Nginx (demo)", Category: "Developer", Icon: "server",
	Description: "A minimal Nginx server — useful for testing a deploy end to end.",
	MinMemoryMB: 32, HTTPPort: 80,
	compose: func(in RenderInput) string {
		return fmt.Sprintf(`services:
  web:
    image: nginx:alpine
    restart: unless-stopped
    ports: ["127.0.0.1:%d:80"]
`, in.HostPort)
	},
}
