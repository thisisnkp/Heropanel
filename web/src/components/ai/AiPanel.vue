<script setup lang="ts">
/**
 * The assistant drawer behind "Ask AI".
 *
 * Most of this panel is not the conversation — it is the proposal card, which
 * exists because the assistant is being asked to change a live server. Before
 * anything happens it states the diff, what the change touches, the risk and the
 * rollback, and offers "Preview only" beside "Apply". A chat that can run
 * commands without showing its work is the version of this feature nobody should
 * ship on a control panel.
 *
 * Applying starts a real entry in the job tray rather than a toast, so the work
 * is still visible after you navigate away from here.
 */
import { ref } from "vue";
import { useAiStore } from "@/stores/ai";
import { useJobsStore } from "@/stores/jobs";
import { useUiStore } from "@/stores/ui";

const ai = useAiStore();
const jobs = useJobsStore();
const ui = useUiStore();

const draft = ref("");

const RISK_ICON = { low: "check-circle", review: "error", high: "warning" } as const;

function send() {
  ai.ask(draft.value);
  draft.value = "";
}

function apply() {
  if (!ai.proposal) return;
  jobs.start(ai.proposal.title, "nexp_novaretail", "Snapshotting database");
}

function preview() {
  ui.toast("Dry run finished — 1 index would be created, 0 rows rewritten.", "info");
}

function dismiss() {
  const previous = ai.proposal;
  ai.dismissProposal();
  // Dismissing loses the diff, the risk note and the rollback plan — all of
  // which took a round trip to produce. Offering it back costs one closure.
  ui.toast("Proposal dismissed.", "info", {
    label: "Undo",
    run: () => (ai.proposal = previous),
  });
}
</script>

<template>
  <Transition name="ai">
    <aside v-if="ai.open" class="ai" role="complementary" aria-label="NexPanel AI">
      <header class="ai__head">
        <span class="ai__mark" aria-hidden="true"><NxIcon name="auto-awesome" size="sm" /></span>
        <div class="ai__titles">
          <div class="ai__title">NexPanel AI</div>
          <div class="ai__sub">Reads your logs, metrics and config</div>
        </div>
        <button type="button" class="ai__close" aria-label="Close AI panel" @click="ai.toggle()">
          <NxIcon name="close" size="md" />
        </button>
      </header>

      <div class="ai__body nxhide">
        <div v-for="(m, i) in ai.messages" :key="i" class="ai__row" :class="'is-' + m.who">
          <p class="ai__bubble" :class="'is-' + m.who">{{ m.text }}</p>
        </div>

        <section v-if="ai.proposal" class="ai__card">
          <header class="ai__card-head">
            <NxIcon name="bolt" size="md" class="ai__card-icon" />
            <h2 class="ai__card-title">{{ ai.proposal.title }}</h2>
          </header>

          <div class="ai__card-body">
            <h3 class="ai__caption">WHAT WILL CHANGE</h3>
            <div class="ai__diff">
              <div
                v-for="(d, i) in ai.proposal.diff"
                :key="i"
                class="ai__diff-line nx-mono"
                :class="'is-' + d.kind"
              >
                {{ d.text }}
              </div>
            </div>

            <h3 class="ai__caption">WHAT IT TOUCHES</h3>
            <div v-for="t in ai.proposal.touches" :key="t.text" class="ai__touch">
              <NxIcon :name="t.icon" size="sm" class="ai__touch-icon" />
              <span>{{ t.text }}</span>
            </div>

            <h3 class="ai__caption">RISK LEVEL</h3>
            <div class="ai__risk" :class="'is-' + ai.proposal.risk">
              <NxIcon :name="RISK_ICON[ai.proposal.risk]" size="sm" class="ai__risk-icon" />
              <span>
                <span class="ai__risk-label">{{ ai.proposal.riskLabel }}</span>
                <span class="ai__risk-why">{{ ai.proposal.riskWhy }}</span>
              </span>
            </div>

            <h3 class="ai__caption">HOW TO UNDO</h3>
            <p class="ai__undo">{{ ai.proposal.undo }}</p>

            <div class="ai__actions">
              <button type="button" class="ai__apply" @click="apply">Apply</button>
              <NxButton @click="preview">Preview only</NxButton>
              <NxButton @click="dismiss">Dismiss</NxButton>
            </div>
          </div>
        </section>
      </div>

      <footer class="ai__foot">
        <div v-if="ai.proposal" class="ai__cost">
          <NxIcon name="receipt-long" size="sm" />
          <span class="nx-mono nx-truncate">{{ ai.proposal.cost }}</span>
        </div>

        <div class="ai__chips">
          <button
            v-for="s in ai.suggestions"
            :key="s"
            type="button"
            class="ai__chip"
            @click="ai.ask(s)"
          >
            {{ s }}
          </button>
        </div>

        <form class="ai__compose" @submit.prevent="send">
          <input
            v-model="draft"
            type="text"
            placeholder="Ask about your server…"
            aria-label="Ask about your server"
          />
          <button type="submit" class="ai__send" aria-label="Send">
            <NxIcon name="arrow-upward" size="sm" />
          </button>
        </form>
      </footer>
    </aside>
  </Transition>
