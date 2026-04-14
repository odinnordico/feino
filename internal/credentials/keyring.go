package credentials

import (
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
)

// keyringApp is the application namespace written to the OS keyring.
// All entries are stored as (keyringApp+":"+service, key).
const keyringApp = "feino"

// keyringIndexKey is the synthetic key used to persist the list of stored keys
// per service, so that List() can enumerate them without OS-level enumeration
// APIs (which go-keyring does not expose).
const keyringIndexKey = "__keys__"

// keyringStore implements Store using the OS keyring via go-keyring.
// A sync.Mutex serialises all operations because the index read-modify-write
// sequence (Get index → mutate → Set index) must be atomic.
type keyringStore struct {
	mu sync.Mutex
}

func (s *keyringStore) Kind() string { return "os-keyring" }

// available probes the keyring by writing and deleting a test entry.
// It runs in a goroutine with a timeout so a missing daemon does not block.
func (s *keyringStore) available(timeout time.Duration) bool {
	type result struct{ err error }
	ch := make(chan result, 1)
	go func() {
		const svc, key = "feino-probe", "probe"
		err := keyring.Set(svc, key, "1")
		if err == nil {
			_ = keyring.Delete(svc, key)
		}
		ch <- result{err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.err == nil
	case <-timer.C:
		return false
	}
}

func (s *keyringStore) Get(service, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	val, err := keyring.Get(keyringApp+":"+service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return val, err
}

func (s *keyringStore) Set(service, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := keyring.Set(keyringApp+":"+service, key, value); err != nil {
		return err
	}
	return s.lockedIndexAdd(service, key)
}

func (s *keyringStore) Delete(service, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := keyring.Delete(keyringApp+":"+service, key); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return s.lockedIndexRemove(service, key)
}

func (s *keyringStore) Clear(service string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Read all keys for the service, then delete each one.
	keys, err := s.lockedList(service)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if delErr := keyring.Delete(keyringApp+":"+service, k); delErr != nil && !errors.Is(delErr, keyring.ErrNotFound) {
			return delErr
		}
	}
	// Remove the index entry itself.
	_ = keyring.Delete(keyringApp+":"+service, keyringIndexKey)
	return nil
}

func (s *keyringStore) List(service string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lockedList(service)
}

// ── locked helpers (caller must hold s.mu) ────────────────────────────────────

func (s *keyringStore) lockedList(service string) ([]string, error) {
	raw, err := keyring.Get(keyringApp+":"+service, keyringIndexKey)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []string
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		return nil, err
	}
	sort.Strings(keys)
	return keys, nil
}

func (s *keyringStore) lockedIndexAdd(service, key string) error {
	keys, err := s.lockedList(service)
	if err != nil {
		return err
	}
	if slices.Contains(keys, key) {
		return nil // already present
	}
	keys = append(keys, key)
	sort.Strings(keys)
	raw, _ := json.Marshal(keys)
	return keyring.Set(keyringApp+":"+service, keyringIndexKey, string(raw))
}

func (s *keyringStore) lockedIndexRemove(service, key string) error {
	keys, err := s.lockedList(service)
	if err != nil {
		return err
	}
	filtered := slices.DeleteFunc(keys, func(k string) bool { return k == key })
	if len(filtered) == 0 {
		_ = keyring.Delete(keyringApp+":"+service, keyringIndexKey)
		return nil
	}
	raw, _ := json.Marshal(filtered)
	return keyring.Set(keyringApp+":"+service, keyringIndexKey, string(raw))
}
