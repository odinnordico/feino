# Package `internal/credentials`

The `credentials` package provides a secure, backend-agnostic store for sensitive values such as API keys, OAuth access tokens, and refresh tokens. It automatically selects the best available backend for the current environment.

---

## Backend selection

```go
store := credentials.New(dir)
```

`New` probes the OS keyring first. If the keyring daemon is not reachable — headless servers, containers, SSH sessions without a forwarded agent — it falls back to an AES-256-GCM encrypted file at `<dir>/credentials.enc`.

```go
// Force encrypted-file backend (useful in CI or testing).
store := credentials.NewEncryptedStore(path)
```

| Backend | When used | Storage |
|---------|-----------|---------|
| OS keyring | macOS Keychain / Linux Secret Service / Windows Credential Manager | OS-managed secure enclave |
| Encrypted file | Keyring unavailable | `~/.feino/credentials.enc`, AES-256-GCM |

---

## Store interface

```go
type Store interface {
    Get(service, key string) (string, error)   // ErrNotFound when absent
    Set(service, key, value string) error
    Delete(service, key string) error           // nil if key does not exist
    Clear(service string) error                 // removes all keys for service
    List(service string) ([]string, error)      // sorted key names
    Kind() string                               // "os-keyring" or "encrypted-file"
}
```

### Namespacing

Credentials are namespaced by `(service, key)` pairs:

| service | key | Value |
|---------|-----|-------|
| `anthropic` | `api_key` | `sk-ant-...` |
| `gmail` | `access_token` | OAuth access token |
| `gmail` | `refresh_token` | OAuth refresh token |
| `gmail` | `password` | App password for IMAP |

Both backends use the same namespace, so swapping backends requires no call-site changes.

---

## Encrypted-file backend

### Key derivation

The encryption key is derived from a machine-specific secret via HKDF-SHA256:

1. Read a stable machine identifier (e.g., `/etc/machine-id` on Linux, IOPlatformUUID on macOS).
2. Derive a 32-byte key using HKDF with SHA-256 and the service name as info.

The environment variable `FEINO_CREDENTIALS_KEY` (base64-encoded 32 bytes, any variant) overrides machine derivation. Use this in CI/CD pipelines or Docker environments where the machine ID changes between runs.

### Storage properties

- **Atomic writes** — temp file + rename, preventing partial writes on crash.
- **File mode** — `0600` (owner read/write only); parent directory `0700`.
- **In-memory cache** — decrypted data is cached after the first read; subsequent `Get`/`List` calls do not hit disk.
- **Concurrent access** — `sync.RWMutex` serialises all in-process operations.

---

## Errors

```go
// Get returns ErrNotFound when the service/key pair does not exist.
var ErrNotFound = errors.New("credential not found")

// Returned when the encrypted file cannot be decrypted (key rotation, corruption).
var ErrDecrypt = errors.New("credentials: decryption failed — ...")
```

On `ErrDecrypt`, the user must either re-authenticate (run the setup wizard) or delete `~/.feino/credentials.enc` and start fresh.

---

## Best practices

- **Use `credentials.New(dir)` in production**, not `NewEncryptedStore`. The OS keyring provides stronger isolation and does not require managing an encryption key.
- **Set `FEINO_CREDENTIALS_KEY` in CI environments.** Without it, the machine-derived key changes between build agents and the encrypted file becomes unreadable.
- **Never log credential values.** The `Kind()` method returns the backend name for status displays without exposing any secrets.
- **Use `Clear(service)` on logout or provider removal**, not individual `Delete` calls. This avoids stale credential fragments.
- **Do not pass credentials through `config.Config`.** The `Session` constructor reads provider API keys from `credentials.Store` at startup; config should only contain non-secret settings.

---

## Extending

### Using a custom keyring backend

The `keyringStore` implementation wraps `github.com/zalando/go-keyring`. To use a different keyring provider (e.g., HashiCorp Vault, AWS Secrets Manager):

1. Implement the `Store` interface in a new file.
2. Update `New` to probe your backend before the OS keyring.

### Storing new credential types

No schema changes are needed. Just use a new `(service, key)` pair:

```go
store.Set("spotify", "access_token", token)
store.Set("spotify", "refresh_token", refresh)
```

Add constants for service and key names to avoid typos:

```go
const (
    ServiceSpotify = "spotify"
    KeyAccessToken = "access_token"
    KeyRefreshToken = "refresh_token"
)
```
