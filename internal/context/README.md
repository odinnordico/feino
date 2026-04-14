# Package `internal/context`

The `context` package assembles the system prompt injected into every inference call. It combines global instructions, project context files, user profile, agent memories, tool descriptions, skill workflows, and semantically-chunked source code — all within a configurable token budget.

---

## Components

| File | Responsibility |
|------|---------------|
| `manager.go` | `FileSystemContextManager` — orchestrates prompt assembly |
| `chunker.go` | `TreeSitterParser` — semantic code extraction (AST for Go, line-based fallback) |
| `skills.go` | Skill workflow discovery from standard skill directories |
| `prompt.tmpl` | Embedded Go template rendered by `AssembleContext` |

---

## FileSystemContextManager

### Construction

```go
mgr := context.NewFileSystemContextManager(workingDir,
    context.WithContextLogger(logger),
    context.WithGlobalConfigPath("~/.feino/config.md"),
    context.WithUserProfile(cfg.User),
    context.WithMemoryStore(memStore),
)
```

### Core methods

```go
// Scan working directory for context files (FEINO.md > GEMINI.md > CLAUDE.md).
// Returns true when a project context file is found.
found := mgr.AutoDetect()

// Register tools so their descriptions appear in the system prompt.
mgr.SetTools(tools)

// Load skill workflows from the first matching skill directory.
count, err := mgr.LoadSkills()

// Assemble the final system prompt within the given token budget.
prompt, err := mgr.AssembleContext(ctx, maxBudget)

// Append a learning note (used by the agent to record insights).
err = mgr.AppendLearning(note)

// Add explicit code context (separate from auto-discovered chunks).
mgr.AddCodeContext(chunks)
```

### Context file priority

`AutoDetect` scans the working directory for these files in order:

1. `FEINO.md`
2. `GEMINI.md`
3. `CLAUDE.md`

The first file found becomes the project instructions block. The global config (`~/.feino/config.md` by default) is always prepended regardless.

---

## Two-pass budget assembly

`AssembleContext` never silently truncates fixed-overhead sections:

1. **Pass 1** — Render the template without code context. Measure the character cost of: user profile, memories, tool descriptions, skills, global config, and project instructions.
2. **Pass 2** — Remaining budget = `maxBudget − pass1Cost`. Fill with semantic code chunks in priority order until the budget is exhausted.

This guarantees that the agent always receives a complete tool list and project context even when the budget is tight.

---

## TreeSitterParser

Extracts semantically coherent chunks from source files.

```go
parser := context.NewTreeSitterParser(logger)
chunks, err := parser.ExtractSemanticChunks(ctx, source, filePath, skeletonOnly)
```

### Language strategies

| Language | Strategy | Extracted units |
|----------|----------|-----------------|
| Go | Tree-sitter AST | Functions, methods, types, interfaces — each with their docstring |
| Other | Line-based | Fixed 50-line windows |

### Skeleton mode

When `skeletonOnly=true`, function and method bodies are replaced with `{ ... }`. Use this for code-generation tasks where the model needs signatures but not implementation noise, significantly reducing token cost.

### SemanticChunk structure

```go
type SemanticChunk struct {
    Type      string // "function", "method", "type", etc.
    Name      string
    Content   string
    FilePath  string
    Language  string
    StartLine int
    EndLine   int
}
```

---

## Skills

Skills are Markdown files with YAML frontmatter describing reusable agent workflows.

### Discovery order

1. `.feino/skills/`
2. `.claude/skills/`
3. `.gemini/skills/`

The first directory that exists and contains `*.md` files is used.

### Skill file format

```markdown
---
name: refactor-function
description: Refactor a function to improve readability
parameters:
  - name: function_name
    description: Name of the function to refactor
    required: true
---

## Steps

1. Read the current function body.
2. Identify long sections and extract helper functions.
3. Rename variables to be descriptive.
4. Update tests accordingly.
```

Skills are injected into the system prompt verbatim so the agent can reference and execute them by name.

---

## Best practices

- **Call `AutoDetect` once per session**, not per turn. The working directory does not change mid-session.
- **Set a realistic `MaxBudget`.** Values below 8,000 characters will likely truncate tool descriptions. 60,000–100,000 is typical for modern long-context models.
- **Call `LoadSkills` after `AutoDetect`** since skills are project-scoped.
- **Use skeleton mode for generation tasks.** When the agent is generating new code (not analysing existing code), pass `skeletonOnly=true` to reduce context size significantly.
- **Prefer `AppendLearning` over modifying the global config file directly.** Learning notes are session-scoped and don't persist to disk.

---

## Extending

### Adding a new context source

1. Add a field to `FileSystemContextManager` (e.g., `dbSchemaPath string`).
2. Add a `WithDatabaseSchema(path string)` option.
3. Add a `{{.DatabaseSchema}}` block to `prompt.tmpl`.
4. Populate it in `AssembleContext` before the two-pass budget calculation.

### Supporting a new language in the chunker

1. Add the tree-sitter grammar as a dependency.
2. Add a language-detection branch in `ExtractSemanticChunks`.
3. Write a tree-sitter query for the target node types (functions, types, etc.).
4. Fall back to line-based chunking for any unrecognised constructs.

### Writing custom skills

Create `<working_dir>/.feino/skills/my-workflow.md` with YAML frontmatter. The agent discovers it automatically on the next `LoadSkills` call. No code changes needed.
