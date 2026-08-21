<script setup lang="ts">
/**
 * File manager — browse, search, edit and upload into the document root.
 *
 * Ported from panel_ui_ref/NexPanel File Manager.dc.html, which is a separate
 * design because it opens in its own tab — the panel calls window.open() rather
 * than routing to it. This route is marked `standalone`, so App.vue renders it
 * with no panel chrome: no icon rail, no site drawer, no breadcrumb bar. It has
 * its own sidebar, and stacking the panel's two around it would leave three
 * columns of navigation next to a file list.
 *
 * The trade-off is that this window has no navigation out, exactly as the design
 * intends: the panel is still open in the tab you came from. The one concession
 * is the "Back to panel" row at the foot of the sidebar, which matters when this
 * URL is reached directly — from a bookmark or a pasted link — and there is no
 * other tab to return to.
 *
 * Delete moves to trash rather than removing: every destructive action in this
 * panel is recoverable for 14 days, and this is the screen where a mis-click is
 * most likely.
 */
import { computed, ref } from "vue";
import { DISK, SENSITIVE, type FsEntry } from "@/data/fileSystem";
import { useFileManagerStore } from "@/stores/fileManager";
import { useSitesStore } from "@/stores/sites";
import { useUiStore } from "@/stores/ui";

const fm = useFileManagerStore();
const sites = useSitesStore();
const ui = useUiStore();

const site = computed(() => sites.current);
const inTrash = computed(() => fm.view === "trash");

const countLabel = computed(() => {
  const q = fm.query.trim();
  if (q) {
    const n = fm.rows.length;
    return n === 1 ? '1 match for "' + q + '"' : n + ' matches for "' + q + '"';
  }
  if (inTrash.value) {
    return fm.trash.length === 1 ? "1 item in trash" : fm.trash.length + " items in trash";
  }
  const folders = fm.rows.filter((r) => r.type === "dir").length;
  return fm.rows.length + " items · " + folders + " folders";
});

const empty = computed(() => {
  if (fm.searching) {
    return {
      icon: "search-off",
      title: 'No file or folder matches "' + fm.query.trim() + '"',
      body: "Names are matched anywhere in your account, not just this folder. Check the spelling or clear the search.",
    };
  }
  if (inTrash.value) {
    return {
      icon: "delete",
      title: "Trash is empty",
      body: "Anything you delete lands here for 14 days before it goes for good.",
    };
  }
  return { icon: "folder-open", title: "This folder is empty", body: "Upload files or create one from the sidebar." };
});

function isSensitive(name: string) {
  return SENSITIVE.has(name);
}

function open(r: FsEntry) {
  if (inTrash.value) {
    ui.toast(r.name + " is in the trash — restore it first.", "info");
    return;
  }
  if (r.where !== undefined && r.where !== fm.pathKey) {
    fm.jumpTo(r.where, r.name);
    return;
  }
  if (r.type === "dir") fm.enter(r.name);
  else fm.openInEditor(r.name);
}

// ---- toolbar ---------------------------------------------------------------

const first = computed(() => fm.rows.find((r) => r.name === fm.selected[0]));

interface Tool {
  readonly label: string;
  readonly icon: string;
  /** Whether the tool needs something selected. */
  readonly needsSelection: boolean;
  readonly run: () => void;
}

const tools = computed<Tool[]>(() => [
  { label: "Rename", icon: "drive-file-rename-outline", needsSelection: true, run: () => openDialog("rename") },
  { label: "Copy", icon: "content-copy", needsSelection: true, run: () => ui.toast(fm.selected.length + " item copied to clipboard.", "info") },
  { label: "Move", icon: "drive-file-move", needsSelection: true, run: () => openDialog("move") },
  { label: "Permissions", icon: "lock", needsSelection: true, run: () => openDialog("perms") },
  { label: "Archive", icon: "folder-zip", needsSelection: true, run: () => ui.toast("Archived " + fm.selected.length + " item into archive.zip.", "success") },
  { label: "Delete", icon: "delete", needsSelection: true, run: () => openDialog("delete") },
  { label: "Upload", icon: "upload", needsSelection: false, run: () => ui.toast("Upload is not wired up yet.", "info") },
  {
    label: "Directory size",
    icon: "calculate",
    needsSelection: true,
    run: () =>
      ui.toast(
        first.value?.type === "dir"
          ? first.value.name + " is " + first.value.size + " across 148 files."
          : (first.value?.name ?? "") + " is " + (first.value?.size ?? ""),
        "info",
      ),
  },
  { label: "Info", icon: "info", needsSelection: true, run: () => openDialog("info") },
]);

