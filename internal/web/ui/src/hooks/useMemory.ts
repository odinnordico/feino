import { useCallback } from "react";
import { feinoClient } from "../client";
import { create } from "@bufbuild/protobuf";
import {
  ListMemoriesRequestSchema,
  WriteMemoryRequestSchema,
  UpdateMemoryRequestSchema,
  DeleteMemoryRequestSchema,
} from "../gen/feino/v1/feino_pb";

export function useMemory() {
  const listMemories = useCallback(async (category = "", query = "") => {
    const res = await feinoClient.listMemories(
      create(ListMemoriesRequestSchema, { category, query })
    );
    return res.entries;
  }, []);

  const writeMemory = useCallback(async (category: string, content: string) => {
    const res = await feinoClient.writeMemory(
      create(WriteMemoryRequestSchema, { category, content })
    );
    return res.entry;
  }, []);

  const updateMemory = useCallback(async (id: string, content: string) => {
    const res = await feinoClient.updateMemory(
      create(UpdateMemoryRequestSchema, { id, content })
    );
    return res.entry;
  }, []);

  const deleteMemory = useCallback(async (id: string) => {
    await feinoClient.deleteMemory(create(DeleteMemoryRequestSchema, { id }));
  }, []);

  return { listMemories, writeMemory, updateMemory, deleteMemory };
}
