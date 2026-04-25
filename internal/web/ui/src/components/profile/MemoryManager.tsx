import { useState, useEffect, useCallback } from "react";
import type { MemoryEntryProto } from "../../gen/feino/v1/feino_pb";
import { useMemory } from "../../hooks/useMemory";
import { Button } from "../shared/Button";
import { Badge } from "../shared/Badge";
import { MemoryRow } from "./MemoryRow";
import { SkeletonCard } from "../shared/Skeleton";
import { EmptyState } from "../shared/EmptyState";

const CATEGORIES = ["profile", "preference", "fact", "note"];

const categoryVariant: Record<string, "primary" | "accent" | "thinking" | "dim"> = {
  profile:    "primary",
  preference: "accent",
  fact:       "thinking",
  note:       "dim",
};

export function MemoryManager() {
  const [entries, setEntries]         = useState<MemoryEntryProto[]>([]);
  const [query, setQuery]             = useState("");
  const [newContent, setNewContent]   = useState("");
  const [newCat, setNewCat]           = useState("note");
  const [loading, setLoading]         = useState(true);
  const { listMemories, writeMemory } = useMemory();

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const items = await listMemories("", query);
      setEntries(items);
    } finally {
      setLoading(false);
    }
  }, [listMemories, query]);

  useEffect(() => { load(); }, [load]);

  async function addMemory() {
    if (!newContent.trim()) {return;}
    const entry = await writeMemory(newCat, newContent.trim());
    if (entry) { setEntries((e) => [entry, ...e]); setNewContent(""); }
  }

  const grouped = CATEGORIES.map((cat) => ({
    cat,
    items: entries.filter((e) => e.category === cat),
  })).filter((g) => g.items.length > 0);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "16px" }}>
      <div style={{ display: "flex", gap: "8px" }}>
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search memories…"
          style={{
            flex: 1,
            background: "var(--color-surface-2)",
            color: "var(--color-text)",
            border: "1px solid var(--color-border)",
            borderRadius: "var(--radius-sm)",
            padding: "6px 10px",
            fontFamily: "var(--font-sans)",
            fontSize: "var(--font-size-sm)",
            outline: "none",
          }}
        />
      </div>

      {/* Add new memory */}
      <div
        style={{
          background: "var(--color-surface-1)",
          border: "1px solid var(--color-border)",
          borderRadius: "var(--radius-md)",
          padding: "12px 14px",
          display: "flex",
          flexDirection: "column",
          gap: "8px",
        }}
      >
        <div style={{ display: "flex", gap: "8px" }}>
          {CATEGORIES.map((c) => (
            <button
              key={c}
              onClick={() => setNewCat(c)}
              aria-pressed={newCat === c}
              style={{
                padding: "2px 10px",
                borderRadius: "var(--radius-pill)",
                border: "1px solid",
                cursor: "pointer",
                fontFamily: "var(--font-mono)",
                fontSize: "var(--font-size-xs)",
                background: newCat === c ? "var(--color-primary-muted)" : "transparent",
                borderColor: newCat === c ? "var(--color-primary)" : "var(--color-border)",
                color: newCat === c ? "var(--color-primary)" : "var(--color-text-dim)",
              }}
            >
              {c}
            </button>
          ))}
        </div>
        <textarea
          value={newContent}
          onChange={(e) => setNewContent(e.target.value)}
          placeholder="Add a new memory…"
          rows={2}
          style={{
            background: "var(--color-surface-2)",
            color: "var(--color-text)",
            border: "1px solid var(--color-border)",
            borderRadius: "var(--radius-sm)",
            padding: "6px 10px",
            fontFamily: "var(--font-sans)",
            fontSize: "var(--font-size-sm)",
            resize: "vertical",
            outline: "none",
          }}
        />
        <Button variant="primary" onClick={addMemory} style={{ alignSelf: "flex-end" }}>
          Add Memory
        </Button>
      </div>

      {loading && (
        <div style={{ display: "flex", flexDirection: "column", gap: "8px" }}>
          {[1, 2, 3].map((i) => <SkeletonCard key={i} height="56px" />)}
        </div>
      )}

      {!loading && entries.length === 0 && (
        <EmptyState
          icon="◉"
          title="No memories yet"
          description="FEINO will remember things you tell it across conversations."
        />
      )}

      {!loading && grouped.map(({ cat, items }) => (
        <div key={cat}>
          <div style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "8px" }}>
            <Badge variant={categoryVariant[cat] ?? "dim"}>{cat}</Badge>
            <span style={{ color: "var(--color-text-faint)", fontSize: "var(--font-size-xs)" }}>
              {items.length} {items.length === 1 ? "entry" : "entries"}
            </span>
          </div>
          <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
            {items.map((entry) => (
              <MemoryRow
                key={entry.id}
                entry={entry}
                onDeleted={(id) => setEntries((e) => e.filter((x) => x.id !== id))}
                onUpdated={(updated) => setEntries((e) => e.map((x) => x.id === updated.id ? updated : x))}
              />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
