import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, rawFetch, uploadWithProgress, type FileEntry, type FileListing } from "@/lib/api";

// Data hooks + raw-byte helpers for the File Manager tab. Directory listings and
// metadata operations go through the JSON envelope; file *content* (download,
// editor save, upload) is raw bytes via rawFetch, since base64'ing a file
// through an envelope would be wasteful and the server already streams it in
// chunks under the broker's wire cap.
//
// Paths are always site-relative with no leading slash ("" is the site root).
// joinPath and parentPath keep that invariant so the breadcrumb and navigation
// never accidentally build an absolute path the server would clamp.

export type { FileEntry, FileListing };

export function joinPath(dir: string, name: string): string {
  if (!dir) return name;
  return `${dir.replace(/\/+$/, "")}/${name}`;
}

export function parentPath(path: string): string {
  const i = path.replace(/\/+$/, "").lastIndexOf("/");
  return i < 0 ? "" : path.slice(0, i);
}

export function baseName(path: string): string {
  const p = path.replace(/\/+$/, "");
  const i = p.lastIndexOf("/");
  return i < 0 ? p : p.slice(i + 1);
}

const q = (path: string) => `path=${encodeURIComponent(path)}`;

export function useFileList(uid: string, path: string) {
  return useQuery({
    queryKey: ["site", uid, "files", path],
    queryFn: () => api.get<FileListing>(`/sites/${uid}/files?${q(path)}`),
  });
}

// invalidateFiles refreshes every open listing for the site (a mutation in one
// directory can affect a parent's counts, and it keeps the code simple).
function useInvalidateFiles(uid: string) {
  const qc = useQueryClient();
  return () => qc.invalidateQueries({ queryKey: ["site", uid, "files"] });
}

export function useMkdir(uid: string) {
  const invalidate = useInvalidateFiles(uid);
  return useMutation({
    mutationFn: (path: string) => api.post(`/sites/${uid}/files/mkdir`, { path }),
    onSuccess: invalidate,
  });
}

export function useRename(uid: string) {
  const invalidate = useInvalidateFiles(uid);
  return useMutation({
    mutationFn: (v: { from: string; to: string }) => api.post(`/sites/${uid}/files/rename`, v),
    onSuccess: invalidate,
  });
}

// useTransfer is the paste half of copy/cut/paste. The server refuses to
// overwrite unless on_conflict is "rename", and it answers with the destination
// it actually used — which is not always the one asked for — so the caller can
// select the result afterwards.
export interface TransferResult {
  from: string;
  to: string;
}

export function useTransfer(uid: string) {
  const invalidate = useInvalidateFiles(uid);
  return useMutation({
    mutationFn: (v: { op: "copy" | "move"; from: string; to: string; onConflict?: "fail" | "rename" }) =>
      api.post<TransferResult>(`/sites/${uid}/files/${v.op}`, {
        from: v.from,
        to: v.to,
        on_conflict: v.onConflict ?? "fail",
      }),
    onSuccess: invalidate,
  });
}

export function useChmod(uid: string) {
  const invalidate = useInvalidateFiles(uid);
  return useMutation({
    mutationFn: (v: { path: string; mode: string }) => api.post(`/sites/${uid}/files/chmod`, v),
    onSuccess: invalidate,
  });
}

export function useRemove(uid: string) {
  const invalidate = useInvalidateFiles(uid);
  return useMutation({
    mutationFn: (path: string) => api.del(`/sites/${uid}/files?${q(path)}`),
    onSuccess: invalidate,
  });
}

export function useExtract(uid: string) {
  const invalidate = useInvalidateFiles(uid);
  return useMutation({
    mutationFn: (v: { archive: string; dest: string }) => api.post(`/sites/${uid}/files/extract`, v),
    onSuccess: invalidate,
  });
}

// useSaveFile writes an editor buffer back and refreshes the listing (size/mtime
// change on save). Wrapped as a mutation so it shares the invalidation path.
export function useSaveFile(uid: string) {
  const invalidate = useInvalidateFiles(uid);
  return useMutation({
    mutationFn: (v: { path: string; content: string }) => saveFile(uid, v.path, v.content),
    onSuccess: invalidate,
  });
}

// UploadProgress is what the UI needs to show a real progress bar: which file is
// in flight, how far through the batch it is, and the overall byte fraction.
export interface UploadProgress {
  file: string;
  index: number; // 1-based, within the batch
  count: number; // files in the batch
  loaded: number; // bytes sent across the whole batch
  total: number; // bytes in the whole batch
  percent: number; // 0–100, overall
}

// useUpload streams one or more picked files into a directory, then refreshes.
//
// Progress is byte-accurate across the *batch*, not per file: an operator
// dropping forty images cares how long the whole drop takes, and a bar that
// restarts forty times tells them nothing. It is tracked in state rather than
// returned by the mutation because it changes continuously while one mutation
// is in flight.
export function useUpload(uid: string) {
  const invalidate = useInvalidateFiles(uid);
  const [progress, setProgress] = useState<UploadProgress | null>(null);
  const abort = useRef<AbortController | null>(null);

  const mutation = useMutation({
    mutationFn: async (v: { dir: string; files: File[] }) => {
      const controller = new AbortController();
      abort.current = controller;
      const total = v.files.reduce((n, f) => n + f.size, 0);
      let sent = 0;

      for (const [i, f] of v.files.entries()) {
        const base = sent;
        setProgress({
          file: f.name,
          index: i + 1,
          count: v.files.length,
          loaded: base,
          total,
          percent: total ? Math.round((base / total) * 100) : 100,
        });
        await uploadFile(uid, joinPath(v.dir, f.name), f, {
          signal: controller.signal,
          onProgress: (loaded) =>
            setProgress({
              file: f.name,
              index: i + 1,
              count: v.files.length,
              loaded: base + loaded,
              total,
              percent: total ? Math.round(((base + loaded) / total) * 100) : 100,
            }),
        });
        sent += f.size;
      }
    },
    // Invalidate on settle, not on success: a batch that failed or was cancelled
    // halfway still wrote the files before the failure, and the listing has to
    // show them.
    onSettled: () => {
      setProgress(null);
      abort.current = null;
      invalidate();
    },
  });

  return { ...mutation, progress, cancel: () => abort.current?.abort() };
}

