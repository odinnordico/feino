package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/odinnordico/feino/internal/agent"
	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/provider"
	"github.com/odinnordico/feino/internal/security"
	"github.com/odinnordico/feino/internal/structs"
)

// ── Test doubles ─────────────────────────────────────────────────────────────

// stubModel implements model.Model. When release is non-nil, Infer blocks
// until the channel is closed (or the context is cancelled), enabling
// ErrBusy and cancellation tests without sleeps.
type stubModel struct {
	name    string
	text    string
	release chan struct{}
}

func (m *stubModel) GetID() string           { return m.name }
func (m *stubModel) GetName() string         { return m.name }
func (m *stubModel) GetDescription() string  { return "stub" }
func (m *stubModel) GetHomepage() string     { return "" }
func (m *stubModel) GetLogger() *slog.Logger { return slog.Default() }
func (m *stubModel) GetContextWindow() int   { return 128000 }
func (m *stubModel) GetMaxOutputTokens() int { return 4096 }
func (m *stubModel) SupportsTools() bool     { return false }
func (m *stubModel) Infer(ctx context.Context, _ []model.Message, _ model.InferOptions, onPart func(model.MessagePart)) (model.Message, model.Usage, error) {
	if m.release != nil {
		select {
		case <-m.release:
		case <-ctx.Done():
		}
	}
	part := model.NewTextMessagePart(model.MessageRoleAssistant, m.text)
	if onPart != nil {
		onPart(part)
	}
	content := structs.NewLinkedList[model.MessagePart]()
	content.PushBack(part)
	msg := model.NewMessage(
		model.WithRole(model.MessageRoleAssistant),
		model.WithContent(content),
	)
	return msg, model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}, nil
}

// stubProvider implements provider.Provider backed by a single model.Model.
type stubProvider struct {
	id    string
	model model.Model
	cb    *provider.CircuitBreaker
}

func newStubProvider(id string, m model.Model) *stubProvider {
	return &stubProvider{
		id:    id,
		model: m,
		cb:    provider.DefaultCircuitBreaker(slog.Default()),
	}
}

