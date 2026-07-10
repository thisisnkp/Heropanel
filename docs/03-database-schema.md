# 03 — Database Schema

## 1. Conventions

- **Engine:** MariaDB 10.11+ (InnoDB, `utf8mb4`, `utf8mb4_0900_ai_ci`/`uca1400`). SQLite mode uses the same logical schema with type affinities.
- **Primary keys:** `BIGINT UNSIGNED AUTO_INCREMENT` internal; every externally-exposed row also has a `uid CHAR(26)` **ULID** (sortable, non-enumerable in the API). APIs expose `uid`, never the auto-increment.
- **Timestamps:** `created_at`, `updated_at` (`TIMESTAMP(6)`), soft-delete via nullable `deleted_at` where retention matters.
- **Money/quotas:** integers in base units (bytes, seconds) — never floats.
- **JSON:** MariaDB `JSON`/`LONGTEXT` for flexible sub-configs (e.g. php.ini overrides), with app-side schema validation.
- **FKs:** enforced; `ON DELETE` chosen per relationship (RESTRICT for accounting rows, CASCADE for owned children).
- **Migrations:** `golang-migrate`, files `internal/repository/migrations/NNNN_description.{up,down}.sql`. Forward-only in prod with tested `down` for dev. CI asserts up→down→up is clean.

Diagram legend: `PK` primary key, `FK→` foreign key, `U` unique, `IX` indexed.

## 2. Identity, Auth & RBAC

```
users
  id PK · uid U · email U · username U · display_name
  password_hash (argon2id, nullable if SSO-only) · password_changed_at
  status ENUM(active,suspended,pending,locked) · locale · timezone
  totp_secret_enc (nullable) · totp_enabled BOOL
  failed_logins INT · locked_until · last_login_at · last_login_ip
  created_at · updated_at · deleted_at

webauthn_credentials
  id PK · user_id FK→users · credential_id U · public_key · sign_count
  aaguid · name · transports JSON · created_at · last_used_at

roles
  id PK · uid · name U · slug U (admin,reseller,developer,client,custom_*)
  is_system BOOL · description · created_at

permissions
  id PK · slug U        # e.g. site.create, dns.zone.edit, docker.container.restart
  resource · action · description

role_permissions            # M:N
  role_id FK→roles · permission_id FK→permissions   (PK composite)

user_roles                  # M:N, role can be scoped to a resource subtree
  user_id FK→users · role_id FK→roles · scope_type (global|site|reseller)
  scope_id (nullable)  (PK composite: user_id,role_id,scope_type,scope_id)

sessions
  id PK · uid · user_id FK→users · token_hash U · ip · user_agent
  device_label · created_at · last_seen_at · expires_at · revoked_at

api_keys
  id PK · uid · user_id FK→users · name · prefix U · key_hash
  scopes JSON · last_used_at · expires_at · revoked_at · created_at

login_attempts (rolling, for brute-force + audit)
  id PK · email · ip · success BOOL · reason · created_at   IX(ip,created_at)
```

RBAC model: **permission-based, role-grouped, optionally resource-scoped**. A reseller's role is scoped so they see only their own sites/clients. Permission checks resolve `(user → roles → permissions)` intersected with `scope`.

## 3. Sites & Isolation

