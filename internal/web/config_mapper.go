package web

import (
	feinov1 "github.com/odinnordico/feino/gen/feino/v1"
	"github.com/odinnordico/feino/internal/config"
)

// configToProto converts a config.Config to its proto representation.
// API keys are never included in the output — only has_api_key is set.
func configToProto(cfg config.Config) *feinov1.ConfigProto {
	vertex := cfg.Providers.Gemini.Vertex != nil && *cfg.Providers.Gemini.Vertex
	disableTools := cfg.Providers.OpenAICompat.DisableTools != nil && *cfg.Providers.OpenAICompat.DisableTools
	enableAST := cfg.Security.EnableASTBlacklist != nil && *cfg.Security.EnableASTBlacklist

	return &feinov1.ConfigProto{
		Providers: &feinov1.ProvidersConfigProto{
			Anthropic: &feinov1.AnthropicConfigProto{
				DefaultModel: cfg.Providers.Anthropic.DefaultModel,
				HasApiKey:    cfg.Providers.Anthropic.APIKey != "",
			},
			Openai: &feinov1.OpenAIConfigProto{
				BaseUrl:      cfg.Providers.OpenAI.BaseURL,
				DefaultModel: cfg.Providers.OpenAI.DefaultModel,
				HasApiKey:    cfg.Providers.OpenAI.APIKey != "",
			},
			Gemini: &feinov1.GeminiConfigProto{
				DefaultModel: cfg.Providers.Gemini.DefaultModel,
				Vertex:       vertex,
				ProjectId:    cfg.Providers.Gemini.ProjectID,
				Location:     cfg.Providers.Gemini.Location,
				HasApiKey:    cfg.Providers.Gemini.APIKey != "",
			},
			Ollama: &feinov1.OllamaConfigProto{
				Host:         cfg.Providers.Ollama.Host,
				DefaultModel: cfg.Providers.Ollama.DefaultModel,
			},
			OpenaiCompat: &feinov1.OpenAICompatConfigProto{
				BaseUrl:      cfg.Providers.OpenAICompat.BaseURL,
				Name:         cfg.Providers.OpenAICompat.Name,
				DefaultModel: cfg.Providers.OpenAICompat.DefaultModel,
				DisableTools: disableTools,
				HasApiKey:    cfg.Providers.OpenAICompat.APIKey != "",
			},
		},
		Agent: &feinov1.AgentConfigProto{
			MaxRetries:              int32(cfg.Agent.MaxRetries),
			HighComplexityThreshold: int32(cfg.Agent.HighComplexityThreshold),
			LowComplexityThreshold:  int32(cfg.Agent.LowComplexityThreshold),
			MetricsPath:             cfg.Agent.MetricsPath,
		},
		Security: &feinov1.SecurityConfigProto{
			PermissionLevel:    cfg.Security.PermissionLevel,
			AllowedPaths:       cfg.Security.AllowedPaths,
			EnableAstBlacklist: enableAST,
		},
		Context: &feinov1.ContextConfigProto{
			WorkingDir:       cfg.Context.WorkingDir,
			GlobalConfigPath: cfg.Context.GlobalConfigPath,
			MaxBudget:        int32(cfg.Context.MaxBudget),
			PluginsDir:       cfg.Context.PluginsDir,
		},
		Ui: &feinov1.UIConfigProto{
			Theme:    cfg.UI.Theme,
			LogLevel: cfg.UI.LogLevel,
			Language: cfg.UI.Language,
		},
		User: &feinov1.UserProfileConfigProto{
			Name:               cfg.User.Name,
			Timezone:           cfg.User.Timezone,
			CommunicationStyle: cfg.User.CommunicationStyle,
		},
		Services: &feinov1.ServicesConfigProto{
			Email: &feinov1.EmailServiceConfigProto{
				Enabled:  cfg.Services.Email.Enabled != nil && *cfg.Services.Email.Enabled,
				Address:  cfg.Services.Email.Address,
				ImapHost: cfg.Services.Email.IMAPHost,
				ImapPort: int32(cfg.Services.Email.IMAPPort),
				SmtpHost: cfg.Services.Email.SMTPHost,
				SmtpPort: int32(cfg.Services.Email.SMTPPort),
				// HasPassword is determined by the credential store, not config.
			},
		},
	}
}

