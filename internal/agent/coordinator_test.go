package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/structs"
)

// helpers

func newUserMessage(text string) model.Message {
	parts := structs.NewLinkedList[model.MessagePart]()
	parts.PushBack(model.NewTextMessagePart(model.MessageRoleUser, text))
	return model.NewMessage(model.WithRole(model.MessageRoleUser), model.WithContent(parts))
}

func fixedInferFn(output string) SubordinateInferFn {
	return func(_ context.Context, _ []model.Message) (string, model.Usage, error) {
		return output, model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}, nil
	}
}

func errorInferFn(err error) SubordinateInferFn {
	return func(_ context.Context, _ []model.Message) (string, model.Usage, error) {
		return "", model.Usage{}, err
	}
}

// TestLeadCoordinator_Dispatch_AllSucceed verifies that every task produces a
// result and that outputs match the expected values.
func TestLeadCoordinator_Dispatch_AllSucceed(t *testing.T) {
	lc := NewLeadCoordinator(WithMaxParallel(3))

	tasks := []SubordinateTask{
		{ID: "t1", Objective: "task 1", InferFn: fixedInferFn("result-1")},
		{ID: "t2", Objective: "task 2", InferFn: fixedInferFn("result-2")},
		{ID: "t3", Objective: "task 3", InferFn: fixedInferFn("result-3")},
	}

	results := lc.Dispatch(context.Background(), tasks)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	byID := make(map[string]SubordinateResult, len(results))
	for _, r := range results {
		byID[r.TaskID] = r
	}

	for _, id := range []string{"t1", "t2", "t3"} {
		r, ok := byID[id]
		if !ok {
			t.Errorf("missing result for task %s", id)
			continue
		}
		if r.Err != nil {
			t.Errorf("task %s: unexpected error: %v", id, r.Err)
		}
		expected := "result-" + strings.TrimPrefix(id, "t")
		if r.Output != expected {
			t.Errorf("task %s: expected output %q, got %q", id, expected, r.Output)
		}
		if r.Usage.TotalTokens != 15 {
			t.Errorf("task %s: expected 15 total tokens, got %d", id, r.Usage.TotalTokens)
		}
	}
}

// TestLeadCoordinator_Dispatch_EmptyTasks verifies a nil return for empty input.
func TestLeadCoordinator_Dispatch_EmptyTasks(t *testing.T) {
	lc := NewLeadCoordinator()
	results := lc.Dispatch(context.Background(), nil)
	if results != nil {
		t.Errorf("expected nil results for empty task slice, got %v", results)
	}
}

// TestLeadCoordinator_Dispatch_PartialFailure verifies that a failing task does
// not block or corrupt the results of succeeding tasks.
func TestLeadCoordinator_Dispatch_PartialFailure(t *testing.T) {
	lc := NewLeadCoordinator(WithMaxParallel(4))

	tasks := []SubordinateTask{
		{ID: "ok1", Objective: "succeed", InferFn: fixedInferFn("good"), MaxRetries: 1},
		{ID: "bad", Objective: "fail", InferFn: errorInferFn(errors.New("provider down")), MaxRetries: 1},
		{ID: "ok2", Objective: "succeed too", InferFn: fixedInferFn("also good"), MaxRetries: 1},
	}

	results := lc.Dispatch(context.Background(), tasks)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	byID := make(map[string]SubordinateResult)
	for _, r := range results {
		byID[r.TaskID] = r
	}

	if byID["ok1"].Err != nil {
		t.Errorf("ok1: unexpected error: %v", byID["ok1"].Err)
	}
	if byID["ok2"].Err != nil {
		t.Errorf("ok2: unexpected error: %v", byID["ok2"].Err)
	}
	if byID["bad"].Err == nil {
		t.Errorf("bad: expected error, got nil")
	}
}

