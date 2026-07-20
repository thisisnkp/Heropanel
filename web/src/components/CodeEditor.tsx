import { useEffect, useRef } from "react";
import { EditorState, Compartment, Prec } from "@codemirror/state";
import { keymap } from "@codemirror/view";
import { EditorView, basicSetup } from "codemirror";
import { php } from "@codemirror/lang-php";
import { html } from "@codemirror/lang-html";
import { javascript } from "@codemirror/lang-javascript";
import { css } from "@codemirror/lang-css";
import { json } from "@codemirror/lang-json";
import { markdown } from "@codemirror/lang-markdown";
import { python } from "@codemirror/lang-python";
import { yaml } from "@codemirror/lang-yaml";
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

// languageFor picks a language extension from the filename suffix. Unknown
// suffixes get no language (plain text with all the editing niceties).
function languageFor(filename: string): Extension {
  const ext = filename.toLowerCase().split(".").pop() ?? "";
  switch (ext) {
    case "php":
    case "phtml":
      return php();
    case "html":
    case "htm":
      return html();
    case "js":
    case "jsx":
    case "mjs":
    case "cjs":
      return javascript();
    case "ts":
    case "tsx":
      return javascript({ typescript: true, jsx: ext === "tsx" });
    case "css":
      return css();
    case "json":
      return json();
    case "md":
    case "markdown":
      return markdown();
    case "py":
      return python();
    case "yml":
    case "yaml":
      return yaml();
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
        language.current.of(languageFor(filename)),
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
    return () => {
      v.destroy();
      view.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Reconfigure the language when the open file changes.
  useEffect(() => {
    view.current?.dispatch({ effects: language.current.reconfigure(languageFor(filename)) });
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