// protoToConfig applies non-zero proto fields onto base config, returning the
// merged result. API keys are only applied when the proto field is non-empty
// (empty string = "leave unchanged").
func protoToConfig(p *feinov1.ConfigProto, base config.Config) config.Config {
	if p == nil {
		return base
	}
	out := base

	if pr := p.GetProviders(); pr != nil {
		if a := pr.GetAnthropic(); a != nil {
			if a.GetDefaultModel() != "" {
				out.Providers.Anthropic.DefaultModel = a.GetDefaultModel()
			}
			if a.GetApiKey() != "" {
				out.Providers.Anthropic.APIKey = a.GetApiKey()
			}
		}
		if o := pr.GetOpenai(); o != nil {
			if o.GetDefaultModel() != "" {
				out.Providers.OpenAI.DefaultModel = o.GetDefaultModel()
			}
			if o.GetApiKey() != "" {
				out.Providers.OpenAI.APIKey = o.GetApiKey()
			}
			if o.GetBaseUrl() != "" {
				out.Providers.OpenAI.BaseURL = o.GetBaseUrl()
			}
		}
		if g := pr.GetGemini(); g != nil {
			if g.GetDefaultModel() != "" {
				out.Providers.Gemini.DefaultModel = g.GetDefaultModel()
			}
			if g.GetApiKey() != "" {
				out.Providers.Gemini.APIKey = g.GetApiKey()
			}
			if g.GetProjectId() != "" {
				out.Providers.Gemini.ProjectID = g.GetProjectId()
			}
			if g.GetLocation() != "" {
				out.Providers.Gemini.Location = g.GetLocation()
			}
			if g.GetVertex() {
				t := true
				out.Providers.Gemini.Vertex = &t
			}
		}
		if ol := pr.GetOllama(); ol != nil {
			if ol.GetHost() != "" {
				out.Providers.Ollama.Host = ol.GetHost()
			}
			if ol.GetDefaultModel() != "" {
				out.Providers.Ollama.DefaultModel = ol.GetDefaultModel()
			}
		}
		if oc := pr.GetOpenaiCompat(); oc != nil {
			if oc.GetBaseUrl() != "" {
				out.Providers.OpenAICompat.BaseURL = oc.GetBaseUrl()
			}
			if oc.GetApiKey() != "" {
				out.Providers.OpenAICompat.APIKey = oc.GetApiKey()
			}
			if oc.GetName() != "" {
				out.Providers.OpenAICompat.Name = oc.GetName()
			}
			if oc.GetDefaultModel() != "" {
				out.Providers.OpenAICompat.DefaultModel = oc.GetDefaultModel()
			}
			if oc.GetDisableTools() {
				t := true
				out.Providers.OpenAICompat.DisableTools = &t
			}
		}
	}

	if a := p.GetAgent(); a != nil {
		if a.GetMaxRetries() != 0 {
			out.Agent.MaxRetries = int(a.GetMaxRetries())
		}
		if a.GetHighComplexityThreshold() != 0 {
			out.Agent.HighComplexityThreshold = int(a.GetHighComplexityThreshold())
		}
		if a.GetLowComplexityThreshold() != 0 {
			out.Agent.LowComplexityThreshold = int(a.GetLowComplexityThreshold())
		}
		if a.GetMetricsPath() != "" {
			out.Agent.MetricsPath = a.GetMetricsPath()
		}
	}

	if s := p.GetSecurity(); s != nil {
		if s.GetPermissionLevel() != "" {
			out.Security.PermissionLevel = s.GetPermissionLevel()
		}
		if len(s.GetAllowedPaths()) > 0 {
			out.Security.AllowedPaths = s.GetAllowedPaths()
		}
		if s.GetEnableAstBlacklist() {
			t := true
			out.Security.EnableASTBlacklist = &t
		}
	}

	if c := p.GetContext(); c != nil {
		if c.GetWorkingDir() != "" {
			out.Context.WorkingDir = c.GetWorkingDir()
		}
		if c.GetGlobalConfigPath() != "" {
			out.Context.GlobalConfigPath = c.GetGlobalConfigPath()
		}
		if c.GetMaxBudget() != 0 {
			out.Context.MaxBudget = int(c.GetMaxBudget())
		}
		if c.GetPluginsDir() != "" {
			out.Context.PluginsDir = c.GetPluginsDir()
		}
	}

	if u := p.GetUi(); u != nil {
		if u.GetTheme() != "" {
			out.UI.Theme = u.GetTheme()
		}
		if u.GetLogLevel() != "" {
			out.UI.LogLevel = u.GetLogLevel()
		}
		if u.GetLanguage() != "" {
			out.UI.Language = u.GetLanguage()
		}
	}

	if u := p.GetUser(); u != nil {
		if u.GetName() != "" {
			out.User.Name = u.GetName()
		}
		if u.GetTimezone() != "" {
			out.User.Timezone = u.GetTimezone()
		}
		if u.GetCommunicationStyle() != "" {
			out.User.CommunicationStyle = u.GetCommunicationStyle()
		}
	}

	if svc := p.GetServices(); svc != nil {
		if e := svc.GetEmail(); e != nil {
			out.Services.Email.Enabled = new(e.GetEnabled())
			if e.GetAddress() != "" {
				out.Services.Email.Address = e.GetAddress()
			}
			if e.GetImapHost() != "" {
				out.Services.Email.IMAPHost = e.GetImapHost()
			}
			if e.GetImapPort() != 0 {
				out.Services.Email.IMAPPort = int(e.GetImapPort())
			}
			if e.GetSmtpHost() != "" {
				out.Services.Email.SMTPHost = e.GetSmtpHost()
			}
			if e.GetSmtpPort() != 0 {
				out.Services.Email.SMTPPort = int(e.GetSmtpPort())
			}
		}
	}

	return out
}
