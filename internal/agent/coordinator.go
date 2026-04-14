package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/structs"
)

// SubordinateInferFn is the provider-agnostic inference hook injected into each
// subordinate at dispatch time. The caller is responsible for binding the chosen
// model (e.g. via TACOS routing) before passing this function. The function
// receives the subordinate's fully isolated history and must not retain a
// reference to it after returning.
type SubordinateInferFn func(ctx context.Context, history []model.Message) (string, model.Usage, error)

// SubordinateTask is the unit of work dispatched to an isolated subagent.
// InitHistory is copied per subordinate so mutations in one task can never
// affect the context window of another.
type SubordinateTask struct {
	// ID uniquely identifies this task within a dispatch call.
	ID string

	// Objective is appended as a user message at the start of the gather phase.
	Objective string

	// InitHistory seeds the subordinate's context window. Every element is
	// treated as read-only; the subordinate prepends its own copy.
	InitHistory []model.Message

	// InferFn is called during the act phase with the subordinate's local history.
	InferFn SubordinateInferFn

	// MaxRetries caps how many times the verify→act loop may repeat before the
	// subordinate is considered failed. Zero uses the StateMachine default (5).
	MaxRetries int
}

// SubordinateResult carries the outcome of a single completed subordinate.
// Err is non-nil when the subordinate's StateMachine reached StateFailed.
type SubordinateResult struct {
	TaskID string
	Output string
	Usage  model.Usage
	Err    error
}

// subordinate is a self-contained agent instance with a strictly isolated
// context window. It owns its history slice — no pointer to it is ever shared
// with the LeadCoordinator or with other subordinates.
type subordinate struct {
	id      string
	task    SubordinateTask
	machine *StateMachine
	history []model.Message // local, never shared across goroutine boundaries
	output  string
	usage   model.Usage
	logger  *slog.Logger
}

// newSubordinate constructs an isolated subordinate and wires its ReAct phases.
// InitHistory is shallow-copied so that appending to the local slice is safe
// without affecting the caller's slice. The Message interface values themselves
// are immutable (no mutation methods), so sharing the underlying pointers is safe.
func newSubordinate(task SubordinateTask, logger *slog.Logger) *subordinate {
	maxRetries := task.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	sm := NewStateMachine()
	sm.SetMaxRetries(maxRetries)

	localHistory := make([]model.Message, len(task.InitHistory))
	copy(localHistory, task.InitHistory)

	s := &subordinate{
		id:      task.ID,
		task:    task,
		machine: sm,
		history: localHistory,
		logger:  logger.With("subordinate_id", task.ID),
	}

	sm.SetHandlers(s.gather, s.act, s.verify)
	return s
}

// gather appends the task objective as a user message to the local history,
// forming the initial context window for this subordinate's act phase.
func (s *subordinate) gather(ctx context.Context) error {
	if strings.TrimSpace(s.task.Objective) == "" {
		return fmt.Errorf("subordinate %s: objective must not be empty", s.id)
	}
	parts := structs.NewLinkedList[model.MessagePart]()
	parts.PushBack(model.NewTextMessagePart(model.MessageRoleUser, s.task.Objective))
	s.history = append(s.history, model.NewMessage(
		model.WithRole(model.MessageRoleUser),
		model.WithContent(parts),
	))
	return nil
}

// act calls the injected InferFn with the subordinate's isolated history.
// The assistant's response is appended to the local history so that the verify
// phase (and any retry loop) has full context.
func (s *subordinate) act(ctx context.Context) error {
	output, usage, err := s.task.InferFn(ctx, s.history)
	if err != nil {
		return fmt.Errorf("subordinate %s: inference failed: %w", s.id, err)
	}

	s.output = output
	s.usage = usage

	// Append the assistant's response to the local context window so that
	// subsequent verify→act retries carry the prior exchange.
	parts := structs.NewLinkedList[model.MessagePart]()
	parts.PushBack(model.NewTextMessagePart(model.MessageRoleAssistant, output))
	s.history = append(s.history, model.NewMessage(
		model.WithRole(model.MessageRoleAssistant),
		model.WithContent(parts),
	))

	return nil
}

// verify checks that the act phase produced non-empty output. A blank response
// is treated as a retryable failure, allowing the StateMachine to loop back to
// act up to MaxRetries times.
func (s *subordinate) verify(_ context.Context) error {
	if strings.TrimSpace(s.output) == "" {
		return fmt.Errorf("subordinate %s: produced empty output", s.id)
	}
	return nil
}

