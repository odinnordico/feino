package agent

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
)

// ReActState represents the explicit operational phase the agent is currently occupying.
type ReActState string

const (
	StateInit     ReActState = "init"
	StateGather   ReActState = "gather"
	StateAct      ReActState = "act"
	StateVerify   ReActState = "verify"
	StateComplete ReActState = "complete"
	StateFailed   ReActState = "failed"
)

// validTransitions defines the strict lifecycle of a ReAct agent.
var validTransitions = map[ReActState][]ReActState{
	StateInit:     {StateGather, StateFailed},
	StateGather:   {StateAct, StateFailed},
	StateAct:      {StateVerify, StateFailed},
	StateVerify:   {StateAct, StateComplete, StateFailed},
	StateComplete: {},
	StateFailed:   {},
}

// PhaseHandler defines the signature for custom orchestration logic inside phases.
type PhaseHandler func(ctx context.Context) error

// StateMachine drives the rigorous sequence flow of a ReAct framework.
// It supports configuration limits scaling dynamic loop execution properly.
// A StateMachine is not safe to reuse after reaching StateComplete or StateFailed
// without first calling Reset.
type StateMachine struct {
	mu           sync.RWMutex
	logger       *slog.Logger
	maxRetries   int
	currentState ReActState
	lastError    error
	retries      int
	listeners    []func(ReActState)

	// Hooks for phase-specific monitoring or UI feedback
	onBefore []func(ReActState)
	onAfter  []func(ReActState, error)

	// Shared state accessible by all phases
	muData sync.RWMutex
	data   map[string]any

	// External Hooks representing functional loops to be provided by the central system
	gatherFn PhaseHandler
	actFn    PhaseHandler
	verifyFn PhaseHandler
}

// NewStateMachine constructs a default Engine allowing standard loop thresholds.
func NewStateMachine() *StateMachine {
	return &StateMachine{
		logger:       slog.Default(),
		maxRetries:   5,
		currentState: StateInit,
		retries:      0,
		listeners:    make([]func(ReActState), 0),
		onBefore:     make([]func(ReActState), 0),
		onAfter:      make([]func(ReActState, error), 0),
		data:         make(map[string]any),
	}
}

// OnBefore adds a hook to be executed before a phase starts.
func (sm *StateMachine) OnBefore(h func(ReActState)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onBefore = append(sm.onBefore, h)
}

// OnAfter adds a hook to be executed after a phase completes.
func (sm *StateMachine) OnAfter(h func(ReActState, error)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onAfter = append(sm.onAfter, h)
}

// SetData stores a value in the shared state map.
func (sm *StateMachine) SetData(key string, val any) {
	sm.muData.Lock()
	defer sm.muData.Unlock()
	sm.data[key] = val
}

// GetData retrieves a value from the shared state map.
func (sm *StateMachine) GetData(key string) (any, bool) {
	sm.muData.RLock()
	defer sm.muData.RUnlock()
	val, ok := sm.data[key]
	return val, ok
}

// Subscribe adds a listener that is notified whenever a state transition occurs.
func (sm *StateMachine) Subscribe(listener func(ReActState)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.listeners = append(sm.listeners, listener)
}

// GetState returns the current operational state in a thread-safe manner.
func (sm *StateMachine) GetState() ReActState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.currentState
}

// GetMaxRetries returns the current maximum retry limit in a thread-safe manner.
func (sm *StateMachine) GetMaxRetries() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.maxRetries
}

// GetLastError returns the last error recorded by the state machine.
// Only meaningful after Run returns; do not call concurrently with Run.
func (sm *StateMachine) GetLastError() error {
	return sm.lastError
}

// Reset returns the state machine to StateInit so it can be run again.
// It clears lastError and the retry counter.
func (sm *StateMachine) Reset() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.currentState = StateInit
	sm.lastError = nil
	sm.retries = 0
}

// ResetRetries zeroes the verify-failure counter without changing any other
// state. Call this from inside verifyFn before returning a sentinel error that
// signals "keep looping" (e.g. after dispatching tool calls) so that normal
// tool-dispatch iterations are not counted against maxRetries.
func (sm *StateMachine) ResetRetries() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.retries = 0
}

// setState updates the internal state and broadcasts to all subscribers.
// It returns an error if the transition is illegal according to the transition map.
func (sm *StateMachine) setState(state ReActState) error {
	sm.mu.Lock()
	current := sm.currentState

	allowed := validTransitions[current]
	if !slices.Contains(allowed, state) {
		sm.mu.Unlock()
		return fmt.Errorf("illegal transition from %s to %s", current, state)
	}

	sm.currentState = state
	listeners := sm.listeners
	sm.mu.Unlock()

	for _, l := range listeners {
		go func(fn func(ReActState)) {
			defer func() {
				if r := recover(); r != nil {
					sm.logger.Error("state listener panicked", "state", state, "panic", r)
				}
			}()
			fn(state)
		}(l)
	}
	return nil
}

// triggerBefore runs all OnBefore hooks synchronously with panic recovery.
func (sm *StateMachine) triggerBefore(state ReActState) {
	sm.mu.RLock()
	hooks := sm.onBefore
	sm.mu.RUnlock()
	for _, h := range hooks {
		func(fn func(ReActState)) {
			defer func() {
				if r := recover(); r != nil {
					sm.logger.Error("OnBefore hook panicked", "state", state, "panic", r)
				}
			}()
			fn(state)
		}(h)
	}
}

