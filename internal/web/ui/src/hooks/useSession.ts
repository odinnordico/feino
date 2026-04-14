import { useEffect } from "react";
import { feinoClient } from "../client";
import { create } from "@bufbuild/protobuf";
import { GetSessionStateRequestSchema } from "../gen/feino/v1/feino_pb";
import { useChatStore } from "../store/chatStore";
import { useSessionStore } from "../store/sessionStore";

const POLL_INTERVAL_MS = 5000;

/** Poll GetSessionState every 5 s and sync into stores. Sets offline flag on failure. */
export function useSession() {
  const setAgentState = useChatStore((s) => s.setAgentState);
  const setBusy       = useChatStore((s) => s.setBusy);
  const setBypass     = useSessionStore((s) => s.setBypass);
  const setOffline    = useSessionStore((s) => s.setOffline);

  useEffect(() => {
    let cancelled = false;

    async function poll() {
      try {
        const res = await feinoClient.getSessionState(
          create(GetSessionStateRequestSchema, {})
        );
        if (cancelled) return;
        setOffline(false);
        setAgentState(res.reactState);
        setBusy(res.busy);
        if (res.bypassActive !== undefined) {
          setBypass(res.bypassActive, null, false);
        }
      } catch {
        if (!cancelled) setOffline(true);
      }
    }

    poll();
    const id = setInterval(poll, POLL_INTERVAL_MS);
    return () => { cancelled = true; clearInterval(id); };
  }, [setAgentState, setBusy, setBypass, setOffline]);
}
