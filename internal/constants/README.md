# Package `internal/constants`

The `constants` package holds shared compile-time constants that are used across multiple packages. Centralising them here prevents duplication and ensures consistency.

---

## Contents

```go
// Timestamp format used throughout — RFC3339 (e.g. "2006-01-02T15:04:05Z07:00").
const TIMESTAMP_FORMAT = time.RFC3339

// Context file names in priority order.
// FileSystemContextManager scans for these in working directories.
const (
    FEINO_MD  = "FEINO.md"
    GEMINI_MD = "GEMINI.md"
    CLAUDE_MD = "CLAUDE.md"
)

// Default model names used when no model is configured.
const (
    CLAUDE_DEFAULT_MODEL = "claude-opus-4-7"
    GEMINI_DEFAULT_MODEL = "gemini-2.0-flash"
    OPENAI_DEFAULT_MODEL = "gpt-4o"
)
```

---

## Usage

Import and reference constants directly:

```go
import "github.com/odinnordico/feino/internal/constants"

ts := time.Now().Format(constants.TIMESTAMP_FORMAT)
```

---

## Best practices

- **Only place truly shared constants here.** Package-specific constants belong in their own package.
- **Avoid "magic strings" scattered in business logic.** If the same string literal appears in two or more packages, move it here.
- **Do not add mutable state.** This package must remain side-effect free and import nothing from the project.