</template>

<style scoped>
.ai {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  width: 380px;
  max-width: 100vw;
  z-index: 55;
  background: var(--nx-surface);
  border-left: 1px solid var(--nx-border);
  box-shadow: -8px 0 28px rgba(27, 27, 25, 0.1);
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.ai__head {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 16px;
  height: 60px;
  flex: 0 0 60px;
  border-bottom: 1px solid var(--nx-primary-border);
  background: var(--nx-primary-soft);
}
.ai__mark {
  width: 28px;
  height: 28px;
  flex: 0 0 28px;
  border-radius: var(--nx-radius-md);
  background: var(--nx-primary);
  color: var(--nx-primary-on);
  display: flex;
  align-items: center;
  justify-content: center;
}
.ai__titles { flex: 1; min-width: 0; }
.ai__title { font-size: var(--nx-text-base); font-weight: 600; letter-spacing: var(--nx-ls-tight); }
.ai__sub { font-size: var(--nx-text-xs); color: var(--nx-text-muted); }
.ai__close {
  border: 0;
  background: transparent;
  cursor: pointer;
  padding: 4px;
  display: flex;
  color: var(--nx-text-muted);
}
.ai__close:hover { color: var(--nx-text); }

.ai__body {
  flex: 1;
  overflow-y: auto;
  min-height: 0;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.ai__row { display: flex; }
.ai__row.is-you { justify-content: flex-end; }
.ai__bubble {
  margin: 0;
  max-width: 86%;
  padding: 12px;
  font-size: var(--nx-text-base);
  line-height: 1.55;
  text-wrap: pretty;
}
.ai__bubble.is-ai {
  background: var(--nx-hover);
  color: var(--nx-text);
  border-radius: 4px 12px 12px 12px;
}
.ai__bubble.is-you {
  background: var(--nx-primary);
  color: var(--nx-primary-on);
  border-radius: 12px 4px 12px 12px;
}

.ai__card {
  border: 1px solid var(--nx-border);
  background: var(--nx-surface);
  border-radius: var(--nx-radius-lg);
  overflow: hidden;
}
.ai__card-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px;
  border-bottom: 1px solid var(--nx-active);
  background: var(--nx-surface-2);
}
.ai__card-icon { color: var(--nx-primary-text); }
.ai__card-title {
  flex: 1;
  min-width: 0;
  margin: 0;
  font-size: var(--nx-text-base);
  font-weight: 500;
  color: var(--nx-text);
}
.ai__card-body { padding: 12px; }
.ai__caption {
  margin: 0;
  font-size: var(--nx-text-xs);
  font-weight: 600;
  letter-spacing: var(--nx-ls-caps);
  color: var(--nx-text-muted);
  padding-bottom: 8px;
}
.ai__caption + * { margin-top: 0; }
.ai__card-body > .ai__caption:not(:first-child) { padding-top: 16px; }

.ai__diff { border: 1px solid var(--nx-active); border-radius: var(--nx-radius-sm); overflow: hidden; }
.ai__diff-line {
  font-size: var(--nx-text-xs);
  line-height: 1.7;
  padding: 4px 8px;
  white-space: pre-wrap;
  word-break: break-word;
}
.ai__diff-line.is-remove { background: var(--nx-danger-soft); color: var(--nx-danger); }
.ai__diff-line.is-add { background: var(--nx-success-soft); color: var(--nx-success); }
.ai__diff-line.is-context { color: var(--nx-text-2); }

.ai__touch { display: flex; align-items: center; gap: 8px; padding: 4px 0; font-size: var(--nx-text-sm); color: var(--nx-text-2); }
.ai__touch-icon { color: var(--nx-text-muted); }

.ai__risk {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-sm);
  padding: 8px;
}
.ai__risk.is-review { border-color: var(--nx-warning-border); background: var(--nx-warning-soft); color: var(--nx-warning); }
.ai__risk.is-high { border-color: var(--nx-danger-border); background: var(--nx-danger-soft); color: var(--nx-danger); }
.ai__risk.is-low { border-color: var(--nx-success-soft); background: var(--nx-success-soft); color: var(--nx-success); }
.ai__risk-label { display: block; font-size: var(--nx-text-sm); font-weight: 500; }
.ai__risk-why { display: block; font-size: var(--nx-text-sm); color: var(--nx-text-2); padding-top: 4px; line-height: 1.5; }