```
sites
  id PK · uid U · owner_id FK→users · reseller_id FK→users (nullable)
  name · primary_domain U · type ENUM(php,laravel,wordpress,node,python,go,static,docker,proxy)
  app_framework (nullable: express,nestjs,nextjs,nuxt,flask,fastapi,django,gin,fiber…)
  deploy_mode ENUM(baremetal,git,docker)          # gates File Manager (doc: FM only for baremetal)
  status ENUM(active,suspended,provisioning,error,disabled)
  webserver ENUM(openlitespeed,nginx,caddy,apache) DEFAULT openlitespeed
  document_root · runtime_version (nullable)       # php/node/python/go version tag
  created_at · updated_at · deleted_at
  IX(owner_id) · IX(status)

site_system_users            # the dedicated Linux user/group per site (doc 05)
  id PK · site_id FK→sites U · linux_user U · linux_uid U · linux_gid U
  home_dir · shell · jailed BOOL · created_at

site_limits                  # resource quotas / process limits
  site_id FK→sites PK · cpu_quota_pct · mem_limit_bytes · pids_max
  io_read_bps · io_write_bps · disk_quota_bytes · inode_quota
  bandwidth_month_bytes · php_workers_max

domains
  id PK · uid · site_id FK→sites · fqdn U · kind ENUM(primary,alias,subdomain,redirect)
  redirect_target (nullable) · redirect_code (nullable)
  ssl_cert_id FK→ssl_certificates (nullable) · force_https BOOL
  status · created_at   IX(site_id)

php_pools                    # dedicated PHP-FPM pool per PHP site
  id PK · site_id FK→sites U · php_version · pm ENUM(dynamic,ondemand,static)
  pm_max_children · pm_start_servers · pm_min_spare · pm_max_spare · pm_max_requests
  memory_limit_mb · upload_max_mb · post_max_mb · max_execution · opcache_enabled
  jit_enabled · ini_overrides JSON · extensions JSON · socket_path · created_at

app_runtimes                 # node/python/go process definitions (non-docker)
  id PK · site_id FK→sites · kind ENUM(node,python,go) · version
  entrypoint · start_command · build_command · env JSON
  port · instances · process_manager ENUM(systemd,pm2ish) · health_path
  status · created_at
```

## 4. Databases (managed DB engines)

```
db_instances                 # a database created for a user/site
  id PK · uid · owner_id FK→users · site_id FK→sites (nullable)
  engine ENUM(mariadb,postgres,mongodb,redis) · name U(per engine) · charset
  size_bytes · status · created_at   IX(owner_id)

db_users
  id PK · uid · engine · username U(per engine) · host
  password_enc · owner_id FK→users · created_at

db_grants                    # M:N db_users ↔ db_instances with privilege set
  db_user_id FK→db_users · db_instance_id FK→db_instances
  privileges JSON  (PK composite)
```

## 5. DNS

```
dns_zones
  id PK · uid · owner_id FK→users · origin U (example.com.) · type ENUM(primary,secondary)
  serial · refresh · retry · expire · minimum · primary_ns · admin_email
  dnssec_enabled BOOL · status · created_at   IX(owner_id)

dns_records
  id PK · uid · zone_id FK→dns_zones · name · type ENUM(A,AAAA,CNAME,MX,TXT,NS,SRV,CAA,PTR,SOA)
  ttl · priority (nullable) · content · flags JSON (weight,port,caa tag…)
  disabled BOOL · created_at · updated_at   IX(zone_id,type)

dnssec_keys
  id PK · zone_id FK→dns_zones · key_tag · algorithm · flags(KSK/ZSK)
  public_key · private_key_enc · ds_record · active BOOL · created_at
```

## 6. SSL / TLS

```
ssl_certificates
  id PK · uid · owner_id FK→users · provider ENUM(letsencrypt,zerossl,custom,self_signed)
  common_name · sans JSON · is_wildcard BOOL
  cert_pem · chain_pem · privkey_enc · issued_at · expires_at
  auto_renew BOOL · renew_after · status ENUM(valid,expiring,expired,pending,failed)
  challenge_type ENUM(http-01,dns-01) · created_at   IX(expires_at)

acme_accounts
  id PK · provider · email · account_key_enc · directory_url · eab_kid · eab_hmac_enc
  created_at
```

## 7. Mail

```
mail_domains
  id PK · uid · owner_id FK→users · domain U · dkim_selector · dkim_privkey_enc
  dkim_pubkey · spf_record · dmarc_policy · status · created_at

mail_accounts
  id PK · uid · mail_domain_id FK→mail_domains · address U · password_enc
  quota_bytes · used_bytes · active BOOL · created_at

mail_aliases
  id PK · mail_domain_id FK→mail_domains · source U · destinations JSON · created_at

mail_forwarders
  id PK · mail_account_id FK→mail_accounts · destination · keep_copy BOOL

mail_queue_snapshots         # periodic snapshot for UI (source of truth is postfix)
  id PK · captured_at · queued INT · deferred INT · held INT · sample JSON
```

