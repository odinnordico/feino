// Package memory provides a persistent store for agent-learned facts about the
// user. Entries are categorised (profile, preference, fact, note), stored as
// JSON at ~/.feino/memory.json, and injected into every system prompt so the
// agent remembers across sessions without the user repeating themselves.
package memory

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Category classifies what kind of information an entry holds.
type Category string

const (
	// CategoryProfile stores identity facts about the user:
	// name, timezone, preferred language, pronouns, etc.
	CategoryProfile Category = "profile"

	// CategoryPreference stores behavioural preferences:
	// "prefers bullet points", "always use metric units", etc.
	CategoryPreference Category = "preference"

	// CategoryFact stores discovered facts about the user's environment or
	// ongoing work: OS, shell, primary project, employer, etc.
	CategoryFact Category = "fact"

	// CategoryNote stores free-form notes that don't fit the other categories.
	CategoryNote Category = "note"
)

// allCategories is the canonical ordering used when rendering memories.
var allCategories = []Category{
	CategoryProfile,
	CategoryPreference,
	CategoryFact,
	CategoryNote,
}

// AllCategories returns the canonical category ordering as a new slice.
// The returned slice is safe to modify; changes do not affect the package.
func AllCategories() []Category {
	out := make([]Category, len(allCategories))
	copy(out, allCategories)
	return out
}

// ErrNotFound is returned by Update and Delete when the requested entry ID
// does not exist in the store.
var ErrNotFound = errors.New("memory: entry not found")

// ValidCategory returns true if c is a known category.
func ValidCategory(c Category) bool {
	return slices.Contains(allCategories, c)
}

// Entry is a single persisted memory record.
type Entry struct {
	ID        string    `json:"id"`
	Category  Category  `json:"category"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store is the interface for reading and writing agent memories.
type Store interface {
	// Write creates a new entry and returns it.
	Write(category Category, content string) (Entry, error)
	// Update replaces the content of an existing entry by ID.
	Update(id, content string) (Entry, error)
	// Delete removes the entry with the given ID.
	Delete(id string) error
	// All returns every entry in insertion order.
	All() ([]Entry, error)
	// ByCategory returns entries that match the given category.
	ByCategory(category Category) ([]Entry, error)
	// Search returns entries whose content or category contains query
	// (case-insensitive substring match).
	Search(query string) ([]Entry, error)
	// FormatPrompt renders all entries as a compact string suitable for
	// injection into the system prompt.
	FormatPrompt() (string, error)
}

// ── disk schema ───────────────────────────────────────────────────────────────

type diskData struct {
	Entries []Entry `json:"entries"`
}

// ── FileStore ─────────────────────────────────────────────────────────────────

// FileStore persists entries to a JSON file. It is safe for concurrent use.
type FileStore struct {
	path string
	mu   sync.RWMutex
}

// NewFileStore returns a FileStore backed by path.
// The parent directory is created with mode 0755 if it does not exist.
func NewFileStore(path string) (*FileStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("memory: create dir: %w", err)
	}
	return &FileStore{path: path}, nil
}

// DefaultPath returns the canonical memory store path: ~/.feino/memory.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("memory: home dir: %w", err)
	}
	return filepath.Join(home, ".feino", "memory.json"), nil
}

// load reads and parses the on-disk store. Caller must hold mu.
func (s *FileStore) load() (diskData, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return diskData{}, nil
		}
		return diskData{}, fmt.Errorf("memory: read %s: %w", s.path, err)
	}
	var dd diskData
	if err := json.Unmarshal(raw, &dd); err != nil {
		return diskData{}, fmt.Errorf("memory: parse %s: %w", s.path, err)
	}
	return dd, nil
}

// save atomically writes dd to disk. Caller must hold mu.
func (s *FileStore) save(dd diskData) error {
	data, err := json.MarshalIndent(dd, "", "  ")
	if err != nil {
		return fmt.Errorf("memory: marshal: %w", err)
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".feino-memory-*.tmp")
	if err != nil {
		return fmt.Errorf("memory: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("memory: chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("memory: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("memory: close temp: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("memory: rename: %w", err)
	}
	tmpName = "" // suppress deferred Remove — rename succeeded
	return nil
}

// newID returns a random 8-character hex string.
func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ── Store implementation ──────────────────────────────────────────────────────

func (s *FileStore) Write(category Category, content string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !ValidCategory(category) {
		return Entry{}, fmt.Errorf("memory: unknown category %q; valid: profile, preference, fact, note", category)
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return Entry{}, errors.New("memory: content must not be empty")
	}

	dd, err := s.load()
	if err != nil {
		return Entry{}, err
	}
	now := time.Now().UTC()
	e := Entry{
		ID:        newID(),
		Category:  category,
		Content:   content,
		CreatedAt: now,
		UpdatedAt: now,
	}
	dd.Entries = append(dd.Entries, e)
	if err := s.save(dd); err != nil {
		return Entry{}, err
	}
	return e, nil
}

func (s *FileStore) Update(id, content string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	content = strings.TrimSpace(content)
	if content == "" {
		return Entry{}, errors.New("memory: content must not be empty")
	}

	dd, err := s.load()
	if err != nil {
		return Entry{}, err
	}
	for i, e := range dd.Entries {
		if e.ID == id {
			dd.Entries[i].Content = content
			dd.Entries[i].UpdatedAt = time.Now().UTC()
			if err := s.save(dd); err != nil {
				return Entry{}, err
			}
			return dd.Entries[i], nil
		}
	}
	return Entry{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

func (s *FileStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dd, err := s.load()
	if err != nil {
		return err
	}
	idx := -1
	for i, e := range dd.Entries {
		if e.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	dd.Entries = slices.Delete(dd.Entries, idx, idx+1)
	return s.save(dd)
}

func (s *FileStore) All() ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dd, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Entry, len(dd.Entries))
	copy(out, dd.Entries)
	return out, nil
}

func (s *FileStore) ByCategory(category Category) ([]Entry, error) {
	all, err := s.All()
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range all {
		if e.Category == category {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *FileStore) Search(query string) ([]Entry, error) {
	all, err := s.All()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return all, nil
	}
	var out []Entry
	for _, e := range all {
		if strings.Contains(strings.ToLower(e.Content), q) ||
			strings.Contains(strings.ToLower(string(e.Category)), q) {
			out = append(out, e)
		}
	}
	return out, nil
}

// FormatPrompt renders all entries grouped by category, one line each.
// Returns an empty string when the store is empty.
func (s *FileStore) FormatPrompt() (string, error) {
	all, err := s.All()
	if err != nil {
		return "", err
	}
	if len(all) == 0 {
		return "", nil
	}

	cats := AllCategories()
	grouped := make(map[Category][]Entry, len(cats))
	for _, e := range all {
		grouped[e.Category] = append(grouped[e.Category], e)
	}

	var sb strings.Builder
	for _, cat := range cats {
		for _, e := range grouped[cat] {
			fmt.Fprintf(&sb, "[%s] %s\n", cat, e.Content)
		}
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}
