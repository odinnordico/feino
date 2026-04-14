package repl

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/odinnordico/feino/internal/app"
	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/provider"
	"github.com/odinnordico/feino/internal/structs"
)

// ── Test doubles ─────────────────────────────────────────────────────────────

type replStubModel struct {
	name string
	text string
}

func (m *replStubModel) GetID() string           { return m.name }
func (m *replStubModel) GetName() string         { return m.name }
func (m *replStubModel) GetDescription() string  { return "stub" }
func (m *replStubModel) GetHomepage() string     { return "" }
func (m *replStubModel) GetLogger() *slog.Logger { return slog.Default() }
func (m *replStubModel) GetContextWindow() int   { return 128000 }
func (m *replStubModel) GetMaxOutputTokens() int { return 4096 }
func (m *replStubModel) SupportsTools() bool     { return false }
func (m *replStubModel) Infer(_ context.Context, _ []model.Message, _ model.InferOptions, onPart func(model.MessagePart)) (model.Message, model.Usage, error) {
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

type replStubProvider struct {
	id    string
	model *replStubModel
	cb    *provider.CircuitBreaker
}

func newReplProvider(text string) *replStubProvider {
	return &replStubProvider{
		id:    "stub",
		model: &replStubModel{name: "stub-model", text: text},
		cb:    provider.DefaultCircuitBreaker(slog.Default()),
	}
}

func (p *replStubProvider) GetName() string                             { return p.id }
func (p *replStubProvider) GetID() string                               { return p.id }
func (p *replStubProvider) GetDescription() string                      { return "stub" }
func (p *replStubProvider) GetHomepage() string                         { return "" }
func (p *replStubProvider) GetLogger() *slog.Logger                     { return slog.Default() }
func (p *replStubProvider) GetCircuitBreaker() *provider.CircuitBreaker { return p.cb }
func (p *replStubProvider) GetMetrics() *provider.ProviderMetrics       { return &provider.ProviderMetrics{} }
func (p *replStubProvider) GetSelectedModel() model.Model               { return p.model }
func (p *replStubProvider) SetModel(_ model.Model)                      {}
func (p *replStubProvider) GetModels(_ context.Context) ([]model.Model, error) {
	return []model.Model{p.model}, nil
}

func newReplSession(t *testing.T, text string) *app.Session {
	t.Helper()
	sess, err := app.New(
		config.Config{},
		app.WithLogger(slog.Default()),
		app.WithProviders(newReplProvider(text)),
	)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	return sess
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestRun_SingleTurn(t *testing.T) {
	sess := newReplSession(t, "Paris")
	in := strings.NewReader("what is the capital of France?\n:quit\n")
	var out bytes.Buffer

	if err := Run(context.Background(), sess, in, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Paris") {
		t.Errorf("expected output to contain %q, got:\n%s", "Paris", got)
	}
}

func TestRun_Quit(t *testing.T) {
	sess := newReplSession(t, "irrelevant")
	in := strings.NewReader(":quit\n")
	var out bytes.Buffer

	if err := Run(context.Background(), sess, in, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// No model call should have happened.
	if len(sess.History()) != 0 {
		t.Errorf("expected empty history after immediate quit, got %d messages", len(sess.History()))
	}
}

func TestRun_Reset(t *testing.T) {
	sess := newReplSession(t, "ok")
	in := strings.NewReader("hello\n:reset\n:quit\n")
	var out bytes.Buffer

	if err := Run(context.Background(), sess, in, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(sess.History()) != 0 {
		t.Errorf("expected empty history after :reset, got %d messages", len(sess.History()))
	}
	if !strings.Contains(out.String(), "conversation reset") {
		t.Errorf("expected reset confirmation in output")
	}
}

func TestRun_History(t *testing.T) {
	sess := newReplSession(t, "ok")
	in := strings.NewReader("hello\n:history\n:quit\n")
	var out bytes.Buffer

	if err := Run(context.Background(), sess, in, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "user") {
		t.Errorf("expected 'user' role in :history output, got:\n%s", got)
	}
}

func TestRun_EmptyInput(t *testing.T) {
	sess := newReplSession(t, "ok")
	in := strings.NewReader("\n  \n\t\n:quit\n")
	var out bytes.Buffer

	if err := Run(context.Background(), sess, in, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sess.History()) != 0 {
		t.Errorf("expected no model calls for blank lines")
	}
}

func TestRun_Help(t *testing.T) {
	sess := newReplSession(t, "ok")
	in := strings.NewReader(":help\n:quit\n")
	var out bytes.Buffer

	if err := Run(context.Background(), sess, in, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), ":quit") {
		t.Errorf("expected :quit mentioned in :help output")
	}
}

func TestRun_EOFExits(t *testing.T) {
	sess := newReplSession(t, "ok")
	// No :quit — just EOF.
	in := strings.NewReader("hello\n")
	var out bytes.Buffer

	if err := Run(context.Background(), sess, in, &out); err != nil {
		t.Fatalf("Run should return nil on EOF: %v", err)
	}
}

func TestRun_Config(t *testing.T) {
	sess := newReplSession(t, "ok")
	in := strings.NewReader(":config\n:quit\n")
	var out bytes.Buffer

	if err := Run(context.Background(), sess, in, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// YAML output must be present; "providers:" is always emitted by yaml.Marshal.
	if !strings.Contains(out.String(), "providers:") {
		t.Errorf("expected YAML config in :config output, got:\n%s", out.String())
	}
}

// TestRun_MultiTurn verifies that output is not duplicated across turns,
// catching the subscriber-leak bug where N turns produced N copies of each
// response.
func TestRun_MultiTurn(t *testing.T) {
	sess := newReplSession(t, "pong")
	in := strings.NewReader("ping\nping\nping\n:quit\n")
	var out bytes.Buffer

	if err := Run(context.Background(), sess, in, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := out.String()
	count := strings.Count(got, "pong")
	if count != 3 {
		t.Errorf("expected exactly 3 'pong' responses for 3 turns, got %d:\n%s", count, got)
	}
}
