import { create } from "zustand";
import type { RenderedMessage, PendingPermission } from "../types/chat";

type ChatState = {
  messages: RenderedMessage[];
  busy: boolean;
  agentState: string;
  pendingPermission: PendingPermission | null;
  promptTokens: number;
  completionTokens: number;
  queuePosition: number; // 0 = not queued

  // Actions
  addMessage: (msg: RenderedMessage) => void;
  updateLastAssistantMessage: (updater: (msg: RenderedMessage) => RenderedMessage) => void;
  setBusy: (busy: boolean) => void;
  setAgentState: (state: string) => void;
  setPendingPermission: (perm: PendingPermission | null) => void;
  setUsage: (prompt: number, completion: number) => void;
  setQueuePosition: (pos: number) => void;
  reset: () => void;
}

export const useChatStore = create<ChatState>((set) => ({
  messages: [],
  busy: false,
  agentState: "idle",
  pendingPermission: null,
  promptTokens: 0,
  completionTokens: 0,
  queuePosition: 0,

  addMessage: (msg) =>
    set((s) => ({ messages: [...s.messages, msg] })),

  updateLastAssistantMessage: (updater) =>
    set((s) => {
      const idx = [...s.messages].reverse().findIndex((m) => m.role === "assistant");
      if (idx === -1) {return s;}
      const realIdx = s.messages.length - 1 - idx;
      const updated = [...s.messages];
      updated[realIdx] = updater(updated[realIdx]);
      return { messages: updated };
    }),

  setBusy: (busy) => set({ busy }),
  setAgentState: (agentState) => set({ agentState }),
  setPendingPermission: (pendingPermission) => set({ pendingPermission }),
  setUsage: (promptTokens, completionTokens) => set({ promptTokens, completionTokens }),
  setQueuePosition: (queuePosition) => set({ queuePosition }),

  reset: () =>
    set({
      messages: [],
      busy: false,
      agentState: "idle",
      pendingPermission: null,
      promptTokens: 0,
      completionTokens: 0,
      queuePosition: 0,
    }),
}));
