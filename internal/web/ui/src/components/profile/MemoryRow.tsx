import { useState } from "react";
import type { MemoryEntryProto } from "../../gen/feino/v1/feino_pb";
import { useMemory } from "../../hooks/useMemory";
import { Button } from "../shared/Button";
import { Modal } from "../shared/Modal";

type MemoryRowProps = {
  entry: MemoryEntryProto;
  onDeleted: (id: string) => void;
  onUpdated: (entry: MemoryEntryProto) => void;
}

export function MemoryRow({ entry, onDeleted, onUpdated }: MemoryRowProps) {
  const [editing, setEditing]       = useState(false);
  const [draft, setDraft]           = useState(entry.content);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const { updateMemory, deleteMemory } = useMemory();

  async function save() {
    const updated = await updateMemory(entry.id, draft);
    if (updated) {onUpdated(updated);}
    setEditing(false);
  }

  async function remove() {
    await deleteMemory(entry.id);
    onDeleted(entry.id);
  }

  return (
    <div
      style={{
        background: "var(--color-surface-1)",
        border: "1px solid var(--color-border)",
        borderRadius: "var(--radius-md)",
        padding: "10px 14px",
        display: "flex",
        flexDirection: "column",
        gap: "8px",
      }}
    >
      {confirmOpen && (
        <Modal title="Delete Memory" onClose={() => setConfirmOpen(false)}>
          <p style={{ color: "var(--color-text)", fontSize: "var(--font-size-sm)", marginBottom: "16px" }}>
            Delete this memory?
          </p>
          <div style={{ display: "flex", gap: "8px", justifyContent: "flex-end" }}>
            <Button variant="ghost" onClick={() => setConfirmOpen(false)}>Cancel</Button>
            <Button variant="danger" onClick={() => { setConfirmOpen(false); remove().catch(console.error); }}>Yes, Delete</Button>
          </div>
        </Modal>
      )}

      {!editing ? (
        <div style={{ color: "var(--color-text)", fontSize: "var(--font-size-sm)" }}>
          {entry.content}
        </div>
      ) : (
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          rows={3}
          style={{
            background: "var(--color-surface-2)",
            color: "var(--color-text)",
            border: "1px solid var(--color-primary)",
            borderRadius: "var(--radius-sm)",
            padding: "6px 10px",
            fontFamily: "var(--font-sans)",
            fontSize: "var(--font-size-sm)",
            resize: "vertical",
            outline: "none",
          }}
        />
      )}

      <div style={{ display: "flex", gap: "8px", justifyContent: "flex-end" }}>
        {editing ? (
          <>
            <Button variant="ghost" onClick={() => setEditing(false)}>Cancel</Button>
            <Button variant="primary" onClick={save}>Save</Button>
          </>
        ) : (
          <>
            <Button variant="ghost" onClick={() => setEditing(true)}>Edit</Button>
            <Button variant="danger" onClick={() => setConfirmOpen(true)}>Delete</Button>
          </>
        )}
      </div>
    </div>
  );
}
