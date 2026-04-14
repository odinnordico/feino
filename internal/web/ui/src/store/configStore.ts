import { create } from "zustand";
import type { ConfigProto } from "../gen/feino/v1/feino_pb";

interface ConfigState {
  config: ConfigProto | null;
  dirty:  boolean;

  setConfig:   (cfg: ConfigProto) => void;
  updateField: <K extends keyof ConfigProto>(key: K, value: ConfigProto[K]) => void;
  markClean:   () => void;
}

export const useConfigStore = create<ConfigState>((set) => ({
  config: null,
  dirty:  false,

  setConfig: (config) => set({ config, dirty: false }),

  updateField: (key, value) =>
    set((s) => ({
      config: s.config ? { ...s.config, [key]: value } : s.config,
      dirty: true,
    })),

  markClean: () => set({ dirty: false }),
}));