func (p *stubProvider) GetName() string                             { return p.id }
func (p *stubProvider) GetID() string                               { return p.id }
func (p *stubProvider) GetDescription() string                      { return "stub" }
func (p *stubProvider) GetHomepage() string                         { return "" }
func (p *stubProvider) GetLogger() *slog.Logger                     { return slog.Default() }
func (p *stubProvider) GetCircuitBreaker() *provider.CircuitBreaker { return p.cb }
func (p *stubProvider) GetMetrics() *provider.Metrics               { return &provider.Metrics{} }
func (p *stubProvider) GetSelectedModel() model.Model               { return p.model }
func (p *stubProvider) SetModel(_ model.Model)                      {}
func (p *stubProvider) GetModels(_ context.Context) ([]model.Model, error) {
	return []model.Model{p.model}, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newTestSession(t *testing.T, text string) *Session {
	t.Helper()
	m := &stubModel{name: "stub-model", text: text}
	prov := newStubProvider("stub", m)
	sess, err := New(
		config.Config{},
		WithLogger(slog.Default()),
		WithProviders(prov),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return sess
}

func newBlockingSession(t *testing.T) (*Session, *stubModel) {
	t.Helper()
	m := &stubModel{name: "block", release: make(chan struct{})}
	prov := newStubProvider("blocking", m)
	sess, err := New(config.Config{}, WithLogger(slog.Default()), WithProviders(prov))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return sess, m
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestNew_WithProviders(t *testing.T) {
	sess := newTestSession(t, "hello")
	if sess == nil {
		t.Fatal("expected non-nil Session")
	}
}

func TestNew_NoProviders_Errors(t *testing.T) {
	// Clear any credentials that might be in the environment.
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	_, err := New(config.Config{})
	if err == nil {
		t.Fatal("expected error when no providers available, got nil")
	}
}

func TestSession_Subscribe_ReceivesComplete(t *testing.T) {
	sess := newTestSession(t, "42")

	var received []Event
	var mu sync.Mutex
	done := make(chan struct{})

	sess.Subscribe(func(e Event) {
		mu.Lock()
		received = append(received, e)
		if e.Kind == EventComplete {
			close(done)
		}
		mu.Unlock()
	})

	if err := sess.Send(context.Background(), "what is the answer?"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for EventComplete")
	}

	mu.Lock()
	defer mu.Unlock()

	var hasComplete, hasPart, hasState bool
	for _, e := range received {
		switch e.Kind {
		case EventComplete:
			hasComplete = true
			msg, ok := e.Payload.(model.Message)
			if !ok {
				t.Errorf("EventComplete payload is not model.Message: %T", e.Payload)
				continue
			}
			if msg.GetTextContent() != "42" {
				t.Errorf("EventComplete text: got %q, want %q", msg.GetTextContent(), "42")
			}
		case EventPartReceived:
			hasPart = true
		case EventStateChanged:
			hasState = true
		}
	}
	if !hasComplete {
		t.Error("expected EventComplete in received events")
	}
	if !hasPart {
		t.Error("expected EventPartReceived in received events")
	}
	if !hasState {
		t.Error("expected EventStateChanged in received events")
	}
}

func TestSession_ErrBusy(t *testing.T) {
	sess, m := newBlockingSession(t)
	ctx := t.Context()

	if err := sess.Send(ctx, "first"); err != nil {
		t.Fatalf("first Send: %v", err)
	}

	// inFlight is set before the goroutine is launched, so no sleep needed.
	if err := sess.Send(ctx, "second"); !errors.Is(err, ErrBusy) {
		t.Errorf("expected ErrBusy for second Send, got: %v", err)
	}

	close(m.release)
}

func TestSession_History(t *testing.T) {
	sess := newTestSession(t, "ok")

	done := make(chan struct{})
	sess.Subscribe(func(e Event) {
		if e.Kind == EventComplete {
			close(done)
		}
	})

	if err := sess.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	<-done

	hist := sess.History()
	if len(hist) < 2 {
		t.Fatalf("expected at least 2 messages in history (user + assistant), got %d", len(hist))
	}
	if hist[0].GetRole() != model.MessageRoleUser {
		t.Errorf("first history message: got role %q, want user", hist[0].GetRole())
	}
}

func TestSession_Reset(t *testing.T) {
	sess := newTestSession(t, "ok")

	done := make(chan struct{})
	sess.Subscribe(func(e Event) {
		if e.Kind == EventComplete {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	})

	if err := sess.Send(context.Background(), "hi"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	<-done

	if len(sess.History()) == 0 {
		t.Fatal("expected non-empty history before reset")
	}

	if err := sess.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	if len(sess.History()) != 0 {
		t.Errorf("expected empty history after Reset, got %d messages", len(sess.History()))
	}
}

func TestSession_Reset_Busy(t *testing.T) {
	sess, m := newBlockingSession(t)

	if err := sess.Send(t.Context(), "start"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := sess.Reset(); !errors.Is(err, ErrBusy) {
		t.Errorf("Reset while busy: got %v, want ErrBusy", err)
	}
	close(m.release)
}

func TestSession_UpdateConfig(t *testing.T) {
	sess := newTestSession(t, "ok")

	override := config.Config{
		Context: config.ContextConfig{MaxBudget: 99999},
		Agent:   config.AgentConfig{MaxRetries: 7},
	}
	if err := sess.UpdateConfig(override); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	got := sess.Config()
	if got.Context.MaxBudget != 99999 {
		t.Errorf("MaxBudget: got %d, want 99999", got.Context.MaxBudget)
	}
	if got.Agent.MaxRetries != 7 {
		t.Errorf("MaxRetries: got %d, want 7", got.Agent.MaxRetries)
	}
	// MaxRetries must also be propagated to the state machine.
	if sess.machine.GetMaxRetries() != 7 {
		t.Errorf("machine.MaxRetries: got %d, want 7", sess.machine.GetMaxRetries())
	}
}

func TestSession_UpdateConfig_Busy(t *testing.T) {
	sess, m := newBlockingSession(t)

	if err := sess.Send(t.Context(), "start"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := sess.UpdateConfig(config.Config{}); !errors.Is(err, ErrBusy) {
		t.Errorf("UpdateConfig while busy: got %v, want ErrBusy", err)
	}
	close(m.release)
}

func TestSession_Config_ReturnsActiveConfig(t *testing.T) {
	m := &stubModel{name: "stub-model", text: "ok"}
	prov := newStubProvider("stub", m)
	sess, err := New(
		config.Config{Security: config.SecurityConfig{PermissionLevel: "write"}},
		WithProviders(prov),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if sess.Config().Security.PermissionLevel != "write" {
		t.Errorf("expected PermissionLevel 'write', got %q", sess.Config().Security.PermissionLevel)
	}
}

func TestParsePermissionLevel(t *testing.T) {
	cases := []struct {
		in   string
		want security.PermissionLevel
	}{
		{"read", security.PermissionRead},
		{"write", security.PermissionWrite},
		{"bash", security.PermissionBash},
		{"danger_zone", security.PermissionDangerZone},
		{"", security.PermissionRead},
		{"unknown", security.PermissionRead},
	}
	for _, tc := range cases {
		if got := parsePermissionLevel(tc.in); got != tc.want {
			t.Errorf("parsePermissionLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSession_Cancel_AbortsInFlight(t *testing.T) {
	sess, m := newBlockingSession(t)

	errCh := make(chan Event, 1)
	sess.Subscribe(func(e Event) {
		if e.Kind == EventError {
			select {
			case errCh <- e:
			default:
			}
		}
	})

	if err := sess.Send(t.Context(), "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	sess.Cancel()

	select {
	case e := <-errCh:
		if e.Payload == nil {
			t.Error("expected non-nil error payload after Cancel")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for EventError after Cancel")
	}

	// Unblock the goroutine so it can exit and clear inFlight.
	close(m.release)

	// Poll inFlight directly (same package) instead of sleeping.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && sess.inFlight.Load() {
		time.Sleep(time.Millisecond)
	}
	if sess.inFlight.Load() {
		t.Fatal("inFlight still set 500ms after Cancel + release")
	}

	if err := sess.Send(t.Context(), "after cancel"); err != nil {
		t.Errorf("Send after cancel: %v", err)
	}
}

func TestSession_GetCurrentState(t *testing.T) {
	sess := newTestSession(t, "ok")
	if got := sess.GetCurrentState(); got != agent.StateInit {
		t.Errorf("expected StateInit before Send, got %v", got)
	}
}

func TestSession_UsageUpdated_Emitted(t *testing.T) {
	sess := newTestSession(t, "ok")

	// UsageManager fires its listeners in goroutines (it holds its own lock
	// while calling notifyListeners, so async dispatch avoids re-entrant
	// deadlocks). Use a dedicated channel so the test waits for the usage
	// event independently of EventComplete.
	usageCh := make(chan struct{}, 1)
	completeCh := make(chan struct{})

	sess.Subscribe(func(e Event) {
		switch e.Kind {
		case EventUsageUpdated:
			select {
			case usageCh <- struct{}{}:
			default:
			}
		case EventComplete:
			select {
			case <-completeCh:
			default:
				close(completeCh)
			}
		}
	})

	if err := sess.Send(t.Context(), "ping"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	<-completeCh

	select {
	case <-usageCh:
		// received
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for EventUsageUpdated")
	}
}
