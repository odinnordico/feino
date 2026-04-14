package credentials

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/hkdf"
)

var (
	derivedKeyOnce  sync.Once
	derivedKeyCache [32]byte
)

// deriveKey returns a 32-byte AES key for the encrypted-file backend.
// The result is computed once and cached for the lifetime of the process.
//
// Priority:
//  1. FEINO_CREDENTIALS_KEY env var (base64-encoded 32 bytes, any base64
//     variant accepted) — intended for CI pipelines and Docker containers
//     where the machine ID is ephemeral.
//  2. Machine-specific secret derived via HKDF-SHA256 from the stable machine
//     identifier (see [machineSecret]).
func deriveKey() [32]byte {
	derivedKeyOnce.Do(func() {
		derivedKeyCache = computeKey()
	})
	return derivedKeyCache
}

// computeKey performs the actual key derivation without caching.
// It is called exactly once by deriveKey via sync.Once, and directly by tests
// that need to exercise env-override behaviour without fighting the cache.
func computeKey() [32]byte {
	if env := os.Getenv("FEINO_CREDENTIALS_KEY"); env != "" {
		// Accept any base64 variant: standard, URL-safe, raw (no padding).
		for _, enc := range []*base64.Encoding{
			base64.StdEncoding,
			base64.URLEncoding,
			base64.RawStdEncoding,
			base64.RawURLEncoding,
		} {
			decoded, err := enc.DecodeString(env)
			if err == nil && len(decoded) == 32 {
				var key [32]byte
				copy(key[:], decoded)
				return key
			}
		}
		// Invalid env var — fall through to machine derivation so the process
		// does not silently produce an all-zero key or panic.
	}

	r := hkdf.New(sha256.New,
		[]byte(machineSecret()),
		[]byte("feino-credentials-v1-salt"),
		[]byte("feino-credentials-v1-aes256gcm"),
	)
	var key [32]byte
	if _, err := io.ReadFull(r, key[:]); err != nil {
		// io.ReadFull on an HKDF reader never fails in practice; returning a
		// zero key is worse than panicking in a crypto context, but the
		// zero key is at least deterministic and will cause decryption failures
		// rather than silent data exposure.
		return [32]byte{}
	}
	return key
}

// machineSecret returns a stable, machine-specific string used as HKDF input.
// It is not a secret in the cryptographic sense — its value is predictable by
// anyone with access to the machine — but it ensures the encrypted file cannot
// be trivially moved and decrypted on a different host.
//
// The derivation tries several sources in priority order:
//  1. /etc/machine-id (systemd, most modern Linux distros)
//  2. /var/lib/dbus/machine-id (older Linux / DBus-only installs)
//  3. os.Hostname() + current user (universal fallback)
func machineSecret() string {
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if data, err := os.ReadFile(path); err == nil {
			if id := strings.TrimSpace(string(data)); id != "" {
				return id + ":" + currentUser()
			}
		}
	}
	host, _ := os.Hostname()
	return host + ":" + currentUser()
}

// currentUser returns a stable identifier for the current OS user.
func currentUser() string {
	for _, env := range []string{"USER", "USERNAME", "LOGNAME"} {
		if u := os.Getenv(env); u != "" {
			return u
		}
	}
	return fmt.Sprintf("uid:%d", os.Getuid())
}
