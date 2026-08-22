<script setup lang="ts">
/**
 * The first-run infrastructure wizard.
 *
 * It asks four things, and three of them are not about the stack — because the
 * stack is not a question. NexPanel installs OpenLiteSpeed, MariaDB, phpMyAdmin,
 * ClamAV, Fail2Ban, ModSecurity with the OWASP rules, and nftables on every
 * host. Those are listed here, not offered: a panel where half the fleet skipped
 * the WAF is a panel that cannot say anything true about its own security.
 *
 * The one stack choice is the web server, and only because one of the two costs
 * money. Everything else the wizard asks — DNS here or elsewhere, mail or no
 * mail, what this installation's own domain is — genuinely depends on the
 * deployment.
 *
 * The catalogs come from npd rather than being written here, so the panel stays
 * the authority on what it can actually manage.
 */
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { api, ApiRequestError, type SetupInfo, type SetupSelection } from "@/lib/api";
import { useSessionStore } from "@/stores/session";

const router = useRouter();
const session = useSessionStore();

const info = ref<SetupInfo | null>(null);
const loading = ref(true);
const busy = ref(false);
const error = ref<string | null>(null);

const webserver = ref("openlitespeed");
const licenseKey = ref("");
const manageDns = ref(true);
const createMail = ref(false);
const panelDomain = ref("");
const panelIpv4 = ref("");

const dbLabel = computed(() => info.value?.db_engines[0]?.label ?? "MariaDB");
const licensed = computed(() => webserver.value === "litespeed_enterprise");

onMounted(async () => {
  try {
    info.value = await api.get<SetupInfo>("/setup");
    if (info.value.state?.webserver) webserver.value = info.value.state.webserver;
  } catch (e) {
    error.value = e instanceof ApiRequestError ? e.message : "Could not load the setup options.";
  } finally {
    loading.value = false;
  }
});

