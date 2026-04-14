// Package i18n provides a thin wrapper around go-i18n/v2 with locale files
// embedded directly in the binary. Call Init once at startup; use T, Tf, and
// Tp everywhere UI strings appear.
package i18n

import (
	"embed"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*.json
var localeFS embed.FS

var (
	mu  sync.RWMutex
	loc *goi18n.Localizer
)

// Init initialises the bundle with the requested language tag (BCP 47, e.g.
// "es-419", "pt-BR", "zh-Hans"). Pass "" to auto-detect from $LANG / $LC_ALL.
// Falls back to English for any missing key. Safe to call multiple times.
func Init(lang string) {
	if lang == "" {
		lang = detectLang()
	}

	bundle := goi18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)

	entries, err := localeFS.ReadDir("locales")
	if err != nil {
		slog.Warn("i18n: cannot read locale directory", "error", err)
		return
	}
	for _, e := range entries {
		data, err := localeFS.ReadFile("locales/" + e.Name())
		if err != nil {
			slog.Warn("i18n: cannot read locale file", "file", e.Name(), "error", err)
			continue
		}
		if _, err := bundle.ParseMessageFileBytes(data, e.Name()); err != nil {
			slog.Warn("i18n: cannot parse locale file", "file", e.Name(), "error", err)
		}
	}

	mu.Lock()
	// Fallback chain: requested lang → English.
	loc = goi18n.NewLocalizer(bundle, lang, "en")
	mu.Unlock()
}

// localizer snapshots the current localizer under a read lock.
// Returns nil when Init has not been called yet.
func localizer() *goi18n.Localizer {
	mu.RLock()
	defer mu.RUnlock()
	return loc
}

// T returns the localised string for id.
// Returns id itself when no translation is found (safe default).
func T(id string) string {
	l := localizer()
	if l == nil {
		return id
	}
	s, err := l.Localize(&goi18n.LocalizeConfig{MessageID: id})
	if err != nil {
		slog.Debug("i18n: missing translation", "id", id, "error", err)
		return id
	}
	return s
}

// Tf returns the localised string for id with Go template data applied.
// data may be any struct or map[string]any.
func Tf(id string, data any) string {
	l := localizer()
	if l == nil {
		return id
	}
	s, err := l.Localize(&goi18n.LocalizeConfig{
		MessageID:    id,
		TemplateData: data,
	})
	if err != nil {
		slog.Debug("i18n: missing translation", "id", id, "error", err)
		return id
	}
	return s
}

// Tp returns the localised plural string for id. count selects the CLDR plural
// form; it should also appear in data under the appropriate key (e.g. "Count")
// so message templates can render it.
func Tp(id string, count int, data any) string {
	l := localizer()
	if l == nil {
		return id
	}
	s, err := l.Localize(&goi18n.LocalizeConfig{
		MessageID:    id,
		PluralCount:  count,
		TemplateData: data,
	})
	if err != nil {
		slog.Debug("i18n: missing plural translation", "id", id, "error", err)
		return id
	}
	return s
}

// detectLang reads $LANGUAGE, $LC_ALL, $LC_MESSAGES, $LANG in order and
// converts the POSIX locale string (e.g. "es_MX.UTF-8") to a BCP 47 tag
// ("es-MX"). Returns "en" when nothing useful is found.
func detectLang() string {
	for _, env := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		v := os.Getenv(env)
		if v == "" || v == "C" || v == "POSIX" {
			continue
		}
		// $LANGUAGE may be a colon-separated priority list ("fr:de:en"); take
		// only the first entry.
		v, _, _ = strings.Cut(v, ":")
		// Strip encoding suffix: "es_MX.UTF-8" → "es_MX"
		v, _, _ = strings.Cut(v, ".")
		if v == "" || v == "C" || v == "POSIX" {
			continue
		}
		// POSIX underscore → BCP 47 hyphen: "es_MX" → "es-MX"
		return strings.ReplaceAll(v, "_", "-")
	}
	return "en"
}
