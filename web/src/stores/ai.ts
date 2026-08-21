import { ref } from "vue";
import { defineStore } from "pinia";

export interface AiMessage {
  readonly who: "ai" | "you";
  readonly text: string;
}

export interface AiDiffLine {
  readonly text: string;
  readonly kind: "add" | "remove" | "context";
}

export interface AiTouch {
  readonly icon: string;
  readonly text: string;
}

export interface AiProposal {
  readonly title: string;
  readonly risk: "low" | "review" | "high";
  readonly riskLabel: string;
  readonly riskWhy: string;
  readonly diff: readonly AiDiffLine[];
  readonly touches: readonly AiTouch[];
  readonly undo: string;
  readonly cost: string;
}

/**
 * The assistant panel's state.
 *
 * The proposal is the point of this screen and the reason it is not a chat
 * bubble with a "do it" button: before the panel changes anything on a server it
 * shows the diff, what the change touches, how risky it is and how to undo it.
 * Those four are held as structured data rather than prose so the panel cannot
 * quietly render a change with no stated rollback — an empty `undo` is visible
 * in review, an unwritten sentence is not.
 *
 * Conversation is a fixture here; the shape is what a real transport fills in.
 */
export const useAiStore = defineStore("ai", () => {
  const open = ref(false);

  const messages = ref<AiMessage[]>([
    { who: "ai", text: "I can see your sites, logs and metrics. Ask me anything — or pick a suggestion below." },
    { who: "you", text: "Why is checkout slow after 6pm?" },
    {
      who: "ai",
      text:
        "novaretail.in gets 3× traffic after 18:00 and orders.created_at has no index — " +
        "every checkout scans 184k rows. Adding the index should cut ~280 ms per request.",
    },
  ]);

  const proposal = ref<AiProposal | null>({
    title: "Add a missing index on orders.created_at",
    risk: "review",
    riskLabel: "Needs review",
    riskWhy: "Rebuilds one index on a 184k-row table. Reads stay online.",
    diff: [
      { text: "- SELECT * FROM orders WHERE created_at > ?", kind: "remove" },
      { text: "+ CREATE INDEX idx_orders_created_at", kind: "add" },
      { text: "+   ON orders (created_at);", kind: "add" },
    ],
    touches: [
      { icon: "database", text: "nexp_novaretail · table orders" },
      { icon: "language", text: "novaretail.in · no restart needed" },
      { icon: "schedule", text: "Expected lock: under 2 seconds" },
    ],
    undo:
      "A database snapshot is taken first. Rollback is DROP INDEX idx_orders_created_at, " +
      "available from Backups for 24 hours.",
    cost: "1,284 tokens · ~₹0.42 · Claude (BYOK)",
  });

  const suggestions: readonly string[] = [
    "Why did my last deploy fail?",
    "Which site uses the most memory?",
    "Is anything about my security score urgent?",
    "Set up a nightly database backup",
  ];

  function toggle() {
    open.value = !open.value;
  }

  function show() {
    open.value = true;
  }

  function ask(text: string) {
    const q = text.trim();
    if (!q) return;
    messages.value = [
      ...messages.value,
      { who: "you", text: q },
      {
        who: "ai",
        text: "Not wired to a model yet — this build ships the panel, not the transport.",
      },
    ];
  }

  function dismissProposal() {
    proposal.value = null;
  }

  return { open, messages, proposal, suggestions, toggle, show, ask, dismissProposal };
});
