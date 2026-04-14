package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// tempStore creates a FileStore backed by a temp file and registers cleanup.
func tempStore(t *testing.T) *FileStore {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.json")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return s
}

// ── Write ─────────────────────────────────────────────────────────────────────

func TestWrite_CreatesEntry(t *testing.T) {
	s := tempStore(t)
	e, err := s.Write(CategoryPreference, "prefers concise answers")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if e.ID == "" {
		t.Error("ID should not be empty")
	}
	if e.Category != CategoryPreference {
		t.Errorf("category: want %q, got %q", CategoryPreference, e.Category)
	}
	if e.Content != "prefers concise answers" {
		t.Errorf("content mismatch: %q", e.Content)
	}
	if e.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestWrite_PersistsToDisk(t *testing.T) {
	s := tempStore(t)
	_, _ = s.Write(CategoryFact, "uses Linux")

	// Re-open from same path.
	s2, _ := NewFileStore(s.path)
	all, err := s2.All()
	if err != nil {
		t.Fatalf("All after re-open: %v", err)
	}
	if len(all) != 1 || all[0].Content != "uses Linux" {
		t.Errorf("expected persisted entry, got %v", all)
	}
}

func TestWrite_EmptyContentRejected(t *testing.T) {
	s := tempStore(t)
	_, err := s.Write(CategoryNote, "   ")
	if err == nil {
		t.Fatal("expected error for empty content, got nil")
	}
}

func TestWrite_InvalidCategoryRejected(t *testing.T) {
	s := tempStore(t)
	_, err := s.Write("bogus", "something")
	if err == nil {
		t.Fatal("expected error for invalid category, got nil")
	}
}

func TestWrite_UniqueIDs(t *testing.T) {
	s := tempStore(t)
	ids := map[string]bool{}
	for i := range 20 {
		e, err := s.Write(CategoryNote, "entry")
		if err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
		if ids[e.ID] {
			t.Fatalf("duplicate ID %q on iteration %d", e.ID, i)
		}
		ids[e.ID] = true
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

func TestUpdate_ChangesContent(t *testing.T) {
	s := tempStore(t)
	before := time.Now().UTC()
	e, _ := s.Write(CategoryProfile, "name: Alice")

	updated, err := s.Update(e.ID, "name: Bob")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Content != "name: Bob" {
		t.Errorf("content: want 'name: Bob', got %q", updated.Content)
	}
	if !updated.UpdatedAt.After(before) {
		t.Error("UpdatedAt should be after the write baseline")
	}
	if updated.CreatedAt != e.CreatedAt {
		t.Error("CreatedAt should not change on update")
	}
}

func TestUpdate_NotFoundReturnsError(t *testing.T) {
	s := tempStore(t)
	_, err := s.Update("deadbeef", "new content")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdate_EmptyContentRejected(t *testing.T) {
	s := tempStore(t)
	e, _ := s.Write(CategoryNote, "original")
	_, err := s.Update(e.ID, "")
	if err == nil {
		t.Fatal("expected error for empty content, got nil")
	}
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestDelete_RemovesEntry(t *testing.T) {
	s := tempStore(t)
	e, _ := s.Write(CategoryFact, "uses Go")

	if err := s.Delete(e.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	all, _ := s.All()
	if len(all) != 0 {
		t.Errorf("expected empty store after delete, got %d entries", len(all))
	}
}

func TestDelete_NotFoundReturnsError(t *testing.T) {
	s := tempStore(t)
	if err := s.Delete("deadbeef"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDelete_MidListPreservesOthers(t *testing.T) {
	s := tempStore(t)
	a, _ := s.Write(CategoryNote, "alpha")
	b, _ := s.Write(CategoryNote, "beta")
	c, _ := s.Write(CategoryNote, "gamma")

	_ = s.Delete(b.ID)
	all, _ := s.All()
	if len(all) != 2 {
		t.Fatalf("want 2 entries, got %d", len(all))
	}
	if all[0].ID != a.ID || all[1].ID != c.ID {
		t.Errorf("wrong entries remain: %v", all)
	}
}

// ── All ───────────────────────────────────────────────────────────────────────

func TestAll_EmptyStoreReturnsNil(t *testing.T) {
	s := tempStore(t)
	all, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected empty, got %d entries", len(all))
	}
}

func TestAll_PreservesInsertionOrder(t *testing.T) {
	s := tempStore(t)
	contents := []string{"first", "second", "third"}
	for _, c := range contents {
		_, _ = s.Write(CategoryNote, c)
	}
	all, _ := s.All()
	for i, want := range contents {
		if all[i].Content != want {
			t.Errorf("[%d] want %q, got %q", i, want, all[i].Content)
		}
	}
}

// ── ByCategory ────────────────────────────────────────────────────────────────

func TestByCategory_Filters(t *testing.T) {
	s := tempStore(t)
	_, _ = s.Write(CategoryProfile, "name: Diego")
	_, _ = s.Write(CategoryPreference, "concise")
	_, _ = s.Write(CategoryProfile, "tz: UTC-5")

	profs, err := s.ByCategory(CategoryProfile)
	if err != nil {
		t.Fatalf("ByCategory: %v", err)
	}
	if len(profs) != 2 {
		t.Errorf("want 2 profile entries, got %d", len(profs))
	}
}

// ── Search ────────────────────────────────────────────────────────────────────

func TestSearch_CaseInsensitive(t *testing.T) {
	s := tempStore(t)
	_, _ = s.Write(CategoryFact, "Uses Go as primary language")
	_, _ = s.Write(CategoryNote, "golang rocks")
	_, _ = s.Write(CategoryPreference, "prefers Python")

	results, err := s.Search("go")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("want 2 results for 'go', got %d", len(results))
	}
}

func TestSearch_EmptyQueryReturnsAll(t *testing.T) {
	s := tempStore(t)
	for range 5 {
		_, _ = s.Write(CategoryNote, "entry")
	}
	results, _ := s.Search("")
	if len(results) != 5 {
		t.Errorf("want 5 results for empty query, got %d", len(results))
	}
}

func TestSearch_CategoryMatchable(t *testing.T) {
	s := tempStore(t)
	_, _ = s.Write(CategoryPreference, "like dark themes")
	_, _ = s.Write(CategoryFact, "works at Acme")

	results, _ := s.Search("preference")
	if len(results) != 1 {
		t.Errorf("want 1 result matching category 'preference', got %d", len(results))
	}
}

// ── FormatPrompt ──────────────────────────────────────────────────────────────

func TestFormatPrompt_EmptyReturnsEmpty(t *testing.T) {
	s := tempStore(t)
	out, err := s.FormatPrompt()
	if err != nil {
		t.Fatalf("FormatPrompt: %v", err)
	}
	if out != "" {
		t.Errorf("want empty string, got %q", out)
	}
}

func TestFormatPrompt_IncludesAllCategories(t *testing.T) {
	s := tempStore(t)
	_, _ = s.Write(CategoryProfile, "name: Diego")
	_, _ = s.Write(CategoryPreference, "concise replies")
	_, _ = s.Write(CategoryFact, "uses Linux")
	_, _ = s.Write(CategoryNote, "working on FEINO")

	out, err := s.FormatPrompt()
	if err != nil {
		t.Fatalf("FormatPrompt: %v", err)
	}
	for _, want := range []string{"[profile]", "[preference]", "[fact]", "[note]"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatPrompt: want %q in output, got:\n%s", want, out)
		}
	}
}

func TestFormatPrompt_GroupedByCategory(t *testing.T) {
	s := tempStore(t)
	// Insert out of category order.
	_, _ = s.Write(CategoryNote, "note one")
	_, _ = s.Write(CategoryProfile, "profile one")
	_, _ = s.Write(CategoryFact, "fact one")
	_, _ = s.Write(CategoryPreference, "pref one")

	out, _ := s.FormatPrompt()
	lines := strings.Split(strings.TrimSpace(out), "\n")

	// Canonical order: profile, preference, fact, note.
	order := []string{"[profile]", "[preference]", "[fact]", "[note]"}
	for i, prefix := range order {
		if !strings.HasPrefix(lines[i], prefix) {
			t.Errorf("line %d: want prefix %q, got %q", i, prefix, lines[i])
		}
	}
}

// ── DefaultPath ───────────────────────────────────────────────────────────────

func TestDefaultPath_ContainsDotFeino(t *testing.T) {
	p, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if !strings.Contains(p, ".feino") {
		t.Errorf("expected .feino in path, got %q", p)
	}
	if !strings.HasSuffix(p, "memory.json") {
		t.Errorf("expected memory.json suffix, got %q", p)
	}
}

// ── ValidCategory ─────────────────────────────────────────────────────────────

func TestValidCategory(t *testing.T) {
	for _, c := range AllCategories() {
		if !ValidCategory(c) {
			t.Errorf("AllCategories contains invalid category %q", c)
		}
	}
	if ValidCategory("bogus") {
		t.Error("bogus should not be valid")
	}
}

// ── concurrent safety ─────────────────────────────────────────────────────────

func TestFileStore_ConcurrentWrites(t *testing.T) {
	s := tempStore(t)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			_, _ = s.Write(CategoryNote, "concurrent entry")
		})
	}
	wg.Wait()
	all, err := s.All()
	if err != nil {
		t.Fatalf("All after concurrent writes: %v", err)
	}
	if len(all) != 20 {
		t.Errorf("want 20 entries, got %d", len(all))
	}
}

// ── missing file ──────────────────────────────────────────────────────────────

func TestAll_MissingFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFileStore(filepath.Join(dir, "nonexistent.json"))
	all, err := s.All()
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("want empty, got %d entries", len(all))
	}
}

func TestNewFileStore_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	deepPath := filepath.Join(dir, "a", "b", "c", "memory.json")
	s, err := NewFileStore(deepPath)
	if err != nil {
		t.Fatalf("NewFileStore with deep path: %v", err)
	}
	_, _ = s.Write(CategoryNote, "test")
	if _, err := os.Stat(filepath.Dir(deepPath)); err != nil {
		t.Errorf("directory not created: %v", err)
	}
}