// triggerAfter runs all OnAfter hooks synchronously with panic recovery.
func (sm *StateMachine) triggerAfter(state ReActState, err error) {
	sm.mu.RLock()
	hooks := sm.onAfter
	sm.mu.RUnlock()
	for _, h := range hooks {
		func(fn func(ReActState, error)) {
			defer func() {
				if r := recover(); r != nil {
					sm.logger.Error("OnAfter hook panicked", "state", state, "panic", r)
				}
			}()
			fn(state, err)
		}(h)
	}
}

// SetHandlers maps the external operational commands to the internal logical phases.
func (sm *StateMachine) SetHandlers(gather, act, verify PhaseHandler) {
	sm.gatherFn = gather
	sm.actFn = act
	sm.verifyFn = verify
}

// SetMaxRetries dynamically configures loop bounds for the Verify logic failure fallback.
func (sm *StateMachine) SetMaxRetries(limit int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if limit >= 0 {
		sm.maxRetries = limit
	}
}

// Run executes the core ReAct sequence. It requires the machine to be in
// StateInit; call Reset first if the machine has already completed or failed.
func (sm *StateMachine) Run(ctx context.Context) error {
	if state := sm.GetState(); state != StateInit {
		return fmt.Errorf("StateMachine must be in %s to run (current: %s); call Reset() first", StateInit, state)
	}

	if sm.gatherFn == nil || sm.actFn == nil || sm.verifyFn == nil {
		err := sm.setState(StateFailed)
		if err != nil {
			sm.logger.Error("state machine cannot run without explicit phase handlers defined", "error", err)
		}
		sm.lastError = fmt.Errorf("state machine cannot run without explicit phase handlers defined")
		return sm.lastError
	}

	// Snapshot maxRetries once so concurrent SetMaxRetries calls during Run
	// do not require per-iteration locking.
	sm.mu.RLock()
	maxRetries := sm.maxRetries
	sm.mu.RUnlock()

	sm.logger.Debug("state machine: starting")

	if err := sm.setState(StateGather); err != nil {
		sm.lastError = fmt.Errorf("initial transition failure: %w", err)
		return sm.lastError
	}
	sm.retries = 0

	for {
		currentState := sm.GetState()
		if currentState == StateComplete || currentState == StateFailed {
			break
		}

		if err := ctx.Err(); err != nil {
			sm.logger.Warn("state machine: context cancelled", "state", currentState, "error", err)
			sm.lastError = fmt.Errorf("state machine was interrupted externally: %w", err)
			if serr := sm.setState(StateFailed); serr != nil {
				sm.logger.Error("state machine: failed to set state to failed", "error", serr)
			}
			return sm.lastError
		}

		sm.triggerBefore(currentState)
		sm.logger.Debug("state machine: entering phase", "state", currentState)

		var err error
		switch currentState {
		case StateGather:
			err = sm.gatherFn(ctx)
			sm.triggerAfter(currentState, err)
			if err != nil {
				sm.lastError = fmt.Errorf("fatal failure during Gather phase: %w", err)
				sm.logger.Error("state machine: gather phase failed", "error", sm.lastError)
				if serr := sm.setState(StateFailed); serr != nil {
					sm.logger.Error("state machine: failed to set state to failed", "error", serr)
				}
				return sm.lastError
			}
			sm.logger.Debug("state machine: gather complete")
			if serr := sm.setState(StateAct); serr != nil {
				sm.lastError = serr
				if serr2 := sm.setState(StateFailed); serr2 != nil {
					sm.logger.Error("state machine: failed to set state to failed", "error", serr2)
				}
				return sm.lastError
			}

		case StateAct:
			err = sm.actFn(ctx)
			sm.triggerAfter(currentState, err)
			if err != nil {
				sm.lastError = fmt.Errorf("fatal failure during Act phase: %w", err)
				sm.logger.Error("state machine: act phase failed", "error", sm.lastError)
				if serr := sm.setState(StateFailed); serr != nil {
					sm.logger.Error("state machine: failed to set state to failed", "error", serr)
				}
				return sm.lastError
			}
			sm.logger.Debug("state machine: act complete")
			if serr := sm.setState(StateVerify); serr != nil {
				sm.lastError = serr
				if serr2 := sm.setState(StateFailed); serr2 != nil {
					sm.logger.Error("state machine: failed to set state to failed", "error", serr2)
				}
				return sm.lastError
			}

		case StateVerify:
			err = sm.verifyFn(ctx)
			sm.triggerAfter(currentState, err)
			if err != nil {
				sm.retries++
				if sm.retries >= maxRetries {
					sm.lastError = fmt.Errorf("verify verification bounds exceeded after %d retries. final violation: %w", maxRetries, err)
					sm.logger.Error("state machine: max retries exceeded", "retries", sm.retries, "error", err)
					if serr := sm.setState(StateFailed); serr != nil {
						sm.logger.Error("state machine: failed to set state to failed", "error", serr)
					}
					return sm.lastError
				}
				sm.logger.Warn("state machine: verify failed, retrying act", "retry", sm.retries, "max", maxRetries, "error", err)
				if serr := sm.setState(StateAct); serr != nil {
					sm.lastError = serr
					if serr2 := sm.setState(StateFailed); serr2 != nil {
						sm.logger.Error("state machine: failed to set state to failed", "error", serr2)
					}
					return sm.lastError
				}
			} else {
				sm.logger.Debug("state machine: verify complete")
				if serr := sm.setState(StateComplete); serr != nil {
					sm.lastError = serr
					if serr2 := sm.setState(StateFailed); serr2 != nil {
						sm.logger.Error("state machine: failed to set state to failed", "error", serr2)
					}
					return sm.lastError
				}
			}
		}
	}

	sm.logger.Debug("state machine: finished", "final_state", sm.GetState())
	return nil
}
