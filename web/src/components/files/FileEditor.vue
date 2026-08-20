<script setup lang="ts">
/**
 * The read-only code viewer that opens when a file is double-clicked.
 *
 * Highlighting is the design's own tokenizer rather than a syntax-highlighting
 * dependency: it covers the three languages this panel actually opens (PHP,
 * Apache config, plain text) in about forty lines, where a general highlighter
 * is a large dependency for a viewer. When this becomes a real editor it gets
 * CodeMirror — that is the Phase 4 decision, and a placeholder highlighter now
 * does not commit us either way.
 *
 * Read-only on purpose: saving a file needs the write endpoint and a conflict
 * story, and an editor that looks writable but silently discards your changes
 * is worse than one that says it is a viewer.
 */
import { computed } from "vue";
import { FILES } from "@/data/fileSystem";
import { useFileManagerStore } from "@/stores/fileManager";

const fm = useFileManagerStore();

const file = computed(() => (fm.openFile ? FILES[fm.openFile] : undefined));

const KEYWORDS = new Set([
  "define", "require", "require_once", "include", "if", "else", "return", "function",
  "class", "new", "exit", "true", "false", "null", "echo", "const", "use", "namespace",
]);

interface Token {
  readonly text: string;
  readonly kind: "plain" | "comment" | "string" | "keyword" | "var" | "number" | "type";
}

function tokenize(line: string, lang: string): Token[] {
  if (line === "") return [{ text: " ", kind: "plain" }];
  if (/^\s*(#|\*|\/\*|\/\/)/.test(line) || /^\s*\*\//.test(line)) return [{ text: line, kind: "comment" }];

  if (lang === "Plain Text") {
    const colon = line.match(/^([A-Za-z-]+:)(.*)$/);
    if (colon) return [{ text: colon[1], kind: "keyword" }, { text: colon[2], kind: "string" }];
    return [{ text: line, kind: "plain" }];
  }

  if (lang === "Apache Conf") {
    if (/^\s*<\/?[A-Za-z]/.test(line)) return [{ text: line, kind: "type" }];
    const m = line.match(/^(\s*)([A-Za-z]+)(\s+)(.*)$/);
    if (m) {
      return [
        { text: m[1], kind: "plain" },
        { text: m[2], kind: "keyword" },
        { text: m[3], kind: "plain" },
        { text: m[4], kind: "string" },
      ];
    }
    return [{ text: line, kind: "plain" }];
  }

  const out: Token[] = [];
  const re = /(\/\/[^\n]*|\/\*\*?|'[^']*'|"[^"]*"|\$[A-Za-z_]\w*|\b[A-Za-z_]\w*\b|\d+)/g;
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(line)) !== null) {
    if (m.index > last) out.push({ text: line.slice(last, m.index), kind: "plain" });
    const tk = m[0];
    let kind: Token["kind"] = "plain";
    if (tk.startsWith("//") || tk.startsWith("/*")) kind = "comment";
    else if (tk[0] === "'" || tk[0] === '"') kind = "string";
    else if (tk[0] === "$") kind = "var";
    else if (KEYWORDS.has(tk)) kind = "keyword";
    else if (/^\d+$/.test(tk)) kind = "number";
    else if (/^[A-Z_]{2,}$/.test(tk)) kind = "var";
    out.push({ text: tk, kind });
    last = m.index + tk.length;
  }
  if (last < line.length) out.push({ text: line.slice(last), kind: "plain" });
  return out.length ? out : [{ text: line, kind: "plain" }];
}

const lines = computed(() =>
  (file.value?.code ?? []).map((l) => tokenize(l, file.value?.lang ?? "Plain Text")),
);
</script>

