export type TokenPoint = {
  turn: number;
  prompt: number;
  completion: number;
}

export type LatencyPoint = {
  turn: number;
  ms: number;
}

export type UsageSummary = {
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  durationMs: number;
}
