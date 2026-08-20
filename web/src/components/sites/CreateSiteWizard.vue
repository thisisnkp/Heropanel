<script setup lang="ts">
/**
 * Create a website — domain and type, then where the files come from.
 *
 * Two steps rather than one form because the second step's questions depend on
 * the first: a WordPress site has no "connect a repo" option, and the summary
 * line can only be written once the type is known. Advancing is blocked until
 * the current step is answerable, so "Create website" is never a button that
 * fails.
 */
import { computed, ref, watch } from "vue";
import { STACKS, STACK_KEYS, type StackKey } from "@/config/stacks";
import { useSitesStore, type Site } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const open = defineModel<boolean>("open", { required: true });

const props = defineProps<{ stack?: StackKey }>();
const emit = defineEmits<{ created: [site: Site] }>();

const sites = useSitesStore();
const ui = useUiStore();

type Source = "manual" | "git" | null;

const step = ref<1 | 2>(1);
const stack = ref<StackKey>(props.stack ?? "static");
const domain = ref("");
const source = ref<Source>(null);
const repo = ref("");
const branch = ref("main");

// Reopening starts clean — a half-filled wizard from a cancelled attempt is a
// good way to create a site with the previous attempt's repo attached.
watch(open, (isOpen) => {
  if (!isOpen) return;
  step.value = 1;
  stack.value = props.stack ?? "static";
  domain.value = "";
  source.value = null;
  repo.value = "";
  branch.value = "main";
});

const meta = computed(() => STACKS[stack.value]);

/** WordPress is installed by the panel, so there is no repo to deploy from. */
const gitAvailable = computed(() => stack.value !== "wp");

const SOURCES = [
  { key: "manual" as const, label: "Manual upload", hint: "Drag a zip or files in. Best for one-off sites and handoffs." },
  { key: "git" as const, label: "GitHub deploy", hint: "Connect a repo and every push to your branch goes live." },
];

const availableSources = computed(() => (gitAvailable.value ? SOURCES : SOURCES.filter((s) => s.key === "manual")));

const canAdvance = computed(() => {
  if (step.value === 1) return domain.value.trim().length > 2;
  if (!source.value) return false;
  return source.value === "manual" || repo.value.trim().length > 2;
});

const summary = computed(() => {
  const d = domain.value.trim() || "your domain";
  const how =
    source.value === "git"
      ? "GitHub deploy from " + (repo.value.trim() || "repository") + "@" + (branch.value.trim() || "main")
      : source.value === "manual"
        ? "manual file upload"
        : "pick a source above";
  return d + " · " + meta.value.label + " · " + how + ". Free SSL and daily backups are switched on automatically.";
});

function back() {
  if (step.value === 1) open.value = false;
  else step.value = 1;
}

function next() {
  if (!canAdvance.value) return;
  if (step.value === 1) {
    step.value = 2;
    return;
  }
  create();
}

function create() {
  const name = domain.value.trim() || "my-site.nexpanel.app";
  const usesGit = gitAvailable.value && source.value === "git";
  const id = Math.max(0, ...sites.sites.map((s) => s.id)) + 1;

  const site: Site = {
    id,
    name,
    domain: name,
    stackKey: stack.value,
    deploy: usesGit ? "GitHub" : "Manual",
    status: "building",
    lastDeploy: "deploying…",
    branch: usesGit ? branch.value.trim() || "main" : "—",
    repo: usesGit ? repo.value.trim() || "username/repository" : "—",
  };

  sites.hydrate([site, ...sites.sites]);
  open.value = false;
  ui.toast("Creating " + name + "…", "info");
  emit("created", site);

  // The design flipped the row to live after a couple of seconds. Kept so the
  // building state is something that has been seen and laid out, rather than a
  // status only the real backend will ever produce.
  window.setTimeout(() => {
    const built = sites.byId(id);
    if (!built) return;
    built.status = "live";
    built.lastDeploy = "just now";
    ui.toast(name + " is live.", "success");
  }, 2200);
}
</script>

