import { useEffect, useMemo, useRef, useState } from "react";
import { ApiRequestError, can, isCancelled, type FileEntry } from "@/lib/api";
import { Alert, Badge, Button, Card, EmptyState, Field, Input, Modal, Select, Spinner, Toggle, cn } from "@/components/ui";
import { ContextMenu, type MenuItems, useContextMenu } from "@/components/ContextMenu";
import { toast } from "@/stores/toast";
import { useMe } from "@/features/auth/auth";
import { isIgnoredNested, parseGitignore, type IgnoreFile } from "../gitignore";
import { FileEditor, type OpenFile } from "./FileEditor";
import {
  baseName,
  downloadFile,
  downloadFolder,
  joinPath,
  parentPath,
  readFileBlob,
  readFileText,
  useChmod,
  useCompress,
  useCreateFile,
  useExtract,
  useFileList,
  useFixOwnership,
  useGitignore,
  useMkdir,
  useRemove,
  useRename,
  useSearch,
  useTransfer,
  useUpload,
} from "../files";

// The editor refuses to open a file larger than this; it is offered for download
// instead. Editing multi-megabyte files in a browser helps no one.
const MAX_EDIT_BYTES = 2 * 1024 * 1024;

const IMAGE_EXT = new Set(["png", "jpg", "jpeg", "gif", "webp", "avif", "bmp", "ico", "svg"]);
const ARCHIVE_RE = /\.(zip|tar|tar\.gz|tgz|tar\.bz2|tar\.xz)$/i;

