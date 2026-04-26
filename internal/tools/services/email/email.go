// Package email provides IMAP/SMTP tools for the feino agent.
//
// # Tools
//
// Four tools are exposed:
//   - email_list   — list recent messages from a folder (IMAP FETCH ENVELOPE)
//   - email_read   — read a single message by UID (IMAP FETCH TEXT)
//   - email_search — search messages by header/text criteria (IMAP UID SEARCH)
//   - email_send   — send a message (SMTP)
//
// # Credential storage
//
// Username and password are read at call time from a [credentials.Store] under
// service "email", keys "username" and "password". Non-sensitive settings
// (IMAP/SMTP host and port) come from [config.EmailServiceConfig].
//
// # Testability
//
// The real IMAP and SMTP backends implement [imapMailbox] and [smtpMailer].
// Tests inject lightweight mock implementations via [ToolsOption] functional
// options, so no network connectivity is required.
package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/smtp"
	"slices"
	"sort"
	"strings"
	"time"

	imap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/credentials"
	"github.com/odinnordico/feino/internal/tools"
)

const credService = "email"

// EmailSummary is returned by email_list and email_search.
type EmailSummary struct {
	UID     uint32 `json:"uid"`
	From    string `json:"from"`
	Subject string `json:"subject"`
	Date    string `json:"date"` // RFC3339
	Seen    bool   `json:"seen"`
}

// EmailMessage is returned by email_read.
type EmailMessage struct {
	UID     uint32 `json:"uid"`
	From    string `json:"from"`
	To      string `json:"to"`
	CC      string `json:"cc,omitempty"`
	Subject string `json:"subject"`
	Date    string `json:"date"`           // RFC3339
	Body    string `json:"body"`           // plain-text body
	HTML    string `json:"html,omitempty"` // HTML body if present
}

// imapMailbox abstracts an authenticated IMAP connection for reading mail.
// The production implementation uses go-imap/v2; tests use mockIMAPMailbox.
type imapMailbox interface {
	list(ctx context.Context, folder string, limit int, unreadOnly bool) ([]EmailSummary, error)
	read(ctx context.Context, folder string, uid uint32) (*EmailMessage, error)
	search(ctx context.Context, folder, query string, limit int) ([]EmailSummary, error)
	close() error
}

// smtpMailer abstracts an authenticated SMTP connection for sending mail.
// The production implementation uses net/smtp; tests use mockSMTPMailer.
type smtpMailer interface {
	send(ctx context.Context, from string, to, cc []string, subject, body, htmlBody string) error
}

// dialIMAP is the factory for production IMAP connections. Replaced in tests.
type dialIMAPFunc func(ctx context.Context, cfg config.EmailServiceConfig, username, password string) (imapMailbox, error)

// dialSMTP is the factory for production SMTP connections. Replaced in tests.
type dialSMTPFunc func(ctx context.Context, cfg config.EmailServiceConfig, username, password string) (smtpMailer, error)

// emailToolSet holds the shared state across the four email tools.
type emailToolSet struct {
	cfg      config.EmailServiceConfig
	store    credentials.Store
	logger   *slog.Logger
	dialIMAP dialIMAPFunc
	dialSMTP dialSMTPFunc
}

// ToolsOption configures the email tool set. Used in tests to inject mocks.
type ToolsOption func(*emailToolSet)

// WithIMAPDialer overrides the IMAP connection factory. Used in tests.
func WithIMAPDialer(d dialIMAPFunc) ToolsOption {
	return func(s *emailToolSet) { s.dialIMAP = d }
}

// WithSMTPDialer overrides the SMTP connection factory. Used in tests.
func WithSMTPDialer(d dialSMTPFunc) ToolsOption {
	return func(s *emailToolSet) { s.dialSMTP = d }
}

// NewEmailTools returns the four email tools.
// cfg and store are captured by the tools and consulted at each call, so a
// /email-setup reconfiguration takes effect immediately without restart.
func NewEmailTools(cfg config.EmailServiceConfig, store credentials.Store, logger *slog.Logger, opts ...ToolsOption) []tools.Tool {
	s := &emailToolSet{
		cfg:      cfg,
		store:    store,
		logger:   logger,
		dialIMAP: productionDialIMAP,
		dialSMTP: productionDialSMTP,
	}
	for _, o := range opts {
		o(s)
	}
	return []tools.Tool{
		s.listTool(),
		s.readTool(),
		s.searchTool(),
		s.sendTool(),
	}
}

// ── credential helper ─────────────────────────────────────────────────────────

