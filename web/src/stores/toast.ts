import { create } from "zustand";

// Toast notifications. The store is deliberately tiny and global: any layer —
// a mutation's onError, a WS event, the job drawer — can raise one without
// threading a callback down through props. The <Toaster/> renders whatever is
// here. This is the "notifications" half of the Phase-0 frontend shell.

export type ToastKind = "success" | "error" | "info";

export interface Toast {
  id: number;
  kind: ToastKind;
  title: string;
  detail?: string;
}

interface ToastState {
  toasts: Toast[];
  push: (t: Omit<Toast, "id">) => number;
  dismiss: (id: number) => void;
}

let seq = 1;

export const useToasts = create<ToastState>((set) => ({
  toasts: [],
  push: (t) => {
    const id = seq++;
    set((s) => ({ toasts: [...s.toasts, { ...t, id }] }));
    // Errors stay until dismissed — a failure the user missed is worse than
    // clutter. Everything else auto-clears.
    if (t.kind !== "error") {
      setTimeout(() => set((s) => ({ toasts: s.toasts.filter((x) => x.id !== id) })), 4000);
    }
    return id;
  },
  dismiss: (id) => set((s) => ({ toasts: s.toasts.filter((x) => x.id !== id) })),
}));

// Convenience helpers so callers do not reach into the store shape.
export const toast = {
  success: (title: string, detail?: string) => useToasts.getState().push({ kind: "success", title, detail }),
  error: (title: string, detail?: string) => useToasts.getState().push({ kind: "error", title, detail }),
  info: (title: string, detail?: string) => useToasts.getState().push({ kind: "info", title, detail }),
};
