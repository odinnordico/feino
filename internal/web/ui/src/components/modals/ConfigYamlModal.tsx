import { useState, useEffect } from "react";
import { useConfig } from "../../hooks/useConfig";
import { Modal } from "../shared/Modal";
import { Button } from "../shared/Button";
import { Spinner } from "../shared/Spinner";

type ConfigYamlModalProps = { onClose: () => void; }

export function ConfigYamlModal({ onClose }: ConfigYamlModalProps) {
  const { getYAML } = useConfig();
  const [yaml, setYaml]     = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError]   = useState<string | null>(null);

  useEffect(() => {
    getYAML()
      .then((y) => { setYaml(y); setLoading(false); })
      .catch((err) => {
        setError(err instanceof Error ? err.message : String(err));
        setLoading(false);
      });
  }, [getYAML]);

  return (
    <Modal title="Config YAML" onClose={onClose}>
      {loading ? (
        <Spinner />
      ) : error ? (
        <div style={{ color: "var(--color-error)", fontSize: "var(--font-size-sm)", marginBottom: "14px" }}>
          Error loading config: {error}
        </div>
      ) : (
        <pre style={{
          background: "var(--color-surface-1)",
          border: "1px solid var(--color-border)",
          borderRadius: "var(--radius-sm)",
          padding: "12px",
          fontFamily: "var(--font-mono)",
          fontSize: "var(--font-size-xs)",
          color: "var(--color-text)",
          overflow: "auto",
          maxHeight: "400px",
          maxWidth: "600px",
          whiteSpace: "pre-wrap",
          wordBreak: "break-word",
          marginBottom: "14px",
        }}>
          {yaml || "(empty)"}
        </pre>
      )}
      <Button variant="ghost" onClick={onClose}>Close</Button>
    </Modal>
  );
}