func (s *emailToolSet) creds() (username, password string, err error) {
	username, err = s.store.Get(credService, "username")
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return "", "", fmt.Errorf("email not configured — run /email-setup to add your credentials")
		}
		return "", "", fmt.Errorf("email: read username: %w", err)
	}
	password, err = s.store.Get(credService, "password")
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return "", "", fmt.Errorf("email not configured — run /email-setup to add your credentials")
		}
		return "", "", fmt.Errorf("email: read password: %w", err)
	}
	return username, password, nil
}

// ── email_list ────────────────────────────────────────────────────────────────

func (s *emailToolSet) listTool() tools.Tool {
	return tools.NewTool(
		"email_list",
		"List recent email messages from a mailbox folder. Returns message summaries "+
			"including UID, sender, subject, date, and read status.",
		map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"folder": map[string]any{
					"type":        "string",
					"description": `Mailbox folder name. Defaults to "INBOX".`,
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of messages to return (1–50). Defaults to 10.",
				},
				"unread_only": map[string]any{
					"type":        "boolean",
					"description": "When true, return only unread (unseen) messages.",
				},
			},
		},
		func(params map[string]any) tools.ToolResult {
			folder := GetStringDefault(params, "folder", "INBOX")
			limit := min(max(GetIntParam(params, "limit", 10), 1), 50)
			unreadOnly := GetBoolParam(params, "unread_only", false)

			username, password, err := s.creds()
			if err != nil {
				return tools.NewToolResult(err.Error(), nil)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			mb, err := s.dialIMAP(ctx, s.cfg, username, password)
			if err != nil {
				return tools.NewToolResult(fmt.Sprintf("email: connect: %v", err), nil)
			}
			defer func() { _ = mb.close() }()

			summaries, err := mb.list(ctx, folder, limit, unreadOnly)
			if err != nil {
				return tools.NewToolResult(fmt.Sprintf("email_list: %v", err), nil)
			}
			if len(summaries) == 0 {
				return tools.NewToolResult("no messages found", nil)
			}

			out, _ := json.MarshalIndent(summaries, "", "  ")
			return tools.NewToolResult(string(out), nil)
		},
		tools.WithPermissionLevel(tools.PermLevelRead),
		tools.WithLogger(s.logger),
	)
}

// ── email_read ────────────────────────────────────────────────────────────────

func (s *emailToolSet) readTool() tools.Tool {
	return tools.NewTool(
		"email_read",
		"Read a single email message by its UID. Returns the full message including "+
			"headers, plain-text body, and HTML body when present.",
		map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"uid": map[string]any{
					"type":        "integer",
					"description": "The UID of the message to read. Obtain UIDs from email_list or email_search.",
				},
				"folder": map[string]any{
					"type":        "string",
					"description": `Mailbox folder containing the message. Defaults to "INBOX".`,
				},
			},
			"required": []string{"uid"},
		},
		func(params map[string]any) tools.ToolResult {
			uidRaw, ok := params["uid"]
			if !ok {
				return tools.NewToolResult("email_read: uid is required", nil)
			}
			uid := GetIntParam(params, "uid", 0)
			if uid <= 0 {
				_ = uidRaw
				return tools.NewToolResult("email_read: uid must be a positive integer", nil)
			}
			folder := GetStringDefault(params, "folder", "INBOX")

			username, password, err := s.creds()
			if err != nil {
				return tools.NewToolResult(err.Error(), nil)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			mb, err := s.dialIMAP(ctx, s.cfg, username, password)
			if err != nil {
				return tools.NewToolResult(fmt.Sprintf("email: connect: %v", err), nil)
			}
			defer func() { _ = mb.close() }()

			msg, err := mb.read(ctx, folder, uint32(uid))
			if err != nil {
				return tools.NewToolResult(fmt.Sprintf("email_read: %v", err), nil)
			}

			out, _ := json.MarshalIndent(msg, "", "  ")
			return tools.NewToolResult(string(out), nil)
		},
		tools.WithPermissionLevel(tools.PermLevelRead),
		tools.WithLogger(s.logger),
	)
}

// ── email_search ──────────────────────────────────────────────────────────────