async function finish() {
  busy.value = true;
  error.value = null;
  const body: SetupSelection = {
    webserver: webserver.value,
    // Sent explicitly rather than left empty: the record should say what this
    // host runs, not "whatever the default was on the day it was installed".
    db_engine: "mariadb",
    manage_dns: manageDns.value,
    create_mail: createMail.value,
    license_key: licensed.value ? licenseKey.value.trim() : "",
    panel_domain: panelDomain.value.trim(),
    panel_ipv4: panelIpv4.value.trim(),
  };
  try {
    await api.post("/setup", body);
    await session.refreshStatus();
    await router.replace("/");
  } catch (e) {
    error.value = e instanceof ApiRequestError ? e.message : "Setup could not be completed.";
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <main id="nx-main" class="setup">
    <div class="setup__inner">
      <header class="setup__head">
        <p class="setup__brand">NexPanel</p>
        <h1 class="setup__title">Set up this server</h1>
        <p class="setup__sub">
          A few questions about this deployment. The hosting stack itself is not one of them.
        </p>
      </header>

      <NxCallout v-if="error" tone="danger">{{ error }}</NxCallout>
      <NxSkeleton v-if="loading" height="220px" />

      <template v-else>
        <NxCard title="Web server">
          <p class="setup__note">
            Both are LiteSpeed. The Enterprise edition needs a licence; everything else about the
            panel is identical.
          </p>
          <div class="setup__choices" role="radiogroup" aria-label="Web server">
            <label v-for="o in info?.webservers ?? []" :key="o.id" class="setup__choice">
              <input v-model="webserver" type="radio" name="webserver" :value="o.id" />
              <span class="setup__choice-body">
                <span class="setup__choice-label">{{ o.label }}</span>
                <span v-if="o.note" class="setup__choice-note">{{ o.note }}</span>
              </span>
            </label>
          </div>

          <NxField
            v-if="licensed"
            label="LiteSpeed licence serial"
            hint="Leave empty to install with a trial licence."
          >
            <template #default="{ id }">
              <NxInput :id="id" v-model="licenseKey" mono placeholder="XXXX-XXXX-XXXX-XXXX" />
            </template>
          </NxField>
        </NxCard>

        <NxCard title="Installed on every host">
          <p class="setup__note">
            These are not options. They are what a NexPanel server is, and sites are configured
            assuming every one of them is present.
          </p>
          <ul class="setup__baseline">
            <li class="setup__item">
              <NxIcon name="database" size="sm" />
              <span class="setup__item-label">{{ dbLabel }}</span>
              <span class="setup__item-note">database server</span>
            </li>
            <li v-for="c in info?.baseline ?? []" :key="c.id" class="setup__item">
              <NxIcon name="check-circle" size="sm" />
              <span class="setup__item-label">{{ c.label }}</span>
              <span v-if="c.note" class="setup__item-note">{{ c.note }}</span>
            </li>
          </ul>
        </NxCard>

        <NxCard title="This deployment">
          <div class="setup__toggles">
            <label class="setup__toggle">
              <NxToggle v-model="manageDns" aria-label="Manage DNS on this server" />
              <span class="setup__choice-body">
                <span class="setup__choice-label">Run DNS here</span>
                <span class="setup__choice-note">
                  Installs BIND and lets the panel serve zones. Leave off if your registrar or
                  Cloudflare holds them.
                </span>
              </span>
            </label>

            <label class="setup__toggle">
              <NxToggle v-model="createMail" aria-label="Run a mail server on this server" />
              <span class="setup__choice-body">
                <span class="setup__choice-label">Run mail here</span>
                <span class="setup__choice-note">
                  Installs Postfix and Dovecot. A new IP address usually has poor sending
                  reputation — plan for that before pointing MX at it.
                </span>
              </span>
            </label>
          </div>

          <NxField
            label="Panel domain"
            hint="Optional. The parent for temporary site addresses, e.g. panel.example.com."
          >
            <template #default="{ id }">
              <NxInput :id="id" v-model="panelDomain" placeholder="panel.example.com" />
            </template>
          </NxField>

          <NxField
            label="Public IPv4"
            hint="Optional. Only used to create the wildcard record for temporary addresses."
          >
            <template #default="{ id }">
              <NxInput :id="id" v-model="panelIpv4" mono placeholder="203.0.113.10" />
            </template>
          </NxField>
        </NxCard>

        <div class="setup__actions">
          <NxButton variant="primary" size="lg" :loading="busy" @click="finish">
            Install and finish
          </NxButton>
          <p class="setup__note">
            This installs packages and enables services on this host. It can take several minutes.
          </p>
        </div>
      </template>
    </div>
  </main>
</template>

<style scoped>
.setup { min-height: 100dvh; background: var(--nx-bg); padding: 40px 24px; }
.setup__inner { max-width: 640px; margin: 0 auto; display: flex; flex-direction: column; gap: 20px; }
.setup__head { display: flex; flex-direction: column; gap: 6px; }
.setup__brand {
  margin: 0;
  font-size: var(--nx-text-sm);
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--nx-primary);
}
.setup__title { margin: 0; font-size: var(--nx-text-2xl); font-weight: 600; color: var(--nx-text); }
.setup__sub { margin: 0; color: var(--nx-text-muted); font-size: var(--nx-text-base); }
.setup__note { margin: 0 0 12px; font-size: var(--nx-text-sm); color: var(--nx-text-muted); line-height: 1.5; }

.setup__choices { display: flex; flex-direction: column; gap: 8px; }
.setup__choice,
.setup__toggle {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  padding: 12px;
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-md);
  cursor: pointer;
}
.setup__choice:has(input:checked) { border-color: var(--nx-primary); background: var(--nx-primary-soft); }
.setup__choice-body { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.setup__choice-label { font-size: var(--nx-text-base); font-weight: 500; color: var(--nx-text); }
.setup__choice-note { font-size: var(--nx-text-sm); color: var(--nx-text-muted); line-height: 1.45; }

.setup__baseline { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
.setup__item { display: flex; align-items: center; gap: 8px; font-size: var(--nx-text-base); color: var(--nx-text-2); }
.setup__item-label { font-weight: 500; color: var(--nx-text); }
.setup__item-note { font-size: var(--nx-text-sm); color: var(--nx-text-muted); }

.setup__toggles { display: flex; flex-direction: column; gap: 8px; margin-bottom: 16px; }
.setup__actions { display: flex; flex-direction: column; gap: 8px; align-items: flex-start; }
</style>
