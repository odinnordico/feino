import { create } from "zustand";
import type { LatencyPoint, TokenPoint, UsageSummary } from "../types/metrics";

const MAX_LATENCY = 20;
const MAX_TOKENS  = 10;

type MetricsState = {
  latencyHistory: LatencyPoint[];
  tokenHistory:   TokenPoint[];
  currentUsage:   UsageSummary | null;
  turnCounter:    number;

  pushLatency: (ms: number) => void;
  pushTokens:  (prompt: number, completion: number, durationMs: number) => void;
  reset:       () => void;
}

export const useMetricsStore = create<MetricsState>((set) => ({
  latencyHistory: [],
  tokenHistory:   [],
  currentUsage:   null,
  turnCounter:    0,

  pushLatency: (ms) =>
    set((s) => {
      const turn = s.turnCounter + 1;
      const history = [...s.latencyHistory, { turn, ms }].slice(-MAX_LATENCY);
      return { latencyHistory: history, turnCounter: turn };
    }),

  pushTokens: (prompt, completion, durationMs) =>
    set((s) => {
      const turn = s.tokenHistory.length > 0
        ? s.tokenHistory[s.tokenHistory.length - 1].turn + 1
        : s.turnCounter;
      const history = [...s.tokenHistory, { turn, prompt, completion }].slice(-MAX_TOKENS);
      return {
        tokenHistory: history,
        currentUsage: {
          promptTokens:     prompt,
          completionTokens: completion,
          totalTokens:      prompt + completion,
          durationMs,
        },
      };
    }),

  reset: () => set({ latencyHistory: [], tokenHistory: [], currentUsage: null, turnCounter: 0 }),
}));
