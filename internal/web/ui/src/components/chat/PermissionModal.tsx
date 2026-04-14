import { create } from "@bufbuild/protobuf";
import { feinoClient } from "../../client";
import { ResolvePermissionRequestSchema } from "../../gen/feino/v1/feino_pb";
import { useChatStore } from "../../store/chatStore";
import { Modal } from "../shared/Modal";
import { Button } from "../shared/Button";

export function PermissionModal() {
  const perm = useChatStore((s) => s.pendingPermission);
  const setPendingPermission = useChatStore((s) => s.setPendingPermission);

  if (!perm) return null;

  async function resolve(approved: boolean) {
    if (!perm) return;
    setPendingPermission(null);
    try {
      await feinoClient.resolvePermission(
        create(ResolvePermissionRequestSchema, {
          requestId: perm.requestId,
          approved,
        })
      );
    } catch {
      // best effort
    }
  }

  return (
    <Modal title="Permission Required">
      <p style={{ color: "var(--color-text)", fontSize: "var(--font-size-sm)", margin: "0 0 12px" }}>
        Tool <strong style={{ color: "var(--color-primary)" }}>{perm.toolName}</strong> is requesting
        permission.
      </p>
      {perm.required && (
        <div style={{ marginBottom: "8px", fontSize: "var(--font-size-xs)" }}>
          <span style={{ color: "var(--color-text-dim)" }}>Required: </span>
          <code style={{ color: "var(--color-accent)" }}>{perm.required}</code>
        </div>
      )}
      {perm.allowed && (
        <div style={{ marginBottom: "16px", fontSize: "var(--font-size-xs)" }}>
          <span style={{ color: "var(--color-text-dim)" }}>Allowed: </span>
          <code style={{ color: "var(--color-accent)" }}>{perm.allowed}</code>
        </div>
      )}
      <div role="group" aria-label="Permission decision" style={{ display: "flex", gap: "10px", justifyContent: "flex-end" }}>
        <Button variant="ghost" onClick={() => resolve(false)} aria-label={`Deny permission for ${perm.toolName}`}>
          Deny
        </Button>
        <Button variant="primary" onClick={() => resolve(true)} aria-label={`Allow permission for ${perm.toolName}`}>
          Allow
        </Button>
      </div>
    </Modal>
  );
}
