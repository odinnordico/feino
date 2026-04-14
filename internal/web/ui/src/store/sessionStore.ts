import { create } from "zustand";
import type { Theme } from "../types/config";

interface SessionState {
  theme:          Theme;
  language:       string;
  bypassActive:   boolean;
  bypassExpiry:   number | null;
  bypassSession:  boolean;
  metricsOpen:    boolean;
  modelName:      string;
  offline:        boolean;

  setTheme:      (theme: Theme) => void;
  setLanguage:   (lang: string) => void;
  setBypass:     (active: boolean, expiry: number | null, session: boolean) => void;
  clearBypass:   () => void;
  toggleMetrics: () => void;
  setModelName:  (model: string) => void;
  setOffline:    (v: boolean) => void;
}

export const useSessionStore = create<SessionState>((set) => ({
  theme:         "dark",
  language:      "en",
  bypassActive:  false,
  bypassExpiry:  null,
  bypassSession: false,
  metricsOpen:   false,
  modelName:     "",
  offline:       false,

  setTheme:    (theme)   => set({ theme }),
  setLanguage: (language) => set({ language }),

  setBypass: (bypassActive, bypassExpiry, bypassSession) =>
    set({ bypassActive, bypassExpiry, bypassSession }),

  clearBypass: () =>
    set({ bypassActive: false, bypassExpiry: null, bypassSession: false }),

  toggleMetrics: () => set((s) => ({ metricsOpen: !s.metricsOpen })),

  setModelName: (modelName) => set({ modelName }),
  setOffline:   (offline)   => set({ offline }),
}));