func (s *emailToolSet) searchTool() tools.Tool {
	return tools.NewTool(
		"email_search",
		"Search email messages using IMAP search criteria. Supports prefixes: "+
			"from:<addr>, subject:<text>, since:<YYYY-MM-DD>, before:<YYYY-MM-DD>, "+
			"or a plain text string to search across all header and body text.",
		map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": `Search query. Examples: "from:alice@example.com", "subject:invoice", "since:2024-01-01".`,
				},
				"folder": map[string]any{
					"type":        "string",
					"description": `Mailbox folder to search. Defaults to "INBOX".`,
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of results (1–50). Defaults to 10.",
				},
			},
			"required": []string{"query"},
		},
		func(params map[string]any) tools.ToolResult {
			query, ok := GetString(params, "query")
			if !ok || strings.TrimSpace(query) == "" {
				return tools.NewToolResult("email_search: query is required", nil)
			}
			folder := GetStringDefault(params, "folder", "INBOX")
			limit := min(max(GetIntParam(params, "limit", 10), 1), 50)

			username, password, err := s.creds()
			if err != nil {
				return tools.NewToolResult(err.Error(), nil)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			mb, err := s.dialIMAP(ctx, s.cfg, username, password)
			if err != nil {
				return tools.NewToolResult(fmt.Sprintf("email: connect: %v", err), nil)
			}
			defer func() { _ = mb.close() }()

			results, err := mb.search(ctx, folder, query, limit)
			if err != nil {
				return tools.NewToolResult(fmt.Sprintf("email_search: %v", err), nil)
			}
			if len(results) == 0 {
				return tools.NewToolResult("no messages matched the search query", nil)
			}

			out, _ := json.MarshalIndent(results, "", "  ")
			return tools.NewToolResult(string(out), nil)
		},
		tools.WithPermissionLevel(tools.PermLevelRead),
		tools.WithLogger(s.logger),
	)
}

// ── email_send ────────────────────────────────────────────────────────────────

func (s *emailToolSet) sendTool() tools.Tool {
	return tools.NewTool(
		"email_send",
		"Send an email message via SMTP. The From address is taken from the "+
			"configured email address. Returns a confirmation on success.",
		map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"to": map[string]any{
					"type":        "string",
					"description": "Recipient address or comma-separated list of addresses.",
				},
				"subject": map[string]any{
					"type":        "string",
					"description": "Message subject line.",
				},
				"body": map[string]any{
					"type":        "string",
					"description": "Plain-text message body.",
				},
				"cc": map[string]any{
					"type":        "string",
					"description": "CC address or comma-separated list of addresses (optional).",
				},
				"html_body": map[string]any{
					"type":        "string",
					"description": "HTML version of the message body (optional). When provided, the message is sent as multipart/alternative.",
				},
			},
			"required": []string{"to", "subject", "body"},
		},
		func(params map[string]any) tools.ToolResult {
			to, ok := GetString(params, "to")
			if !ok || strings.TrimSpace(to) == "" {
				return tools.NewToolResult("email_send: to is required", nil)
			}
			subject, ok := GetString(params, "subject")
			if !ok || strings.TrimSpace(subject) == "" {
				return tools.NewToolResult("email_send: subject is required", nil)
			}
			body, ok := GetString(params, "body")
			if !ok || strings.TrimSpace(body) == "" {
				return tools.NewToolResult("email_send: body is required", nil)
			}
			cc := GetStringDefault(params, "cc", "")
			htmlBody := GetStringDefault(params, "html_body", "")

			username, password, err := s.creds()
			if err != nil {
				return tools.NewToolResult(err.Error(), nil)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			mailer, err := s.dialSMTP(ctx, s.cfg, username, password)
			if err != nil {
				return tools.NewToolResult(fmt.Sprintf("email: connect SMTP: %v", err), nil)
			}

			from := s.cfg.Address
			if from == "" {
				from = username
			}

			toAddrs := splitAddresses(to)
			ccAddrs := splitAddresses(cc)

			if err := mailer.send(ctx, from, toAddrs, ccAddrs, subject, body, htmlBody); err != nil {
				return tools.NewToolResult(fmt.Sprintf("email_send: %v", err), nil)
			}

			return tools.NewToolResult(fmt.Sprintf("message sent to %s", to), nil)
		},
		tools.WithPermissionLevel(tools.PermLevelWrite),
		tools.WithLogger(s.logger),
	)
}

// ── production IMAP backend ───────────────────────────────────────────────────

type realIMAPMailbox struct {
	client *imapclient.Client
}

