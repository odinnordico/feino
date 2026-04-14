# Pending First-Citizen Tools

Tools that should be implemented as native built-ins in `internal/tools/`.
Each entry notes the proposed tool name(s), the backing service or stdlib, the
permission level, and why it belongs here rather than as a script plugin.

**Permission levels:** `read` · `write` · `bash` · `danger_zone`

---

## Implemented

| Tool | File | Permission |
|------|------|------------|
| `shell_exec` | `shell.go` | bash |
| `file_read`, `file_write`, `file_edit`, `file_search`, `list_files` | `files.go` | read / write |
| `git_status`, `git_log`, `git_diff`, `git_blame` | `git.go` | read |
| `web_fetch`, `web_search` | `web.go` | read |
| `currency_rates`, `currency_convert` | `currency.go` | read |
| `email_list`, `email_read`, `email_search`, `email_send` | `tools/services/email/email.go` | read / write |
| `sys_info` | `sysinfo.go` | read |
| `notify` | `notify.go` | read |
| `http_request` | `http.go` | read |
| `weather_current`, `weather_forecast` | `weather.go` | read |
| `memory_write`, `memory_list`, `memory_update`, `memory_forget` | `memory.go` (injected via `WithExtraTools`, not `NewNativeTools`) | write / read |

> **Email — auth gap:** The current implementation uses username + password / app-password only
> (IMAP `LOGIN`, SMTP `PlainAuth`). OAuth 2.0 is not yet supported. See the
> [OAuth roadmap](#oauth-roadmap) section below before extending this tool.

---

## Pending

### Unit conversion
**Tools:** `unit_convert`
**Backing:** stdlib math (no external service)
**Permission:** read
**Why:** Length, mass, temperature, speed, area, volume, pressure, energy. Complementary to `currency_convert`. Pure computation — no network call needed.

---

### Date / time arithmetic
**Tools:** `datetime_now`, `datetime_diff`, `datetime_add`
**Backing:** `time` stdlib
**Permission:** read
**Why:** Agents frequently need "what day is it in Tokyo", "how many days between X and Y", or "add 90 days to 2024-03-01". These are trivial in Go but tedious to pipe through `shell_exec`. Timezone-aware from the start.

---

### System information ✅ implemented
**Tools:** `sys_info`
**File:** `internal/tools/sysinfo.go`
**Backing:** [`gopsutil/v4`](https://github.com/shirou/gopsutil) (cross-platform)
**Permission:** read
**Why:** CPU count/usage, total/free RAM, disk usage per mount point, OS/kernel version, hostname. Useful for devops queries and for the agent to self-orient in the environment without spawning `uname`/`free`/`df` subprocesses.

---

### Process list
**Tools:** `process_list`, `process_kill`
**Backing:** `gopsutil` + `os` stdlib
**Permission:** read / bash
**Why:** `process_list` (read) returns PID, name, CPU%, memory% for running processes. `process_kill` (bash) sends a signal by PID. More structured and cross-platform than `ps aux | grep` through `shell_exec`.

---

### Hashing and encoding
**Tools:** `hash`, `base64_encode`, `base64_decode`
**Backing:** `crypto/sha256`, `crypto/md5`, `encoding/base64` stdlib
**Permission:** read
**Why:** Pure local computation needed constantly for checksums, secret comparison, and encoding payloads. Replacing `echo … | sha256sum` shell invocations removes OS portability concerns (macOS vs Linux flag differences).

---

### Archive operations
**Tools:** `archive_create`, `archive_extract`, `archive_list`
**Backing:** `archive/tar`, `archive/zip` stdlib
**Permission:** write / read
**Why:** Creating and inspecting `.zip`/`.tar.gz` archives is a common agent task (packaging releases, inspecting downloaded files). The stdlib handles both formats without external binaries.

---

### SQLite query
**Tools:** `sqlite_query`, `sqlite_exec`
**Backing:** [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite) — pure-Go, no CGO
**Permission:** read / write
**Why:** Many local tools, config stores, and embedded databases use SQLite. `sqlite_query` (SELECT, read) and `sqlite_exec` (INSERT/UPDATE/DDL, write) give agents direct access without piping through the `sqlite3` CLI.

---

### JSON / YAML transform
**Tools:** `json_query`, `yaml_to_json`, `json_to_yaml`
**Backing:** `encoding/json` + `gopkg.in/yaml.v3` (already a dependency)
**Permission:** read
**Why:** Agents spend a lot of tool turns piping JSON through `jq` via `shell_exec`. A native `json_query` with a JMESPath or JSONPath expression avoids the `jq` binary dependency and works on all platforms. `yaml_to_json` / `json_to_yaml` are one-liners in Go given the existing yaml dep.

---

### Diff / patch
**Tools:** `text_diff`, `text_patch`
**Backing:** [`github.com/sergi/go-diff`](https://github.com/sergi/go-diff) or stdlib
**Permission:** read / write
**Why:** `file_edit` does exact-string replacement. For larger structural changes an agent benefits from seeing a unified diff and applying a patch file. `text_diff` returns a unified diff between two strings; `text_patch` applies one.

---

### UUID / random
**Tools:** `uuid_generate`, `random_bytes`
**Backing:** `crypto/rand` + `github.com/google/uuid` (or manual RFC 4122)
**Permission:** read
**Why:** Generating UUIDs, random tokens, and test fixture IDs is a daily need. Avoids `uuidgen` (not available everywhere) and `openssl rand` shell calls.

---

### Clipboard
**Tools:** `clipboard_read`, `clipboard_write`
**Backing:** [`golang.design/x/clipboard`](https://github.com/golang-design/clipboard)
**Permission:** read / write
**Why:** Desktop agents benefit from reading what the user just copied and writing results back to the clipboard without the user having to select and copy output manually. Opt-in: only register these tools when a display is available (`$DISPLAY` / `$WAYLAND_DISPLAY` set).

---

### Notifications ✅ implemented
**Tools:** `notify`
**File:** `internal/tools/notify.go`
**Backing:** [`github.com/gen2brain/beeep`](https://github.com/gen2brain/beeep) v0.11.2 (cross-platform)
**Permission:** read
**Why:** Long-running agent tasks can pop a desktop notification when finished instead of requiring the user to watch the TUI. Complements async / queued messages.

Parameters: `title` (required), `message` (required), `icon` (optional path), `alert` (bool — modal dialog instead of passive toast). On Linux, gated behind `$DISPLAY` / `$WAYLAND_DISPLAY`; silently no-ops with an explanatory message in headless/SSH sessions. Returns `beeep.ErrUnsupported` message on unsupported platforms rather than an error.

---

## Service integrations

These require OAuth2 flows or third-party API keys. They should live in
`internal/tools/services/` and be registered separately (opt-in via config)
rather than in `NewNativeTools`, since they need credentials that not every
user will have.

### Credential storage ✅ implemented

`internal/credentials` provides the secure store all service tools must use.
**Never write OAuth tokens or secrets to `config.yaml`.**

```go
store := credentials.New(feino.DataDir()) // auto-selects backend

// Writing a token after an OAuth exchange:
store.Set("gmail", "access_token",  tok.AccessToken)
store.Set("gmail", "refresh_token", tok.RefreshToken)
store.Set("gmail", "expires_at",    tok.Expiry.Format(time.RFC3339))

// Reading it back:
access, err := store.Get("gmail", "access_token")
```

Backend selection (automatic):
| Environment | Backend | Storage |
|---|---|---|
| Desktop (Linux w/ Secret Service, macOS, Windows) | `os-keyring` | OS credential manager |
| Headless / container / SSH | `encrypted-file` | `~/.feino/credentials.enc` (chmod 600) |
| CI / Docker (explicit) | `encrypted-file` + env | `FEINO_CREDENTIALS_KEY` overrides machine derivation |

The encrypted-file key is derived via HKDF-SHA256 from the machine ID
(`/etc/machine-id`) + current username. Set `FEINO_CREDENTIALS_KEY` (base64,
32 bytes) to override, e.g. for containers where the machine ID is ephemeral.

---

### Email — Gmail
**Tools:** `gmail_list`, `gmail_read`, `gmail_send`, `gmail_search`, `gmail_label`
**Backing:** [Gmail API v1](https://developers.google.com/gmail/api) · OAuth2 (`gmail.modify` scope)
**Auth:** OAuth2 PKCE flow; refresh token stored in `~/.feino/tokens/gmail.json`
**Permission:** read / write
**Why:** Reading, writing, labelling, and searching email is one of the most-requested agent capabilities. The Gmail API returns structured JSON (sender, subject, body, attachments, labels) which is far richer than IMAP/SMTP plain text and avoids app-password workarounds.

---

### Email — Outlook / Microsoft 365
**Tools:** `outlook_list`, `outlook_read`, `outlook_send`, `outlook_search`
**Backing:** [Microsoft Graph API](https://learn.microsoft.com/en-us/graph/api/resources/mail-api-overview) · OAuth2
**Auth:** Azure AD app registration; PKCE flow; refresh token in `~/.feino/tokens/outlook.json`
**Permission:** read / write
**Why:** Covers corporate Microsoft 365 accounts that do not use Gmail. Graph API also exposes calendar and contacts in the same auth scope, enabling future cross-tool workflows.

---

### Email — Generic IMAP / SMTP ✅ implemented (password auth only)
**Tools:** `email_list`, `email_read`, `email_search`, `email_send`
**File:** `internal/tools/services/email/email.go`
**Backing:** [`github.com/emersion/go-imap/v2`](https://github.com/emersion/go-imap) + `net/smtp` stdlib
**Auth:** Username + password / app-password (`credentials.Store` service `"email"`, keys `"username"` / `"password"`)
**Permission:** read / write
**Why:** Fallback for self-hosted mail servers, FastMail, ProtonMail Bridge, and any provider that does not expose a REST API. Pure-Go libraries, no system mail client dependency.

**Auth gap — OAuth not yet supported.** The current dial path calls `c.Login(username, password)` for IMAP and `smtp.PlainAuth` for SMTP. Neither can carry an OAuth 2.0 bearer token. See [OAuth roadmap](#oauth-roadmap) for what must change before Gmail or Outlook accounts can authenticate without an app-password.

---

### Calendar — Google Calendar
**Tools:** `gcal_list_events`, `gcal_create_event`, `gcal_update_event`, `gcal_delete_event`
**Backing:** [Google Calendar API v3](https://developers.google.com/calendar/api) · OAuth2 (same credentials as Gmail)
**Auth:** Reuses Gmail OAuth token if `calendar` scope is added at consent time
**Permission:** read / write
**Why:** Creating meetings, checking availability, and listing upcoming events are natural follow-ups to email workflows. Shares the OAuth infrastructure with Gmail.

---

### Calendar — Outlook Calendar
**Tools:** `outlook_cal_list`, `outlook_cal_create`, `outlook_cal_update`, `outlook_cal_delete`
**Backing:** Microsoft Graph API (same app registration as Outlook email)
**Auth:** Reuses Outlook OAuth token with `Calendars.ReadWrite` scope
**Permission:** read / write
**Why:** Corporate users manage their schedule in Outlook. Graph returns rich event objects with attendees, recurrence, and Teams meeting links.

---

### Music — Spotify
**Tools:** `spotify_now_playing`, `spotify_search`, `spotify_play`, `spotify_queue`, `spotify_playlist_list`, `spotify_playlist_add`
**Backing:** [Spotify Web API](https://developer.spotify.com/documentation/web-api) · OAuth2
**Auth:** PKCE flow; `user-read-playback-state user-modify-playback-state playlist-modify-public` scopes; token in `~/.feino/tokens/spotify.json`
**Permission:** read / write
**Why:** Controlling playback, queueing tracks, and managing playlists without leaving the terminal. The Web API is well-documented, has a free tier (playback control requires Spotify Premium on the account, not on the API key).

---

### Music — Last.fm
**Tools:** `lastfm_now_playing`, `lastfm_recent_tracks`, `lastfm_top_artists`, `lastfm_top_tracks`, `lastfm_scrobble`
**Backing:** [Last.fm API](https://www.last.fm/api) · API key (free) + shared secret for write operations
**Auth:** API key stored in config; `sk` session key for scrobbling
**Permission:** read / write
**Why:** Last.fm is scrobbler-agnostic and works alongside any player. The API key is free and does not require OAuth. Useful for music history queries ("what have I been listening to this week?") and manual scrobbling.

---

### Office documents — Word (DOCX)
**Tools:** `docx_read`, `docx_create`, `docx_append`
**Backing:** [`github.com/fumiama/go-docx`](https://github.com/fumiama/go-docx) or [`github.com/lukasjarosch/go-docx`](https://github.com/lukasjarosch/go-docx)
**Permission:** read / write
**Why:** Reading and generating `.docx` files is a common request (contracts, reports, meeting notes). Extracting plain text from DOCX avoids piping through LibreOffice or `pandoc`.

---

### Office documents — Excel (XLSX)
**Tools:** `xlsx_read_sheet`, `xlsx_write_sheet`, `xlsx_list_sheets`
**Backing:** [`github.com/xuri/excelize/v2`](https://github.com/xuri/excelize) — mature, widely used
**Permission:** read / write
**Why:** Spreadsheets are ubiquitous for data, budgets, and reporting. `xlsx_read_sheet` returns rows as JSON arrays; `xlsx_write_sheet` accepts the same format. Far more reliable than `csv` round-tripping when formulas, multiple sheets, or formatting must be preserved.

---

### Office documents — PDF
**Tools:** `pdf_read`, `pdf_merge`, `pdf_split`
**Backing:** [`github.com/pdfcpu/pdfcpu`](https://github.com/pdfcpu/pdfcpu) — pure Go, no CGO
**Permission:** read / write
**Why:** `pdf_read` extracts plain text (and optionally page metadata) from PDFs — the single most common "read this document" request. `pdf_merge` / `pdf_split` cover the manipulation side. Pure-Go avoids a `poppler` / `ghostscript` system dependency.

---

### Office documents — Presentations (PPTX)
**Tools:** `pptx_read`, `pptx_create`
**Backing:** [`github.com/EndFirstCorp/peekaboo`](https://github.com/EndFirstCorp/peekaboo) or direct `archive/zip` + XML parsing (PPTX is a ZIP of XML files)
**Permission:** read / write
**Why:** Extracting slide text and speaker notes from `.pptx` files, or generating simple slide decks from structured data. Lower priority than DOCX/XLSX but rounds out the Office suite.

---

### Contacts — Google Contacts
**Tools:** `contacts_list`, `contacts_search`, `contacts_create`
**Backing:** [Google People API](https://developers.google.com/people) · OAuth2 (reuses Gmail credentials with `contacts.readonly` or `contacts` scope)
**Permission:** read / write
**Why:** Resolves email addresses from names, enriches email drafts with contact details, and enables "add this person to my contacts" workflows.

---

### Messaging — Slack
**Tools:** `slack_post`, `slack_read_channel`, `slack_search`, `slack_list_channels`
**Backing:** [Slack Web API](https://api.slack.com/web) · Bot OAuth token
**Auth:** Slack App bot token (`xoxb-…`) stored in config
**Permission:** write / read
**Why:** Posting summaries, alerts, and reports to Slack channels is a very common automation target. Reading recent messages enables "summarise what happened in #engineering today" workflows.

---

### Messaging — Telegram
**Tools:** `telegram_send`, `telegram_read`
**Backing:** [Telegram Bot API](https://core.telegram.org/bots/api) · Bot token (no OAuth, just `HTTP_TOKEN`)
**Auth:** Bot token from BotFather, stored in config
**Permission:** write / read
**Why:** Telegram bots require no OAuth dance — just a token. Simple to set up for personal notification and command workflows.

---

---

## OAuth roadmap

OAuth support is required before Gmail, Outlook, Google Calendar, Spotify, and
Google Contacts can authenticate without app-passwords. The same infrastructure
serves all of them.

### What must be built

#### 1. Token store (`internal/credentials`)

The existing `Store` interface is sufficient — OAuth tokens are just string
values. Agreed key layout (service = provider slug, e.g. `"gmail"`):

| Key | Value |
|-----|-------|
| `access_token` | Bearer token |
| `refresh_token` | Long-lived refresh token |
| `expires_at` | RFC 3339 timestamp |
| `token_type` | Usually `"Bearer"` |

```go
store.Set("gmail", "access_token",  tok.AccessToken)
store.Set("gmail", "refresh_token", tok.RefreshToken)
store.Set("gmail", "expires_at",    tok.Expiry.Format(time.RFC3339))
```

No changes to `credentials` package are needed.

#### 2. OAuth helper package (`internal/oauth`)

New package with three responsibilities:

**a) PKCE authorization flow** — works in a TUI without a browser window:

```go
// StartFlow opens the authorization URL (prints it + optionally xdg-opens),
// starts a local redirect server on a random port, waits for the callback,
// exchanges the code for tokens, and returns them.
func StartFlow(ctx context.Context, cfg ProviderConfig) (*Token, error)
```

- Generates `code_verifier` / `code_challenge` (S256)
- Picks a random local port; registers `redirect_uri = http://localhost:{port}/callback`
- Prints the URL for manual opening; also calls `xdg-open` / `open` when a display is available
- HTTP server handles `/callback`, extracts `code`, performs token exchange, shuts down

**b) Token refresh**

```go
func Refresh(ctx context.Context, cfg ProviderConfig, store credentials.Store) (*Token, error)
```

- Reads `refresh_token` + `expires_at` from the store
- Returns the cached token if not yet expired (with a 60 s buffer)
- Otherwise POSTs to the provider's token endpoint and persists updated tokens

**c) Provider registry**

```go
type ProviderConfig struct {
    Name         string   // "gmail", "outlook", "spotify", …
    AuthURL      string
    TokenURL     string
    ClientID     string
    ClientSecret string   // empty for public clients (PKCE only)
    Scopes       []string
}

var Providers = map[string]ProviderConfig{
    "gmail":   { /* accounts.google.com */ },
    "outlook": { /* login.microsoftonline.com */ },
    "spotify": { /* accounts.spotify.com */ },
}
```

Dependency: `golang.org/x/oauth2` handles PKCE, token exchange, and refresh
internally; use it rather than re-implementing RFC 7636.

#### 3. IMAP OAuth (`internal/tools/services/email`)

Replace `c.Login(username, password)` with SASL `XOAUTH2`:

```go
// go-imap/v2 supports custom SASL via imapclient.Options.SASLClient
import "github.com/emersion/go-sasl"

saslClient := sasl.NewXoauth2Client(username, accessToken)
c, err := imapclient.DialTLS(addr, &imapclient.Options{
    SASLClient: saslClient,
})
```

The `productionDialIMAP` function gains an `authMode` discriminator:

```go
type authMode int
const (
    authPassword authMode = iota
    authOAuth
)
```

Token is fetched via `oauth.Refresh(ctx, oauth.Providers["gmail"], store)` before
dialing; if `ErrNotFound` is returned for `access_token` the tool returns a
"run /email-setup to authenticate" message.

#### 4. SMTP OAuth (`internal/tools/services/email`)

Replace `smtp.PlainAuth` with a custom `smtp.Auth` that sends `XOAUTH2`:

```go
type xoauth2Auth struct{ username, token string }

func (a xoauth2Auth) Start(server *smtp.ServerInfo) (string, []byte, error) {
    blob := fmt.Sprintf("user=%s\x01auth=Bearer %s\x01\x01", a.username, a.token)
    return "XOAUTH2", []byte(blob), nil
}
func (a xoauth2Auth) Next(fromServer []byte, more bool) ([]byte, error) { return nil, nil }
```

#### 5. TUI wizard (`internal/tui/wizard/email.go`)

Add an `authType` step before the credential step:

```
Auth type: [Password / App-password]  [OAuth 2.0]
```

- **Password path** — existing flow unchanged
- **OAuth path** — skip username/password fields; call `oauth.StartFlow`; on
  success display "✓ authenticated as user@gmail.com"; store tokens via
  `wizard.SaveEmailSetup`

#### 6. Affected providers (in order of priority)

| Priority | Service | OAuth provider | Scope(s) |
|----------|---------|----------------|----------|
| 1 | Gmail IMAP/SMTP | Google | `https://mail.google.com/` |
| 2 | Outlook IMAP/SMTP | Microsoft | `https://outlook.office.com/IMAP.AccessAsUser.All https://outlook.office.com/SMTP.Send` |
| 3 | Gmail API tools | Google | `gmail.modify` |
| 4 | Google Calendar | Google | `calendar` (add to Gmail consent) |
| 5 | Outlook Calendar | Microsoft | `Calendars.ReadWrite` (add to Outlook consent) |
| 6 | Spotify | Spotify | `user-read-playback-state user-modify-playback-state playlist-modify-public` |
| 7 | Google Contacts | Google | `contacts` (add to Gmail consent) |

Gmail, Google Calendar, and Google Contacts share one OAuth app registration
and one refresh token — they differ only in scopes. Same for Outlook + Outlook
Calendar.

---

## Notes

- All tools should follow the existing pattern: `NewXxxTools(logger) []Tool`, registered in `NewNativeTools`.
- Every tool needs a `_test.go` with at least: happy path, missing required params, and error handling.
- Network-backed tools (weather, HTTP) must use `context.WithTimeout` and signal truncation when responses are capped.
- When a tool needs a new external dependency, add it via `go get` and document it here.
