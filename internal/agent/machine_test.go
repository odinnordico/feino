package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStateMachine_SuccessfulExecutionFlow(t *testing.T) {
	sm := NewStateMachine()

	gatherHits := 0
	actHits := 0
	verifyHits := 0

	sm.SetHandlers(
		func(ctx context.Context) error { gatherHits++; return nil },
		func(ctx context.Context) error { actHits++; return nil },
		func(ctx context.Context) error { verifyHits++; return nil },
	)

	err := sm.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected sequential run error: %v", err)
	}

	if sm.GetState() != StateComplete {
		t.Errorf("Expected state to be 'complete', got %s", sm.GetState())
	}

	if gatherHits != 1 || actHits != 1 || verifyHits != 1 {
		t.Errorf("Handlers execution counts incorrect. G: %d, A: %d, V: %d", gatherHits, actHits, verifyHits)
	}
}

func TestStateMachine_VerifyFailureRetryLoop(t *testing.T) {
	sm := NewStateMachine()
	sm.SetMaxRetries(3)

	actHits := 0
	verifyHits := 0

	sm.SetHandlers(
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { actHits++; return nil },
		func(ctx context.Context) error {
			verifyHits++
			if verifyHits < 2 {
				return errors.New("temporary verification failure")
			}
			return nil
		},
	)

	err := sm.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected sequential run error, should have succeeded on 2nd verify: %v", err)
	}

	if sm.GetState() != StateComplete {
		t.Errorf("Expected state to be 'complete', got %s", sm.GetState())
	}

	if actHits != 2 || verifyHits != 2 {
		t.Errorf("Loop reversion failed bounds. A: %d, V: %d", actHits, verifyHits)
	}
}

func TestStateMachine_MaxRetriesBreach(t *testing.T) {
	sm := NewStateMachine()
	sm.SetMaxRetries(4)

	verifyHits := 0

	sm.SetHandlers(
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error {
			verifyHits++
			return errors.New("hard verification failure")
		},
	)

	err := sm.Run(context.Background())
	if err == nil {
		t.Fatalf("expected fatal sequence error when breaching limits but succeeded")
	}

	if sm.GetState() != StateFailed {
		t.Errorf("Expected state to be explicitly failed, got %s", sm.GetState())
	}

	if verifyHits != 4 {
		t.Errorf("Verify bounds constraint error. Expected 4 attempts, got %d", verifyHits)
	}
}

func TestStateMachine_Subscribe(t *testing.T) {
	sm := NewStateMachine()

	var mu sync.Mutex
	states := make([]ReActState, 0)

	// The happy path produces exactly 4 transitions: gather, act, verify, complete.
	var wg sync.WaitGroup
	wg.Add(4)

	sm.Subscribe(func(s ReActState) {
		mu.Lock()
		states = append(states, s)
		mu.Unlock()
		wg.Done()
	})

	sm.SetHandlers(
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return nil },
	)

	_ = sm.Run(context.Background())

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for state broadcasts")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(states) < 4 {
		t.Errorf("Expected at least 4 state transitions, got %d: %v", len(states), states)
	}
}

func TestStateMachine_TransitionGuard(t *testing.T) {
	sm := NewStateMachine()

	err := sm.setState(StateComplete)
	if err == nil {
		t.Errorf("Expected error for illegal transition from Init to Complete, got nil")
	}

	if !strings.Contains(err.Error(), "illegal transition") {
		t.Errorf("Expected illegal transition error message, got: %v", err)
	}
}

func TestStateMachine_SharedData(t *testing.T) {
	sm := NewStateMachine()

	sm.SetHandlers(
		func(ctx context.Context) error {
			sm.SetData("key", "value")
			return nil
		},
		func(ctx context.Context) error {
			val, _ := sm.GetData("key")
			if val != "value" {
				return errors.New("missing shared data")
			}
			return nil
		},
		func(ctx context.Context) error { return nil },
	)

	err := sm.Run(context.Background())
	if err != nil {
		t.Fatalf("failed to pass shared data: %v", err)
	}
}

func TestStateMachine_Hooks(t *testing.T) {
	sm := NewStateMachine()

	beforeCalls := 0
	afterCalls := 0

	sm.OnBefore(func(s ReActState) {
		beforeCalls++
	})
	sm.OnAfter(func(s ReActState, err error) {
		afterCalls++
	})

	sm.SetHandlers(
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return nil },
	)

	_ = sm.Run(context.Background())

	if beforeCalls != 3 {
		t.Errorf("Expected 3 before calls, got %d", beforeCalls)
	}
	if afterCalls != 3 {
		t.Errorf("Expected 3 after calls, got %d", afterCalls)
	}
}

func TestStateMachine_Reset(t *testing.T) {
	sm := NewStateMachine()
	sm.SetHandlers(
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return nil },
	)

	if err := sm.Run(context.Background()); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if sm.GetState() != StateComplete {
		t.Fatalf("expected complete after first run, got %s", sm.GetState())
	}

	// Second run without reset must be rejected.
	if err := sm.Run(context.Background()); err == nil {
		t.Fatal("expected error running from non-init state, got nil")
	}

	sm.Reset()
	if sm.GetState() != StateInit {
		t.Fatalf("expected init after reset, got %s", sm.GetState())
	}

	// Second run after reset must succeed.
	if err := sm.Run(context.Background()); err != nil {
		t.Fatalf("second run after reset failed: %v", err)
	}
}