// ---- dialogs ---------------------------------------------------------------

type DialogKind = "file" | "folder" | "rename" | "move" | "perms" | "delete" | "info";

const dialog = ref<DialogKind | null>(null);
const dialogName = ref("");

const DIALOG_TITLE: Record<DialogKind, string> = {
  file: "Create file",
  folder: "Create folder",
  rename: "Rename",
  move: "Move to",
  perms: "Permissions",
  delete: "Move to trash",
  info: "File info",
};

function openDialog(kind: DialogKind) {
  if (kind !== "file" && kind !== "folder" && !fm.hasSelection) {
    ui.toast("Select a file or folder first.", "info");
    return;
  }
  dialog.value = kind;
  dialogName.value = kind === "rename" ? (fm.selected[0] ?? "") : kind === "move" ? "public_html/" : "";
}

const permBits = ref({ or: true, ow: true, ox: true, gr: true, gw: false, gx: true, pr: false, pw: false, px: false });

const PERM_GROUPS = [
  { label: "Owner", bits: [{ k: "or", label: "Read" }, { k: "ow", label: "Write" }, { k: "ox", label: "Execute" }] },
  { label: "Group", bits: [{ k: "gr", label: "Read" }, { k: "gw", label: "Write" }, { k: "gx", label: "Execute" }] },
  { label: "Public", bits: [{ k: "pr", label: "Read" }, { k: "pw", label: "Write" }, { k: "px", label: "Execute" }] },
] as const;

const octal = computed(() => {
  const p = permBits.value;
  const digit = (r: boolean, w: boolean, x: boolean) => (r ? 4 : 0) + (w ? 2 : 0) + (x ? 1 : 0);
  return "0" + digit(p.or, p.ow, p.ox) + digit(p.gr, p.gw, p.gx) + digit(p.pr, p.pw, p.px);
});

const infoRows = computed(() => {
  const r = first.value;
  if (!r) return [];
  return [
    { label: "Name", value: r.name },
    { label: "Type", value: r.type === "dir" ? "Folder" : "File" },
    { label: "Size", value: r.size },
    { label: "Last modified", value: r.mod },
    { label: "Permissions", value: r.perm },
    { label: "Path", value: "/" + [fm.pathKey, r.name].filter(Boolean).join("/") },
  ];
});

function confirmDialog() {
  const kind = dialog.value;
  const name = dialogName.value.trim();
  dialog.value = null;

  if (kind === "file" || kind === "folder") {
    if (!name) return;
    fm.create(kind === "folder" ? "dir" : "file", name);
    ui.toast("Created " + name + ".", "success");
    return;
  }
  if (kind === "delete") {
    // Named before the move, because moveToTrash clears the selection.
    const names = [...fm.selected];
    fm.moveToTrash(names);
    // Genuinely reversible — restore() puts each file back in the folder it came
    // from, not the one that happens to be open — so the offer is real.
    ui.toast(names.length + (names.length === 1 ? " item" : " items") + " moved to trash.", "success", {
      label: "Undo",
      run: () => names.forEach((n) => fm.restore(n)),
    });
    return;
  }
  if (kind === "rename" && name) {
    ui.toast("Rename is not wired up yet.", "info");
    return;
  }
  if (kind === "move" && name) {
    ui.toast("Move is not wired up yet.", "info");
    return;
  }
  if (kind === "perms") {
    ui.toast("Permissions set to " + octal.value + ".", "success");
  }
}

function restore(name: string) {
  fm.restore(name);
  ui.toast(name + " restored.", "success");
}
</script>

