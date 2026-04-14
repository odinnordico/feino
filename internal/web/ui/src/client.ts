import { createConnectTransport } from "@connectrpc/connect-web";
import { createClient } from "@connectrpc/connect";
import { FeinoService } from "./gen/feino/v1/feino_pb";

export const transport = createConnectTransport({
  baseUrl: window.location.origin,
  useBinaryFormat: true,
});

export const feinoClient = createClient(FeinoService, transport);