// TestLeadCoordinator_Dispatch_MaxParallelism verifies that no more than
// MaxParallel subordinates execute simultaneously.
func TestLeadCoordinator_Dispatch_MaxParallelism(t *testing.T) {
	const maxParallel = 2
	const numTasks = 6

	lc := NewLeadCoordinator(WithMaxParallel(maxParallel))

	var (
		mu             sync.Mutex
		concurrent     int
		peakConcurrent int
	)

	// Each task blocks briefly so we can observe concurrent execution.
	blockingInferFn := func(_ context.Context, _ []model.Message) (string, model.Usage, error) {
		mu.Lock()
		concurrent++
		if concurrent > peakConcurrent {
			peakConcurrent = concurrent
		}
		mu.Unlock()

		time.Sleep(20 * time.Millisecond)

		mu.Lock()
		concurrent--
		mu.Unlock()

		return "done", model.Usage{}, nil
	}

	tasks := make([]SubordinateTask, numTasks)
	for i := range numTasks {
		tasks[i] = SubordinateTask{
			ID:        fmt.Sprintf("t%d", i),
			Objective: "work",
			InferFn:   blockingInferFn,
		}
	}

	results := lc.Dispatch(context.Background(), tasks)

	if len(results) != numTasks {
		t.Fatalf("expected %d results, got %d", numTasks, len(results))
	}

	mu.Lock()
	peak := peakConcurrent
	mu.Unlock()

	if peak > maxParallel {
		t.Errorf("peak concurrent subordinates %d exceeded MaxParallel %d", peak, maxParallel)
	}
	if peak == 0 {
		t.Error("no tasks appeared to run concurrently at all")
	}
}

// TestLeadCoordinator_Dispatch_ContextCancellation verifies that tasks waiting
// at the semaphore are unblocked immediately when the parent context is cancelled.
func TestLeadCoordinator_Dispatch_ContextCancellation(t *testing.T) {
	// MaxParallel=1 so that only one task runs at a time; the rest queue at
	// the semaphore. Cancelling the context should flush the queue immediately.
	lc := NewLeadCoordinator(WithMaxParallel(1))

	ctx, cancel := context.WithCancel(context.Background())

	started := make(chan struct{})
	blockingInferFn := func(innerCtx context.Context, _ []model.Message) (string, model.Usage, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		// Hold the semaphore until the context is cancelled.
		<-innerCtx.Done()
		return "", model.Usage{}, innerCtx.Err()
	}

	const numTasks = 4
	tasks := make([]SubordinateTask, numTasks)
	for i := range numTasks {
		tasks[i] = SubordinateTask{
			ID:         fmt.Sprintf("t%d", i),
			Objective:  "block",
			InferFn:    blockingInferFn,
			MaxRetries: 1,
		}
	}

	done := make(chan []SubordinateResult, 1)
	go func() {
		done <- lc.Dispatch(ctx, tasks)
	}()

	// Wait for at least one task to start, then cancel.
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first task to start")
	}
	cancel()

	select {
	case results := <-done:
		if len(results) != numTasks {
			t.Errorf("expected %d results (one per task), got %d", numTasks, len(results))
		}
		errCount := 0
		for _, r := range results {
			if r.Err != nil {
				errCount++
			}
		}
		if errCount == 0 {
			t.Error("expected at least one cancelled/error result")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Dispatch did not return after context cancellation")
	}
}

// TestLeadCoordinator_Dispatch_IsolatedContextWindows verifies that each
// subordinate's InferFn receives only its own history — seeds from other tasks
// do not bleed through.
func TestLeadCoordinator_Dispatch_IsolatedContextWindows(t *testing.T) {
	lc := NewLeadCoordinator(WithMaxParallel(2))

	// Each task seeds a unique message in InitHistory. The InferFn inspects the
	// received history and checks that no other task's seed message is present.
	makeIsolatedTask := func(id, seed, objective string) SubordinateTask {
		return SubordinateTask{
			ID:          id,
			Objective:   objective,
			InitHistory: []model.Message{newUserMessage(seed)},
			InferFn: func(_ context.Context, history []model.Message) (string, model.Usage, error) {
				// Flatten all history text.
				var sb strings.Builder
				for _, msg := range history {
					sb.WriteString(msg.GetTextContent())
				}
				fullText := sb.String()

				// The seed for this task must be present.
				if !strings.Contains(fullText, seed) {
					return "", model.Usage{}, fmt.Errorf("task %s: own seed %q not found in history", id, seed)
				}

				// No other task's seed should appear.
				for _, otherSeed := range []string{"seed-A", "seed-B", "seed-C"} {
					if otherSeed != seed && strings.Contains(fullText, otherSeed) {
						return "", model.Usage{}, fmt.Errorf("task %s: foreign seed %q leaked into history", id, otherSeed)
					}
				}
				return "isolated-ok", model.Usage{}, nil
			},
		}
	}

	tasks := []SubordinateTask{
		makeIsolatedTask("tA", "seed-A", "objective-A"),
		makeIsolatedTask("tB", "seed-B", "objective-B"),
		makeIsolatedTask("tC", "seed-C", "objective-C"),
	}

	results := lc.Dispatch(context.Background(), tasks)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for _, r := range results {
		if r.Err != nil {
			t.Errorf("task %s: isolation failure: %v", r.TaskID, r.Err)
		}
	}
}