export function useCompress(uid: string) {
  const invalidate = useInvalidateFiles(uid);
  return useMutation({
    mutationFn: (v: { sources: string[]; archive: string; format?: string }) =>
      api.post(`/sites/${uid}/files/compress`, v),
    onSuccess: invalidate,
  });
}

// useFixOwnership resets a path's ownership to the site's Linux user. The target
// account is not a parameter — the server always uses the site's own user.
export function useFixOwnership(uid: string) {
  const invalidate = useInvalidateFiles(uid);
  return useMutation({
    mutationFn: (path: string) => api.post(`/sites/${uid}/files/chown`, { path }),
    onSuccess: invalidate,
  });
}

export interface SearchHit {
  name: string;
  path: string; // relative to the searched directory
  kind: FileEntry["kind"];
  size: number;
}

export interface SearchResults {
  path: string;
  entries: SearchHit[];
  truncated: boolean;
}

// useSearch is a recursive server-side search. It is disabled until a query is
// supplied, so mounting the tab does not walk the tree.
export function useSearch(uid: string, dir: string, query: string, mode: "name" | "content") {
  return useQuery({
    queryKey: ["site", uid, "files-search", dir, query, mode],
    queryFn: () =>
      api.get<SearchResults>(
        `/sites/${uid}/files/search?path=${encodeURIComponent(dir)}&q=${encodeURIComponent(query)}&mode=${mode}`,
      ),
    enabled: query.trim().length > 0,
  });
}

// useGitignore loads every .gitignore governing the directory being browsed —
// the site root's, plus one per ancestor down to `dir` — so the browser can grey
// out build output and vendor folders with git's own precedence. A missing file
// at any level is not an error; most directories do not have one.
//
// Only the chain from the root to `dir` is fetched, which is exactly the set
// that can affect this listing: an ignore file in a sibling subtree governs
// nothing here. See gitignore.ts for the documented pattern subset.
export function useGitignore(uid: string, dir = "") {
  const dirs = ancestorDirs(dir);
  return useQuery({
    queryKey: ["site", uid, "gitignore", dir],
    queryFn: async () => {
      const loaded = await Promise.all(
        dirs.map(async (d) => {
          try {
            return { dir: d, text: await readFileText(uid, joinPath(d, ".gitignore")) };
          } catch {
            return { dir: d, text: "" };
          }
        }),
      );
      return loaded.filter((f) => f.text.trim() !== "");
    },
    staleTime: 60_000,
  });
}

// ancestorDirs lists the site root and every directory down to `dir`, shallowest
// first: "a/b" yields ["", "a", "a/b"].
export function ancestorDirs(dir: string): string[] {
  const out = [""];
  const segments = dir.split("/").filter(Boolean);
  let acc = "";
  for (const s of segments) {
    acc = acc ? `${acc}/${s}` : s;
    out.push(acc);
  }
  return out;
}

// useCreateFile writes an empty file. It reuses the normal write path, so the
// new file is owned by the site user like everything else.
export function useCreateFile(uid: string) {
  const invalidate = useInvalidateFiles(uid);
  return useMutation({
    mutationFn: (path: string) => saveFile(uid, path, ""),
    onSuccess: invalidate,
  });
}

// ── raw content transfer ──────────────────────────────────────────────────────

export async function readFileText(uid: string, path: string): Promise<string> {
  const res = await rawFetch("GET", `/sites/${uid}/files/content?${q(path)}`);
  return res.text();
}

export async function readFileBlob(uid: string, path: string): Promise<Blob> {
  const res = await rawFetch("GET", `/sites/${uid}/files/content?${q(path)}`);
  return res.blob();
}

// saveFile writes an editor buffer back. The body is the exact text; the server
// truncates then writes.
export async function saveFile(uid: string, path: string, content: string): Promise<void> {
  await rawFetch("PUT", `/sites/${uid}/files/content?${q(path)}`, content, "application/octet-stream");
}

// uploadFile streams a picked File to the given destination path (which includes
// the filename). The File is sent as the raw body, through the XHR transport so
// the caller can watch it go out and cancel it.
export async function uploadFile(
  uid: string,
  path: string,
  file: File,
  opts: { onProgress?: (loaded: number, total: number) => void; signal?: AbortSignal } = {},
): Promise<void> {
  await uploadWithProgress(`/sites/${uid}/files/content?${q(path)}`, file, {
    contentType: file.type || "application/octet-stream",
    onProgress: opts.onProgress,
    signal: opts.signal,
  });
}

// downloadFile fetches a file and triggers a browser "Save as" via an object URL.
export async function downloadFile(uid: string, path: string): Promise<void> {
  const blob = await readFileBlob(uid, path);
  saveAs(blob, baseName(path) || "download");
}

// downloadFolder downloads a directory as a .zip. The server builds the archive,
// streams it back, and deletes it — so unlike "compress, then download the file
// you just made", nothing is left behind in the site's tree.
export async function downloadFolder(uid: string, path: string): Promise<void> {
  const res = await rawFetch("GET", `/sites/${uid}/files/archive?${q(path)}`);
  saveAs(await res.blob(), `${baseName(path) || "site"}.zip`);
}

function saveAs(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}
