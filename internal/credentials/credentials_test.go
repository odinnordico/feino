package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// newTestStore always returns an encryptedStore in a temp directory so tests
// never touch the OS keyring and run identically in CI.
func newTestStore(t *testing.T) Store {
	t.Helper()
	return NewEncryptedStore(filepath.Join(t.TempDir(), "credentials.enc"))
}

// ── Get / Set ─────────────────────────────────────────────────────────────────

func TestSet_Get_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("gmail", "access_token", "ya29.abc"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get("gmail", "access_token")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "ya29.abc" {
		t.Errorf("want %q, got %q", "ya29.abc", got)
	}
}

func TestGet_ErrNotFound_MissingService(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get("nonexistent", "key")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestGet_ErrNotFound_MissingKey(t *testing.T) {
	s := newTestStore(t)
	_ = s.Set("spotify", "access_token", "tok")
	_, err := s.Get("spotify", "refresh_token")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestSet_Overwrites_Existing_Value(t *testing.T) {
	s := newTestStore(t)
	_ = s.Set("svc", "key", "first")
	_ = s.Set("svc", "key", "second")
	got, _ := s.Get("svc", "key")
	if got != "second" {
		t.Errorf("want %q, got %q", "second", got)
	}
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestDelete_RemovesKey(t *testing.T) {
	s := newTestStore(t)
	_ = s.Set("svc", "key", "val")
	if err := s.Delete("svc", "key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := s.Get("svc", "key")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound after delete, got %v", err)
	}
}

func TestDelete_Idempotent(t *testing.T) {
	s := newTestStore(t)
	// Deleting a key that was never set must not return an error.
	if err := s.Delete("svc", "no-such-key"); err != nil {
		t.Errorf("want nil, got %v", err)
	}
}

func TestDelete_RemovesServiceWhenEmpty(t *testing.T) {
	s := newTestStore(t)
	_ = s.Set("svc", "only-key", "v")
	_ = s.Delete("svc", "only-key")
	keys, err := s.List("svc")
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("want empty list, got %v", keys)
	}
}

// ── List ──────────────────────────────────────────────────────────────────────

func TestList_ReturnsSortedKeys(t *testing.T) {
	s := newTestStore(t)
	_ = s.Set("svc", "z_key", "1")
	_ = s.Set("svc", "a_key", "2")
	_ = s.Set("svc", "m_key", "3")

	keys, err := s.List("svc")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"a_key", "m_key", "z_key"}
	if len(keys) != len(want) {
		t.Fatalf("want %v, got %v", want, keys)
	}
	for i, k := range keys {
		if k != want[i] {
			t.Errorf("index %d: want %q, got %q", i, want[i], k)
		}
	}
}

func TestList_NilForUnknownService(t *testing.T) {
	s := newTestStore(t)
	keys, err := s.List("nobody")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if keys != nil {
		t.Errorf("want nil, got %v", keys)
	}
}

// ── Persistence ───────────────────────────────────────────────────────────────

func TestEncryptedStore_PersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.enc")

	s1 := NewEncryptedStore(path)
	_ = s1.Set("github", "token", "ghp_secret")

	// New instance from the same file and same derived key.
	s2 := NewEncryptedStore(path)
	got, err := s2.Get("github", "token")
	if err != nil {
		t.Fatalf("Get on second instance: %v", err)
	}
	if got != "ghp_secret" {
		t.Errorf("want %q, got %q", "ghp_secret", got)
	}
}

// ── Multi-service isolation ───────────────────────────────────────────────────

func TestMultipleServices_Isolated(t *testing.T) {
	s := newTestStore(t)
	_ = s.Set("gmail", "token", "gmail-tok")
	_ = s.Set("spotify", "token", "spotify-tok")

	g, _ := s.Get("gmail", "token")
	sp, _ := s.Get("spotify", "token")
	if g != "gmail-tok" {
		t.Errorf("gmail token: want %q, got %q", "gmail-tok", g)
	}
	if sp != "spotify-tok" {
		t.Errorf("spotify token: want %q, got %q", "spotify-tok", sp)
	}
}

// ── FEINO_CREDENTIALS_KEY override ───────────────────────────────────────────