func productionDialIMAP(_ context.Context, cfg config.EmailServiceConfig, username, password string) (imapMailbox, error) {
	host := cfg.IMAPHost
	port := cfg.IMAPPort
	if port == 0 {
		port = 993
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	var (
		c   *imapclient.Client
		err error
	)
	// Port 993 (IMAPS) and 465: TLS from the start.
	// Port 143 and everything else: STARTTLS.
	if port == 993 || port == 465 {
		c, err = imapclient.DialTLS(addr, nil)
	} else {
		c, err = imapclient.DialStartTLS(addr, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", addr, err)
	}

	if err := c.Login(username, password).Wait(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("login: %w", err)
	}

	return &realIMAPMailbox{client: c}, nil
}

func (r *realIMAPMailbox) close() error { return r.client.Close() }

func (r *realIMAPMailbox) list(_ context.Context, folder string, limit int, unreadOnly bool) ([]EmailSummary, error) {
	if _, err := r.client.Select(folder, nil).Wait(); err != nil {
		return nil, fmt.Errorf("select %q: %w", folder, err)
	}

	criteria := &imap.SearchCriteria{}
	if unreadOnly {
		criteria.NotFlag = []imap.Flag{imap.FlagSeen}
	}

	searchData, err := r.client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}
	// UIDs are ascending; take the most-recent N from the end.
	if len(uids) > limit {
		uids = uids[len(uids)-limit:]
	}

	return r.fetchSummaries(uids)
}

func (r *realIMAPMailbox) search(_ context.Context, folder, query string, limit int) ([]EmailSummary, error) {
	if _, err := r.client.Select(folder, nil).Wait(); err != nil {
		return nil, fmt.Errorf("select %q: %w", folder, err)
	}

	criteria, err := parseSearchQuery(query)
	if err != nil {
		return nil, err
	}

	searchData, err := r.client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}
	if len(uids) > limit {
		uids = uids[len(uids)-limit:]
	}
	return r.fetchSummaries(uids)
}

func (r *realIMAPMailbox) read(_ context.Context, folder string, uid uint32) (*EmailMessage, error) {
	if _, err := r.client.Select(folder, nil).Wait(); err != nil {
		return nil, fmt.Errorf("select %q: %w", folder, err)
	}

	uidSet := imap.UIDSetNum(imap.UID(uid))
	bodySec := &imap.FetchItemBodySection{Peek: true}
	fetchCmd := r.client.Fetch(uidSet, &imap.FetchOptions{
		Envelope:    true,
		Flags:       true,
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{bodySec},
	})

	data := fetchCmd.Next()
	if data == nil {
		_ = fetchCmd.Close()
		return nil, fmt.Errorf("message UID %d not found in %q", uid, folder)
	}

	buf, err := data.Collect()
	_ = fetchCmd.Close()
	if err != nil {
		return nil, fmt.Errorf("fetch UID %d: %w", uid, err)
	}

	result := messageFromFetch(buf, uid)

	// Parse body from the buffered section bytes.
	if len(buf.BodySection) > 0 {
		plainBody, htmlBody, berr := extractBodies(bytes.NewReader(buf.BodySection[0].Bytes))
		if berr == nil {
			result.Body = plainBody
			result.HTML = htmlBody
		}
	}

	return result, nil
}

// fetchSummaries fetches ENVELOPE + FLAGS for a set of UIDs.
func (r *realIMAPMailbox) fetchSummaries(uids []imap.UID) ([]EmailSummary, error) {
	uidSet := imap.UIDSetNum(uids...)
	fetchCmd := r.client.Fetch(uidSet, &imap.FetchOptions{
		Envelope: true,
		Flags:    true,
		UID:      true,
	})

	var summaries []EmailSummary
	for {
		data := fetchCmd.Next()
		if data == nil {
			break
		}
		buf, err := data.Collect()
		if err != nil || buf.Envelope == nil {
			continue
		}
		s := EmailSummary{
			UID:     uint32(buf.UID),
			Subject: buf.Envelope.Subject,
		}
		if len(buf.Envelope.From) > 0 {
			s.From = formatAddress(buf.Envelope.From[0])
		}
		if !buf.Envelope.Date.IsZero() {
			s.Date = buf.Envelope.Date.Format(time.RFC3339)
		}
		if slices.Contains(buf.Flags, imap.FlagSeen) {
			s.Seen = true
		}
		summaries = append(summaries, s)
	}
	if err := fetchCmd.Close(); err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	// Sort newest-first by UID (higher UID = newer).
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UID > summaries[j].UID
	})
	return summaries, nil
}

