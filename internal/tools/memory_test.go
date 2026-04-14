package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odinnordico/feino/internal/memory"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func tempMemStore(t *testing.T) memory.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := memory.NewFileStore(filepath.Join(dir, "memory.json"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return s
}

func runMemoryTool(store memory.Store, name string, params map[string]any) ToolResult {
	tools := NewMemoryTools(store, nil)
	for _, tool := range tools {
		if tool.GetName() == name {
			return tool.Run(params)
		}
	}
	panic("tool not found: " + name)
}

// ── memory_write ──────────────────────────────────────────────────────────────

func TestMemoryWrite_CreatesEntry(t *testing.T) {
	store := tempMemStore(t)
	res := runMemoryTool(store, "memory_write", map[string]any{
		"category": "profile",
		"content":  "User's name is Diego",
	})
	if res.GetError() != nil {
		t.Fatalf("unexpected error: %v", res.GetError())
	}

	var e memory.Entry
	content, _ := res.GetContent().(string)
	if err := json.Unmarshal([]byte(content), &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.ID == "" {
		t.Error("ID should be set")
	}
	if e.Category != memory.CategoryProfile {
		t.Errorf("category: want profile, got %q", e.Category)
	}
	if e.Content != "User's name is Diego" {
		t.Errorf("content mismatch: %q", e.Content)
	}
}

func TestMemoryWrite_InvalidCategory(t *testing.T) {
	store := tempMemStore(t)
	res := runMemoryTool(store, "memory_write", map[string]any{
		"category": "bogus",
		"content":  "something",
	})
	if res.GetError() == nil {
		t.Fatal("expected error for invalid category")
	}
}

func TestMemoryWrite_MissingContent(t *testing.T) {
	store := tempMemStore(t)
	res := runMemoryTool(store, "memory_write", map[string]any{"category": "note"})
	if res.GetError() == nil {
		t.Fatal("expected error for missing content")
	}
}

func TestMemoryWrite_MissingCategory(t *testing.T) {
	store := tempMemStore(t)
	res := runMemoryTool(store, "memory_write", map[string]any{"content": "hello"})
	if res.GetError() == nil {
		t.Fatal("expected error for missing category")
	}
}

func TestMemoryWrite_AllCategories(t *testing.T) {
	store := tempMemStore(t)
	for _, cat := range []string{"profile", "preference", "fact", "note"} {
		res := runMemoryTool(store, "memory_write", map[string]any{
			"category": cat,
			"content":  "test " + cat,
		})
		if res.GetError() != nil {
			t.Errorf("category %q: unexpected error: %v", cat, res.GetError())
		}
	}
}

// ── memory_list ───────────────────────────────────────────────────────────────

func TestMemoryList_Empty(t *testing.T) {
	store := tempMemStore(t)
	res := runMemoryTool(store, "memory_list", map[string]any{})
	if res.GetError() != nil {
		t.Fatalf("unexpected error: %v", res.GetError())
	}
	content, _ := res.GetContent().(string)
	var out map[string]any
	_ = json.Unmarshal([]byte(content), &out)
	if count, ok := out["count"].(float64); !ok || count != 0 {
		t.Errorf("want count=0, got %v", out["count"])
	}
}

func TestMemoryList_All(t *testing.T) {
	store := tempMemStore(t)
	_, _ = store.Write(memory.CategoryProfile, "name: Diego")
	_, _ = store.Write(memory.CategoryFact, "uses Linux")

	res := runMemoryTool(store, "memory_list", map[string]any{})
	content, _ := res.GetContent().(string)
	var out map[string]any
	_ = json.Unmarshal([]byte(content), &out)
	if count, _ := out["count"].(float64); count != 2 {
		t.Errorf("want count=2, got %v", out["count"])
	}
}

func TestMemoryList_FilterByCategory(t *testing.T) {
	store := tempMemStore(t)
	_, _ = store.Write(memory.CategoryProfile, "name: Diego")
	_, _ = store.Write(memory.CategoryPreference, "concise")
	_, _ = store.Write(memory.CategoryProfile, "tz: UTC-5")

	res := runMemoryTool(store, "memory_list", map[string]any{"category": "profile"})
	content, _ := res.GetContent().(string)
	var out map[string]any
	_ = json.Unmarshal([]byte(content), &out)
	if count, _ := out["count"].(float64); count != 2 {
		t.Errorf("want 2 profile entries, got %v", out["count"])
	}
}

func TestMemoryList_FilterByQuery(t *testing.T) {
	store := tempMemStore(t)
	_, _ = store.Write(memory.CategoryFact, "uses Go as primary language")
	_, _ = store.Write(memory.CategoryNote, "golang rocks")
	_, _ = store.Write(memory.CategoryPreference, "prefers Python")

	res := runMemoryTool(store, "memory_list", map[string]any{"query": "go"})
	content, _ := res.GetContent().(string)
	var out map[string]any
	_ = json.Unmarshal([]byte(content), &out)
	if count, _ := out["count"].(float64); count != 2 {
		t.Errorf("want 2 results for 'go', got %v", out["count"])
	}
}

func TestMemoryList_InvalidCategory(t *testing.T) {
	store := tempMemStore(t)
	res := runMemoryTool(store, "memory_list", map[string]any{"category": "bogus"})
	if res.GetError() == nil {
		t.Fatal("expected error for invalid category")
	}
}

// ── memory_update ─────────────────────────────────────────────────────────────

func TestMemoryUpdate_ChangesContent(t *testing.T) {
	store := tempMemStore(t)
	e, _ := store.Write(memory.CategoryProfile, "name: Alice")

	res := runMemoryTool(store, "memory_update", map[string]any{
		"id":      e.ID,
		"content": "name: Bob",
	})
	if res.GetError() != nil {
		t.Fatalf("unexpected error: %v", res.GetError())
	}

	content, _ := res.GetContent().(string)
	if !strings.Contains(content, "Bob") {
		t.Errorf("expected updated name in response, got: %s", content)
	}
}

func TestMemoryUpdate_NotFound(t *testing.T) {
	store := tempMemStore(t)
	res := runMemoryTool(store, "memory_update", map[string]any{
		"id":      "deadbeef",
		"content": "new content",
	})
	if res.GetError() == nil {
		t.Fatal("expected error for unknown ID")
	}
}

func TestMemoryUpdate_MissingID(t *testing.T) {
	store := tempMemStore(t)
	res := runMemoryTool(store, "memory_update", map[string]any{"content": "x"})
	if res.GetError() == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestMemoryUpdate_MissingContent(t *testing.T) {
	store := tempMemStore(t)
	e, _ := store.Write(memory.CategoryNote, "original")
	res := runMemoryTool(store, "memory_update", map[string]any{"id": e.ID})
	if res.GetError() == nil {
		t.Fatal("expected error for missing content")
	}
}

// ── memory_forget ─────────────────────────────────────────────────────────────

func TestMemoryForget_DeletesEntry(t *testing.T) {
	store := tempMemStore(t)
	e, _ := store.Write(memory.CategoryNote, "to forget")

	res := runMemoryTool(store, "memory_forget", map[string]any{"id": e.ID})
	if res.GetError() != nil {
		t.Fatalf("unexpected error: %v", res.GetError())
	}

	all, _ := store.All()
	if len(all) != 0 {
		t.Errorf("expected empty store after forget, got %d entries", len(all))
	}

	content, _ := res.GetContent().(string)
	if !strings.Contains(content, `"deleted":true`) {
		t.Errorf("expected deleted:true in response, got: %s", content)
	}
}

func TestMemoryForget_NotFound(t *testing.T) {
	store := tempMemStore(t)
	res := runMemoryTool(store, "memory_forget", map[string]any{"id": "deadbeef"})
	if res.GetError() == nil {
		t.Fatal("expected error for unknown ID")
	}
}

func TestMemoryForget_MissingID(t *testing.T) {
	store := tempMemStore(t)
	res := runMemoryTool(store, "memory_forget", map[string]any{})
	if res.GetError() == nil {
		t.Fatal("expected error for missing id")
	}
}

// ── NewMemoryTools ────────────────────────────────────────────────────────────

func TestNewMemoryTools_FourTools(t *testing.T) {
	store := tempMemStore(t)
	tools := NewMemoryTools(store, nil)
	if len(tools) != 4 {
		t.Errorf("want 4 memory tools, got %d", len(tools))
	}
}

func TestNewMemoryTools_Names(t *testing.T) {
	store := tempMemStore(t)
	names := map[string]bool{}
	for _, tool := range NewMemoryTools(store, nil) {
		names[tool.GetName()] = true
	}
	for _, want := range []string{"memory_write", "memory_list", "memory_update", "memory_forget"} {
		if !names[want] {
			t.Errorf("tool %q not found", want)
		}
	}
}

func TestNewMemoryTools_PermissionLevels(t *testing.T) {
	store := tempMemStore(t)
	wantLevels := map[string]int{
		"memory_write":  PermLevelWrite,
		"memory_list":   PermLevelRead,
		"memory_update": PermLevelWrite,
		"memory_forget": PermLevelWrite,
	}
	for _, tool := range NewMemoryTools(store, nil) {
		c, ok := tool.(Classified)
		if !ok {
			t.Errorf("%s: does not implement Classified", tool.GetName())
			continue
		}
		want, exists := wantLevels[tool.GetName()]
		if !exists {
			continue
		}
		if c.PermissionLevel() != want {
			t.Errorf("%s: want level %d, got %d", tool.GetName(), want, c.PermissionLevel())
		}
	}
}

// ── integration: write → list → update → forget ───────────────────────────────

func TestMemoryTools_FullRoundTrip(t *testing.T) {
	store := tempMemStore(t)

	// 1. Write
	writeRes := runMemoryTool(store, "memory_write", map[string]any{
		"category": "preference",
		"content":  "prefers dark themes",
	})
	if writeRes.GetError() != nil {
		t.Fatalf("write: %v", writeRes.GetError())
	}
	var written memory.Entry
	writeContent, _ := writeRes.GetContent().(string)
	_ = json.Unmarshal([]byte(writeContent), &written)

	// 2. List — should have 1
	listRes := runMemoryTool(store, "memory_list", map[string]any{})
	listContent, _ := listRes.GetContent().(string)
	var listOut map[string]any
	_ = json.Unmarshal([]byte(listContent), &listOut)
	if count, _ := listOut["count"].(float64); count != 1 {
		t.Fatalf("list after write: want 1, got %v", listOut["count"])
	}

	// 3. Update
	updateRes := runMemoryTool(store, "memory_update", map[string]any{
		"id":      written.ID,
		"content": "prefers light themes",
	})
	if updateRes.GetError() != nil {
		t.Fatalf("update: %v", updateRes.GetError())
	}

	// 4. Forget
	forgetRes := runMemoryTool(store, "memory_forget", map[string]any{"id": written.ID})
	if forgetRes.GetError() != nil {
		t.Fatalf("forget: %v", forgetRes.GetError())
	}

	// 5. List — should be empty
	listRes2 := runMemoryTool(store, "memory_list", map[string]any{})
	listContent2, _ := listRes2.GetContent().(string)
	var listOut2 map[string]any
	_ = json.Unmarshal([]byte(listContent2), &listOut2)
	if count, _ := listOut2["count"].(float64); count != 0 {
		t.Errorf("list after forget: want 0, got %v", listOut2["count"])
	}
}

// ── file store survives process restart ───────────────────────────────────────

func TestMemoryTools_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.json")

	store1, _ := memory.NewFileStore(path)
	runMemoryTool(store1, "memory_write", map[string]any{
		"category": "fact",
		"content":  "primary language is Go",
	})

	// Simulate restart — new store from same path.
	store2, _ := memory.NewFileStore(path)
	res := runMemoryTool(store2, "memory_list", map[string]any{})
	content, _ := res.GetContent().(string)
	if !strings.Contains(content, "primary language is Go") {
		t.Errorf("memory should survive restart, got: %s", content)
	}

	// Verify the file was written with 0600 permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file permissions: want 0600, got %04o", perm)
	}
}
