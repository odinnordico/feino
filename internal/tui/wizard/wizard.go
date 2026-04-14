package wizard

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"

	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/i18n"
)

// Run executes the multi-step setup wizard and returns a completed WizardResult.
//
// Navigation: Enter / Tab advances; Shift+Tab goes back to the previous step.
// Ctrl+C aborts at any point and returns ErrAborted.
//
// All steps run inside a single huh.Form (one bubbletea program). Credential
// groups are conditionally shown via WithHideFunc, so Shift+Tab back-navigation
// works across all steps without any terminal-state glitches.
func Run(ctx context.Context, existing config.Config) (WizardResult, error) {
	var res WizardResult
	res.Theme = "neo"
	res.WorkingDir = defaultWorkingDir()

	if existing.UI.Theme != "" {
		res.Theme = existing.UI.Theme
	}

	providers := buildProviders(&res)

	// Pre-fill credentials from existing config. Each provider inspects its
	// own config section; the first one that finds credentials becomes the
	// pre-selected provider so the user can jump straight to confirming.
	for _, p := range providers {
		if p.prefill != nil && p.prefill(existing, &res) {
			if res.Provider == "" {
				res.Provider = p.id
			}
		}
	}

	// Build provider select options from the registry.
	providerOpts := make([]huh.Option[string], len(providers))
	for i, p := range providers {
		providerOpts[i] = huh.NewOption(p.label, p.id)
	}

	// Collect credential groups and optional provider-specific model groups.
	var credGroups []*huh.Group
	var extraModelGroups []*huh.Group
	for _, p := range providers {
		credGroups = append(credGroups, p.credGroups...)
		if p.modelGroup != nil {
			extraModelGroups = append(extraModelGroups, p.modelGroup)
		}
	}

	var confirmed bool

	// Build the complete group list: intro + credentials + model steps + shared steps + confirm.
	groups := []*huh.Group{
		// Step 1: provider selection.
		huh.NewGroup(
			huh.NewNote().
				Title(i18n.T("wizard_title")).
				Description(i18n.T("wizard_nav_hint")),
			huh.NewSelect[string]().
				Title(i18n.T("wizard_provider_title")).
				Description(i18n.T("wizard_provider_desc")).
				Options(providerOpts...).
				Value(&res.Provider),
		),
	}
	groups = append(groups, credGroups...)
	groups = append(groups, extraModelGroups...)
	groups = append(groups,
		// Step 3: working directory.
		huh.NewGroup(
			huh.NewInput().
				Title(i18n.T("wizard_workdir_title")).
				Description(i18n.T("wizard_workdir_desc")).
				Placeholder(defaultWorkingDir()).
				Value(&res.WorkingDir),
		),

		// Step 4: UI theme.
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(i18n.T("wizard_theme_title")).
				Description(i18n.T("wizard_theme_desc")).
				Options(
					huh.NewOption(i18n.T("wizard_theme_neo"), "neo"),
					huh.NewOption(i18n.T("wizard_theme_auto"), "auto"),
					huh.NewOption(i18n.T("wizard_theme_dark"), "dark"),
					huh.NewOption(i18n.T("wizard_theme_light"), "light"),
				).
				Value(&res.Theme),
		),

		// Step 5: user profile (all fields optional).
		huh.NewGroup(
			huh.NewNote().
				Title(i18n.T("wizard_profile_title")).
				Description(i18n.T("wizard_profile_desc")),
			huh.NewInput().
				Title(i18n.T("wizard_profile_name_title")).
				Description(i18n.T("wizard_profile_name_desc")).
				Placeholder("e.g. Diego").
				Value(&res.Name),
			huh.NewInput().
				Title(i18n.T("wizard_profile_tz_title")).
				Description(i18n.T("wizard_profile_tz_desc")).
				Placeholder(defaultTimezone()).
				Value(&res.Timezone),
			huh.NewSelect[string]().
				Title(i18n.T("wizard_profile_style_title")).
				Description(i18n.T("wizard_profile_style_desc")).
				Options(
					huh.NewOption(i18n.T("wizard_profile_style_none"), ""),
					huh.NewOption(i18n.T("wizard_profile_style_concise"), "concise"),
					huh.NewOption(i18n.T("wizard_profile_style_detailed"), "detailed"),
					huh.NewOption(i18n.T("wizard_profile_style_technical"), "technical"),
					huh.NewOption(i18n.T("wizard_profile_style_friendly"), "friendly"),
				).
				Value(&res.CommunicationStyle),
		),

		// Step 6: confirmation.
		huh.NewGroup(
			huh.NewNote().
				Title(i18n.T("wizard_summary_title")).
				DescriptionFunc(func() string {
					return buildSummary(res, providers)
				}, &res),
			huh.NewConfirm().
				Title(i18n.T("wizard_save_title")).
				Value(&confirmed),
		),
	)

	if err := huh.NewForm(groups...).RunWithContext(ctx); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return WizardResult{}, ErrAborted
		}
		return WizardResult{}, fmt.Errorf("wizard: %w", err)
	}

	if !confirmed {
		return WizardResult{}, ErrAborted
	}

	if res.WorkingDir == "" {
		res.WorkingDir = defaultWorkingDir()
	}

	// Let the active provider finalise any derived fields.
	for _, p := range providers {
		if p.id == res.Provider && p.finalize != nil {
			p.finalize(&res)
			break
		}
	}

	return res, nil
}

// buildSummary produces the human-readable configuration summary shown at the
// confirmation step. The credential section is delegated to the active provider.
func buildSummary(res WizardResult, providers []*wizardProvider) string {
	providerLabel := res.Provider
	credLine := ""
	for _, p := range providers {
		if p.id == res.Provider {
			providerLabel = p.label
			credLine = p.summary()
			break
		}
	}
	return fmt.Sprintf(
		"%s: %s\n%s\n%s: %s\n%s: %s\n%s: %s",
		i18n.T("summary_provider"), providerLabel,
		credLine,
		i18n.T("summary_model"), res.DefaultModel,
		i18n.T("summary_workdir"), res.WorkingDir,
		i18n.T("wizard_theme_title"), res.Theme,
	)
}
