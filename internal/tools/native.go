package tools

import (
	"log/slog"
)

// NewNativeTools returns all built-in tool instances: shell, file, git, web,
// http, currency, sysinfo, notify, and browser tools. This is the convenience
// entry point for wiring the full native tool suite into an agent or context manager.
func NewNativeTools(logger *slog.Logger) []Tool {
	all := make([]Tool, 0, 11)
	all = append(all, NewShellTools(logger)...)
	all = append(all, NewFileTools(logger)...)
	all = append(all, NewGitTools(logger)...)
	all = append(all, NewWebTools(logger)...)
	all = append(all, NewHTTPTools(logger)...)
	all = append(all, NewCurrencyTools(logger)...)
	all = append(all, NewSysInfoTools(logger)...)
	all = append(all, NewNotifyTools(logger)...)
	all = append(all, NewWeatherTools(logger)...)
	all = append(all, NewBrowserTools(logger, 0)...)
	return all
}
