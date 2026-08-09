import { useEffect, useRef } from "react";
import { EditorState, Compartment, Prec } from "@codemirror/state";
import { keymap } from "@codemirror/view";
import { EditorView, basicSetup } from "codemirror";
import type { Extension } from "@codemirror/state";

// CodeEditor wraps CodeMirror 6. basicSetup already gives line numbers, an undo
// history, bracket matching, and the Ctrl/Cmd-F search panel, so the file
// manager gets find/replace for free. The theme is driven entirely by the
// panel's CSS custom properties (rgb triples that flip under `.dark`), so the
// editor follows the app's light/dark toggle without a second theme system.
//
// CodeMirror's stylesheet is injected through the CSSOM (insertRule), not as an
// inline <style> element, so it works under the app's strict `default-src
// 'self'` CSP without needing `'unsafe-inline'`.

// loadLanguage resolves the CodeMirror grammar for a filename suffix, importing
// it on demand. Each `@codemirror/lang-*` package is a sizeable grammar, and
// statically importing all eight bundled every one into the editor chunk whether
// a session ever opened that file type or not. Dynamic import splits each grammar
// into its own chunk, so opening a `.py` file fetches only the Python grammar and
// the editor's base bundle carries none of them. An unknown suffix resolves to no
// language — plain text with all the editing niceties.
async function loadLanguage(filename: string): Promise<Extension> {
  const ext = filename.toLowerCase().split(".").pop() ?? "";
  switch (ext) {
    case "php":
    case "phtml":
      return (await import("@codemirror/lang-php")).php();
    case "html":
    case "htm":
      return (await import("@codemirror/lang-html")).html();
    case "js":
    case "jsx":
    case "mjs":
    case "cjs":
      return (await import("@codemirror/lang-javascript")).javascript();
    case "ts":
      return (await import("@codemirror/lang-javascript")).javascript({ typescript: true });
    case "tsx":
      return (await import("@codemirror/lang-javascript")).javascript({ typescript: true, jsx: true });
    case "css":
      return (await import("@codemirror/lang-css")).css();
    case "json":
      return (await import("@codemirror/lang-json")).json();
    case "md":
    case "markdown":
      return (await import("@codemirror/lang-markdown")).markdown();
    case "py":
      return (await import("@codemirror/lang-python")).python();
    case "yml":
    case "yaml":
      return (await import("@codemirror/lang-yaml")).yaml();
    default:
      return [];
  }
}

const panelTheme = EditorView.theme({
  "&": {
    color: "rgb(var(--fg))",
    backgroundColor: "rgb(var(--panel))",
    fontSize: "13px",
    height: "100%",
  },
  ".cm-content": {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
    caretColor: "rgb(var(--fg))",
  },
  "&.cm-focused": { outline: "none" },
  ".cm-gutters": {
    backgroundColor: "rgb(var(--surface))",
    color: "rgb(var(--muted))",
    border: "none",
    borderRight: "1px solid rgb(var(--border))",
  },
  ".cm-activeLine": { backgroundColor: "rgb(var(--surface) / 0.5)" },
  ".cm-activeLineGutter": { backgroundColor: "rgb(var(--surface))" },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection": {
    backgroundColor: "rgb(var(--brand) / 0.25)",
  },
  ".cm-cursor, .cm-dropCursor": { borderLeftColor: "rgb(var(--fg))" },
  ".cm-panels": {
    backgroundColor: "rgb(var(--panel))",
    color: "rgb(var(--fg))",
    borderTop: "1px solid rgb(var(--border))",
  },
  ".cm-searchMatch": { backgroundColor: "rgb(var(--brand) / 0.3)" },
  ".cm-searchMatch.cm-searchMatch-selected": { backgroundColor: "rgb(var(--brand) / 0.5)" },
  ".cm-panel input, .cm-panel button": {
    backgroundColor: "rgb(var(--surface))",
    color: "rgb(var(--fg))",
    border: "1px solid rgb(var(--border))",
    borderRadius: "4px",
  },
});

export function CodeEditor({
  value,
  filename,
  onChange,
  onSave,
  readOnly = false,
}: {
  value: string;
  filename: string;
  onChange?: (next: string) => void;
  /** Invoked on Ctrl/⌘-S. Bound inside CodeMirror so it fires with focus in the
   * editor and suppresses the browser's own "save page" dialog. */
  onSave?: () => void;
  readOnly?: boolean;
}) {
  const host = useRef<HTMLDivElement | null>(null);
  const view = useRef<EditorView | null>(null);
  const language = useRef(new Compartment());
  // Monotonic token so a slow grammar import that resolves after the file has
  // already changed (or the editor was torn down) is ignored rather than swapping
  // in a stale language.
  const langReq = useRef(0);
  const applyLanguage = useRef((filename: string) => {
    const token = ++langReq.current;
    void loadLanguage(filename).then((ext) => {
      if (token !== langReq.current || !view.current) return;
      view.current.dispatch({ effects: language.current.reconfigure(ext) });
    });
  });
  // Keep the latest callbacks without re-creating the editor on every render.
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  const onSaveRef = useRef(onSave);
  onSaveRef.current = onSave;

  // Create the editor once. value/filename changes after mount are handled by
  // the effects below rather than by tearing the view down (which would lose the
  // cursor and undo history).
  useEffect(() => {
    if (!host.current) return;
    const state = EditorState.create({
      doc: value,
      extensions: [
        // Highest precedence so Ctrl/⌘-S wins over anything basicSetup binds and
        // never reaches the browser's own save dialog. Ctrl/⌘-A (select all),
        // Ctrl/⌘-F (find), Ctrl/⌘-D (select next occurrence), Ctrl/⌘-Z / -Y
        // (undo/redo) and the rest come from basicSetup's standard keymaps.
        Prec.highest(
          keymap.of([
            {
              key: "Mod-s",
              preventDefault: true,
              run: () => {
                onSaveRef.current?.();
                return true;
              },
            },
          ]),
        ),
        basicSetup,
        // Start with no grammar; the on-demand import swaps the real one in as
        // soon as it resolves (usually within a frame or two of the editor
        // mounting), so the first paint never blocks on a grammar download.
        language.current.of([]),
        panelTheme,
        EditorView.lineWrapping,
        EditorState.readOnly.of(readOnly),
        EditorView.updateListener.of((u) => {
          if (u.docChanged) onChangeRef.current?.(u.state.doc.toString());
        }),
      ],
    });
    const v = new EditorView({ state, parent: host.current });
    view.current = v;
    applyLanguage.current(filename); // load the initial grammar on demand
    return () => {
      v.destroy();
      view.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Reconfigure the language when the open file changes (loaded on demand).
  useEffect(() => {
    if (view.current) applyLanguage.current(filename);
  }, [filename]);

  // If the parent swaps in a different file's contents, replace the document.
  // (A user's own keystrokes flow the other way via onChange, so this only fires
  // when `value` diverges from what the editor already holds.)
  useEffect(() => {
    const v = view.current;
    if (!v) return;
    const current = v.state.doc.toString();
    if (value !== current) {
      v.dispatch({ changes: { from: 0, to: current.length, insert: value } });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value]);

  return <div ref={host} className="h-full overflow-hidden rounded-lg border border-border" />;
}