func TestEncryptedStore_EnvKeyOverride(t *testing.T) {
	// Use computeKey() directly to bypass the process-wide sync.Once cache,
	// so the env var is honoured regardless of test execution order.
	t.Setenv("FEINO_CREDENTIALS_KEY", "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=")
	key := computeKey()

	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.enc")

	s := newEncryptedStoreWithKey(path, key)
	_ = s.Set("svc", "k", "v")

	// Another instance with the same key must decrypt successfully.
	s2 := newEncryptedStoreWithKey(path, key)
	got, err := s2.Get("svc", "k")
	if err != nil {
		t.Fatalf("Get with env key: %v", err)
	}
	if got != "v" {
		t.Errorf("want %q, got %q", "v", got)
	}
}

func TestEncryptedStore_WrongKeyReturnsErrDecrypt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.enc")

	// Use explicit keys to bypass the process-wide sync.Once key cache.
	// 32 bytes of 0x00 (key A) and 32 bytes of 0x01 (key B).
	var keyA, keyB [32]byte
	for i := range keyB {
		keyB[i] = 0x01
	}

	// Write with key A.
	s1 := newEncryptedStoreWithKey(path, keyA)
	_ = s1.Set("svc", "k", "v")

	// Read with key B — must fail with ErrDecrypt.
	s2 := newEncryptedStoreWithKey(path, keyB)
	_, err := s2.Get("svc", "k")
	if !errors.Is(err, ErrDecrypt) {
		t.Errorf("want ErrDecrypt, got %v", err)
	}
}

// ── File permissions ──────────────────────────────────────────────────────────

func TestEncryptedStore_FileMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.enc")
	s := NewEncryptedStore(path)
	_ = s.Set("svc", "k", "v")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("want 0600, got %04o", perm)
	}
}

// ── Concurrency ───────────────────────────────────────────────────────────────

func TestEncryptedStore_ConcurrentAccess(t *testing.T) {
	s := newTestStore(t)
	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			key := "key"
			_ = s.Set("svc", key, "value")
			_, _ = s.Get("svc", key)
			if n%2 == 0 {
				_, _ = s.List("svc")
			}
		}(i)
	}
	wg.Wait()
	// If the race detector catches a data race, this test fails.
}

// ── Kind ──────────────────────────────────────────────────────────────────────

func TestEncryptedStore_Kind(t *testing.T) {
	s := newTestStore(t)
	if s.Kind() != "encrypted-file" {
		t.Errorf("want %q, got %q", "encrypted-file", s.Kind())
	}
}

// ── New() backend selection ───────────────────────────────────────────────────

func TestNew_FallsBackToEncryptedStore(t *testing.T) {
	// The keyring is unlikely to be available in a test runner; if it is, the
	// test is still valid — we just check that New returns a non-nil Store.
	dir := t.TempDir()
	s := New(dir)
	if s == nil {
		t.Fatal("New returned nil")
	}
	// Smoke-test the returned store.
	if err := s.Set("test", "probe", "1"); err != nil {
		t.Fatalf("Set on New() store: %v", err)
	}
	v, err := s.Get("test", "probe")
	if err != nil {
		t.Fatalf("Get on New() store: %v", err)
	}
	if v != "1" {
		t.Errorf("want %q, got %q", "1", v)
	}
	_ = s.Delete("test", "probe")
}

// ── Clear ─────────────────────────────────────────────────────────────────────