// TestSubordinate_EmptyOutputRetried verifies that an empty response from
// InferFn triggers the verify→act retry loop up to MaxRetries times.
func TestSubordinate_EmptyOutputRetried(t *testing.T) {
	const maxRetries = 3
	var calls atomic.Int32

	inferFn := func(_ context.Context, _ []model.Message) (string, model.Usage, error) {
		n := calls.Add(1)
		// Return empty until the last allowed attempt.
		if int(n) < maxRetries {
			return "", model.Usage{}, nil
		}
		return "finally-non-empty", model.Usage{}, nil
	}

	task := SubordinateTask{
		ID:         "retry-task",
		Objective:  "keep trying",
		InferFn:    inferFn,
		MaxRetries: maxRetries,
	}

	sub := newSubordinate(task, slog.Default())
	result := sub.run(context.Background())

	if result.Err != nil {
		t.Fatalf("expected success after retries, got: %v", result.Err)
	}
	if result.Output != "finally-non-empty" {
		t.Errorf("expected final output, got %q", result.Output)
	}
	if int(calls.Load()) != maxRetries {
		t.Errorf("expected %d inference calls, got %d", maxRetries, calls.Load())
	}
}

// TestSubordinate_ExhaustedRetries verifies StateFailed is reached when every
// attempt returns empty output.
func TestSubordinate_ExhaustedRetries(t *testing.T) {
	var calls atomic.Int32

	task := SubordinateTask{
		ID:        "empty-task",
		Objective: "always empty",
		InferFn: func(_ context.Context, _ []model.Message) (string, model.Usage, error) {
			calls.Add(1)
			return "", model.Usage{}, nil
		},
		MaxRetries: 2,
	}

	sub := newSubordinate(task, slog.Default())
	result := sub.run(context.Background())

	if result.Err == nil {
		t.Fatal("expected error when retries exhausted, got nil")
	}
	if sub.machine.GetState() != StateFailed {
		t.Errorf("expected StateFailed, got %s", sub.machine.GetState())
	}
}

// TestSubordinate_EmptyObjectiveRejected verifies that a blank objective is
// rejected in the gather phase before any inference call is made.
func TestSubordinate_EmptyObjectiveRejected(t *testing.T) {
	called := false
	task := SubordinateTask{
		ID:        "no-objective",
		Objective: "   ",
		InferFn: func(_ context.Context, _ []model.Message) (string, model.Usage, error) {
			called = true
			return "output", model.Usage{}, nil
		},
	}

	sub := newSubordinate(task, slog.Default())
	result := sub.run(context.Background())

	if result.Err == nil {
		t.Fatal("expected error for empty objective, got nil")
	}
	if called {
		t.Error("InferFn should not be called when objective is empty")
	}
}

// TestLeadCoordinator_Dispatch_UsageAggregation verifies that Usage fields are
// accurately carried through from InferFn to the final SubordinateResult.
func TestLeadCoordinator_Dispatch_UsageAggregation(t *testing.T) {
	lc := NewLeadCoordinator()

	expectedUsage := model.Usage{
		PromptTokens:     42,
		CompletionTokens: 17,
		TotalTokens:      59,
		Duration:         150 * time.Millisecond,
	}

	tasks := []SubordinateTask{
		{
			ID:        "usage-task",
			Objective: "measure usage",
			InferFn: func(_ context.Context, _ []model.Message) (string, model.Usage, error) {
				return "output", expectedUsage, nil
			},
		},
	}

	results := lc.Dispatch(context.Background(), tasks)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	got := results[0].Usage
	if got.PromptTokens != expectedUsage.PromptTokens ||
		got.CompletionTokens != expectedUsage.CompletionTokens ||
		got.TotalTokens != expectedUsage.TotalTokens ||
		got.Duration != expectedUsage.Duration {
		t.Errorf("usage mismatch: expected %+v, got %+v", expectedUsage, got)
	}
}