<template>
  <!-- Carries the skip-link target itself: in a standalone window there is no
       shell above to own it, and the link in App.vue still has to land. -->
  <div id="nx-main" class="fm">
    <FileEditor v-if="fm.openFile" class="fm__editor" />

    <div v-else class="fm__shell">
      <aside class="fm__side nxhide">
        <!-- This window's own identity. It has to name the site: the panel's
             breadcrumb is not here to say which document root you are in, and
             "public_html" is the same string on every site you own. -->
        <div class="fm__brand">
          <span class="fm__mark" aria-hidden="true">N</span>
          <h1 class="fm__title">File manager</h1>
        </div>
        <p class="fm__domain nx-mono nx-truncate">{{ site?.domain ?? "—" }}</p>

        <ul class="fm__side-list">
          <li>
            <button
              type="button"
              class="fm__side-item"
              :class="{ 'is-current': !inTrash }"
              :aria-current="!inTrash ? 'page' : undefined"
              @click="fm.goTo(0)"
            >
              <NxIcon name="folder-open" size="md" />
              <span class="nx-row__grow nx-truncate">public_html</span>
            </button>
          </li>
          <li>
            <button type="button" class="fm__side-item" @click="openDialog('file')">
              <NxIcon name="note-add" size="md" />
              <span class="nx-row__grow nx-truncate">Create file</span>
            </button>
          </li>
          <li>
            <button type="button" class="fm__side-item" @click="openDialog('folder')">
              <NxIcon name="create-new-folder" size="md" />
              <span class="nx-row__grow nx-truncate">Create folder</span>
            </button>
          </li>
          <li>
            <button
              type="button"
              class="fm__side-item"
              :class="{ 'is-current': inTrash }"
              :aria-current="inTrash ? 'page' : undefined"
              @click="fm.setView('trash')"
            >
              <NxIcon name="delete" size="md" />
              <span class="nx-row__grow nx-truncate">Trash</span>
              <span class="fm__side-count nx-mono">{{ fm.trash.length }}</span>
            </button>
          </li>
        </ul>

        <div class="fm__spacer" />

        <RouterLink
          v-if="site"
          :to="{ name: 'site-overview', params: { id: String(site.id) } }"
          class="fm__side-item fm__back"
        >
          <NxIcon name="arrow-back" size="md" />
          <span class="nx-row__grow nx-truncate">Back to panel</span>
        </RouterLink>

        <div class="fm__disk">
          <NxMeter :value="DISK.pct" height="4px" :note="'Disk ' + DISK.used + ' of ' + DISK.total" />
        </div>
      </aside>

      <main class="fm__main">
        <div class="fm__tools">
          <button
            v-for="t in tools"
            :key="t.label"
            type="button"
            class="fm__tool"
            :title="t.label"
            :disabled="t.needsSelection && !fm.hasSelection"
            @click="t.run()"
          >
            <NxIcon :name="t.icon" size="lg" />
            <span class="fm__tool-label">{{ t.label }}</span>
          </button>
          <button
            type="button"
            class="fm__tool"
            :class="{ 'is-on': fm.multiSelect }"
            :aria-pressed="fm.multiSelect"
            @click="fm.toggleMultiSelect()"
          >
            <NxIcon :name="fm.multiSelect ? 'check-box' : 'check-box-outline-blank'" size="lg" />
            <span class="fm__tool-label">{{ fm.multiSelect ? "Selecting" : "Select multiple" }}</span>
          </button>
        </div>

        <div class="fm__bar">
          <nav class="fm__crumbs" aria-label="Breadcrumb">
            <button
              v-for="(c, i) in fm.breadcrumbs"
              :key="c.index"
              type="button"
              class="fm__crumb nx-mono"
              :class="{ 'is-last': i === fm.breadcrumbs.length - 1 }"
              @click="fm.goTo(c.index)"
            >
              {{ i === 0 ? c.label : "/ " + c.label }}
            </button>
          </nav>

          <span class="fm__spacer" />

          <div class="fm__search">
            <NxInput
              v-model="fm.query"
              icon="search"
              type="search"
              placeholder="Search files and folders"
              aria-label="Search files and folders"
            />
          </div>
          <span class="fm__count">{{ countLabel }}</span>
        </div>

        <div class="fm__list nxhide">
          <NxCallout v-if="inTrash" tone="warning" icon="schedule" class="fm__trash-note">
            Deleted items stay here for 14 days, then go for good. Restoring puts a file back exactly where it came
            from.
          </NxCallout>

          <div v-if="!fm.isEmpty" role="table" aria-label="Files" class="fm__table">
            <div role="row" class="fm__head">
              <span v-if="fm.multiSelect" role="columnheader" class="fm__col-check" />
              <span role="columnheader" class="fm__col-name">Name</span>
              <span role="columnheader" class="fm__col-size">Size</span>
              <span role="columnheader" class="fm__col-mod">Last modified</span>
              <span role="columnheader" class="fm__col-perm">Perms</span>
              <span v-if="inTrash" role="columnheader" class="fm__col-action">Restore</span>
            </div>

            <div
              v-for="r in fm.rows"
              :key="r.name"
              role="row"
              class="fm__row"
              :class="{ 'is-picked': fm.selected.includes(r.name) }"
              :title="inTrash ? 'Restore to open' : r.type === 'dir' ? 'Double-click to open folder' : 'Double-click to edit'"
              @click="fm.selectOne(r.name)"
              @dblclick="open(r)"
            >
              <span v-if="fm.multiSelect" class="fm__col-check">
                <input
                  type="checkbox"
                  class="fm__check"
                  :checked="fm.selected.includes(r.name)"
                  :aria-label="r.name"
                  @click.stop
                  @change="fm.toggleSelect(r.name)"
                />
              </span>

              <span role="cell" class="fm__col-name">
                <NxIcon
                  :name="r.type === 'dir' ? 'folder' : isSensitive(r.name) ? 'warning' : 'description'"
                  size="md"
                  :class="r.type === 'dir' ? 'fm__ic-dir' : isSensitive(r.name) ? 'fm__ic-warn' : 'fm__ic-file'"
                />
                <span class="fm__name nx-mono nx-truncate" :class="{ 'is-dir': r.type === 'dir' }">{{ r.name }}</span>
                <span v-if="r.tag" class="fm__tag nx-truncate">{{ r.tag }}</span>
              </span>

              <span role="cell" class="fm__col-size nx-mono">{{ r.size }}</span>
              <span role="cell" class="fm__col-mod">{{ r.mod }}</span>
              <span role="cell" class="fm__col-perm nx-mono">{{ r.perm }}</span>
              <span v-if="inTrash" role="cell" class="fm__col-action">
                <NxButton size="sm" @click.stop="restore(r.name)">Restore</NxButton>
              </span>
            </div>
          </div>

          <NxEmptyState v-else :icon="empty.icon" :title="empty.title" :description="empty.body" />
        </div>

        <div v-if="fm.hasSelection" class="fm__selbar">
          <span class="fm__sel-label">
            {{ fm.selected.length === 1 ? "1 item selected" : fm.selected.length + " items selected" }}
          </span>
          <span class="fm__spacer" />
          <NxButton @click="fm.clearSelection()">Clear selection</NxButton>
          <NxButton variant="danger" @click="openDialog('delete')">Move to trash</NxButton>
        </div>
      </main>
    </div>

    <NxModal
      :open="dialog !== null"
      :title="dialog ? DIALOG_TITLE[dialog] : ''"
      @update:open="(v) => { if (!v) dialog = null; }"
    >
      <NxField
        v-if="dialog === 'file' || dialog === 'folder' || dialog === 'rename' || dialog === 'move'"
        :label="dialog === 'move' ? 'Destination folder' : 'Name'"
      >
        <template #default="{ id }">
          <NxInput :id="id" v-model="dialogName" mono autocomplete="off" />
        </template>
      </NxField>

      <div v-else-if="dialog === 'perms'">
        <div class="fm__perms">
          <fieldset v-for="g in PERM_GROUPS" :key="g.label" class="fm__perm-group">
            <legend class="fm__perm-legend">{{ g.label }}</legend>
            <label v-for="b in g.bits" :key="b.k" class="fm__perm-bit">
              <input v-model="permBits[b.k]" type="checkbox" />
              {{ b.label }}
            </label>
          </fieldset>
        </div>
        <p class="fm__octal nx-mono">Mode {{ octal }}</p>
      </div>

      <dl v-else-if="dialog === 'info'" class="fm__info">
        <template v-for="i in infoRows" :key="i.label">
          <dt>{{ i.label }}</dt>
          <dd class="nx-mono">{{ i.value }}</dd>
        </template>
      </dl>

      <NxCallout v-else-if="dialog === 'delete'" tone="warning" title="Moving to trash">
        {{ fm.selected.length === 1 ? "1 item" : fm.selected.length + " items" }} will be recoverable from Trash for
        14 days.
      </NxCallout>

      <template #footer>
        <NxButton @click="dialog = null">Cancel</NxButton>
        <NxButton v-if="dialog !== 'info'" variant="primary" @click="confirmDialog">
          {{ dialog === "delete" ? "Move to trash" : "Confirm" }}
        </NxButton>
      </template>
    </NxModal>
  </div>
