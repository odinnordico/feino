# Package `internal/agent`

The `agent` package implements the reasoning engine of FEINO. It provides:

- **`StateMachine`** — the ReAct phase orchestrator
- **`TACOSRouter`** — latency-aware, complexity-tiered model selection
- **`ModelMetrics`** — per-model EWMA latency tracking with Z-score outlier detection
- **`LeadCoordinator`** — parallel multi-agent task dispatch

---

## StateMachine (`machine.go`)

### States and transitions

```
StateInit ──► StateGather ──► StateAct ──► StateVerify ──► StateComplete
                                  ▲              │
                                  └──────────────┘  (retry on tool dispatch)
          Any state ──► StateFailed
```

Transitions are validated at every step. An illegal transition returns an error and moves the machine to `StateFailed`.

### Key API

```go
sm := agent.NewStateMachine()
sm.SetHandlers(gatherFn, actFn, verifyFn) // inject phase logic
sm.SetMaxRetries(5)

// Hooks run synchronously before/after each phase.
sm.OnBefore(func(s ReActState) { /* log */ })
sm.OnAfter(func(s ReActState, err error) { /* metrics */ })

// State change listeners run in goroutines (fire-and-forget).
sm.Subscribe(func(s ReActState) { /* update UI */ })

// Shared state map — phases communicate through this.
sm.SetData("system_prompt", prompt)
val, ok := sm.GetData("system_prompt")

// Run blocks until Complete or Failed.
err := sm.Run(ctx)

// Reset to StateInit for reuse.
sm.Reset()

// Zero the verify-failure counter without changing state.
// Call this inside verifyFn after dispatching tool calls so that
// normal tool iterations don't consume the retry budget.
sm.ResetRetries()
```

### Phase handler contract

```go
type PhaseHandler func(ctx context.Context) error
```

- **Gather** — return `nil` to advance to Act, return `error` to fail.
- **Act** — return `nil` to advance to Verify, return `error` to fail.
- **Verify** — return `nil` to transition to Complete; return `error` to retry Act. After `maxRetries` consecutive Verify errors the machine fails.

---

## TACOSRouter (`tacos.go`)

**Token-Adjusted Latency Outlier Selection** — picks the best provider/model for each inference request.

### Scoring algorithm

```
score = ema_latency_per_token   (base)

if circuit_breaker == HalfOpen:  score += 2000
if circuit_breaker == Open:      skip model entirely

tier1 := model name contains a tier1 keyword (e.g. "opus", "gpt-4o")

if estTokens < LowComplexityThreshold (500):   // speed tier
    if !tier1: score -= 100    // prefer fast/smaller models
elif estTokens >= HighComplexityThreshold (2000):  // intelligence tier
    if tier1:  score -= 1500   // strongly prefer capable models
    else:      score += 500
else:                                           // normal tier
    if tier1:  score -= 300

if currentZScore > 2.0:  score += 5000   // penalise outlier models
```

Models are ranked ascending; the top `MaxRecommendations` (default 6) are returned.

### Key API

```go
router := agent.NewTACOSRouter(estimator,
    agent.WithTACOSLogger(logger),
    agent.WithPersistencePath("/tmp/test-metrics.json"),
)

router.RegisterProvider(anthropicProvider)
router.RegisterProvider(openaiProvider, 0.5) // custom EMA alpha

// Returns ordered candidates; caller picks the first that works.
recs, err := router.SelectOptimalModel(ctx, history)

// Record latency after inference for future routing decisions.
router.RecordUsage(provider, model, usage)

// Dynamic tuning.
router.SetComplexityThresholds(300, 1500)
router.SetTier1Models([]string{"opus", "gpt-4o", "gemini-2.0-flash-thinking"})
```

### Persistence

Metrics are saved to `~/.feino/metrics.json` every 10 `RecordUsage` calls and on explicit `SaveMetrics()`. Entries older than 30 days are garbage-collected on load and save. Use `WithPersistencePath` in tests to prevent touching production metrics.

---

## ModelMetrics (`metrics.go`)

Ring-buffer EWMA with Z-score tracking.

```go
m := agent.NewModelMetrics(50, 0.3) // capacity=50 samples, alpha=0.3
m.AddLatencyPerToken(duration, tokenCount)
mean, stddev := m.Stats()
isOutlier := m.IsOutlier(currentLpt, 2.0)
z := m.CurrentZScore()
```

- `alpha=0.3` means recent observations have 30% weight; adjust upward for more reactive routing.
- `IsOutlier` returns true when the most recent request's Z-score exceeds the threshold.

---

## LeadCoordinator (`coordinator.go`)

Dispatches multiple subordinate agents in parallel with bounded concurrency.

```go
coord := agent.NewLeadCoordinator(
    agent.WithMaxParallel(4),
    agent.WithCoordinatorLogger(logger),
)

tasks := []agent.SubordinateTask{
    {ID: "t1", Objective: "summarise file A", InferFn: inferFnA, MaxRetries: 3},
    {ID: "t2", Objective: "summarise file B", InferFn: inferFnB, MaxRetries: 3},
}

results := coord.Dispatch(ctx, tasks)
for _, r := range results {
    if r.Err != nil { /* handle */ }
    fmt.Println(r.Output)
}
```

Each subordinate gets an isolated `StateMachine` and history; failures in one task do not cancel others.

---

## Best practices

- **Call `sm.ResetRetries()` inside `verifyFn` after dispatching tool calls.** Without this, each tool-call iteration counts against `maxRetries` and the machine fails prematurely.
- **Panic recovery is built in** for state listeners and phase hooks — a panic in a subscriber does not crash the loop.
- **Do not call `sm.Run` concurrently.** The machine is not designed for concurrent execution; wrap it in a goroutine and coordinate via the `Send/Subscribe` pattern at the `Session` layer.
- **Never store `ReActState` strings as magic literals** outside the `agent` package; use the exported constants (`StateGather`, `StateAct`, etc.).

---

## Extending the agent

### Adding a new state

1. Add the constant to `machine.go`: `const StateReview ReActState = "review"`.
2. Add entries in `validTransitions` for every state that should reach it and all states it may reach.
3. Add the `case StateReview:` branch in the `Run` loop.
4. Expose a `SetReviewHandler` setter so callers can inject the phase logic.

### Tuning TACOS routing

| Knob | Effect |
|------|--------|
| `SetComplexityThresholds(low, high)` | Widens or narrows the speed / intelligence tiers |
| `SetTier1Models(names)` | Adjusts which models get tier-based score bonuses |
| `RegisterProvider(p, alpha)` | Per-provider EMA responsiveness |
| `ZScoreOutlierThreshold` (constant) | Raise to tolerate more latency variance |

### Adding a new router strategy

`SelectOptimalModel` calls `rankModels`, which is a pure function over `discoveryResult` slices. Replace or augment `rankModels` to implement alternative strategies (e.g., cost-based routing, round-robin, sticky provider).

---

## File map

| File | Responsibility |
|------|---------------|
| `machine.go` | ReAct state machine |
| `tacos.go` | Model selection router |
| `metrics.go` | Per-model EWMA + Z-score |
| `coordinator.go` | Parallel multi-agent dispatch |
| `*_test.go` | Unit and integration tests |
