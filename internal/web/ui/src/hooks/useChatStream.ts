import { useCallback, useRef } from "react";
import { create } from "@bufbuild/protobuf";
import { ConnectError, Code } from "@connectrpc/connect";
import { feinoClient } from "../client";
import { SendMessageRequestSchema, CancelTurnRequestSchema } from "../gen/feino/v1/feino_pb";
import { useChatStore } from "../store/chatStore";
import type { RenderedMessage, ToolCallPart } from "../types/chat";

function newId(): string {
  return Math.random().toString(36).slice(2, 10);
}

export function useChatStream() {
  const abortRef = useRef<AbortController | null>(null);

  const addMessage            = useChatStore((s) => s.addMessage);
  const updateLastAssistant   = useChatStore((s) => s.updateLastAssistantMessage);
  const setBusy               = useChatStore((s) => s.setBusy);
  const setAgentState         = useChatStore((s) => s.setAgentState);
  const setPendingPermission  = useChatStore((s) => s.setPendingPermission);
  const setUsage              = useChatStore((s) => s.setUsage);
  const setQueuePosition      = useChatStore((s) => s.setQueuePosition);
  const busy                  = useChatStore((s) => s.busy);

  const sendMessage = useCallback(
    async (text: string) => {
      if (busy) {return;}

      // Add user message immediately.
      const userMsg: RenderedMessage = {
        id: newId(),
        role: "user",
        parts: [{ kind: "text", text }],
        timestamp: Date.now(),
      };
      addMessage(userMsg);

      // Placeholder assistant message.
      const assistantId = newId();
      const assistantMsg: RenderedMessage = {
        id: assistantId,
        role: "assistant",
        parts: [],
        timestamp: Date.now(),
      };
      addMessage(assistantMsg);
      setBusy(true);

      abortRef.current = new AbortController();

      try {
        const req = create(SendMessageRequestSchema, { text });
        const stream = feinoClient.sendMessage(req, {
          signal: abortRef.current.signal,
        });

        for await (const evt of stream) {
          const e = evt.event;
          if (!e.case) {continue;}

          switch (e.case) {
            case "partReceived": {
              const chunk = e.value.text ?? "";
              updateLastAssistant((msg) => {
                const parts = [...msg.parts];
                const last = parts[parts.length - 1];
                if (last && last.kind === "text") {
                  return {
                    ...msg,
                    parts: [...parts.slice(0, -1), { kind: "text" as const, text: last.text + chunk }],
                  };
                }
                return { ...msg, parts: [...parts, { kind: "text" as const, text: chunk }] };
              });
              break;
            }

            case "thoughtReceived": {
              const thought = e.value.text ?? "";
              updateLastAssistant((msg) => ({
                ...msg,
                parts: [...msg.parts, { kind: "thought" as const, text: thought }],
              }));
              break;
            }

            case "toolCall": {
              const tc = e.value;
              const part: ToolCallPart = {
                kind: "tool_call",
                callId: tc.callId,
                name: tc.name,
                arguments: tc.arguments,
              };
              updateLastAssistant((msg) => ({
                ...msg,
                parts: [...msg.parts, part],
              }));
              break;
            }

            case "toolResult": {
              const tr = e.value;
              updateLastAssistant((msg) => {
                const parts = msg.parts.map((p) => {
                  if (p.kind === "tool_call" && p.callId === tr.callId) {
                    return { ...p, result: tr.content, isError: tr.isError };
                  }
                  return p;
                });
                return { ...msg, parts };
              });
              break;
            }

            case "stateChanged":
              setAgentState(e.value.state);
              break;

            case "usageUpdated":
              setUsage(
                e.value.usage?.promptTokens ?? 0,
                e.value.usage?.completionTokens ?? 0
              );
              break;

            case "permissionRequest": {
              const pr = e.value;
              setPendingPermission({
                requestId: pr.requestId,
                toolName: pr.toolName,
                required: pr.required,
                allowed: pr.allowed,
              });
              break;
            }

            case "queuePosition":
              setQueuePosition(e.value.position);
              break;

            case "complete":
              setQueuePosition(0);
              break;

            case "error": {
              addMessage({
                id: newId(),
                role: "error",
                parts: [{ kind: "text", text: e.value.message }],
                timestamp: Date.now(),
              });
              setQueuePosition(0);
              break;
            }
          }
        }
      } catch (err: unknown) {
        const isAbort = err instanceof Error && err.name === "AbortError";
        const isConnectCancel = err instanceof ConnectError && err.code === Code.Canceled;
        if (!isAbort && !isConnectCancel) {
          addMessage({
            id: newId(),
            role: "error",
            parts: [{ kind: "text", text: String(err) }],
            timestamp: Date.now(),
          });
        }
      } finally {
        setBusy(false);
        setAgentState("idle");
        abortRef.current = null;
      }
    },
    [busy, addMessage, updateLastAssistant, setBusy, setAgentState, setPendingPermission, setUsage, setQueuePosition]
  );

  const cancel = useCallback(() => {
    abortRef.current?.abort();
    feinoClient.cancelTurn(create(CancelTurnRequestSchema, {})).catch(() => undefined);
  }, []);

  return { sendMessage, cancel };
}
