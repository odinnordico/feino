package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// encryptedStore implements Store using a single AES-256-GCM encrypted file.
// The plaintext is a JSON-encoded [storeData] struct. Every write re-encrypts
// the entire file with a fresh random nonce (authenticated encryption makes
// partial writes undetectable). A sync.Mutex serialises concurrent access.
//
// The cipher.AEAD (GCM) is created once at construction time rather than on
// every call, avoiding the cost of cipher.NewGCM on the hot path.
//
// Decrypted data is cached in memory after the first successful load (read or
// write). The cache is replaced on every write so stale reads are impossible
// within the same process.
type encryptedStore struct {
	path string
	key  [32]byte
	gcm  cipher.AEAD
	mu   sync.RWMutex

	// in-memory cache — nil means "not loaded yet"
	cache *storeData
}

// storeData is the JSON structure serialised before encryption.
// v is a format version field reserved for future migrations.
type storeData struct {
	V    int                          `json:"v"`
	Data map[string]map[string]string `json:"data"` // service → key → value
}

func newEncryptedStore(path string) *encryptedStore {
	return newEncryptedStoreWithKey(path, deriveKey())
}

// newEncryptedStoreWithKey creates a store using an explicit key. Used by tests
// that need to exercise key-mismatch scenarios without relying on the
// process-wide sync.Once key cache.
func newEncryptedStoreWithKey(path string, key [32]byte) *encryptedStore {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		// AES-256 with a 32-byte key never fails; if it somehow does, the
		// store is unusable — surface the error on first operation via a
		// nil gcm guard in load/save.
		return &encryptedStore{path: path, key: key}
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return &encryptedStore{path: path, key: key}
	}
	return &encryptedStore{path: path, key: key, gcm: gcm}
}

func (s *encryptedStore) Kind() string { return "encrypted-file" }

func (s *encryptedStore) Get(service, key string) (string, error) {
	d, err := s.cachedRead()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", err
	}
	val, ok := d.Data[service][key]
	if !ok {
		return "", ErrNotFound
	}
	return val, nil
}

func (s *encryptedStore) Set(service, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Always read from disk for writes so cross-process changes are not lost.
	d, err := s.load()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if d.Data == nil {
		d = storeData{V: 1, Data: make(map[string]map[string]string)}
	}
	if d.Data[service] == nil {
		d.Data[service] = make(map[string]string)
	}
	d.Data[service][key] = value
	if err := s.save(d); err != nil {
		return err
	}
	s.cache = &d
	return nil
}

func (s *encryptedStore) Delete(service, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, err := s.load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // nothing to delete
		}
		return err
	}
	svc, ok := d.Data[service]
	if !ok {
		return nil // service not present — no write needed
	}
	if _, ok := svc[key]; !ok {
		return nil // key not present — no write needed
	}
	delete(svc, key)
	if len(svc) == 0 {
		delete(d.Data, service)
	}
	if err := s.save(d); err != nil {
		return err
	}
	s.cache = &d
	return nil
}

func (s *encryptedStore) Clear(service string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, err := s.load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if _, ok := d.Data[service]; !ok {
		return nil // service not present — no write needed
	}
	delete(d.Data, service)
	if err := s.save(d); err != nil {
		return err
	}
	s.cache = &d
	return nil
}

func (s *encryptedStore) List(service string) ([]string, error) {
	d, err := s.cachedRead()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	svc := d.Data[service]
	if len(svc) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(svc))
	for k := range svc {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys, nil
}

// ── cache helpers ─────────────────────────────────────────────────────────────

// cachedRead returns the in-memory cache when already populated (using a shared
// read lock), and falls back to a full write-lock disk load on first access.
func (s *encryptedStore) cachedRead() (storeData, error) {
	s.mu.RLock()
	if s.cache != nil {
		d := *s.cache
		s.mu.RUnlock()
		return d, nil
	}
	s.mu.RUnlock()

	// Cache is empty — take an exclusive lock to load from disk.
	s.mu.Lock()
	defer s.mu.Unlock()
	// Re-check after acquiring the write lock (another goroutine may have loaded).
	if s.cache != nil {
		return *s.cache, nil
	}
	d, err := s.load()
	if err != nil {
		return storeData{}, err
	}
	s.cache = &d
	return d, nil
}

// ── encryption / decryption ───────────────────────────────────────────────────

func (s *encryptedStore) load() (storeData, error) {
	if s.gcm == nil {
		return storeData{}, fmt.Errorf("credentials: cipher not initialised")
	}

	ciphertext, err := os.ReadFile(s.path)
	if err != nil {
		return storeData{}, err
	}

	ns := s.gcm.NonceSize()
	if len(ciphertext) < ns {
		return storeData{}, fmt.Errorf("credentials: file too short to contain a valid nonce")
	}
	nonce, body := ciphertext[:ns], ciphertext[ns:]
	plain, err := s.gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return storeData{}, fmt.Errorf("%w: %s", ErrDecrypt, err.Error())
	}

	var d storeData
	if err := json.Unmarshal(plain, &d); err != nil {
		return storeData{}, fmt.Errorf("credentials: decode: %w", err)
	}
	return d, nil
}

func (s *encryptedStore) save(d storeData) error {
	if s.gcm == nil {
		return fmt.Errorf("credentials: cipher not initialised")
	}

	plain, err := json.Marshal(d)
	if err != nil {
		return err
	}

	nonce := make([]byte, s.gcm.NonceSize())
	if _, readErr := io.ReadFull(rand.Reader, nonce); readErr != nil {
		return fmt.Errorf("credentials: generate nonce: %w", readErr)
	}

	ciphertext := s.gcm.Seal(nonce, nonce, plain, nil)

	// Ensure parent directory exists with tight permissions.
	dir := filepath.Dir(s.path)
	if mkdirErr := os.MkdirAll(dir, 0o700); mkdirErr != nil {
		return fmt.Errorf("credentials: create dir: %w", mkdirErr)
	}

	// Atomic write: temp file + rename so a crash never leaves a partial file.
	tmp, err := os.CreateTemp(dir, ".feino-creds-*")
	if err != nil {
		return fmt.Errorf("credentials: create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(ciphertext); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("credentials: write temp: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("credentials: chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("credentials: close temp: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("credentials: rename: %w", err)
	}
	return nil
}