## 8. Docker & One-Click Apps

```
docker_stacks                # a compose project / deployment
  id PK · uid · owner_id FK→users · site_id FK→sites (nullable) · name U
  template_slug (nullable: n8n,supabase,ghost,…) · compose_yaml LONGTEXT
  status ENUM(running,stopped,deploying,error) · created_at   IX(owner_id)

docker_containers            # cached inventory (source of truth = docker engine)
  id PK · stack_id FK→docker_stacks (nullable) · container_id U · name · image
  state · status · ports JSON · restart_policy · created_at · synced_at

docker_volumes  / docker_networks / docker_images   # thin inventory caches
  id PK · name · driver/size/… · in_use BOOL · synced_at

app_templates                # catalog of one-click templates (seeded, updatable)
  id PK · slug U · name · category · description · icon · compose_template LONGTEXT
  variables JSON (name,label,type,default,required,secret)
  min_ram_mb · version · source · verified BOOL
```

## 9. Git Deployments

```
git_sources
  id PK · uid · owner_id FK→users · provider ENUM(github,gitlab,bitbucket,generic)
  repo_url · default_branch · auth_kind ENUM(deploy_key,pat,oauth)
  credential_ref (→secrets, encrypted) · created_at

git_deployments
  id PK · uid · site_id FK→sites · git_source_id FK→git_sources · branch
  auto_deploy BOOL · build_command · install_command · output_dir
  webhook_secret_enc · webhook_path U · created_at

deploy_runs
  id PK · uid · git_deployment_id FK→git_deployments · commit_sha · commit_msg
  status ENUM(queued,building,deploying,succeeded,failed,rolled_back)
  triggered_by ENUM(webhook,manual,schedule) · started_at · finished_at
  log_ref (→ /var/log/heropanel/jobs/…) · artifact_ref   IX(git_deployment_id,started_at)

ssh_keys                     # panel-managed keypairs (deploy keys, user keys)
  id PK · uid · owner_id FK→users · name · public_key · private_key_enc (nullable)
  fingerprint U · kind ENUM(deploy,user,host) · created_at
```

## 10. Backups

```
backup_targets               # where backups go
  id PK · uid · owner_id FK→users · kind ENUM(local,s3,r2,b2,gdrive,onedrive,dropbox,sftp)
  config_enc JSON (bucket, keys, path…) · created_at

backup_policies
  id PK · uid · owner_id FK→users · name · scope ENUM(site,database,mail,full,custom)
  scope_ref JSON · target_id FK→backup_targets
  mode ENUM(full,incremental) · schedule_cron · retention_count · retention_days
  compression ENUM(none,zstd,gzip) · encryption BOOL · enabled BOOL · created_at

backup_runs
  id PK · uid · policy_id FK→backup_policies · mode · status
  size_bytes · started_at · finished_at · manifest_ref · error   IX(policy_id,started_at)

restore_runs
  id PK · uid · backup_run_id FK→backup_runs · target_site_id · status
  strategy ENUM(in_place,new_site) · started_at · finished_at · log_ref
```

## 11. Scheduler (cron)

```
cron_jobs
  id PK · uid · owner_id FK→users · site_id FK→sites (nullable) · run_as_user
  name · schedule (cron expr) · command · working_dir · env JSON
  timeout_sec · overlap_policy ENUM(skip,queue,kill) · enabled BOOL
  system_ref (crontab/systemd-timer id) · created_at   IX(owner_id)

cron_runs
  id PK · cron_job_id FK→cron_jobs · status · exit_code
  started_at · finished_at · output_ref   IX(cron_job_id,started_at)
```

## 12. Security

