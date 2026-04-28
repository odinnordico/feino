package wizard

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/credentials"
	"github.com/odinnordico/feino/internal/i18n"
)

// validateEmail rejects blank input and requires an "@" character.
func validateEmail(s string) error {
	if err := requireNonEmpty(i18n.T("email_setup_address"))(s); err != nil {
		return err
	}
	if !strings.Contains(s, "@") {
		return errors.New(i18n.T("validation_email_format"))
	}
	return nil
}

// EmailSetupResult carries everything collected by RunEmailSetup.
type EmailSetupResult struct {
	// Non-sensitive settings stored in config.yaml.
	Address  string
	IMAPHost string
	IMAPPort int
	SMTPHost string
	SMTPPort int

	// Sensitive credentials stored in the credentials store.
	Username string
	Password string
}

// RunEmailSetup presents a huh-based wizard to collect IMAP/SMTP settings.
// The caller is responsible for persisting the result via SaveEmailSetup.
// Returns ErrAborted if the user cancels.
func RunEmailSetup(ctx context.Context, existing config.EmailServiceConfig) (EmailSetupResult, error) {
	res := EmailSetupResult{
		Address:  existing.Address,
		IMAPHost: existing.IMAPHost,
		IMAPPort: existing.IMAPPort,
		SMTPHost: existing.SMTPHost,
		SMTPPort: existing.SMTPPort,
	}

	// Default ports when not yet set.
	imapPortStr := "993"
	smtpPortStr := "587"
	if res.IMAPPort > 0 {
		imapPortStr = strconv.Itoa(res.IMAPPort)
	}
	if res.SMTPPort > 0 {
		smtpPortStr = strconv.Itoa(res.SMTPPort)
	}

	var (
		address  = res.Address
		username string
		password string
		imapHost = res.IMAPHost
		imapPort = imapPortStr
		smtpHost = res.SMTPHost
		smtpPort = smtpPortStr
		confirm  bool
	)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(i18n.T("email_setup_address")).
				Description(i18n.T("email_setup_address_desc")).
				Value(&address).
				Validate(validateEmail),
			huh.NewInput().
				Title(i18n.T("email_setup_username")).
				Value(&username),
			huh.NewInput().
				Title(i18n.T("email_setup_password")).
				EchoMode(huh.EchoModePassword).
				Value(&password).
				Validate(requireNonEmpty(i18n.T("email_setup_password"))),
		),
		huh.NewGroup(
			huh.NewInput().
				Title(i18n.T("email_setup_imap_host")).
				Description(i18n.T("email_setup_imap_host_desc")).
				Value(&imapHost).
				Validate(requireNonEmpty(i18n.T("email_setup_imap_host"))),
			huh.NewInput().
				Title(i18n.T("email_setup_imap_port")).
				Value(&imapPort).
				Validate(validatePort),
			huh.NewInput().
				Title(i18n.T("email_setup_smtp_host")).
				Description(i18n.T("email_setup_smtp_host_desc")).
				Value(&smtpHost).
				Validate(requireNonEmpty(i18n.T("email_setup_smtp_host"))),
			huh.NewInput().
				Title(i18n.T("email_setup_smtp_port")).
				Value(&smtpPort).
				Validate(validatePort),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title(i18n.T("email_setup_confirm")).
				Value(&confirm),
		),
	).WithTheme(huh.ThemeBase())

	if err := form.RunWithContext(ctx); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return EmailSetupResult{}, ErrAborted
		}
		return EmailSetupResult{}, fmt.Errorf("email setup: %w", err)
	}

	if !confirm {
		return EmailSetupResult{}, ErrAborted
	}

	// Use address as username when left blank.
	if strings.TrimSpace(username) == "" {
		username = address
	}

	iPort, _ := strconv.Atoi(imapPort)
	sPort, _ := strconv.Atoi(smtpPort)

	return EmailSetupResult{
		Address:  strings.TrimSpace(address),
		Username: strings.TrimSpace(username),
		Password: password,
		IMAPHost: strings.TrimSpace(imapHost),
		IMAPPort: iPort,
		SMTPHost: strings.TrimSpace(smtpHost),
		SMTPPort: sPort,
	}, nil
}

// SaveEmailSetup persists the result: non-sensitive settings into cfg (in-memory
// only — the caller must call config.Save), and credentials into the store.
func SaveEmailSetup(res *EmailSetupResult, cfg *config.Config, store credentials.Store) error {
	cfg.Defaults()
	cfg.Services.Email = config.EmailServiceConfig{
		Enabled:  new(true),
		Address:  res.Address,
		IMAPHost: res.IMAPHost,
		IMAPPort: res.IMAPPort,
		SMTPHost: res.SMTPHost,
		SMTPPort: res.SMTPPort,
	}

	if err := store.Set("email", "username", res.Username); err != nil {
		return fmt.Errorf("save email username: %w", err)
	}
	if err := store.Set("email", "password", res.Password); err != nil {
		return fmt.Errorf("save email password: %w", err)
	}
	return nil
}

// validatePort checks that a string is a valid TCP port number.
func validatePort(s string) error {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return errors.New(i18n.T("validation_port_format"))
	}
	if n < 1 || n > 65535 {
		return errors.New(i18n.T("validation_port_range"))
	}
	return nil
}
