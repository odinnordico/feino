import { useCallback } from "react";
import { feinoClient } from "../client";
import { create } from "@bufbuild/protobuf";
import {
  SetBypassModeRequestSchema,
  ClearBypassModeRequestSchema,
  GetBypassStateRequestSchema,
} from "../gen/feino/v1/feino_pb";
import { useSessionStore } from "../store/sessionStore";

export function useBypass() {
  const { bypassActive, bypassExpiry, bypassSession, setBypass, clearBypass } = useSessionStore();

  const activate = useCallback(async (durationSec: number, sessionLong = false) => {
    const res = await feinoClient.setBypassMode(
      create(SetBypassModeRequestSchema, { durationSec: BigInt(durationSec), sessionLong })
    );
    const expiry = res.expiresAt
      ? Number(res.expiresAt.seconds) * 1000
      : null;
    setBypass(true, expiry, res.sessionLong);
  }, [setBypass]);

  const deactivate = useCallback(async () => {
    await feinoClient.clearBypassMode(create(ClearBypassModeRequestSchema, {}));
    clearBypass();
  }, [clearBypass]);

  const refresh = useCallback(async () => {
    const res = await feinoClient.getBypassState(create(GetBypassStateRequestSchema, {}));
    const expiry = res.expiresAt
      ? Number(res.expiresAt.seconds) * 1000
      : null;
    setBypass(res.active, expiry, res.sessionLong);
  }, [setBypass]);

  return { bypassActive, bypassExpiry, bypassSession, activate, deactivate, refresh };
}