</template>

<style scoped>
/* This is the whole window, not a card on a page: the route is `standalone`, so
 * there is no panel chrome above or beside it and the file list should use every
 * pixel. Sizing off the viewport rather than off a guessed header height also
 * retires the `100vh - 230px` this used to carry, which was only ever correct at
 * the one window height it was measured at. */
.fm {
  display: flex;
  flex-direction: column;
  height: 100vh;
  min-height: 0;
  overflow: hidden;
  background: var(--nx-bg);
}
.fm__editor { flex: 1; min-height: 0; }

.fm__shell {
  display: flex;
  flex: 1;
  min-height: 0;
  background: var(--nx-surface);
  overflow: hidden;
}

.fm__side {
  width: 236px;
  flex: 0 0 236px;
  border-right: 1px solid var(--nx-border);
  display: flex;
  flex-direction: column;
  padding: 20px 16px 16px;
  overflow-y: auto;
}
.fm__brand { display: flex; align-items: center; gap: 12px; padding: 4px 8px; }
.fm__mark {
  width: 26px;
  height: 26px;
  flex: 0 0 26px;
  border-radius: var(--nx-radius-md);
  background: var(--nx-violet-900);
  color: var(--nx-gold-400);
  font-size: var(--nx-text-md);
  font-weight: 600;
  line-height: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}