func TestClear_RemovesAllKeys(t *testing.T) {
	s := newTestStore(t)
	_ = s.Set("svc", "k1", "v1")
	_ = s.Set("svc", "k2", "v2")
	if err := s.Clear("svc"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	keys, err := s.List("svc")
	if err != nil {
		t.Fatalf("List after Clear: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("want empty list after Clear, got %v", keys)
	}
	_, err = s.Get("svc", "k1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound after Clear, got %v", err)
	}
}

func TestClear_Idempotent(t *testing.T) {
	s := newTestStore(t)
	// Clearing a service that does not exist must not return an error.
	if err := s.Clear("nonexistent"); err != nil {
		t.Errorf("want nil, got %v", err)
	}
}

func TestClear_DoesNotAffectOtherServices(t *testing.T) {
	s := newTestStore(t)
	_ = s.Set("a", "key", "val-a")
	_ = s.Set("b", "key", "val-b")
	_ = s.Clear("a")
	got, err := s.Get("b", "key")
	if err != nil {
		t.Fatalf("Get after Clear of other service: %v", err)
	}
	if got != "val-b" {
		t.Errorf("want %q, got %q", "val-b", got)
	}
}

// ── Empty string value ────────────────────────────────────────────────────────

func TestSet_EmptyStringValue(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("svc", "key", ""); err != nil {
		t.Fatalf("Set empty value: %v", err)
	}
	got, err := s.Get("svc", "key")
	if err != nil {
		t.Fatalf("Get empty value: %v", err)
	}
	if got != "" {
		t.Errorf("want empty string, got %q", got)
	}
}

// ── deriveKey determinism ─────────────────────────────────────────────────────

func TestDeriveKey_Deterministic(t *testing.T) {
	k1 := deriveKey()
	k2 := deriveKey()
	if k1 != k2 {
		t.Error("deriveKey is not deterministic")
	}
}

// The following tests call computeKey() directly because deriveKey() caches
// its result via sync.Once — setting env vars after the first call has no
// effect on deriveKey(), but computeKey() always reads the current env.

func TestDeriveKey_EnvOverride_Length(t *testing.T) {
	// 32 bytes of 0x01 — non-zero, valid base64.
	t.Setenv("FEINO_CREDENTIALS_KEY", "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=")
	k := computeKey()
	var zero [32]byte
	if k == zero {
		t.Error("env key produced all-zero key")
	}
}

func TestDeriveKey_URLSafeBase64_Accepted(t *testing.T) {
	// 32 bytes of 0xFB encodes to a string containing '-' and '_', which are
	// URL-safe base64 characters. base64.StdEncoding rejects '-' and '_', so
	// this value can only be decoded by URLEncoding or RawURLEncoding — it
	// exercises the multi-variant fallback in computeKey.
	//
	// Verified: base64.URLEncoding.EncodeToString(bytes.Repeat([]byte{0xFB}, 32))
	//         = "-_v7-_v7-_v7-_v7-_v7-_v7-_v7-_v7-_v7-_v7-_s="
	const urlSafeKey = "-_v7-_v7-_v7-_v7-_v7-_v7-_v7-_v7-_v7-_v7-_s="
	t.Setenv("FEINO_CREDENTIALS_KEY", urlSafeKey)

	k := computeKey()

	var want [32]byte
	for i := range want {
		want[i] = 0xFB
	}
	if k != want {
		t.Errorf("URL-safe base64 key: want [32]byte{0xFB...}, got %x", k)
	}
}

func TestDeriveKey_InvalidEnvFallsBackToMachine(t *testing.T) {
	t.Setenv("FEINO_CREDENTIALS_KEY", "not-valid-base64!!!")
	// Should not panic; returns machine-derived key.
	k := computeKey()
	var zero [32]byte
	if k == zero {
		// Zero is astronomically unlikely from HKDF; treat it as a bug.
		t.Error("deriveKey returned all-zero key")
	}
}

// ── No-op write optimisation ──────────────────────────────────────────────────

func TestDelete_DoesNotCreateFileWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.enc")
	s := NewEncryptedStore(path)

	// Delete a key that was never written — file must not be created.
	if err := s.Delete("svc", "key"); err != nil {
		t.Fatalf("Delete on empty store: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("Delete created the credentials file even though no data was present")
	}
}

func TestDelete_DoesNotWriteWhenKeyAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.enc")
	s := NewEncryptedStore(path)
	_ = s.Set("svc", "other", "val")

	info1, _ := os.Stat(path)
	// Delete a key that does not exist in an otherwise populated store.
	if err := s.Delete("svc", "nonexistent"); err != nil {
		t.Fatalf("Delete of absent key: %v", err)
	}
	info2, _ := os.Stat(path)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("Delete rewrote the file even though the key was absent")
	}
}

func TestClear_DoesNotCreateFileWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.enc")
	s := NewEncryptedStore(path)

	if err := s.Clear("svc"); err != nil {
		t.Fatalf("Clear on empty store: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("Clear created the credentials file even though no data was present")
	}
}