// run executes the subordinate's ReAct loop and returns the result.
// It is safe to call from any goroutine; all state is local.
func (s *subordinate) run(ctx context.Context) SubordinateResult {
	if err := s.machine.Run(ctx); err != nil {
		return SubordinateResult{TaskID: s.id, Output: s.output, Usage: s.usage, Err: err}
	}
	return SubordinateResult{TaskID: s.id, Output: s.output, Usage: s.usage}
}

// CoordinatorOption configures a LeadCoordinator.
type CoordinatorOption func(*LeadCoordinator)

// WithMaxParallel sets the maximum number of subordinates that may execute
// concurrently. Values less than 1 are ignored; the default is 4.
func WithMaxParallel(n int) CoordinatorOption {
	return func(lc *LeadCoordinator) {
		if n >= 1 {
			lc.maxParallel = n
		}
	}
}

// WithCoordinatorLogger sets the logger propagated to every subordinate.
func WithCoordinatorLogger(logger *slog.Logger) CoordinatorOption {
	return func(lc *LeadCoordinator) {
		lc.logger = logger
	}
}

// LeadCoordinator is the primary coordinator that dispatches independent
// subagents with strictly isolated context windows and bounded concurrency.
//
// The coordinator is stateless between calls to Dispatch — it holds no mutable
// per-dispatch state, so multiple Dispatch calls may run concurrently without
// interfering with one another.
type LeadCoordinator struct {
	logger      *slog.Logger
	maxParallel int
}

// NewLeadCoordinator constructs a LeadCoordinator with the given options.
func NewLeadCoordinator(opts ...CoordinatorOption) *LeadCoordinator {
	lc := &LeadCoordinator{
		logger:      slog.Default(),
		maxParallel: 4,
	}
	for _, opt := range opts {
		opt(lc)
	}
	return lc
}

// Dispatch runs all tasks concurrently, respecting the MaxParallel cap, and
// returns a slice of SubordinateResult — one per task, in completion order.
//
// Each subordinate receives a child context derived from ctx so that individual
// tasks may be cancelled independently if the parent is cancelled. If ctx is
// cancelled while tasks are queued at the semaphore, those tasks return
// immediately with ctx.Err() as their error.
//
// Dispatch blocks until every task has either completed or been cancelled.
// Results are returned in the same order as the input tasks slice.
func (lc *LeadCoordinator) Dispatch(ctx context.Context, tasks []SubordinateTask) []SubordinateResult {
	if len(tasks) == 0 {
		return nil
	}

	lc.logger.Info("coordinator: dispatching tasks", "count", len(tasks), "max_parallel", lc.maxParallel)

	// Pre-allocate by task index so each goroutine writes to its own slot —
	// no channel needed and results are always in input order.
	results := make([]SubordinateResult, len(tasks))

	// sem is a counting semaphore: acquiring a slot (send) gates execution;
	// releasing (receive) opens a slot for the next waiting goroutine.
	sem := make(chan struct{}, lc.maxParallel)

	var wg sync.WaitGroup
	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t SubordinateTask) {
			defer wg.Done()

			// Gate on semaphore or context cancellation.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				lc.logger.Warn("coordinator: task cancelled while waiting for slot", "task_id", t.ID, "error", ctx.Err())
				results[idx] = SubordinateResult{TaskID: t.ID, Err: ctx.Err()}
				return
			}
			defer func() { <-sem }()

			// Each subordinate's context is independently cancellable so that
			// one failing subordinate cannot tear down its siblings.
			subCtx, cancel := context.WithCancel(ctx)
			defer cancel()

			lc.logger.Debug("coordinator: starting subordinate", "task_id", t.ID)
			sub := newSubordinate(t, lc.logger)
			result := sub.run(subCtx)
			if result.Err != nil {
				lc.logger.Error("coordinator: subordinate failed", "task_id", t.ID, "error", result.Err)
			} else {
				lc.logger.Debug("coordinator: subordinate completed", "task_id", t.ID,
					"prompt_tokens", result.Usage.PromptTokens,
					"completion_tokens", result.Usage.CompletionTokens,
				)
			}
			results[idx] = result
		}(i, task)
	}

	wg.Wait()

	var failed int
	for _, r := range results {
		if r.Err != nil {
			failed++
		}
	}
	lc.logger.Info("coordinator: all tasks complete", "total", len(results), "failed", failed)

	return results
}