```
firewall_rules
  id PK · uid · direction ENUM(in,out) · action ENUM(allow,deny,drop)
  protocol · port_range · source_cidr · dest_cidr · priority · comment
  backend ENUM(nftables,ufw) · enabled BOOL · created_at

fail2ban_jails / crowdsec_decisions          # thin state/inventory
  id PK · name/ip · reason · bantime · created_at · expires_at

ip_lists
  id PK · list ENUM(allow,block,geo) · cidr_or_country · scope · comment · created_at

scan_runs                    # malware / rootkit / lynis scans
  id PK · uid · engine ENUM(clamav,maldet,rkhunter,lynis) · scope_ref
  status · findings INT · report_ref · started_at · finished_at   IX(engine,started_at)

quarantine_items
  id PK · scan_run_id FK→scan_runs · original_path · quarantine_path
  sha256 · signature · action ENUM(quarantined,restored,deleted) · created_at

file_integrity_baselines / file_integrity_events    # FIM
  id PK · path · sha256 · mode · owner · baseline_at   /   id PK · path · change_type · detected_at

audit_log                    # append-only, hash-chained (see doc 05)
  id PK · uid · actor_user_id (nullable) · actor_ip · actor_kind ENUM(user,apikey,system,broker)
  action · resource_type · resource_id · outcome ENUM(success,failure,denied)
  detail JSON · prev_hash · row_hash · created_at   IX(actor_user_id,created_at) · IX(resource_type,resource_id)
```

## 13. Monitoring & Metrics

```
metric_samples               # short-retention high-res; rolled up then trimmed
  id PK · scope ENUM(node,site,container,service,db) · scope_id
  metric (cpu,mem,disk,io_r,io_w,net_in,net_out,temp,conns…) · value DOUBLE
  captured_at   IX(scope,scope_id,metric,captured_at)     # PARTITION by day; TTL-trim

metric_rollups               # 1m→5m→1h aggregates for long-range charts
  id PK · scope · scope_id · metric · bucket · window ENUM(5m,1h,1d)
  min · max · avg · p95 · bucket_start   IX(scope,scope_id,metric,window,bucket_start)

alerts
  id PK · uid · name · scope · metric · comparator · threshold · duration_sec
  channels JSON (email,webhook,telegram…) · enabled BOOL · created_at

alert_events
  id PK · alert_id FK→alerts · state ENUM(firing,resolved) · value · at
```

## 14. Modules, Notifications, Settings, Jobs

```
modules                      # installed module registry (mirrors /opt/heropanel/modules)
  id PK · slug U · name · version · state ENUM(installed,enabled,disabled,error)
  arch · checksum · signature · installed_at · enabled_at · config_ref · health

notifications
  id PK · uid · user_id FK→users (nullable=broadcast) · level ENUM(info,success,warning,error)
  title · body · resource_type · resource_id · read_at · created_at   IX(user_id,read_at)

settings                     # hot-reloadable key/value (branding, flags, limits)
  key PK · value JSON · category · updated_by · updated_at

jobs                         # durable mirror of Redis Stream job entries
  id PK · uid U · type · status ENUM(queued,running,succeeded,failed,cancelled)
  owner_user_id · resource_type · resource_id · progress TINYINT
  payload JSON · result JSON · error · attempts · started_at · finished_at
  log_ref · created_at   IX(status) · IX(owner_user_id,created_at)
```

## 15. Encryption of secrets at rest
Columns suffixed `_enc` are encrypted with **AES-256-GCM** using a data key wrapped by a master key from `/etc/heropanel/secrets.env` (or OS keyring). The DB never stores plaintext credentials, private keys, or tokens. Rotation re-wraps data keys without re-encrypting every row (envelope encryption). Details in [05](05-security-architecture.md).

## 16. Indexing & Partitioning strategy
- Hot list endpoints (`sites`, `jobs`, `audit_log`, `metric_samples`) get covering composite indexes matching their default sort/filter.
- `metric_samples` and `audit_log` are **range-partitioned by day**; a scheduled job trims/rolls up and drops old partitions cheaply.
- All `uid` columns are unique-indexed (API lookups by ULID).

---
Next: [04 — API Design](04-api-design.md)
