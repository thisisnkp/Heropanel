import { computed, ref } from "vue";
import { defineStore } from "pinia";
import { DIRS, INITIAL_TRASH, type FsEntry } from "@/data/fileSystem";

export type FileView = "browse" | "trash";

/** A trashed entry remembers where it came from, so Restore can put it back. */
interface TrashedEntry extends FsEntry {
  readonly origin: string;
}

/**
 * File manager state: where you are, what is selected, and what is in the trash.
 *
 * A store rather than component state because the browser, the context menu and
 * the editor tabs all act on the same selection, and because navigating into a
 * folder and then opening the editor should not lose your place.
 *
 * The fixture tree in `data/fileSystem` is immutable, so changes are kept as
 * two overlays over it — `added` and `removed`, both keyed by directory. That
 * is also the shape the real API works in: the server owns the tree and the
 * client only ever describes deltas to it.
 */
export const useFileManagerStore = defineStore("fileManager", () => {
  const path = ref<string[]>(["public_html"]);
  const view = ref<FileView>("browse");
  const selected = ref<string[]>([]);
  const multiSelect = ref(false);
  const query = ref("");

  const added = ref<Record<string, FsEntry[]>>({});
  const removed = ref<Record<string, string[]>>({});
  const trash = ref<TrashedEntry[]>(INITIAL_TRASH.map((e) => ({ ...e, origin: "public_html" })));

  const tabs = ref<string[]>([]);
  const openFile = ref<string | null>(null);

  const pathKey = computed(() => path.value.join("/"));

  /** The live contents of one directory: fixture, plus additions, minus removals. */
  function listing(key: string): FsEntry[] {
    const gone = removed.value[key] ?? [];
    return [...(DIRS[key] ?? []), ...(added.value[key] ?? [])].filter((r) => !gone.includes(r.name));
  }

  /**
   * The rows to show.
   *
   * A search that matches nothing in the current folder falls back to every
   * folder, tagging each hit with where it lives. The design did this so a name
   * you remember is never a dead end — a file manager that says "no results"
   * while the file sits one level up is technically correct and useless.
   */
  const rows = computed<FsEntry[]>(() => {
    if (view.value === "trash") return trash.value;

    const q = query.value.trim().toLowerCase();
    const here = listing(pathKey.value);
    if (!q) return here;

    const local = here.filter((r) => r.name.toLowerCase().includes(q));
    if (local.length) return local;

    const everywhere: FsEntry[] = [];
    for (const key of Object.keys(DIRS)) {
      for (const r of listing(key)) {
        if (r.name.toLowerCase().includes(q)) {
          everywhere.push({ ...r, tag: "in " + (key || "account root"), where: key });
        }
      }
    }
    return everywhere;
  });

  const isEmpty = computed(() => rows.value.length === 0);
  const hasSelection = computed(() => selected.value.length > 0);
  const searching = computed(() => query.value.trim().length > 0);

  const breadcrumbs = computed(() => {
    const crumbs = [{ label: "account root", index: -1 }];
    path.value.forEach((seg, i) => crumbs.push({ label: seg, index: i }));
    return crumbs;
  });

  function goTo(index: number) {
    path.value = index < 0 ? [] : path.value.slice(0, index + 1);
    view.value = "browse";
    selected.value = [];
    query.value = "";
  }

  function enter(dir: string) {
    path.value = [...path.value, dir];
    selected.value = [];
    query.value = "";
  }

  /** Jumps to the folder a cross-folder search hit actually lives in. */
  function jumpTo(where: string, select: string) {
    path.value = where ? where.split("/") : [];
    query.value = "";
    selected.value = [select];
    view.value = "browse";
  }

  function setView(next: FileView) {
    view.value = next;
    selected.value = [];
    query.value = "";
  }

  function toggleSelect(name: string) {
    selected.value = selected.value.includes(name)
      ? selected.value.filter((x) => x !== name)
      : [...selected.value, name];
  }

  /** Click behaviour: replaces the selection unless multi-select is on. */
  function selectOne(name: string) {
    if (multiSelect.value) toggleSelect(name);
    else selected.value = [name];
  }

  function clearSelection() {
    selected.value = [];
  }

  function toggleMultiSelect() {
    multiSelect.value = !multiSelect.value;
    selected.value = [];
  }

  function moveToTrash(names: readonly string[]) {
    const key = pathKey.value;
    const going = listing(key).filter((r) => names.includes(r.name));
    trash.value = [
      ...going.map((r) => ({ ...r, tag: "from " + (key || "account root"), origin: key })),
      ...trash.value,
    ];
    removed.value = { ...removed.value, [key]: [...(removed.value[key] ?? []), ...names] };
    selected.value = [];
  }

  function restore(name: string) {
    const entry = trash.value.find((t) => t.name === name);
    if (!entry) return;
    trash.value = trash.value.filter((t) => t.name !== name);

    const { origin, ...rest } = entry;
    // Un-remove it if it came from the fixture; otherwise re-add it. Either way
    // it reappears in the folder it was deleted from, not in whatever folder
    // happens to be open.
    const gone = removed.value[origin] ?? [];
    if (gone.includes(name)) {
      removed.value = { ...removed.value, [origin]: gone.filter((n) => n !== name) };
    } else {
      added.value = { ...added.value, [origin]: [...(added.value[origin] ?? []), { ...rest, tag: "" }] };
    }
  }

  function create(kind: "file" | "dir", name: string) {
    const key = pathKey.value;
    const entry: FsEntry = {
      name,
      type: kind,
      size: kind === "dir" ? "—" : "0 B",
      mod: "just now",
      perm: kind === "dir" ? "0755" : "0644",
      tag: "",
    };
    added.value = { ...added.value, [key]: [...(added.value[key] ?? []), entry] };
  }

  function openInEditor(name: string) {
    if (!tabs.value.includes(name)) tabs.value = [...tabs.value, name];
    openFile.value = name;
  }

  function closeTab(name: string) {
    tabs.value = tabs.value.filter((t) => t !== name);
    if (openFile.value === name) openFile.value = tabs.value.at(-1) ?? null;
  }

  function closeEditor() {
    openFile.value = null;
  }

  return {
    path, view, selected, multiSelect, query, trash, tabs, openFile,
    pathKey, rows, isEmpty, hasSelection, searching, breadcrumbs,
    goTo, enter, jumpTo, setView, toggleSelect, selectOne, clearSelection,
    toggleMultiSelect, moveToTrash, restore, create, openInEditor, closeTab, closeEditor,
  };
});
