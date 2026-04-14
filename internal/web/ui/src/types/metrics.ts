export interface TokenPoint {
  turn: number;
  prompt: number;
  completion: number;
}

export interface LatencyPoint {
  turn: number;
  ms: number;
}

export interface UsageSummary {
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  durationMs: number;
}