.fm__title {
  margin: 0;
  font-size: var(--nx-text-base);
  font-weight: 600;
  letter-spacing: var(--nx-ls-tight);
  color: var(--nx-text);
}
.fm__domain {
  margin: 0;
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
  padding: 4px 8px 20px;
}
.fm__back { color: var(--nx-text-muted); }
.fm__back:hover { color: var(--nx-text); }

@media (max-width: 900px) {
  /* Stacked, and the window scrolls as one: with the sidebar above the list
     there is no second axis to give the list its own scroller. */
  .fm { height: auto; min-height: 100dvh; overflow: visible; }
  .fm__shell { flex-direction: column; }
  .fm__side { width: auto; flex: 0 0 auto; border-right: 0; border-bottom: 1px solid var(--nx-border); padding: 12px; }
  .fm__side-list { flex-direction: row; flex-wrap: wrap; }
  .fm__brand, .fm__domain { padding-left: 0; padding-right: 0; }
  .fm__domain { padding-bottom: 12px; }
}
.fm__side-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
.fm__side-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  text-align: left;
  border: 0;
  background: transparent;
  cursor: pointer;
  padding: 8px 12px;
  border-radius: var(--nx-radius-md);
  font-size: var(--nx-text-base);
  font-family: inherit;
  color: var(--nx-text-3);
  transition: background 130ms ease, color 130ms ease;
}
.fm__side-item:hover { background: var(--nx-hover); }
.fm__side-item.is-current {
  background: var(--nx-primary-soft);
  color: var(--nx-primary-text);
  font-weight: 500;
}
.fm__side-count { font-size: var(--nx-text-xs); color: var(--nx-text-muted); }
.fm__spacer { flex: 1; }
.fm__disk { border-top: 1px solid var(--nx-active); padding: 12px 8px 0; margin-top: 12px; }

.fm__main { flex: 1; min-width: 0; display: flex; flex-direction: column; overflow: hidden; }

.fm__tools {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
  border-bottom: 1px solid var(--nx-border);
  padding: 10px 16px;
}
.fm__tool {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  width: 64px;
  border: 0;
  background: transparent;
  border-radius: var(--nx-radius-md);
  padding: 8px 4px;
  cursor: pointer;
  font-family: inherit;
  color: var(--nx-text-2);
  transition: background 130ms ease;
}
.fm__tool:hover:not(:disabled) { background: var(--nx-hover); }
.fm__tool:disabled { opacity: 0.4; cursor: not-allowed; }
.fm__tool.is-on { background: var(--nx-primary-soft); color: var(--nx-primary-text); }
.fm__tool-label { font-size: var(--nx-text-xs); text-align: center; line-height: 1.25; }

.fm__bar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--nx-border);
  background: var(--nx-surface-2);
  flex-wrap: wrap;
}
.fm__crumbs { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; }
.fm__crumb {
  border: 0;
  background: transparent;
  cursor: pointer;
  padding: 2px 4px;
  font-size: var(--nx-text-base);
  color: var(--nx-text-muted);
  border-radius: var(--nx-radius-sm);
}
.fm__crumb:hover { background: var(--nx-hover); }
.fm__crumb.is-last { color: var(--nx-text); font-weight: 600; }
.fm__search { width: 248px; flex: 0 0 auto; }
@media (max-width: 640px) {
  .fm__search { width: 100%; }
}
.fm__count { font-size: var(--nx-text-sm); color: var(--nx-text-muted); white-space: nowrap; }

