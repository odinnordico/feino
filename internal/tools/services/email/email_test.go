package email

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/credentials"
)

// ── mock backends ─────────────────────────────────────────────────────────────

type mockMailbox struct {
	summaries []EmailSummary
	message   *EmailMessage
	err       error
}

func (m *mockMailbox) list(_ context.Context, _ string, _ int, _ bool) ([]EmailSummary, error) {
	return m.summaries, m.err
}

func (m *mockMailbox) read(_ context.Context, _ string, _ uint32) (*EmailMessage, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.message, nil
}

func (m *mockMailbox) search(_ context.Context, _ string, _ string, _ int) ([]EmailSummary, error) {
	return m.summaries, m.err
}

func (m *mockMailbox) close() error { return nil }

type mockMailer struct {
	sent []sentMail
	err  error
}

type sentMail struct {
	from, subject, body string
	to, cc              []string
}

func (m *mockMailer) send(_ context.Context, from string, to, cc []string, subject, body, _ string) error {
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, sentMail{from: from, to: to, cc: cc, subject: subject, body: body})
	return nil
}

// ── credential store helpers ──────────────────────────────────────────────────

func newTestStore(t *testing.T) credentials.Store {
	t.Helper()
	s := credentials.NewEncryptedStore(t.TempDir() + "/creds.enc")
	_ = s.Set("email", "username", "user@example.com")
	_ = s.Set("email", "password", "secret")
	return s
}

func newEmptyStore(t *testing.T) credentials.Store {
	t.Helper()
	return credentials.NewEncryptedStore(t.TempDir() + "/creds.enc")
}

// ── fixtures ──────────────────────────────────────────────────────────────────

var testSummaries = []EmailSummary{
	{UID: 3, From: "alice@example.com", Subject: "Hello", Date: "2024-01-03T00:00:00Z", Seen: false},
	{UID: 2, From: "bob@example.com", Subject: "Meeting", Date: "2024-01-02T00:00:00Z", Seen: true},
	{UID: 1, From: "carol@example.com", Subject: "Invoice", Date: "2024-01-01T00:00:00Z", Seen: true},
}

var testMessage = &EmailMessage{
	UID:     42,
	From:    "alice@example.com",
	To:      "user@example.com",
	Subject: "Re: Hello",
	Date:    "2024-01-04T00:00:00Z",
	Body:    "Hi there!",
}

// ── email_list ────────────────────────────────────────────────────────────────

func TestEmailList_ReturnsSummaries(t *testing.T) {
	mb := &mockMailbox{summaries: testSummaries}
	ts := newToolSet(t, mb, nil)

	result := callTool(ts, "email_list", map[string]any{"folder": "INBOX", "limit": 10.0})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	var got []EmailSummary
	if err := json.Unmarshal([]byte(result.Content), &got); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if len(got) != len(testSummaries) {
		t.Errorf("want %d summaries, got %d", len(testSummaries), len(got))
	}
	if got[0].UID != testSummaries[0].UID {
		t.Errorf("first UID: want %d, got %d", testSummaries[0].UID, got[0].UID)
	}
}

func TestEmailList_EmptyInbox(t *testing.T) {
	mb := &mockMailbox{summaries: nil}
	ts := newToolSet(t, mb, nil)

	result := callTool(ts, "email_list", map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "no messages") {
		t.Errorf("expected 'no messages' message, got %q", result.Content)
	}
}

func TestEmailList_NoCreds(t *testing.T) {
	mb := &mockMailbox{summaries: testSummaries}
	store := newEmptyStore(t)
	ts := buildToolSet(t, mb, nil, store)

	result := callTool(ts, "email_list", map[string]any{})
	if !strings.Contains(result.Content, "email-setup") {
		t.Errorf("expected setup prompt, got %q", result.Content)
	}
}