const ext = (name: string) => name.toLowerCase().split(".").pop() ?? "";
const isImage = (name: string) => IMAGE_EXT.has(ext(name));
const isArchive = (name: string) => ARCHIVE_RE.test(name);
const isHidden = (name: string) => name.startsWith(".");

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let n = bytes / 1024;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n.toFixed(n < 10 ? 1 : 0)} ${units[i]}`;
}

const kindIcon: Record<FileEntry["kind"], string> = { dir: "📁", file: "📄", symlink: "🔗", other: "•" };

type SortKey = "name" | "size" | "mode" | "mtime";
type Sort = { key: SortKey; dir: "asc" | "desc" };

// Clipboard is a *pending intent*, not a transfer: nothing moves until paste.
// It records the folder the names were taken from, because by then the operator
// has usually navigated somewhere else — that is the whole point of cut/paste.
type Clipboard = { op: "copy" | "cut"; dir: string; names: string[] };

// Row dialogs live on the parent rather than inside each row, so the row's
// buttons, the context menu, and the keyboard shortcuts all open the same one.
type RowDialog = { kind: "rename" | "chmod" | "delete"; entry: FileEntry };

// FilesTab is the baremetal-only file manager: browse the tree, edit files in a
// multi-tab CodeMirror workspace, upload/download, search, copy/cut/paste, and
// do the housekeeping ops — every one performed by the broker as the site's
// Linux user.
export function FilesTab({ uid }: { uid: string }) {
  const { data: me } = useMe();
  const canWrite = can(me, "file.write");

  const [cwd, setCwd] = useState("");
  const { data: listing, isLoading, error, refetch, isFetching } = useFileList(uid, cwd);

  // Editor state: several files may be open at once.
  const [open, setOpen] = useState<OpenFile[]>([]);
  const [activePath, setActivePath] = useState("");

  const [preview, setPreview] = useState<{ path: string; url: string } | null>(null);
  const [busyPath, setBusyPath] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [dragging, setDragging] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [hideIgnored, setHideIgnored] = useState(false);
  const [showHidden, setShowHidden] = useState(true);
  const [sort, setSort] = useState<Sort>({ key: "name", dir: "asc" });
  const [clipboard, setClipboard] = useState<Clipboard | null>(null);
  const [dialog, setDialog] = useState<"newfile" | "newfolder" | "compress" | null>(null);
  const [rowDialog, setRowDialog] = useState<RowDialog | null>(null);
  const [bulkDelete, setBulkDelete] = useState(false);
  const [pasting, setPasting] = useState(false);

  // Recursive search. `query` is what is committed (on submit), so typing does
  // not walk the tree on every keystroke.
  const [searchDraft, setSearchDraft] = useState("");
  const [searchMode, setSearchMode] = useState<"name" | "content">("name");
  const [query, setQuery] = useState("");
  const search = useSearch(uid, cwd, query, searchMode);

  const upload = useUpload(uid);
  const remove = useRemove(uid);
  const transfer = useTransfer(uid);
  const extract = useExtract(uid);
  const chown = useFixOwnership(uid);
  const fileInput = useRef<HTMLInputElement | null>(null);
  const filterInput = useRef<HTMLInputElement | null>(null);

  // `entry === null` is the background menu (paste, new file, …).
  const menu = useContextMenu<FileEntry | null>();

  // Every .gitignore from the site root down to here, so a nested ignore file
  // can override the root's — git's own precedence.
  const { data: gitignoreFiles } = useGitignore(uid, cwd);
  const ignoreFiles = useMemo<IgnoreFile[]>(
    () => (gitignoreFiles ?? []).map((f) => ({ dir: f.dir, rules: parseGitignore(f.text) })),
    [gitignoreFiles],
  );

  // Revoke the preview's object URL whenever it changes or the tab unmounts.
  useEffect(() => {
    if (!preview) return;
    const url = preview.url;
    return () => URL.revokeObjectURL(url);
  }, [preview]);

  // Changing directory clears state that referred to the old listing. The
  // clipboard deliberately survives — pasting somewhere else is its only use.
  useEffect(() => {
    setFilter("");
    setSelected(new Set());
    setQuery("");
    setSearchDraft("");
  }, [cwd]);

  const segments = cwd ? cwd.split("/") : [];

  const decorate = (e: FileEntry) => ({
    ...e,
    ignored: ignoreFiles.length > 0 && isIgnoredNested(ignoreFiles, joinPath(cwd, e.name), e.kind === "dir"),
  });

  const visible = useMemo(() => {
    if (!listing) return [];
    const dirFirst = (a: FileEntry, b: FileEntry) => (a.kind === "dir" ? 0 : 1) - (b.kind === "dir" ? 0 : 1);
    return listing.entries
      .map(decorate)
      .filter((e) => !filter || e.name.toLowerCase().includes(filter.toLowerCase()))
      .filter((e) => !hideIgnored || !e.ignored)
      .filter((e) => showHidden || !isHidden(e.name))
      .sort((a, b) => {
        // Folders stay above files whichever column is sorted: a listing that
        // interleaves them is harder to scan, and no file manager does it.
        const d = dirFirst(a, b);
        if (d !== 0) return d;
        let r: number;
        switch (sort.key) {
          case "size":
            r = a.size - b.size;
            break;
          case "mtime":
            r = a.mtime - b.mtime;
            break;
          case "mode":
            r = a.mode.localeCompare(b.mode);
            break;
          default:
            r = a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
        }
        // Equal keys fall back to the name, so the order never jitters between
        // renders for files that share a size or timestamp.
        if (r === 0) r = a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
        return sort.dir === "asc" ? r : -r;
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [listing, filter, hideIgnored, showHidden, sort, ignoreFiles, cwd]);

  const openFileInEditor = async (path: string, size: number) => {
    // Already open? Just focus its tab.
    if (open.some((f) => f.path === path)) {
      setActivePath(path);
      return;
    }
    if (size > MAX_EDIT_BYTES) {
      toast.success("File is large — downloading instead of opening");
      await downloadFile(uid, path);
      return;
    }
    const text = await readFileText(uid, path);
    setOpen((prev) => [...prev, { path, content: text, saved: text }]);
    setActivePath(path);
  };

  const openEntry = async (entry: FileEntry) => {
    const path = joinPath(cwd, entry.name);
    if (entry.kind === "dir") {
      setCwd(path);
      return;
    }
    try {
      setBusyPath(path);
      if (isImage(entry.name)) {
        const blob = await readFileBlob(uid, path);
        setPreview({ path, url: URL.createObjectURL(blob) });
        return;
      }
      await openFileInEditor(path, entry.size);
    } catch (e) {
      toast.error("Could not open file", e instanceof ApiRequestError ? e.message : undefined);
    } finally {
      setBusyPath(null);
    }
  };

  const startUpload = (files: FileList | null) => {
    if (!canWrite || !files || files.length === 0) return;
    const list = Array.from(files);
    upload.mutate(
      { dir: cwd, files: list },
      {
        onSuccess: () => toast.success(list.length === 1 ? `Uploaded ${list[0].name}` : `${list.length} files uploaded`),
        // A cancel is the operator's own doing, not a failure to report as one.
        onError: (e) => {
          if (isCancelled(e)) {
            toast.success("Upload cancelled");
            return;
          }
          toast.error("Upload failed", e instanceof ApiRequestError ? e.message : undefined);
        },
      },
    );
    if (fileInput.current) fileInput.current.value = "";
  };

  const onDrop = (e: React.DragEvent) => {
    e.preventDefault();
    setDragging(false);
    if (!canWrite) return;
    const items = Array.from(e.dataTransfer.items ?? []);
    if (items.some((it) => it.webkitGetAsEntry?.()?.isDirectory)) {
      toast.error("Folders can't be dropped", "Zip the folder and drop that — then use Extract.");
    }
    startUpload(e.dataTransfer.files);
  };

  const toggleSelect = (name: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  // ── copy / cut / paste ──────────────────────────────────────────────────────

  // What an action applies to. Right-clicking inside the selection acts on the
  // whole selection; right-clicking outside it acts on just that row, which is
  // what every desktop file manager does and what people expect.
  const targetsFor = (entry: FileEntry | null): string[] => {
    if (!entry) return [...selected];
    return selected.has(entry.name) && selected.size > 0 ? [...selected] : [entry.name];
  };

  const stage = (op: "copy" | "cut", names: string[]) => {
    if (names.length === 0) return;
    setClipboard({ op, dir: cwd, names });
    toast.success(
      `${names.length} item${names.length === 1 ? "" : "s"} ready to ${op === "copy" ? "copy" : "move"}`,
      "Navigate to the destination folder and paste.",
    );
  };

  const paste = async (intoDir = cwd) => {
    if (!clipboard || !canWrite || pasting) return;
    const { op, dir, names } = clipboard;
    const httpOp = op === "copy" ? "copy" : "move";
    // Pasting a copy back into the folder it came from is a duplicate, and the
    // only sensible outcome is a new name beside the original. Anywhere else, a
    // name collision is a surprise and must be reported, never silently merged.
    const onConflict = op === "copy" && dir === intoDir ? "rename" : "fail";

    setPasting(true);
    let done = 0;
    const failed: string[] = [];
    for (const name of names) {
      const from = joinPath(dir, name);
      const to = joinPath(intoDir, name);
      if (op === "cut" && from === to) continue; // moving onto itself is a no-op
      try {
        await transfer.mutateAsync({ op: httpOp, from, to, onConflict });
        done++;
      } catch (e) {
        failed.push(`${name}: ${e instanceof ApiRequestError ? e.message : "failed"}`);
      }
    }
    setPasting(false);

    if (done > 0) {
      toast.success(`${done} item${done === 1 ? "" : "s"} ${op === "copy" ? "copied" : "moved"}`);
    }
    if (failed.length > 0) {
      toast.error(`${failed.length} item${failed.length === 1 ? "" : "s"} could not be pasted`, failed.join(" · "));
    }
    // A cut is one-shot: once the items have moved, the clipboard points at
    // paths that no longer exist. A copy stays, so it can be pasted repeatedly.
    if (op === "cut" && failed.length === 0) setClipboard(null);
  };

  const duplicate = async (names: string[]) => {
    for (const name of names) {
      const p = joinPath(cwd, name);
      try {
        await transfer.mutateAsync({ op: "copy", from: p, to: p, onConflict: "rename" });
      } catch (e) {
        toast.error(`Could not duplicate ${name}`, e instanceof ApiRequestError ? e.message : undefined);
        return;
      }
    }
    toast.success(names.length === 1 ? `Duplicated ${names[0]}` : `Duplicated ${names.length} items`);
  };

  const runExtract = (name: string) =>
    extract.mutate(
      { archive: joinPath(cwd, name), dest: cwd },
      {
        onSuccess: () => toast.success(`Extracted ${name}`),
        onError: (e) => toast.error("Extract failed", e instanceof ApiRequestError ? e.message : undefined),
      },
    );

  const runChown = (names: string[]) => {
    for (const name of names) {
      chown.mutate(joinPath(cwd, name), {
        onSuccess: () => toast.success(`Ownership repaired on ${name}`),
        onError: (e) => toast.error("Could not repair ownership", e instanceof ApiRequestError ? e.message : undefined),
      });
    }
  };

  const runDownload = async (entry: FileEntry) => {
    try {
      setBusyPath(joinPath(cwd, entry.name));
      if (entry.kind === "dir") await downloadFolder(uid, joinPath(cwd, entry.name));
      else await downloadFile(uid, joinPath(cwd, entry.name));
    } catch (e) {
      toast.error("Download failed", e instanceof ApiRequestError ? e.message : undefined);
    } finally {
      setBusyPath(null);
    }
  };

  const runBulkDelete = async () => {
    const names = [...selected];
    for (const n of names) {
      try {
        await remove.mutateAsync(joinPath(cwd, n));
      } catch (e) {
        toast.error(`Could not delete ${n}`, e instanceof ApiRequestError ? e.message : undefined);
      }
    }
    toast.success(names.length === 1 ? `Deleted ${names[0]}` : `Deleted ${names.length} items`);
    setSelected(new Set());
    setBulkDelete(false);
  };

  // ── the context menu ────────────────────────────────────────────────────────

  const menuItems = (entry: FileEntry | null): MenuItems => {
    const names = targetsFor(entry);
    const many = names.length > 1;
    const label = many ? `${names.length} items` : names[0];
    const clip = clipboard ? `${clipboard.names.length} item${clipboard.names.length === 1 ? "" : "s"}` : "";

    if (!entry) {
      // Background: acts on the folder itself.
      return [
        canWrite && {
          label: clipboard ? `Paste ${clip}` : "Paste",
          shortcut: "Ctrl+V",
          disabled: !clipboard,
          onSelect: () => void paste(),
        },
        canWrite && { label: "New file", separatorBefore: true, onSelect: () => setDialog("newfile") },
        canWrite && { label: "New folder", onSelect: () => setDialog("newfolder") },
        canWrite && { label: "Upload files…", onSelect: () => fileInput.current?.click() },
        { label: "Select all", shortcut: "Ctrl+A", separatorBefore: true, onSelect: () => setSelected(new Set(visible.map((v) => v.name))) },
        { label: "Refresh", onSelect: () => void refetch() },
      ];
    }

    return [
      { label: entry.kind === "dir" ? "Open folder" : "Open", shortcut: "Enter", onSelect: () => void openEntry(entry) },
      { label: entry.kind === "dir" ? "Download as .zip" : "Download", onSelect: () => void runDownload(entry) },
      canWrite && isArchive(entry.name) && { label: "Extract here", onSelect: () => runExtract(entry.name) },

      canWrite && { label: `Copy ${many ? label : ""}`.trim(), shortcut: "Ctrl+C", separatorBefore: true, onSelect: () => stage("copy", names) },
      canWrite && { label: `Cut ${many ? label : ""}`.trim(), shortcut: "Ctrl+X", onSelect: () => stage("cut", names) },
      canWrite && { label: "Duplicate", onSelect: () => void duplicate(names) },
      canWrite &&
        entry.kind === "dir" &&
        clipboard && { label: `Paste ${clip} into ${entry.name}`, onSelect: () => void paste(joinPath(cwd, entry.name)) },

      canWrite && { label: "Rename…", shortcut: "F2", separatorBefore: true, disabled: many, onSelect: () => setRowDialog({ kind: "rename", entry }) },
      canWrite && { label: "Permissions…", disabled: many, onSelect: () => setRowDialog({ kind: "chmod", entry }) },
      canWrite && { label: "Repair ownership", onSelect: () => runChown(names) },
      canWrite && {
        label: "Compress…",
        onSelect: () => {
          setSelected(new Set(names));
          setDialog("compress");
        },
      },

      canWrite && {
        label: many ? `Delete ${label}` : "Delete…",
        shortcut: "Del",
        separatorBefore: true,
        danger: true,
        onSelect: () => {
          if (many) {
            setSelected(new Set(names));
            setBulkDelete(true);
          } else {
            setRowDialog({ kind: "delete", entry });
          }
        },
      },
    ];
  };

  // ── keyboard ────────────────────────────────────────────────────────────────

  // Shortcuts for the browser. They are skipped while typing in an input, so the
  // filter box never swallows a delete key or a Ctrl+C that means "copy text".
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement | null;
      const typing = !!el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable);
      if (e.key === "Escape") {
        setSelected(new Set());
        setQuery("");
        return;
      }
      if (typing) return;
      const mod = e.metaKey || e.ctrlKey;
      const key = e.key.toLowerCase();
      const one = selected.size === 1 ? visible.find((v) => v.name === [...selected][0]) : undefined;

      if (mod && key === "a") {
        e.preventDefault();
        setSelected(new Set(visible.map((v) => v.name)));
        return;
      }
      if (mod && key === "c" && canWrite && selected.size > 0) {
        // Only hijack Ctrl+C when nothing is selected in the document — otherwise
        // the operator is copying text they highlighted, not files.
        if (window.getSelection()?.toString()) return;
        e.preventDefault();
        stage("copy", [...selected]);
        return;
      }
      if (mod && key === "x" && canWrite && selected.size > 0) {
        e.preventDefault();
        stage("cut", [...selected]);
        return;
      }
      if (mod && key === "v" && canWrite && clipboard) {
        e.preventDefault();
        void paste();
        return;
      }
      if (e.key === "F2" && canWrite && one) {
        e.preventDefault();
        setRowDialog({ kind: "rename", entry: one });
        return;
      }
      if (e.key === "Enter" && one) {
        e.preventDefault();
        void openEntry(one);
        return;
      }
      if (e.key === "/") {
        e.preventDefault();
        filterInput.current?.focus();
        return;
      }
      if (e.key === "Delete" && canWrite && selected.size > 0) {
        e.preventDefault();
        setBulkDelete(true);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible, selected, canWrite, clipboard, cwd, pasting]);

  // The editor takes over the panel when files are open.
  if (open.length > 0) {
    return (
      <FileEditor
        uid={uid}
        files={open}
        activePath={activePath}
        onActivate={setActivePath}
        onChange={(path, next) => setOpen((prev) => prev.map((f) => (f.path === path ? { ...f, content: next } : f)))}
        onSaved={(path, saved) => setOpen((prev) => prev.map((f) => (f.path === path ? { ...f, saved } : f)))}
        onCloseFile={(path) =>
          setOpen((prev) => {
            const next = prev.filter((f) => f.path !== path);
            if (path === activePath && next.length) setActivePath(next[next.length - 1].path);
            return next;
          })
        }
        onCloseAll={() => {
          setOpen([]);
          setActivePath("");
        }}
      />
    );
  }

  return (
    <div className="space-y-4">
      {/* Toolbar: breadcrumb + actions */}
      <Card className="space-y-3 p-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <nav className="flex flex-wrap items-center gap-1 text-sm">
            <button className="rounded px-2 py-1 text-muted hover:bg-border/50 hover:text-fg" onClick={() => setCwd("")}>
              Home /
            </button>
            {segments.map((seg, i) => {
              const to = segments.slice(0, i + 1).join("/");
              const last = i === segments.length - 1;
              return (
                <button
                  key={to}
                  onClick={() => setCwd(to)}
                  className={cn(
                    "rounded px-2 py-1",
                    last ? "font-medium text-fg" : "text-muted hover:bg-border/50 hover:text-fg",
                  )}
                >
                  {seg} {last ? "" : "/"}
                </button>
              );
            })}
          </nav>
          <div className="flex flex-wrap items-center gap-2">
            <Input
              ref={filterInput}
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Filter (/)"
              className="h-9 w-36"
            />
            <Button variant="ghost" className="h-9 px-3" onClick={() => refetch()}>
              {isFetching ? <Spinner /> : "Refresh"}
            </Button>
            {canWrite && (
              <>
                <Button variant="ghost" className="h-9 px-3" onClick={() => setDialog("newfile")}>
                  New file
                </Button>
                <Button variant="ghost" className="h-9 px-3" onClick={() => setDialog("newfolder")}>
                  New folder
                </Button>
                <input ref={fileInput} type="file" multiple className="hidden" onChange={(e) => startUpload(e.target.files)} />
                <Button className="h-9 px-3" loading={upload.isPending} onClick={() => fileInput.current?.click()}>
                  Upload
                </Button>
              </>
            )}
          </div>
        </div>

        {/* Recursive search + display toggles */}
        <div className="flex flex-wrap items-center gap-3 border-t border-border pt-3">
          <form
            className="flex flex-1 flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              setQuery(searchDraft.trim());
            }}
          >
            <Input
              value={searchDraft}
              onChange={(e) => setSearchDraft(e.target.value)}
              placeholder={`Search ${cwd ? `/${cwd}` : "the whole site"} recursively…`}
              className="h-9 min-w-[12rem] flex-1"
            />
            <Select
              value={searchMode}
              onChange={(e) => setSearchMode(e.target.value as "name" | "content")}
              className="h-9 w-36"
            >
              <option value="name">by name</option>
              <option value="content">by content</option>
            </Select>
            <Button type="submit" variant="ghost" className="h-9 px-3">
              Search
            </Button>
            {query && (
              <Button type="button" variant="ghost" className="h-9 px-3" onClick={() => { setQuery(""); setSearchDraft(""); }}>
                Clear
              </Button>
            )}
          </form>
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted">Show hidden</span>
            <Toggle checked={showHidden} onChange={setShowHidden} label="Show hidden files (dotfiles)" />
          </div>
          {ignoreFiles.length > 0 && (
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted">Hide git-ignored</span>
              <Toggle checked={hideIgnored} onChange={setHideIgnored} label="Hide git-ignored files" />
            </div>
          )}
        </div>
      </Card>

      {/* Upload progress: a real byte-accurate bar, and a way out of it */}
      {upload.progress && (
        <Card className="space-y-2 p-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <span className="truncate text-sm text-fg">
              Uploading{" "}
              {upload.progress.count > 1
                ? `${upload.progress.index} of ${upload.progress.count} — ${upload.progress.file}`
                : upload.progress.file}
            </span>
            <div className="flex items-center gap-3">
              <span className="text-xs tabular-nums text-muted">
                {formatSize(upload.progress.loaded)} / {formatSize(upload.progress.total)} ·{" "}
                {upload.progress.percent}%
              </span>
              <Button variant="ghost" className="h-8 px-3" onClick={() => upload.cancel()}>
                Cancel
              </Button>
            </div>
          </div>
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-border">
            <div
              className="h-full rounded-full bg-brand transition-[width] duration-150"
              style={{ width: `${upload.progress.percent}%` }}
              role="progressbar"
              aria-valuenow={upload.progress.percent}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-label="Upload progress"
            />
          </div>
        </Card>
      )}

      {/* Clipboard bar: what is staged, and where it came from */}
      {clipboard && canWrite && (
        <Card className="flex flex-wrap items-center justify-between gap-3 border-dashed p-3">
          <span className="text-sm text-fg">
            {clipboard.names.length} item{clipboard.names.length === 1 ? "" : "s"} to{" "}
            {clipboard.op === "copy" ? "copy" : "move"} from{" "}
            <code className="rounded bg-surface px-1 py-0.5 font-mono text-xs">/{clipboard.dir || ""}</code>
          </span>
          <div className="flex items-center gap-2">
            <Button className="h-9 px-3" loading={pasting} onClick={() => void paste()}>
              Paste here
            </Button>
            <Button variant="ghost" className="h-9 px-3" onClick={() => setClipboard(null)}>
              Cancel
            </Button>
          </div>
        </Card>
      )}

      {/* Bulk action bar */}
      {selected.size > 0 && canWrite && (
        <Card className="flex flex-wrap items-center justify-between gap-3 border-brand p-3">
          <span className="text-sm text-fg">{selected.size} selected</span>
          <div className="flex items-center gap-2">
            <Button variant="ghost" className="h-9 px-3" onClick={() => stage("copy", [...selected])}>
              Copy
            </Button>
            <Button variant="ghost" className="h-9 px-3" onClick={() => stage("cut", [...selected])}>
              Cut
            </Button>
            <Button variant="ghost" className="h-9 px-3" onClick={() => setDialog("compress")}>
              Compress
            </Button>
            <Button variant="danger" className="h-9 px-3" onClick={() => setBulkDelete(true)}>
              Delete
            </Button>
            <Button variant="ghost" className="h-9 px-3" onClick={() => setSelected(new Set())}>
              Clear
            </Button>
          </div>
        </Card>
      )}

      {/* Search results replace the listing while a query is active */}
      {query ? (
        <SearchResultsPanel
          uid={uid}
          cwd={cwd}
          query={query}
          loading={search.isLoading}
          results={search.data}
          onOpenDir={(p) => { setQuery(""); setCwd(p); }}
          onOpenFile={async (p) => {
            try {
              await openFileInEditor(p, 0);
            } catch (e) {
              toast.error("Could not open file", e instanceof ApiRequestError ? e.message : undefined);
            }
          }}
        />
      ) : error ? (
        <Alert>
          {error instanceof ApiRequestError && error.status === 403
            ? "You do not have permission to browse this site's files."
            : "Could not list this directory."}
        </Alert>
      ) : isLoading ? (
        <div className="flex items-center gap-2 text-muted">
          <Spinner /> Loading…
        </div>
      ) : (
        <div
          className="relative"
          onDragOver={(e) => {
            if (!canWrite) return;
            e.preventDefault();
            setDragging(true);
          }}
          onDragLeave={(e) => {
            if (e.currentTarget.contains(e.relatedTarget as Node)) return;
            setDragging(false);
          }}
          onDrop={onDrop}
          onContextMenu={(e) => menu.open(e, null)}
        >
          {dragging && (
            <div className="pointer-events-none absolute inset-0 z-10 grid place-items-center rounded-xl border-2 border-dashed border-brand bg-brand/10">
              <p className="text-sm font-medium text-fg">Drop to upload into {cwd ? `/${cwd}` : "the site root"}</p>
            </div>
          )}
          <Card className="overflow-hidden">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs text-muted">
                  {canWrite && (
                    <th className="w-10 px-3 py-2">
                      <input
                        type="checkbox"
                        aria-label="Select all"
                        checked={visible.length > 0 && selected.size === visible.length}
                        onChange={(e) =>
                          setSelected(e.target.checked ? new Set(visible.map((v) => v.name)) : new Set())
                        }
                      />
                    </th>
                  )}
                  <SortHeader label="Name" sortKey="name" sort={sort} onSort={setSort} />
                  <SortHeader label="Size" sortKey="size" sort={sort} onSort={setSort} />
                  <SortHeader label="Mode" sortKey="mode" sort={sort} onSort={setSort} />
                  <SortHeader label="Modified" sortKey="mtime" sort={sort} onSort={setSort} />
                  <th className="px-4 py-2" />
                </tr>
              </thead>
              <tbody>
                {cwd !== "" && (
                  <tr className="border-b border-border/60">
                    <td colSpan={canWrite ? 6 : 5} className="px-4 py-2">
                      <button className="text-muted hover:text-fg" onClick={() => setCwd(parentPath(cwd))}>
                        ← ..
                      </button>
                    </td>
                  </tr>
                )}
                {visible.length > 0 ? (
                  visible.map((entry) => (
                    <FileRow
                      key={entry.name}
                      cwd={cwd}
                      entry={entry}
                      ignored={entry.ignored}
                      canWrite={canWrite}
                      busy={busyPath === joinPath(cwd, entry.name)}
                      checked={selected.has(entry.name)}
                      cut={clipboard?.op === "cut" && clipboard.dir === cwd && clipboard.names.includes(entry.name)}
                      onToggle={() => toggleSelect(entry.name)}
                      onOpen={() => openEntry(entry)}
                      onDownload={() => void runDownload(entry)}
                      onMenu={(e) => menu.open(e, entry)}
                    />
                  ))
                ) : (
                  <tr>
                    <td colSpan={canWrite ? 6 : 5}>
                      {filter || hideIgnored || !showHidden ? (
                        <EmptyState title="Nothing matches" hint="Clear the filter, or turn the display toggles back on." />
                      ) : (
                        <EmptyState
                          title="Empty directory"
                          hint={canWrite ? "Upload a file, drop one here, or right-click to create." : undefined}
                        />
                      )}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </Card>
          <p className="mt-2 text-xs text-muted">
            <span className="font-medium text-fg">Shortcuts:</span> right-click for actions · / filter · Ctrl/⌘-A select
            all · Ctrl/⌘-C copy · Ctrl/⌘-X cut · Ctrl/⌘-V paste · F2 rename · Enter open · Delete remove · Esc clear
          </p>
        </div>
      )}

      {menu.menu && (
        <ContextMenu x={menu.menu.x} y={menu.menu.y} items={menuItems(menu.menu.target)} onClose={menu.close} />
      )}

      {dialog === "newfile" && <NewEntryDialog uid={uid} cwd={cwd} kind="file" onClose={() => setDialog(null)} />}
      {dialog === "newfolder" && <NewEntryDialog uid={uid} cwd={cwd} kind="folder" onClose={() => setDialog(null)} />}
      {dialog === "compress" && (
        <CompressDialog
          uid={uid}
          cwd={cwd}
          sources={[...selected]}
          onClose={() => setDialog(null)}
          onDone={() => setSelected(new Set())}
        />
      )}

      {rowDialog?.kind === "rename" && (
        <RenameDialog uid={uid} cwd={cwd} entry={rowDialog.entry} onClose={() => setRowDialog(null)} />
      )}
      {rowDialog?.kind === "chmod" && (
        <ChmodDialog
          uid={uid}
          path={joinPath(cwd, rowDialog.entry.name)}
          current={rowDialog.entry.mode}
          onClose={() => setRowDialog(null)}
        />
      )}
      {rowDialog?.kind === "delete" && (
        <DeleteDialog uid={uid} cwd={cwd} entry={rowDialog.entry} onClose={() => setRowDialog(null)} />
      )}

      {bulkDelete && (
        <Modal title={`Delete ${selected.size} item${selected.size === 1 ? "" : "s"}?`} onClose={() => setBulkDelete(false)}>
          <p className="text-sm text-muted">
            This permanently deletes the selected items, including everything inside any folders. This cannot be undone.
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setBulkDelete(false)}>
              Cancel
            </Button>
            <Button variant="danger" loading={remove.isPending} onClick={runBulkDelete}>
              Delete
            </Button>
          </div>
        </Modal>
      )}

      {preview && (
        <Modal title={baseName(preview.path)} wide onClose={() => setPreview(null)}>
          <img src={preview.url} alt={baseName(preview.path)} className="mx-auto max-h-[70vh] max-w-full rounded-lg" />
        </Modal>
      )}
    </div>
  );
}

// SortHeader is a clickable column header: click to sort by it, click again to
// reverse. The arrow marks which column is active, so the order is never a
// mystery the operator has to infer from the rows.
function SortHeader({
  label,
  sortKey,
  sort,
  onSort,
}: {
  label: string;
  sortKey: SortKey;
  sort: Sort;
  onSort: (s: Sort) => void;
}) {
  const active = sort.key === sortKey;
  return (
    <th className="px-4 py-2 font-medium">
      <button
        className={cn("flex items-center gap-1 hover:text-fg", active && "text-fg")}
        aria-sort={active ? (sort.dir === "asc" ? "ascending" : "descending") : "none"}
        onClick={() => onSort({ key: sortKey, dir: active && sort.dir === "asc" ? "desc" : "asc" })}
      >
        {label}
        <span aria-hidden className={cn("text-[10px]", !active && "opacity-0")}>
          {sort.dir === "asc" ? "▲" : "▼"}
        </span>
      </button>
    </th>
  );
}

// SearchResultsPanel shows recursive search hits, each navigable.
function SearchResultsPanel({
  cwd,
  query,
  loading,
  results,
  onOpenDir,
  onOpenFile,
}: {
  uid: string;
  cwd: string;
  query: string;
  loading: boolean;
  results?: { entries: { name: string; path: string; kind: string; size: number }[]; truncated: boolean };
  onOpenDir: (path: string) => void;
  onOpenFile: (path: string) => void;
}) {
  if (loading) {
    return (
      <div className="flex items-center gap-2 text-muted">
        <Spinner /> Searching…
      </div>
    );
  }
  const entries = results?.entries ?? [];
  return (
    <Card className="overflow-hidden">
      <div className="flex items-center justify-between border-b border-border px-4 py-3 text-sm">
        <span className="text-fg">
          {entries.length} result{entries.length === 1 ? "" : "s"} for “{query}”
        </span>
        {results?.truncated && <Badge>showing the first 500</Badge>}
      </div>
      {entries.length === 0 ? (
        <EmptyState title="No matches" hint="Try a different term, or switch between name and content search." />
      ) : (
        <table className="w-full text-sm">
          <tbody>
            {entries.map((e) => {
              const full = joinPath(cwd, e.path);
              return (
                <tr key={e.path} className="border-b border-border/60 last:border-0 hover:bg-surface/60">
                  <td className="px-4 py-2.5">
                    <button
                      className="flex items-center gap-2 text-left hover:underline"
                      onClick={() => (e.kind === "dir" ? onOpenDir(full) : onOpenFile(full))}
                    >
                      <span aria-hidden>{e.kind === "dir" ? "📁" : "📄"}</span>
                      <span className="break-all text-fg">{e.name}</span>
                      <span className="break-all text-xs text-muted">/{full}</span>
                    </button>
                  </td>
                  <td className="px-4 py-2.5 text-right text-muted">{e.kind === "dir" ? "—" : formatSize(e.size)}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </Card>
  );
}

function FileRow({
  cwd,
  entry,
  ignored,
  canWrite,
  busy,
  checked,
  cut,
  onToggle,
  onOpen,
  onDownload,
  onMenu,
}: {
  cwd: string;
  entry: FileEntry;
  ignored?: boolean;
  canWrite: boolean;
  busy: boolean;
  checked: boolean;
  cut?: boolean;
  onToggle: () => void;
  onOpen: () => void;
  onDownload: () => void;
  onMenu: (e: React.MouseEvent) => void;
}) {
  void cwd;
  return (
    <tr
      onContextMenu={onMenu}
      className={cn(
        "border-b border-border/60 last:border-0 hover:bg-surface/60",
        checked && "bg-brand/5",
        // A cut entry is still here until the paste lands; showing it faded is
        // how the operator knows the cut registered.
        cut && "opacity-50",
      )}
    >
      {canWrite && (
        <td className="px-3 py-2.5">
          <input type="checkbox" aria-label={`Select ${entry.name}`} checked={checked} onChange={onToggle} />
        </td>
      )}
      <td className="px-4 py-2.5">
        <button className="flex items-center gap-2 text-left hover:underline" onClick={onOpen} disabled={busy}>
          <span aria-hidden>{busy ? <Spinner /> : kindIcon[entry.kind]}</span>
          <span className={cn("break-all", ignored ? "text-muted" : "text-fg")}>{entry.name}</span>
          {ignored && <Badge>git-ignored</Badge>}
        </button>
      </td>
      <td className="px-4 py-2.5 text-muted">{entry.kind === "dir" ? "—" : formatSize(entry.size)}</td>
      <td className="px-4 py-2.5 font-mono text-xs text-muted">{entry.mode}</td>
      <td className="px-4 py-2.5 text-xs text-muted">
        {entry.mtime ? new Date(entry.mtime * 1000).toLocaleString() : "—"}
      </td>
      <td className="px-4 py-2.5">
        {/* Two buttons, not eight: the rest of the actions live in the menu, which
            is where a right-click already looks for them. */}
        <div className="flex items-center justify-end gap-1">
          <IconAction label={entry.kind === "dir" ? "Download as .zip" : "Download"} onClick={onDownload}>
            ↓
          </IconAction>
          <IconAction label="More actions" onClick={onMenu}>
            ⋯
          </IconAction>
        </div>
      </td>
    </tr>
  );
}

function IconAction({
  children,
  label,
  onClick,
  danger,
  loading,
}: {
  children: React.ReactNode;
  label: string;
  onClick: (e: React.MouseEvent) => void;
  danger?: boolean;
  loading?: boolean;
}) {
  return (
    <button
      title={label}
      aria-label={label}
      onClick={onClick}
      disabled={loading}
      className={cn(
        "grid h-8 w-8 place-items-center rounded-lg border border-border text-sm transition-colors hover:bg-border/50 disabled:opacity-50",
        danger ? "text-danger" : "text-muted hover:text-fg",
      )}
    >
      {loading ? <Spinner /> : children}
    </button>
  );
}

// NewEntryDialog creates an empty file or a folder.
function NewEntryDialog({
  uid,
  cwd,
  kind,
  onClose,
}: {
  uid: string;
  cwd: string;
  kind: "file" | "folder";
  onClose: () => void;
}) {
  const [name, setName] = useState("");
  const mkdir = useMkdir(uid);
  const createFile = useCreateFile(uid);
  const busy = kind === "folder" ? mkdir.isPending : createFile.isPending;

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const n = name.trim();
    if (!n) return;
    const path = joinPath(cwd, n);
    const opts = {
      onSuccess: () => {
        toast.success(kind === "folder" ? "Folder created" : `Created ${n}`);
        onClose();
      },
      onError: (er: unknown) =>
        toast.error(`Could not create the ${kind}`, er instanceof ApiRequestError ? er.message : undefined),
    };
    if (kind === "folder") mkdir.mutate(path, opts);
    else createFile.mutate(path, opts);
  };

  return (
    <Modal title={kind === "folder" ? "New folder" : "New file"} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <Field label={kind === "folder" ? "Folder name" : "File name"}>
          <Input
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={kind === "folder" ? "assets" : "index.php"}
          />
        </Field>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={busy}>
            Create
          </Button>
        </div>
      </form>
    </Modal>
  );
}

// CompressDialog archives the selected entries into this folder.
function CompressDialog({
  uid,
  cwd,
  sources,
  onClose,
  onDone,
}: {
  uid: string;
  cwd: string;
  sources: string[];
  onClose: () => void;
  onDone: () => void;
}) {
  const [name, setName] = useState(sources.length === 1 ? `${sources[0]}.zip` : "archive.zip");
  const [format, setFormat] = useState("zip");
  const compress = useCompress(uid);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const archive = name.trim();
    if (!archive) return;
    compress.mutate(
      {
        sources: sources.map((s) => joinPath(cwd, s)),
        archive: joinPath(cwd, archive),
        format,
      },
      {
        onSuccess: () => {
          toast.success(`Created ${archive}`);
          onDone();
          onClose();
        },
        onError: (er) => toast.error("Could not create the archive", er instanceof ApiRequestError ? er.message : undefined),
      },
    );
  };

  return (
    <Modal title={`Compress ${sources.length} item${sources.length === 1 ? "" : "s"}`} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <Field label="Archive name" hint="Created in this folder; download it from the list afterwards.">
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Format">
          <Select
            value={format}
            onChange={(e) => {
              const f = e.target.value;
              setFormat(f);
              setName((n) => n.replace(/\.(zip|tar\.gz|tgz)$/i, "") + (f === "zip" ? ".zip" : ".tar.gz"));
            }}
          >
            <option value="zip">zip</option>
            <option value="tar.gz">tar.gz</option>
          </Select>
        </Field>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={compress.isPending}>
            Compress
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function RenameDialog({
  uid,
  cwd,
  entry,
  onClose,
}: {
  uid: string;
  cwd: string;
  entry: FileEntry;
  onClose: () => void;
}) {
  const [name, setName] = useState(entry.name);
  const rename = useRename(uid);
  const input = useRef<HTMLInputElement | null>(null);

  // Preselect the stem, not the extension: renaming "logo.png" almost always
  // means changing "logo", and having to skip past ".png" every time is friction
  // the operator notices.
  useEffect(() => {
    const el = input.current;
    if (!el) return;
    el.focus();
    const dot = entry.name.lastIndexOf(".");
    el.setSelectionRange(0, dot > 0 ? dot : entry.name.length);
  }, [entry.name]);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const to = name.trim();
    if (!to || to === entry.name) {
      onClose();
      return;
    }
    rename.mutate(
      { from: joinPath(cwd, entry.name), to: joinPath(cwd, to) },
      {
        onSuccess: () => {
          toast.success("Renamed");
          onClose();
        },
        onError: (er) => toast.error("Rename failed", er instanceof ApiRequestError ? er.message : undefined),
      },
    );
  };

  return (
    <Modal title={`Rename ${entry.name}`} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <Field label="New name" hint="Renames the entry in place, within this folder.">
          <Input ref={input} value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={rename.isPending}>
            Rename
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function ChmodDialog({
  uid,
  path,
  current,
  onClose,
}: {
  uid: string;
  path: string;
  current: string;
  onClose: () => void;
}) {
  const [mode, setMode] = useState(current);
  const chmod = useChmod(uid);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    chmod.mutate(
      { path, mode },
      {
        onSuccess: () => {
          toast.success("Permissions changed");
          onClose();
        },
        onError: (er) => toast.error("Could not change mode", er instanceof ApiRequestError ? er.message : undefined),
      },
    );
  };

  return (
    <Modal title={`Permissions — ${baseName(path)}`} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <Field label="Octal mode" hint="3–4 octal digits, e.g. 644 for files, 755 for directories.">
          <Input autoFocus value={mode} onChange={(e) => setMode(e.target.value)} className="font-mono" />
        </Field>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={chmod.isPending}>
            Apply
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function DeleteDialog({
  uid,
  cwd,
  entry,
  onClose,
}: {
  uid: string;
  cwd: string;
  entry: FileEntry;
  onClose: () => void;
}) {
  const remove = useRemove(uid);
  const run = () =>
    remove.mutate(joinPath(cwd, entry.name), {
      onSuccess: () => {
        toast.success(`Deleted ${entry.name}`);
        onClose();
      },
      onError: (e) => toast.error("Delete failed", e instanceof ApiRequestError ? e.message : undefined),
    });

  return (
    <Modal title={`Delete ${entry.name}?`} onClose={onClose}>
      <p className="text-sm text-muted">
        {entry.kind === "dir"
          ? "This deletes the directory and everything inside it. This cannot be undone."
          : "This permanently deletes the file. This cannot be undone."}
      </p>
      <div className="mt-4 flex justify-end gap-2">
        <Button variant="ghost" onClick={onClose}>
          Cancel
        </Button>
        <Button variant="danger" loading={remove.isPending} onClick={run}>
          Delete
        </Button>
      </div>
    </Modal>
  );
}
