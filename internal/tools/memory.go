package tools

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/odinnordico/feino/internal/memory"
)

// NewMemoryTools returns the four memory-management tools.
// store must not be nil; pass a memory.FileStore obtained from memory.NewFileStore.
func NewMemoryTools(store memory.Store, logger *slog.Logger) []Tool {
	return []Tool{
		newMemoryWriteTool(store, logger),
		newMemoryListTool(store, logger),
		newMemoryUpdateTool(store, logger),
		newMemoryForgetTool(store, logger),
	}
}

// ── memory_write ──────────────────────────────────────────────────────────────

func newMemoryWriteTool(store memory.Store, logger *slog.Logger) Tool {
	return NewTool(
		"memory_write",
		"Persist a new memory about the user. "+
			"Use this proactively when you learn the user's name, timezone, preferences, "+
			"or any facts that should be remembered across sessions. "+
			"Check memory_list first to avoid duplicates. "+
			"Categories: profile (identity), preference (how they like things done), "+
			"fact (environment / work context), note (free-form).",
		map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"category": map[string]any{
					"type":        "string",
					"description": `Category of the memory. One of: "profile", "preference", "fact", "note".`,
					"enum":        []string{"profile", "preference", "fact", "note"},
				},
				"content": map[string]any{
					"type":        "string",
					"description": "The memory text to store. Be specific and self-contained (e.g. \"User's name is Diego\", not \"his name\").",
				},
			},
			"required": []string{"category", "content"},
		},
		func(params map[string]any) ToolResult {
			catStr, ok := getString(params, "category")
			if !ok || catStr == "" {
				return NewToolResult("", fmt.Errorf("memory_write: category is required"))
			}
			cat := memory.Category(strings.ToLower(catStr))
			if !memory.ValidCategory(cat) {
				return NewToolResult("", fmt.Errorf("memory_write: unknown category %q; valid: profile, preference, fact, note", catStr))
			}

			content, ok := getString(params, "content")
			if !ok || strings.TrimSpace(content) == "" {
				return NewToolResult("", fmt.Errorf("memory_write: content is required"))
			}

			e, err := store.Write(cat, content)
			if err != nil {
				return NewToolResult("", fmt.Errorf("memory_write: %w", err))
			}

			safeLogger(logger).Debug("memory_write", "id", e.ID, "category", e.Category)

			out, _ := json.MarshalIndent(e, "", "  ")
			return NewToolResult(string(out), nil)
		},
		WithPermissionLevel(PermLevelWrite),
		WithLogger(logger),
	)
}

// ── memory_list ───────────────────────────────────────────────────────────────

func newMemoryListTool(store memory.Store, logger *slog.Logger) Tool {
	return NewTool(
		"memory_list",
		"List stored memories. Optionally filter by category or search for a keyword. "+
			"Returns entries with their IDs — use these IDs with memory_update or memory_forget.",
		map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"category": map[string]any{
					"type":        "string",
					"description": `Filter by category. One of: "profile", "preference", "fact", "note". Omit for all.`,
					"enum":        []string{"profile", "preference", "fact", "note"},
				},
				"query": map[string]any{
					"type":        "string",
					"description": "Case-insensitive substring filter applied to content and category. Omit for no filtering.",
				},
			},
		},
		func(params map[string]any) ToolResult {
			query := getStringDefault(params, "query", "")
			catStr := getStringDefault(params, "category", "")

			var entries []memory.Entry
			var err error

			switch {
			case catStr != "":
				cat := memory.Category(strings.ToLower(catStr))
				if !memory.ValidCategory(cat) {
					return NewToolResult("", fmt.Errorf("memory_list: unknown category %q", catStr))
				}
				entries, err = store.ByCategory(cat)
			case query != "":
				entries, err = store.Search(query)
			default:
				entries, err = store.All()
			}

			if err != nil {
				return NewToolResult("", fmt.Errorf("memory_list: %w", err))
			}

			safeLogger(logger).Debug("memory_list", "count", len(entries))

			type listItem struct {
				ID       string          `json:"id"`
				Category memory.Category `json:"category"`
				Content  string          `json:"content"`
			}
			items := make([]listItem, len(entries))
			for i, e := range entries {
				items[i] = listItem{ID: e.ID, Category: e.Category, Content: e.Content}
			}

			out, _ := json.MarshalIndent(map[string]any{
				"count":   len(items),
				"entries": items,
			}, "", "  ")
			return NewToolResult(string(out), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── memory_update ─────────────────────────────────────────────────────────────

func newMemoryUpdateTool(store memory.Store, logger *slog.Logger) Tool {
	return NewTool(
		"memory_update",
		"Replace the content of an existing memory entry by its ID. "+
			"Use this instead of forget + write when correcting or refining a fact. "+
			"The category cannot be changed; delete and re-create for that.",
		map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id": map[string]any{
					"type":        "string",
					"description": "The ID of the entry to update (from memory_list).",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "New content to replace the existing text.",
				},
			},
			"required": []string{"id", "content"},
		},
		func(params map[string]any) ToolResult {
			id, ok := getString(params, "id")
			if !ok || strings.TrimSpace(id) == "" {
				return NewToolResult("", fmt.Errorf("memory_update: id is required"))
			}
			content, ok := getString(params, "content")
			if !ok || strings.TrimSpace(content) == "" {
				return NewToolResult("", fmt.Errorf("memory_update: content is required"))
			}

			e, err := store.Update(id, content)
			if err != nil {
				return NewToolResult("", fmt.Errorf("memory_update: %w", err))
			}

			safeLogger(logger).Debug("memory_update", "id", e.ID)

			out, _ := json.MarshalIndent(e, "", "  ")
			return NewToolResult(string(out), nil)
		},
		WithPermissionLevel(PermLevelWrite),
		WithLogger(logger),
	)
}

// ── memory_forget ─────────────────────────────────────────────────────────────

func newMemoryForgetTool(store memory.Store, logger *slog.Logger) Tool {
	return NewTool(
		"memory_forget",
		"Permanently delete a memory entry by its ID. "+
			"Use memory_list to find the ID first. "+
			"Prefer memory_update when the content is wrong but the fact category is right.",
		map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id": map[string]any{
					"type":        "string",
					"description": "The ID of the entry to delete (from memory_list).",
				},
			},
			"required": []string{"id"},
		},
		func(params map[string]any) ToolResult {
			id, ok := getString(params, "id")
			if !ok || strings.TrimSpace(id) == "" {
				return NewToolResult("", fmt.Errorf("memory_forget: id is required"))
			}

			if err := store.Delete(id); err != nil {
				return NewToolResult("", fmt.Errorf("memory_forget: %w", err))
			}

			safeLogger(logger).Debug("memory_forget", "id", id)

			return NewToolResult(fmt.Sprintf(`{"deleted":true,"id":%q}`, id), nil)
		},
		WithPermissionLevel(PermLevelWrite),
		WithLogger(logger),
	)
}
