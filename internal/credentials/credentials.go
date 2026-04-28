// Package credentials provides a secure, backend-agnostic store for sensitive
// credentials such as OAuth tokens and API session keys.
//
// # Backend selection
//
// [New] probes the OS keyring first (macOS Keychain, Linux Secret Service,
// Windows Credential Manager via [github.com/zalando/go-keyring]). If the
// keyring daemon is not reachable — headless servers, containers, SSH sessions
// without a forwarded agent — it falls back to an AES-256-GCM encrypted file
// at <dir>/credentials.enc.
//
// [NewEncryptedStore] always uses the encrypted-file backend, which is useful
// for testing and for environments where the OS keyring is explicitly undesired.
//
// # Encrypted-file backend
//
// The encryption key is derived deterministically from a machine-specific
// secret (see [machineSecret]) via HKDF-SHA256. The variable
// FEINO_CREDENTIALS_KEY (base64-encoded 32 bytes, any base64 variant) overrides
// machine derivation, which is intended for CI pipelines or Docker environments
// where the machine ID changes between runs.
//
// All writes are atomic (temp-file + rename) and the file is created with mode
// 0600. A sync.RWMutex serialises concurrent in-process access. Decrypted data
// is kept in an in-memory cache so repeated Get/List calls do not hit disk.
//
// # Key namespacing
//
// Credentials are namespaced by service ("gmail", "spotify") and key
// ("access_token", "refresh_token"). Both backends use the same namespace so
// that backends can be swapped without changing call sites.
package credentials

import (
	"errors"
	"log/slog"
	"path/filepath"
	"time"
)

// ErrNotFound is returned by Get when the requested service/key pair does not
// exist in the store.
var ErrNotFound = errors.New("credential not found")

// ErrDecrypt is returned when the encrypted-file backend cannot decrypt the
// credentials file, typically because the encryption key has changed (different
// machine, rotated FEINO_CREDENTIALS_KEY) or the file is corrupt.
var ErrDecrypt = errors.New("credentials: decryption failed — re-authenticate or delete ~/.feino/credentials.enc")

// Store is a secure key-value store for sensitive credentials.
// Implementations must be safe for concurrent use.
type Store interface {
	// Get retrieves a credential value. Returns [ErrNotFound] when absent.
	Get(service, key string) (string, error)

	// Set stores or overwrites a credential.
	Set(service, key, value string) error

	// Delete removes a credential. Returns nil if the key did not exist.
	Delete(service, key string) error

	// Clear removes all credentials stored under service in one atomic
	// operation. Returns nil when no credentials exist for the service.
	Clear(service string) error

	// List returns all key names stored under service, sorted lexicographically.
	// Returns nil (not an error) when no keys exist for the service.
	List(service string) ([]string, error)

	// Kind returns a short human-readable label identifying the backend,
	// e.g. "os-keyring" or "encrypted-file". Useful for status displays.
	Kind() string
}

// New returns the most secure Store available in the current environment.
// dir is used as the parent directory for the encrypted-file fallback.
// The chosen backend is logged at debug level.
func New(dir string) Store {
	k := &keyringStore{}
	if k.available(3 * time.Second) {
		slog.Debug("credentials: using os-keyring backend")
		return k
	}
	path := filepath.Join(dir, "credentials.enc")
	slog.Debug("credentials: keyring unavailable, using encrypted-file backend", "path", path)
	return newEncryptedStore(path)
}

// NewEncryptedStore always returns an encrypted-file Store backed by path.
// The file is created on the first write; the parent directory is created with
// mode 0700 if it does not exist.
func NewEncryptedStore(path string) Store {
	return newEncryptedStore(path)
}
