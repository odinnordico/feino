import { useCallback, useState } from "react";
import { feinoClient } from "../client";
import { create } from "@bufbuild/protobuf";
import {
  ListFilesRequestSchema,
  UploadFileRequestSchema,
  type FileEntry,
} from "../gen/feino/v1/feino_pb";
import { useToastStore } from "../store/toastStore";

export function useFiles() {
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [loading, setLoading]  = useState(false);

  const listFiles = useCallback(async (path = "", dirsOnly = false) => {
    setLoading(true);
    try {
      const res = await feinoClient.listFiles(
        create(ListFilesRequestSchema, { path, dirsOnly })
      );
      setEntries(res.entries);
      return res.entries;
    } catch (err) {
      useToastStore.getState().push("error", `Failed to list files: ${err instanceof Error ? err.message : String(err)}`);
      return [];
    } finally {
      setLoading(false);
    }
  }, []);

  const uploadFile = useCallback(async (file: File): Promise<string> => {
    try {
      const bytes = new Uint8Array(await file.arrayBuffer());
      const res = await feinoClient.uploadFile(
        create(UploadFileRequestSchema, { filename: file.name, content: bytes })
      );
      return res.token;
    } catch (err) {
      useToastStore.getState().push("error", `Failed to upload file: ${err instanceof Error ? err.message : String(err)}`);
      throw err;
    }
  }, []);

  return { entries, loading, listFiles, uploadFile };
}
