# Package `internal/i18n`

The `i18n` package wraps `github.com/nicksnyder/go-i18n` to provide simple string localisation for FEINO's user-facing messages. Locale JSON files are embedded at compile time.

---

## Initialisation

```go
// Init must be called once at startup before any T/Tf/Tp calls.
// lang is a BCP 47 language tag: "en", "es", "es-419", "pt-BR".
// An empty string auto-detects from the $LANG environment variable.
i18n.Init(cfg.UI.Language)
```

If the requested language has no locale file, the package falls back to English.

---

## API

```go
// T looks up a translation by message ID.
label := i18n.T("settings.save_button") // → "Save"

// Tf looks up a translation and substitutes Go template data.
msg := i18n.Tf("greeting", map[string]string{"Name": "Diego"})
// locale: "Hello, {{.Name}}!" → "Hello, Diego!"

// Tp selects a plural form based on count.
label := i18n.Tp("items_count", 3) // → "3 items"
```

---

## Adding locale files

Locale files live in `internal/i18n/locales/` as JSON and are embedded at compile time:

```json
// locales/es.json
{
  "settings.save_button": {
    "other": "Guardar"
  },
  "greeting": {
    "other": "Hola, {{.Name}}!"
  },
  "items_count": {
    "one": "{{.Count}} elemento",
    "other": "{{.Count}} elementos"
  }
}
```

To add a new language:

1. Create `locales/<lang>.json`.
2. Add the file to the `//go:embed` directive in `i18n.go`.
3. Run `go build ./...` to verify embedding.

---

## Adding new message IDs

1. Add the ID to `locales/en.json` first (canonical source).
2. Add translations to all other locale files.
3. Use `i18n.T("new.message.id")` at call sites.

If a message ID is missing from a locale file the package returns the ID string itself, making missing translations visible during testing.

---

## Best practices

- **Call `Init` before any UI code runs.** A panic-recovery wrapper returns the ID string on uninitialised lookups, but this should never happen in production.
- **Use dot-separated namespaced IDs** (`section.subsection.key`) to keep message IDs organised.
- **Keep messages short and action-oriented** in button labels and status messages. Reserve `Tf` for longer, parameterised messages.
- **Test with `LANG=es` or `LANG=pt_BR`** during development to catch missing translations early.
