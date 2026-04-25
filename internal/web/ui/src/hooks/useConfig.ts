import { useCallback } from "react";
import { feinoClient } from "../client";
import { create } from "@bufbuild/protobuf";
import {
  GetConfigRequestSchema,
  UpdateConfigRequestSchema,
  GetConfigYAMLRequestSchema,
  type ConfigProto,
} from "../gen/feino/v1/feino_pb";
import { useConfigStore } from "../store/configStore";

export function useConfig() {
  const { config, dirty, setConfig, markClean } = useConfigStore();

  const loadConfig = useCallback(async () => {
    const res = await feinoClient.getConfig(create(GetConfigRequestSchema, {}));
    if (res.config) {setConfig(res.config);}
    return res.config ?? null;
  }, [setConfig]);

  const saveConfig = useCallback(async (cfg: ConfigProto) => {
    const res = await feinoClient.updateConfig(
      create(UpdateConfigRequestSchema, { config: cfg })
    );
    if (res.config) { setConfig(res.config); markClean(); }
    return res;
  }, [setConfig, markClean]);

  const getYAML = useCallback(async () => {
    const res = await feinoClient.getConfigYAML(create(GetConfigYAMLRequestSchema, {}));
    return res.yaml;
  }, []);

  return { config, dirty, loadConfig, saveConfig, getYAML };
}
