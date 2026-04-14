import { useCallback, useState } from "react";
import { feinoClient } from "../client";
import { create } from "@bufbuild/protobuf";
import {
  GetHistoryRequestSchema,
  ResetSessionRequestSchema,
  type HistoryMessage,
} from "../gen/feino/v1/feino_pb";
import { useChatStore } from "../store/chatStore";

export function useHistory() {
  const [messages, setMessages] = useState<HistoryMessage[]>([]);
  const [loading, setLoading] = useState(false);
  const resetStore = useChatStore((s) => s.reset);

  const loadHistory = useCallback(async () => {
    setLoading(true);
    try {
      const res = await feinoClient.getHistory(create(GetHistoryRequestSchema, {}));
      setMessages(res.messages);
      return res.messages;
    } finally {
      setLoading(false);
    }
  }, []);

  const resetSession = useCallback(async () => {
    await feinoClient.resetSession(create(ResetSessionRequestSchema, {}));
    resetStore();
    setMessages([]);
  }, [resetStore]);

  return { messages, loading, loadHistory, resetSession };
}