.ai__undo { margin: 0; font-size: var(--nx-text-sm); color: var(--nx-text-2); line-height: 1.55; text-wrap: pretty; }
.ai__actions { display: flex; gap: 8px; flex-wrap: wrap; padding-top: 16px; }
.ai__apply {
  border: 0;
  background: var(--nx-primary);
  color: var(--nx-primary-on);
  border-radius: var(--nx-radius-md);
  padding: 8px 16px;
  font-size: var(--nx-text-base);
  font-family: inherit;
  font-weight: 500;
  cursor: pointer;
}
.ai__apply:hover { background: var(--nx-primary-hover); }

.ai__foot { border-top: 1px solid var(--nx-border); padding: 12px 16px 16px; }
.ai__cost {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-bottom: 12px;
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
  min-width: 0;
}
.ai__chips { display: flex; gap: 6px; flex-wrap: wrap; padding-bottom: 12px; }
.ai__chip {
  border: 1px solid var(--nx-border);
  background: var(--nx-surface);
  border-radius: var(--nx-radius-lg);
  padding: 6px 12px;
  font-size: var(--nx-text-sm);
  font-family: inherit;
  line-height: 1.4;
  white-space: normal;
  cursor: pointer;
  color: var(--nx-text-3);
  text-align: left;
}
.ai__chip:hover { background: var(--nx-hover); }
.ai__compose {
  display: flex;
  align-items: center;
  gap: 8px;
  border: 1px solid var(--nx-border);
  border-radius: var(--nx-radius-md);
  padding: 8px 12px;
}
.ai__compose input {
  flex: 1;
  min-width: 0;
  border: 0;
  outline: 0;
  font-size: var(--nx-text-base);
  font-family: inherit;
  background: transparent;
  color: var(--nx-text);
}
.ai__send {
  border: 0;
  background: var(--nx-text);
  color: var(--nx-primary-on);
  border-radius: var(--nx-radius-md);
  width: 28px;
  height: 28px;
  flex: 0 0 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}
.ai__send:hover { background: var(--nx-dark-2); }

.ai-enter-active,
.ai-leave-active { transition: transform 240ms cubic-bezier(0.16, 1, 0.3, 1); }
.ai-enter-from,
.ai-leave-to { transform: translateX(100%); }
</style>
