import { create } from "zustand";

export type ToastKind = "success" | "error" | "info" | "warning";

export interface Toast {
  id: string;
  kind: ToastKind;
  message: string;
  /** auto-dismiss after ms (default 3500, 0 = never) */
  duration: number;
}

interface ToastState {
  toasts: Toast[];
  push: (kind: ToastKind, message: string, duration?: number) => void;
  dismiss: (id: string) => void;
}

export const useToastStore = create<ToastState>((set) => ({
  toasts: [],

  push: (kind, message, duration = 3500) => {
    const id = crypto.randomUUID();
    set((s) => ({ toasts: [...s.toasts, { id, kind, message, duration }] }));
    if (duration > 0) {
      setTimeout(() => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })), duration);
    }
  },

  dismiss: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}));

/** Convenience accessor — call outside React components. */
export const toast = {
  success: (msg: string, duration?: number) => useToastStore.getState().push("success", msg, duration),
  error:   (msg: string, duration?: number) => useToastStore.getState().push("error",   msg, duration),
  info:    (msg: string, duration?: number) => useToastStore.getState().push("info",    msg, duration),
  warning: (msg: string, duration?: number) => useToastStore.getState().push("warning", msg, duration),
};
