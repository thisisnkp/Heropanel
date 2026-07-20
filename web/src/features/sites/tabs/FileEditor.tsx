import { lazy, Suspense, useEffect, useState } from "react";
import { ApiRequestError } from "@/lib/api";
import { Badge, Button, Modal, Spinner, cn } from "@/components/ui";
import { toast } from "@/stores/toast";
import { diffLines } from "@/lib/diff";
import { baseName, useSaveFile } from "../files";

// CodeMirror and its language packages are large, so the editor loads only when
// a file is actually opened.
const CodeEditor = lazy(() => import("@/components/CodeEditor").then((m) => ({ default: m.CodeEditor })));

// OpenFile is one buffer in the editor. `saved` is the content as it exists on
// disk; comparing it with `content` gives both the dirty flag and the diff.
export interface OpenFile {
  path: string;
  content: string;
  saved: string;
}

// FileEditor is the multi-tab editing workspace. Several files can be open at
// once (the roadmap's "tabs"), each with its own dirty state, and a diff view
// shows exactly what a save would change before it is written.
export function FileEditor({
  uid,
  files,
  activePath,
  onActivate,
  onChange,
  onSaved,
  onCloseFile,
  onCloseAll,
}: {
  uid: string;
  files: OpenFile[];
  activePath: string;
  onActivate: (path: string) => void;
  onChange: (path: string, next: string) => void;
  onSaved: (path: string, saved: string) => void;
  onCloseFile: (path: string) => void;
  onCloseAll: () => void;
}) {
  const save = useSaveFile(uid);
  const [showDiff, setShowDiff] = useState(false);
  const [confirmClose, setConfirmClose] = useState<string | null>(null);

  const active = files.find((f) => f.path === activePath) ?? files[0];
  const dirty = !!active && active.content !== active.saved;
  const anyDirty = files.some((f) => f.content !== f.saved);

  const doSave = (file: OpenFile | undefined = active) => {
    if (!file || file.content === file.saved) return;
    save.mutate(
      { path: file.path, content: file.content },
      {
        onSuccess: () => {
          onSaved(file.path, file.content);
          toast.success(`Saved ${baseName(file.path)}`);
        },
        onError: (e) => toast.error("Save failed", e instanceof ApiRequestError ? e.message : undefined),
      },
    );
  };

  // Ctrl/⌘-S also works when focus is outside the editor (on the tab strip, a
  // button, anywhere in this panel). Inside the editor CodeMirror's own keymap
  // handles it — see CodeEditor — so both paths save and neither reaches the
  // browser's "save page" dialog.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "s") {
        e.preventDefault();
        doSave();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  const tryCloseFile = (path: string) => {
    const f = files.find((x) => x.path === path);
    if (f && f.content !== f.saved) {
      setConfirmClose(path);
      return;
    }
    onCloseFile(path);
  };

  if (!active) return null;

  const diff = showDiff ? diffLines(active.saved, active.content) : null;

  return (
    <div className="space-y-3">
      {/* Tab strip */}
      <div className="flex flex-wrap items-center gap-2">
        <button className="text-sm text-muted hover:text-fg" onClick={() => (anyDirty ? setConfirmClose("*") : onCloseAll())}>
          ← Files
        </button>
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1">
          {files.map((f) => {
            const fDirty = f.content !== f.saved;
            const isActive = f.path === active.path;
            return (
              <div
                key={f.path}
                className={cn(
                  "group inline-flex max-w-full items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-xs transition-colors",
                  isActive ? "border-brand bg-surface text-fg" : "border-border text-muted hover:text-fg",
                )}
              >
                <button className="truncate" title={f.path} onClick={() => onActivate(f.path)}>
                  {baseName(f.path)}
                </button>
                {fDirty && <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-brand" title="unsaved changes" />}
                <button
                  className="shrink-0 rounded px-1 text-muted hover:bg-border/60 hover:text-fg"
                  aria-label={`Close ${baseName(f.path)}`}
                  onClick={() => tryCloseFile(f.path)}
                >
                  ×
                </button>
              </div>
            );
          })}
        </div>
      </div>

      {/* Active file + actions */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <span className="break-all font-mono text-xs text-muted">{active.path}</span>
          {dirty && <Badge>unsaved</Badge>}
        </div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" className="h-9 px-3" disabled={!dirty} onClick={() => setShowDiff(true)}>
            Diff
          </Button>
          <Button className="h-9 px-3" loading={save.isPending} disabled={!dirty} onClick={() => doSave()}>
            Save
          </Button>
        </div>
      </div>

      <div className="h-[65vh]">
        <Suspense
          fallback={
            <div className="flex h-full items-center justify-center rounded-lg border border-border text-muted">
              <Spinner /> <span className="ml-2">Loading editor…</span>
            </div>
          }
        >
          {/* Keyed by path so switching tabs swaps the document cleanly. */}
          <CodeEditor
            key={active.path}
            value={active.content}
            filename={active.path}
            onChange={(next) => onChange(active.path, next)}
            onSave={() => doSave()}
          />
        </Suspense>
      </div>

      <p className="text-xs text-muted">
        <span className="font-medium text-fg">Shortcuts:</span> Ctrl/⌘-S save · Ctrl/⌘-F find · Ctrl/⌘-A select all ·
        Ctrl/⌘-D select next occurrence · Ctrl/⌘-Z / -Y undo &amp; redo · Alt-click for multiple cursors. Edits are
        written as the site's Linux user.
      </p>

      {showDiff && diff && (
        <Modal title={`Changes — ${baseName(active.path)}`} wide onClose={() => setShowDiff(false)}>
          <div className="mb-3 flex items-center gap-3 text-xs">
            <span className="text-emerald-500">+{diff.added} added</span>
            <span className="text-danger">−{diff.removed} removed</span>
          </div>
          {diff.tooLarge ? (
            <p className="text-sm text-muted">
              This change is too large to display line by line ({diff.removed} lines replaced by {diff.added}).
            </p>
          ) : (
            <div className="max-h-[60vh] overflow-auto rounded-lg border border-border">
              <table className="w-full border-collapse font-mono text-xs">
                <tbody>
                  {diff.lines.map((l, i) => (
                    <tr
                      key={i}
                      className={cn(
                        l.kind === "add" && "bg-emerald-500/10",
                        l.kind === "del" && "bg-danger/10",
                      )}
                    >
                      <td className="w-10 select-none border-r border-border px-2 py-0.5 text-right text-muted">
                        {l.a ?? ""}
                      </td>
                      <td className="w-10 select-none border-r border-border px-2 py-0.5 text-right text-muted">
                        {l.b ?? ""}
                      </td>
                      <td
                        className={cn(
                          "w-5 select-none px-1 py-0.5 text-center",
                          l.kind === "add" && "text-emerald-500",
                          l.kind === "del" && "text-danger",
                        )}
                      >
                        {l.kind === "add" ? "+" : l.kind === "del" ? "−" : ""}
                      </td>
                      <td className="whitespace-pre-wrap break-all px-2 py-0.5 text-fg">{l.text || " "}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setShowDiff(false)}>
              Close
            </Button>
            <Button
              loading={save.isPending}
              onClick={() => {
                doSave();
                setShowDiff(false);
              }}
            >
              Save changes
            </Button>
          </div>
        </Modal>
      )}

      {confirmClose && (
        <Modal title="Discard unsaved changes?" onClose={() => setConfirmClose(null)}>
          <p className="text-sm text-muted">
            {confirmClose === "*"
              ? "Some open files have unsaved changes. Close the editor without saving them?"
              : `${baseName(confirmClose)} has unsaved changes. Close it without saving?`}
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setConfirmClose(null)}>
              Keep editing
            </Button>
            <Button
              variant="danger"
              onClick={() => {
                if (confirmClose === "*") onCloseAll();
                else onCloseFile(confirmClose);
                setConfirmClose(null);
              }}
            >
              Discard
            </Button>
          </div>
        </Modal>
      )}
    </div>
  );
}