.fm__list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  /* Never let a row sit flush against the clipping edge: a half-cut row is
   * awkward to hit deliberately, and scrolling it into view mid-gesture moves
   * every other row with it. */
  padding-bottom: 44px;
  scroll-padding-bottom: 44px;
}
.fm__trash-note { margin: 16px; }
.fm__table { width: 100%; }
.fm__head,
.fm__row { display: flex; align-items: center; }
.fm__head {
  background: var(--nx-surface-2);
  border-bottom: 1px solid var(--nx-border);
  font-size: var(--nx-text-xs);
  letter-spacing: var(--nx-ls-caps);
  color: var(--nx-text-muted);
  font-weight: 600;
  text-transform: uppercase;
}
.fm__row {
  border-bottom: 1px solid var(--nx-active);
  cursor: pointer;
  user-select: none;
  transition: background 110ms ease;
}
.fm__row:hover { background: var(--nx-hover); }
.fm__row.is-picked { background: var(--nx-primary-soft); }

.fm__col-check { box-sizing: border-box; flex: 0 0 48px; padding: 12px 0 12px 16px; }
.fm__col-name {
  box-sizing: border-box;
  flex: 1 1 0%;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
}
.fm__col-size {
  box-sizing: border-box;
  flex: 0 0 96px;
  text-align: right;
  padding: 12px 16px;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-3);
  white-space: nowrap;
}
.fm__col-mod {
  box-sizing: border-box;
  flex: 0 0 150px;
  padding: 12px 16px;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
  white-space: nowrap;
}
.fm__col-perm {
  box-sizing: border-box;
  flex: 0 0 88px;
  padding: 12px 16px;
  font-size: var(--nx-text-sm);
  color: var(--nx-text-muted);
}
.fm__col-action { box-sizing: border-box; flex: 0 0 110px; text-align: right; padding: 8px 16px; }
@media (max-width: 720px) {
  .fm__col-mod,
  .fm__col-perm { display: none; }
}

.fm__ic-dir { color: var(--nx-primary-text); }
.fm__ic-warn { color: var(--nx-warning); }
.fm__ic-file { color: var(--nx-text-muted); }
.fm__name { flex: 1 1 0%; min-width: 0; font-size: var(--nx-text-base); color: var(--nx-text); }
.fm__name.is-dir { font-weight: 500; }
.fm__tag {
  flex: 0 0 auto;
  max-width: 132px;
  font-size: var(--nx-text-xs);
  color: var(--nx-text-muted);
  background: var(--nx-hover);
  border-radius: var(--nx-radius-sm);
  padding: 0 6px;
}
.fm__check { width: 16px; height: 16px; cursor: pointer; accent-color: var(--nx-primary); }

.fm__selbar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 12px;
  /* Fixed height so the strip occupies the same space whether or not anything
   * is selected — see the note on the element. */
  height: 57px;
  border-top: 1px solid var(--nx-border);
  padding: 0 16px;
  background: var(--nx-surface);
}
.fm__sel-label { font-size: var(--nx-text-base); color: var(--nx-text-2); }
.fm__sel-hint { font-size: var(--nx-text-sm); color: var(--nx-text-placeholder); }

.fm__perms { display: flex; gap: 24px; flex-wrap: wrap; }
.fm__perm-group { border: 0; margin: 0; padding: 0; }
.fm__perm-legend {
  padding: 0 0 8px;
  font-size: var(--nx-text-sm);
  font-weight: 500;
  color: var(--nx-text-2);
}
.fm__perm-bit {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  font-size: var(--nx-text-base);
  cursor: pointer;
}
.fm__perm-bit input { accent-color: var(--nx-primary); }
.fm__octal { margin: 16px 0 0; font-size: var(--nx-text-base); color: var(--nx-text-muted); }

.fm__info {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 8px 16px;
  margin: 0;
  font-size: var(--nx-text-base);
}
.fm__info dt { color: var(--nx-text-muted); }
.fm__info dd { margin: 0; overflow-wrap: anywhere; }
</style>
