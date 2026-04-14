import { useEffect } from "react";
import { feinoClient } from "../client";
import { create } from "@bufbuild/protobuf";
import { ConnectError, Code } from "@connectrpc/connect";
import { StreamMetricsRequestSchema } from "../gen/feino/v1/feino_pb";
import { useMetricsStore } from "../store/metricsStore";
import { toast } from "../store/toastStore";

const MAX_RETRIES    = 5;
const BASE_DELAY_MS  = 1000;

/** Subscribe to the StreamMetrics server-streaming RPC and push events into metricsStore.
 *  Reconnects with exponential backoff on transient errors. */
export function useMetrics() {
  const { pushLatency, pushTokens } = useMetricsStore();

  useEffect(() => {
    const abort = new AbortController();

    (async () => {
      let attempt = 0;
      while (!abort.signal.aborted) {
        try {
          const stream = feinoClient.streamMetrics(
            create(StreamMetricsRequestSchema, {}),
            { signal: abort.signal }
          );
          attempt = 0; // reset on successful connection
          for await (const evt of stream) {
            if (evt.latencyMs > 0) pushLatency(evt.latencyMs);
            if (evt.usage) {
              pushTokens(
                evt.usage.promptTokens,
                evt.usage.completionTokens,
                evt.usage.durationMs,
              );
            }
          }
        } catch (err) {
          if (abort.signal.aborted) break;
          const isCanceled = err instanceof ConnectError && err.code === Code.Canceled;
          if (isCanceled) break;

          attempt++;
          if (attempt >= MAX_RETRIES) {
            toast.error("Metrics stream unavailable — live metrics disabled.");
            break;
          }
          const delay = BASE_DELAY_MS * Math.pow(2, attempt - 1);
          await new Promise<void>((res) => {
            const t = setTimeout(res, delay);
            abort.signal.addEventListener("abort", () => { clearTimeout(t); res(); });
          });
        }
      }
    })();

    return () => abort.abort();
  }, [pushLatency, pushTokens]);
}