<template>
  <NxModal
    v-model:open="open"
    width="560px"
    :title="'New ' + meta.label + ' website'"
    :description="'Step ' + step + ' of 2 · ' + (step === 1 ? 'Domain' : 'Source')"
  >
    <div v-if="step === 1" class="wiz__step">
      <NxField label="Domain" required>
        <template #default="{ id, describedBy }">
          <NxInput
            :id="id"
            v-model="domain"
            mono
            placeholder="example.com"
            :aria-describedby="describedBy"
            autocomplete="off"
          />
        </template>
      </NxField>
      <p class="wiz__note">
        Do not own it yet? We will give you a free
        <span class="nx-mono">*.nexpanel.app</span> subdomain to start.
      </p>

      <fieldset class="wiz__fieldset">
        <legend class="wiz__legend">Type</legend>
        <div class="wiz__chips">
          <button
            v-for="k in STACK_KEYS"
            :key="k"
            type="button"
            class="wiz__chip"
            :class="{ 'is-picked': stack === k }"
            :aria-pressed="stack === k"
            @click="stack = k"
          >
            {{ STACKS[k].label }}
          </button>
        </div>
      </fieldset>
    </div>

    <div v-else class="wiz__step">
      <fieldset class="wiz__fieldset">
        <legend class="wiz__legend">How will files get here?</legend>
        <div class="nx-grid nx-grid--2">
          <button
            v-for="s in availableSources"
            :key="s.key"
            type="button"
            class="wiz__source"
            :class="{ 'is-picked': source === s.key }"
            :aria-pressed="source === s.key"
            @click="source = s.key"
          >
            <span class="wiz__source-label">{{ s.label }}</span>
            <span class="wiz__source-hint">{{ s.hint }}</span>
          </button>
        </div>
      </fieldset>

      <div v-if="source === 'git'" class="nx-stack">
        <NxField label="Repository" required>
          <template #default="{ id }">
            <NxInput :id="id" v-model="repo" mono placeholder="username/repository" autocomplete="off" />
          </template>
        </NxField>
        <div class="nx-grid nx-grid--2">
          <NxField label="Branch">
            <template #default="{ id }">
              <NxInput :id="id" v-model="branch" mono autocomplete="off" />
            </template>
          </NxField>
          <NxField label="Auto-deploy" hint="Every push to the branch above goes live.">
            <div class="wiz__static">On every push</div>
          </NxField>
        </div>
      </div>

      <p class="wiz__summary">{{ summary }}</p>
    </div>

    <template #footer>
      <NxButton size="lg" @click="back">{{ step === 1 ? "Cancel" : "Back" }}</NxButton>
      <span class="wiz__foot-note">{{ step === 1 ? "You can change the type later" : "Live in about 40 seconds" }}</span>
      <NxButton variant="primary" size="lg" :disabled="!canAdvance" @click="next">
        {{ step === 1 ? "Next" : "Create website" }}
      </NxButton>
    </template>
  </NxModal>
</template>

<style scoped>
.wiz__step { display: flex; flex-direction: column; gap: 20px; padding-top: 4px; }
.wiz__note {
  margin: -12px 0 0;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  line-height: 1.5;
}
.wiz__fieldset { border: 0; margin: 0; padding: 0; min-width: 0; }
.wiz__legend {
  padding: 0 0 8px;
  font-size: var(--nx-text-sm);
  font-weight: 500;
  color: var(--nx-text-2);
}
.wiz__chips { display: flex; gap: 8px; flex-wrap: wrap; }
.wiz__chip {
  border: 1px solid var(--nx-border);
  background: var(--nx-surface);
  color: var(--nx-text-3);
  border-radius: var(--nx-radius-md);
  padding: 8px 16px;
  font-size: var(--nx-text-base);
  font-family: inherit;
  cursor: pointer;
}
.wiz__chip:hover { background: var(--nx-hover); }
.wiz__chip.is-picked {
  border-color: var(--nx-primary);
  background: var(--nx-info-soft);
  color: var(--nx-primary);
  font-weight: 600;
}

.wiz__source {
  text-align: left;
  border: 1px solid var(--nx-border);
  background: var(--nx-surface);
  border-radius: var(--nx-radius-md);
  padding: 16px;
  cursor: pointer;
  font-family: inherit;
}
.wiz__source:hover { background: var(--nx-hover); }
.wiz__source.is-picked { border-color: var(--nx-primary); background: var(--nx-primary-soft); }
.wiz__source-label {
  display: block;
  font-size: var(--nx-text-base);
  font-weight: 600;
  color: var(--nx-text);
}
.wiz__source-hint {
  display: block;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
  padding-top: 4px;
  line-height: 1.5;
}

.wiz__static {
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-md);
  padding: 8px 12px;
  font-size: var(--nx-text-base);
  color: var(--nx-text-2);
  background: var(--nx-surface-2);
}

.wiz__summary {
  margin: 0;
  background: var(--nx-surface-2);
  border: 1px solid var(--nx-active);
  border-radius: var(--nx-radius-md);
  padding: 12px 16px;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  line-height: 1.6;
}

.wiz__foot-note {
  flex: 1;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-placeholder);
}
</style>
