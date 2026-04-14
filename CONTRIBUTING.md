# Contributing to FEINO

Thank you for taking the time to contribute! The following guidelines keep the review process smooth for everyone.

---

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting started](#getting-started)
- [How to contribute](#how-to-contribute)
  - [Reporting bugs](#reporting-bugs)
  - [Suggesting features](#suggesting-features)
  - [Submitting a pull request](#submitting-a-pull-request)
- [Development setup](#development-setup)
- [Coding standards](#coding-standards)
- [Commit messages](#commit-messages)
- [Tests](#tests)

---

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating you agree to abide by its terms.

---

## Getting started

1. **Fork** the repository and clone your fork.
2. Create a feature branch: `git checkout -b feat/my-feature`
3. Make your changes, add tests, and verify everything passes.
4. Open a pull request against `main`.

---

## How to contribute

### Reporting bugs

Use the **Bug report** issue template. Include:

- FEINO version (`./feino --version` or the commit hash)
- Operating system and Go version
- Minimal reproduction steps
- Expected vs. actual behaviour
- Relevant log output (`~/.feino/feino.log` for TUI sessions)

### Suggesting features

Use the **Feature request** issue template. Describe the problem you are trying to solve, not just the solution — this helps us consider alternatives and tradeoffs.

### Submitting a pull request

- One logical change per PR. Large refactors should be discussed in an issue first.
- Update documentation and the relevant `README.md` inside the package if behaviour changes.
- All tests must pass: `go test -race ./...`
- Run `go vet ./...` and `goimports` before pushing.
- PRs that add new tools must include tests covering the parameter schema and at least one success and one failure path.

---

## Development setup

```bash
git clone https://github.com/odinnordico/feino
cd feino

# Build (no embedded frontend)
go build -o feino ./cmd/feino

# Build with embedded React SPA
make web
go build -tags web -o feino ./cmd/feino

# Run tests
go test -race ./...

# Frontend development
cd internal/web/ui
npm install
npm run dev   # Vite dev server at :5173, proxies API to :7700
```

See [CLAUDE.md](CLAUDE.md) for the full development reference.

---

## Coding standards

- **Go version**: 1.26+. Use the standard library where possible.
- **Formatting**: `goimports` (superset of `gofmt`). CI enforces this.
- **Error handling**: return errors; never `panic` in library code.
- **Comments**: only when the *why* is non-obvious. No docstring walls.
- **Security**: all new tools must declare a permission level via `WithPermissionLevel`. Unclassified tools default to `DangerZone`.
- **Tests**: table-driven where it reduces duplication. Use `internal/testserver` for provider-level integration tests — no mocking the HTTP transport.

### Package layout conventions

| Where to add | What |
|---|---|
| `internal/tools/` | New native tool or tool category |
| `internal/provider/<name>/` | New LLM provider |
| `internal/tui/` | New TUI component |
| `internal/web/` + `proto/` | New web API endpoint |
| `~/.feino/plugins/` | Custom plugin (no code change needed) |

---

## Commit messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add browser_drag tool
fix: reconnect browser pool after CDP timeout
docs: update tool reference table in README
test: add missing coverage for TACOS Z-score outlier path
refactor: split ensureConnected into probe + connect phases
chore: bump chromedp to v0.15.2
```

Breaking changes must include `BREAKING CHANGE:` in the commit footer.

---

## Tests

```bash
# All packages
go test -race ./...

# Single package
go test -race ./internal/agent/...

# Specific test
go test ./internal/agent/ -run TestStateMachine

# Coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

Provider tests that hit a real API are skipped unless the corresponding `*_API_KEY` environment variable is set — they will never fail in CI without credentials.