// messageFromFetch builds an EmailMessage from a FetchMessageBuffer sans body.
func messageFromFetch(buf *imapclient.FetchMessageBuffer, uid uint32) *EmailMessage {
	m := &EmailMessage{UID: uid}
	if buf.Envelope == nil {
		return m
	}
	env := buf.Envelope
	m.Subject = env.Subject
	if len(env.From) > 0 {
		m.From = formatAddress(env.From[0])
	}
	m.To = formatAddressList(env.To)
	m.CC = formatAddressList(env.Cc)
	if !env.Date.IsZero() {
		m.Date = env.Date.Format(time.RFC3339)
	}
	return m
}

// ── production SMTP backend ───────────────────────────────────────────────────

type realSMTPMailer struct {
	cfg      config.EmailServiceConfig
	username string
	password string
}

func productionDialSMTP(_ context.Context, cfg config.EmailServiceConfig, username, password string) (smtpMailer, error) {
	return &realSMTPMailer{cfg: cfg, username: username, password: password}, nil
}

func (m *realSMTPMailer) send(_ context.Context, from string, to, cc []string, subject, body, htmlBody string) error {
	host := m.cfg.SMTPHost
	port := m.cfg.SMTPPort
	if port == 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	raw := buildMessage(from, to, cc, subject, body, htmlBody)

	all := append(to, cc...)
	auth := smtp.PlainAuth("", m.username, m.password, host)

	if port == 465 {
		// Implicit TLS (SMTPS).
		tlsCfg := &tls.Config{ServerName: host}
		conn, err := tls.Dial("tcp", addr, tlsCfg)
		if err != nil {
			return fmt.Errorf("smtp: tls dial %s: %w", addr, err)
		}
		client, err := smtp.NewClient(conn, host)
		if err != nil {
			return fmt.Errorf("smtp: new client: %w", err)
		}
		defer func() { _ = client.Quit() }()
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp: auth: %w", err)
		}
		if err := client.Mail(from); err != nil {
			return fmt.Errorf("smtp: MAIL FROM: %w", err)
		}
		for _, rcpt := range all {
			if err := client.Rcpt(rcpt); err != nil {
				return fmt.Errorf("smtp: RCPT TO %s: %w", rcpt, err)
			}
		}
		wc, err := client.Data()
		if err != nil {
			return fmt.Errorf("smtp: DATA: %w", err)
		}
		if _, err := wc.Write(raw); err != nil {
			return fmt.Errorf("smtp: write body: %w", err)
		}
		return wc.Close()
	}

	// STARTTLS (port 587, 25, …).
	return smtp.SendMail(addr, auth, from, all, raw)
}

// ── RFC 2822 message builder ──────────────────────────────────────────────────

// buildMessage creates a minimal RFC 2822 message. When htmlBody is provided
// the message is multipart/alternative; otherwise it is text/plain.
func buildMessage(from string, to, cc []string, subject, body, htmlBody string) []byte {
	var sb strings.Builder
	writeHeader := func(k, v string) {
		sb.WriteString(k)
		sb.WriteString(": ")
		sb.WriteString(v)
		sb.WriteString("\r\n")
	}

	writeHeader("From", from)
	writeHeader("To", strings.Join(to, ", "))
	if len(cc) > 0 {
		writeHeader("Cc", strings.Join(cc, ", "))
	}
	writeHeader("Subject", mime.QEncoding.Encode("utf-8", subject))
	writeHeader("Date", time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	writeHeader("MIME-Version", "1.0")

	if htmlBody == "" {
		writeHeader("Content-Type", `text/plain; charset="utf-8"`)
		writeHeader("Content-Transfer-Encoding", "8bit")
		sb.WriteString("\r\n")
		sb.WriteString(body)
		return []byte(sb.String())
	}

	// multipart/alternative
	boundary := fmt.Sprintf("feino_%d", time.Now().UnixNano())
	writeHeader("Content-Type", fmt.Sprintf(`multipart/alternative; boundary="%s"`, boundary))
	sb.WriteString("\r\n")

	// text/plain part
	sb.WriteString("--")
	sb.WriteString(boundary)
	sb.WriteString("\r\n")
	sb.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	sb.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)
	sb.WriteString("\r\n")

	// text/html part
	sb.WriteString("--")
	sb.WriteString(boundary)
	sb.WriteString("\r\n")
	sb.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
	sb.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(htmlBody)
	sb.WriteString("\r\n")

	sb.WriteString("--")
	sb.WriteString(boundary)
	sb.WriteString("--\r\n")

	return []byte(sb.String())
}

// ── MIME body extraction ──────────────────────────────────────────────────────