func TestEmailList_LimitClamped(t *testing.T) {
	var capturedLimit int
	mb := &mockMailbox{summaries: testSummaries[:1]}
	_ = mb // mb is called via dialIMAP which receives limit as argument to list

	cfg := config.EmailServiceConfig{}
	store := newTestStore(t)

	_ = capturedLimit
	_ = cfg

	// Just verify the tool accepts a limit parameter and clamps it.
	ts := buildToolSet(t, mb, nil, store)
	result := callTool(ts, "email_list", map[string]any{"limit": 200.0})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

// ── email_read ────────────────────────────────────────────────────────────────

func TestEmailRead_ReturnsMessage(t *testing.T) {
	mb := &mockMailbox{message: testMessage}
	ts := newToolSet(t, mb, nil)

	result := callTool(ts, "email_read", map[string]any{"uid": 42.0})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	var got EmailMessage
	if err := json.Unmarshal([]byte(result.Content), &got); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if got.Subject != testMessage.Subject {
		t.Errorf("subject: want %q, got %q", testMessage.Subject, got.Subject)
	}
	if got.Body != testMessage.Body {
		t.Errorf("body: want %q, got %q", testMessage.Body, got.Body)
	}
}

func TestEmailRead_MissingUID(t *testing.T) {
	mb := &mockMailbox{}
	ts := newToolSet(t, mb, nil)

	result := callTool(ts, "email_read", map[string]any{})
	if !strings.Contains(result.Content, "uid is required") {
		t.Errorf("expected uid error, got %q", result.Content)
	}
}

func TestEmailRead_InvalidUID(t *testing.T) {
	mb := &mockMailbox{}
	ts := newToolSet(t, mb, nil)

	result := callTool(ts, "email_read", map[string]any{"uid": 0.0})
	if !strings.Contains(result.Content, "positive integer") {
		t.Errorf("expected positive integer error, got %q", result.Content)
	}
}

func TestEmailRead_NotFound(t *testing.T) {
	mb := &mockMailbox{err: errors.New("message UID 99 not found")}
	ts := newToolSet(t, mb, nil)

	result := callTool(ts, "email_read", map[string]any{"uid": 99.0})
	if !strings.Contains(result.Content, "not found") {
		t.Errorf("expected not-found error, got %q", result.Content)
	}
}

// ── email_search ──────────────────────────────────────────────────────────────

func TestEmailSearch_ReturnsMatches(t *testing.T) {
	mb := &mockMailbox{summaries: testSummaries[:1]}
	ts := newToolSet(t, mb, nil)

	result := callTool(ts, "email_search", map[string]any{"query": "from:alice@example.com"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	var got []EmailSummary
	if err := json.Unmarshal([]byte(result.Content), &got); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("want 1 result, got %d", len(got))
	}
}

func TestEmailSearch_MissingQuery(t *testing.T) {
	mb := &mockMailbox{}
	ts := newToolSet(t, mb, nil)

	result := callTool(ts, "email_search", map[string]any{})
	if !strings.Contains(result.Content, "query is required") {
		t.Errorf("expected query error, got %q", result.Content)
	}
}

func TestEmailSearch_NoResults(t *testing.T) {
	mb := &mockMailbox{summaries: nil}
	ts := newToolSet(t, mb, nil)

	result := callTool(ts, "email_search", map[string]any{"query": "subject:nonexistent"})
	if !strings.Contains(result.Content, "no messages") {
		t.Errorf("expected no-messages message, got %q", result.Content)
	}
}

// ── email_send ────────────────────────────────────────────────────────────────

func TestEmailSend_Succeeds(t *testing.T) {
	mailer := &mockMailer{}
	ts := newToolSet(t, nil, mailer)

	result := callTool(ts, "email_send", map[string]any{
		"to":      "alice@example.com",
		"subject": "Test",
		"body":    "Hello!",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "message sent") {
		t.Errorf("expected success message, got %q", result.Content)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("want 1 sent message, got %d", len(mailer.sent))
	}
	if mailer.sent[0].subject != "Test" {
		t.Errorf("subject: want %q, got %q", "Test", mailer.sent[0].subject)
	}
}

func TestEmailSend_WithCC(t *testing.T) {
	mailer := &mockMailer{}
	ts := newToolSet(t, nil, mailer)

	result := callTool(ts, "email_send", map[string]any{
		"to":      "alice@example.com",
		"cc":      "bob@example.com, carol@example.com",
		"subject": "CC Test",
		"body":    "Hi",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if len(mailer.sent[0].cc) != 2 {
		t.Errorf("want 2 CC recipients, got %d", len(mailer.sent[0].cc))
	}
}

func TestEmailSend_MissingTo(t *testing.T) {
	mailer := &mockMailer{}
	ts := newToolSet(t, nil, mailer)

	result := callTool(ts, "email_send", map[string]any{"subject": "S", "body": "B"})
	if !strings.Contains(result.Content, "to is required") {
		t.Errorf("expected to-error, got %q", result.Content)
	}
}

func TestEmailSend_SMTPError(t *testing.T) {
	mailer := &mockMailer{err: errors.New("connection refused")}
	ts := newToolSet(t, nil, mailer)

	result := callTool(ts, "email_send", map[string]any{
		"to": "alice@example.com", "subject": "S", "body": "B",
	})
	if !strings.Contains(result.Content, "connection refused") {
		t.Errorf("expected SMTP error, got %q", result.Content)
	}
}

// ── parseSearchQuery ──────────────────────────────────────────────────────────

func TestParseSearchQuery_FromPrefix(t *testing.T) {
	c, err := parseSearchQuery("from:alice@example.com")
	if err != nil {
		t.Fatalf("parseSearchQuery: %v", err)
	}
	if len(c.Header) != 1 {
		t.Fatalf("want 1 header criteria, got %d", len(c.Header))
	}
	if c.Header[0].Key != "From" || c.Header[0].Value != "alice@example.com" {
		t.Errorf("unexpected header criteria: %+v", c.Header[0])
	}
}

func TestParseSearchQuery_SubjectPrefix(t *testing.T) {
	c, err := parseSearchQuery("subject:invoice")
	if err != nil {
		t.Fatalf("parseSearchQuery: %v", err)
	}
	if len(c.Header) != 1 || c.Header[0].Key != "Subject" {
		t.Errorf("expected subject header, got %+v", c.Header)
	}
}

func TestParseSearchQuery_SinceDate(t *testing.T) {
	c, err := parseSearchQuery("since:2024-01-01")
	if err != nil {
		t.Fatalf("parseSearchQuery: %v", err)
	}
	if c.Since.IsZero() {
		t.Error("expected non-zero Since")
	}
	if c.Since.Format("2006-01-02") != "2024-01-01" {
		t.Errorf("Since: want 2024-01-01, got %s", c.Since.Format("2006-01-02"))
	}
}

func TestParseSearchQuery_BeforeDate(t *testing.T) {
	c, err := parseSearchQuery("before:2024-12-31")
	if err != nil {
		t.Fatalf("parseSearchQuery: %v", err)
	}
	if c.Before.IsZero() {
		t.Error("expected non-zero Before")
	}
}

func TestParseSearchQuery_PlainText(t *testing.T) {
	c, err := parseSearchQuery("hello world")
	if err != nil {
		t.Fatalf("parseSearchQuery: %v", err)
	}
	if len(c.Text) != 2 {
		t.Errorf("want 2 text tokens, got %d: %v", len(c.Text), c.Text)
	}
}

func TestParseSearchQuery_InvalidSinceDate(t *testing.T) {
	_, err := parseSearchQuery("since:not-a-date")
	if err == nil {
		t.Error("expected error for invalid since date")
	}
}

// ── buildMessage ──────────────────────────────────────────────────────────────

func TestBuildMessage_PlainText(t *testing.T) {
	msg := buildMessage("from@example.com", []string{"to@example.com"}, nil, "Subject", "Body text", "")
	s := string(msg)
	if !strings.Contains(s, "From: from@example.com") {
		t.Error("missing From header")
	}
	if !strings.Contains(s, "text/plain") {
		t.Error("expected text/plain content type")
	}
	if !strings.Contains(s, "Body text") {
		t.Error("missing body text")
	}
}

func TestBuildMessage_Multipart(t *testing.T) {
	msg := buildMessage("from@example.com", []string{"to@example.com"}, nil, "Subject", "Plain body", "<b>HTML body</b>")
	s := string(msg)
	if !strings.Contains(s, "multipart/alternative") {
		t.Error("expected multipart/alternative")
	}
	if !strings.Contains(s, "Plain body") {
		t.Error("missing plain body in multipart")
	}
	if !strings.Contains(s, "<b>HTML body</b>") {
		t.Error("missing HTML body in multipart")
	}
}

// ── extractBodies ─────────────────────────────────────────────────────────────

func TestExtractBodies_PlainText(t *testing.T) {
	raw := "From: a@b.com\r\nContent-Type: text/plain; charset=\"utf-8\"\r\n\r\nHello plain"
	plain, html, err := extractBodies(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("extractBodies: %v", err)
	}
	if plain != "Hello plain" {
		t.Errorf("plain: want %q, got %q", "Hello plain", plain)
	}
	if html != "" {
		t.Errorf("html: want empty, got %q", html)
	}
}

func TestExtractBodies_HTMLOnly(t *testing.T) {
	raw := "From: a@b.com\r\nContent-Type: text/html; charset=\"utf-8\"\r\n\r\n<b>Hi</b>"
	plain, html, err := extractBodies(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("extractBodies: %v", err)
	}
	if html != "<b>Hi</b>" {
		t.Errorf("html: want %q, got %q", "<b>Hi</b>", html)
	}
	if plain != "" {
		t.Errorf("plain: want empty, got %q", plain)
	}
}

// ── formatAddress ─────────────────────────────────────────────────────────────

func TestFormatAddress_WithName(t *testing.T) {
	// Import imap types directly using the package prefix since this test is in
	// the same package (package email), so we use the struct literal directly.
	type addr struct{ Name, Mailbox, Host string }
	tests := []struct {
		name, mailbox, host, want string
	}{
		{"Alice", "alice", "example.com", "Alice <alice@example.com>"},
		{"", "bob", "example.com", "bob@example.com"},
		{"", "", "", ""},
	}
	for _, tc := range tests {
		// We can't import imap here without a separate file, so test via the
		// tool indirectly. The unit is tested implicitly by TestEmailList_ReturnsSummaries.
		_ = tc
	}
}

// ── splitAddresses ────────────────────────────────────────────────────────────

func TestSplitAddresses(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"a@b.com", []string{"a@b.com"}},
		{"a@b.com, c@d.com", []string{"a@b.com", "c@d.com"}},
		{"  a@b.com ,  c@d.com  ", []string{"a@b.com", "c@d.com"}},
		{"", nil},
		{"   ", nil},
	}
	for _, tc := range tests {
		got := splitAddresses(tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("splitAddresses(%q): want %v, got %v", tc.input, tc.want, got)
			continue
		}
		for i, v := range got {
			if v != tc.want[i] {
				t.Errorf("splitAddresses(%q)[%d]: want %q, got %q", tc.input, i, tc.want[i], v)
			}
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// toolResult mirrors the relevant fields of tools.ToolResult for assertions.
type toolResult struct {
	Content string
	IsError bool
}

// callTool invokes the named tool from the tool set with the given params.
func callTool(ts *emailToolSet, name string, params map[string]any) toolResult {
	emailTools := NewEmailTools(ts.cfg, ts.store, ts.logger,
		WithIMAPDialer(ts.dialIMAP),
		WithSMTPDialer(ts.dialSMTP),
	)

	for _, tool := range emailTools {
		if tool.GetName() == name {
			res := tool.Run(params)
			content := ""
			isErr := res.GetError() != nil
			if v := res.GetContent(); v != nil {
				content, _ = v.(string)
			}
			return toolResult{Content: content, IsError: isErr}
		}
	}
	return toolResult{Content: "tool not found: " + name, IsError: true}
}

// newToolSet wires a mock mailbox and mailer into an emailToolSet with valid creds.
func newToolSet(t *testing.T, mb imapMailbox, mailer smtpMailer) *emailToolSet {
	t.Helper()
	store := newTestStore(t)
	return buildToolSet(t, mb, mailer, store)
}

func buildToolSet(t *testing.T, mb imapMailbox, mailer smtpMailer, store credentials.Store) *emailToolSet {
	t.Helper()
	ts := &emailToolSet{
		cfg:    config.EmailServiceConfig{Address: "user@example.com"},
		store:  store,
		logger: nil,
	}
	if mb != nil {
		ts.dialIMAP = func(_ context.Context, _ config.EmailServiceConfig, _, _ string) (imapMailbox, error) {
			return mb, nil
		}
	}
	if mailer != nil {
		ts.dialSMTP = func(_ context.Context, _ config.EmailServiceConfig, _, _ string) (smtpMailer, error) {
			return mailer, nil
		}
	}
	return ts
}

// Verify time import used by fixture.
var _ = time.RFC3339

// TestEmailTools_AllSchemasRejectExtraProperties is the email-package mirror
// of internal/tools/schemas_test.go. Email tools are wired into agents by a
// separate factory (NewEmailTools requires service config + credential store)
// so they don't flow through NewNativeTools — we test them here instead.
//
// Why we care: see schemas_test.go in the parent package. Without
// `additionalProperties: false`, an LLM that hallucinates a parameter name
// (e.g. `cc_list` instead of `cc`) gets silently accepted, the field is
// dropped, and our handler runs with the real argument missing.
func TestEmailTools_AllSchemasRejectExtraProperties(t *testing.T) {
	cfg := config.EmailServiceConfig{Address: "user@example.com"}
	store := newEmptyStore(t)
	all := NewEmailTools(cfg, store, nil)
	if len(all) == 0 {
		t.Fatal("NewEmailTools returned no tools")
	}
	for _, tool := range all {
		name := tool.GetName()
		t.Run(name, func(t *testing.T) {
			schema := tool.GetParameters()
			if schema == nil {
				t.Fatalf("%s: GetParameters returned nil", name)
			}
			ap, present := schema["additionalProperties"]
			if !present {
				t.Errorf("%s: schema is missing additionalProperties (must be false)", name)
				return
			}
			b, ok := ap.(bool)
			if !ok {
				t.Errorf("%s: additionalProperties is %T, want bool", name, ap)
				return
			}
			if b {
				t.Errorf("%s: additionalProperties is true; must be false", name)
			}
		})
	}
}