<template>
  <div class="ed">
    <div class="ed__tabs" role="tablist" aria-label="Open files">
      <div
        v-for="t in fm.tabs"
        :key="t"
        class="ed__tab"
        :class="{ 'is-current': t === fm.openFile }"
        role="tab"
        :aria-selected="t === fm.openFile"
      >
        <button type="button" class="ed__tab-name nx-mono" @click="fm.openInEditor(t)">{{ t }}</button>
        <button type="button" class="ed__tab-close" :aria-label="'Close ' + t" @click="fm.closeTab(t)">
          <NxIcon name="close" size="sm" />
        </button>
      </div>
      <span class="ed__spacer" />
      <NxButton size="sm" class="ed__back" @click="fm.closeEditor()">
        <NxIcon name="arrow-back" size="sm" />
        Back to files
      </NxButton>
    </div>

    <div v-if="file" class="ed__body nxscroll">
      <div class="ed__meta nx-mono">{{ fm.openFile }} · {{ file.lang }} · read-only</div>
      <ol class="ed__code">
        <li v-for="(tokens, i) in lines" :key="i" class="ed__line">
          <span class="ed__gutter" aria-hidden="true">{{ i + 1 }}</span>
          <code class="ed__text"><span
            v-for="(tk, j) in tokens"
            :key="j"
            :class="'ed__tk--' + tk.kind"
          >{{ tk.text }}</span></code>
        </li>
      </ol>
    </div>

    <NxEmptyState
      v-else
      icon="description"
      title="No preview for this file"
      description="Binary and very large files are not opened in the browser. Download it instead."
    />
  </div>
</template>

<style scoped>
.ed {
  display: flex;
  flex-direction: column;
  min-height: 0;
  flex: 1;
  background: var(--nx-dark-2);
  border-radius: var(--nx-radius-lg);
  overflow: hidden;
}
.ed__tabs {
  display: flex;
  align-items: center;
  gap: 2px;
  background: var(--nx-dark);
  border-bottom: 1px solid var(--nx-dark-border);
  padding: 6px 8px;
  overflow-x: auto;
}
.ed__tab {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 4px 4px 10px;
  border-radius: var(--nx-radius-sm);
  color: var(--nx-text-muted);
  white-space: nowrap;
}
.ed__tab.is-current { background: var(--nx-dark-2); color: var(--nx-text-on-dark); }
.ed__tab-name {
  border: 0;
  background: transparent;
  color: inherit;
  font-size: var(--nx-text-sm);
  cursor: pointer;
  padding: 0;
}
.ed__tab-close {
  display: flex;
  border: 0;
  background: transparent;
  color: inherit;
  opacity: 0.6;
  cursor: pointer;
  padding: 2px;
  border-radius: var(--nx-radius-sm);
}
.ed__tab-close:hover { opacity: 1; background: var(--nx-dark-border); }
.ed__spacer { flex: 1; }
.ed__back {
  border-color: var(--nx-dark-border-2);
  background: transparent;
  color: var(--nx-text-on-dark);
}
.ed__back:hover { background: var(--nx-dark-border); }

.ed__body { flex: 1; min-height: 0; overflow: auto; padding: 12px 0 24px; }
.ed__meta {
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
  padding: 4px 16px 12px;
}
.ed__code {
  list-style: none;
  margin: 0;
  padding: 0;
  font-family: "JetBrains Mono", ui-monospace, monospace;
  font-size: var(--nx-text-base);
  line-height: 1.7;
}
.ed__line { display: flex; gap: 16px; }
.ed__gutter {
  flex: 0 0 44px;
  text-align: right;
  color: var(--nx-text-muted);
  opacity: 0.55;
  user-select: none;
}
.ed__text { white-space: pre; color: var(--nx-text-on-dark); }

.ed__tk--plain { color: var(--nx-text-on-dark); }
.ed__tk--comment { color: #7d8b74; }
.ed__tk--string { color: #cE9178; }
.ed__tk--keyword { color: #c586c0; }
.ed__tk--var { color: #9cdcfe; }
.ed__tk--number { color: #b5cea8; }
.ed__tk--type { color: #4ec9b0; }
</style>
