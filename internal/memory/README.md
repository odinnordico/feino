# Package `internal/memory`

The `memory` package provides a persistent store for facts the agent learns about the user. Entries are written to `~/.feino/memory.json`, loaded on startup, and injected into every system prompt so the agent remembers context across sessions without the user repeating themselves.

---

## Categories

```go
const (
    CategoryProfile    Category = "profile"    // identity: name, timezone, pronouns
    CategoryPreference Category = "preference" // behavioural: "prefer metric units"
    CategoryFact       Category = "fact"       // environment: OS, shell, employer
    CategoryNote       Category = "note"       // free-form notes
)
```

Use `memory.AllCategories()` to get the canonical ordering (profile → preference → fact → note). This ordering is used by `FormatPrompt` and UI grouping.

---

## Store interface

```go
type Store interface {
    Write(category Category, content string) (Entry, error)
    Update(id, content string) (Entry, error)   // ErrNotFound if id missing
    Delete(id string) error                      // ErrNotFound if id missing
    All() ([]Entry, error)
    ByCategory(category Category) ([]Entry, error)
    Search(query string) ([]Entry, error)        // case-insensitive substring
    FormatPrompt() (string, error)
}
```

### Entry structure

```go
type Entry struct {
    ID        string    // 8-character random hex
    Category  Category
    Content   string
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

---

## FileStore

```go
store, err := memory.NewFileStore(path)

// Default path: ~/.feino/memory.json
path, err := memory.DefaultPath()
store, err := memory.NewFileStore(path)
```

### Implementation details

- **Atomic writes** — all mutations write to a temp file then rename, preventing partial writes on crash.
- **Concurrent access** — a `sync.RWMutex` serialises all reads and writes within a single process.
- **In-memory cache** — entries are loaded once on first access and kept in memory; disk is only hit again on writes.
- **IDs** — 8 random hex bytes from `crypto/rand`. Collision probability is negligible for typical usage volumes.
- **File mode** — `0600` (owner read/write only); parent directory created with `0700`.

---

## FormatPrompt

Returns a compact, grouped string suitable for system-prompt injection:

```
## Memories

[profile] Name: Diego
[profile] Timezone: America/Bogota
[preference] Always use metric units
[preference] Prefer bullet points over prose
[fact] Primary project: feino
[note] Discussing TACOS router performance improvements
```

The agent sees this block in every turn and can reference stored facts without re-asking the user.

---

## Best practices

- **Write facts as they are discovered**, not after the session ends. The agent can call `memory_write` via the native memory tool during a turn.
- **Use specific categories.** `profile` facts (name, timezone) are displayed prominently; `note` entries are more ephemeral. Category matters for `ByCategory` queries in the UI.
- **Content should be self-contained.** Each entry is shown in isolation; avoid references like "the file we discussed" — write the actual file path.
- **Search is substring-based.** For structured querying, prefer `ByCategory` and filter in the caller.

---

## Extending

### Using memory in the system prompt

The `context.WithMemoryStore(store)` option on `FileSystemContextManager` automatically calls `FormatPrompt` during context assembly and injects the result. No manual integration is needed.

### Adding a new category

1. Add a constant to `memory.go`.
2. Append it to `allCategories` in the correct display order.
3. Update `ValidCategory` (no change needed — it uses `slices.Contains(allCategories, c)`).
4. Update the UI's `categoryVariant` map in `MemoryManager.tsx` to assign a badge colour.

### Migrating the storage format

The on-disk format is a single JSON object `{"entries": [...]}`. To add new fields to `Entry`:

1. Add the field to the `Entry` struct with `json:",omitempty"`.
2. Old entries deserialise cleanly (omitempty means the field is zero for old entries).
3. New entries persist the field normally.

For breaking changes (rename or remove a field), write a migration function that reads the old format, transforms it, and writes the new format.