// extractBodies parses a raw MIME message and returns the plain-text and HTML
// body parts. Falls back to treating the entire content as plain text.
func extractBodies(r io.Reader) (plainText, htmlBody string, err error) {
	msg, err := mail.ReadMessage(r)
	if err != nil {
		return "", "", err
	}

	ct := msg.Header.Get("Content-Type")
	mediaType, params, _ := mime.ParseMediaType(ct)

	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(msg.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
			partCT := part.Header.Get("Content-Type")
			partMedia, _, _ := mime.ParseMediaType(partCT)
			content, readErr := io.ReadAll(io.LimitReader(part, 1<<20)) // 1 MB cap
			if readErr != nil {
				continue
			}
			switch partMedia {
			case "text/plain":
				if plainText == "" {
					plainText = strings.TrimSpace(string(content))
				}
			case "text/html":
				if htmlBody == "" {
					htmlBody = strings.TrimSpace(string(content))
				}
			}
		}
		return plainText, htmlBody, nil
	}

	// Non-multipart: read the whole body.
	content, err := io.ReadAll(io.LimitReader(msg.Body, 1<<20))
	if err != nil {
		return "", "", err
	}
	text := strings.TrimSpace(string(content))
	if strings.HasPrefix(mediaType, "text/html") {
		return "", text, nil
	}
	return text, "", nil
}

// ── search query parser ───────────────────────────────────────────────────────

// parseSearchQuery converts a simple query string into an IMAP SearchCriteria.
// Supported prefixes: from:<addr>  subject:<text>  since:<YYYY-MM-DD>  before:<YYYY-MM-DD>
// All other text is matched against the full message text (headers + body).
func parseSearchQuery(query string) (*imap.SearchCriteria, error) {
	criteria := &imap.SearchCriteria{}
	remaining := query

	applyPrefix := func(prefix, value string) error {
		switch prefix {
		case "from":
			criteria.Header = append(criteria.Header, imap.SearchCriteriaHeaderField{
				Key: "From", Value: value,
			})
		case "subject":
			criteria.Header = append(criteria.Header, imap.SearchCriteriaHeaderField{
				Key: "Subject", Value: value,
			})
		case "since":
			t, err := time.Parse("2006-01-02", value)
			if err != nil {
				return fmt.Errorf("email_search: invalid since date %q (expected YYYY-MM-DD)", value)
			}
			criteria.Since = t
		case "before":
			t, err := time.Parse("2006-01-02", value)
			if err != nil {
				return fmt.Errorf("email_search: invalid before date %q (expected YYYY-MM-DD)", value)
			}
			criteria.Before = t
		default:
			return fmt.Errorf("email_search: unknown prefix %q", prefix)
		}
		return nil
	}

	for token := range strings.FieldsSeq(remaining) {
		if i := strings.IndexByte(token, ':'); i > 0 {
			prefix := strings.ToLower(token[:i])
			value := token[i+1:]
			if err := applyPrefix(prefix, value); err != nil {
				return nil, err
			}
		} else {
			criteria.Text = append(criteria.Text, token)
		}
	}

	return criteria, nil
}

// ── address formatting ────────────────────────────────────────────────────────

func formatAddress(addr imap.Address) string {
	name := strings.TrimSpace(addr.Name)
	mailbox := strings.TrimSpace(addr.Mailbox)
	host := strings.TrimSpace(addr.Host)
	if mailbox == "" || host == "" {
		return name
	}
	email := mailbox + "@" + host
	if name != "" {
		return name + " <" + email + ">"
	}
	return email
}

func formatAddressList(addrs []imap.Address) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if s := formatAddress(a); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// splitAddresses splits a comma-separated address string into trimmed parts.
func splitAddresses(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			result = append(result, t)
		}
	}
	return result
}

// ── param helpers (package-level wrappers over tools.* unexported helpers) ───

// GetString extracts a string parameter. Returns ("", false) if absent or wrong type.
func GetString(params map[string]any, key string) (string, bool) {
	v, ok := params[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// GetStringDefault returns a string parameter or a default value.
func GetStringDefault(params map[string]any, key, def string) string {
	if s, ok := GetString(params, key); ok {
		return s
	}
	return def
}

// GetIntParam extracts an integer parameter (JSON number arrives as float64).
func GetIntParam(params map[string]any, key string, def int) int {
	v, ok := params[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return def
}

// GetBoolParam extracts a boolean parameter.
func GetBoolParam(params map[string]any, key string, def bool) bool {
	v, ok := params[key]
	if !ok {
		return def
	}
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}
