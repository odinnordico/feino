import { useBypass } from "../../hooks/useBypass";
import { Modal } from "../shared/Modal";
import { Button } from "../shared/Button";

const DURATIONS = [
  { label: "5 min",   sec: 300 },
  { label: "10 min",  sec: 600 },
  { label: "30 min",  sec: 1800 },
  { label: "Session", sec: 0, session: true },
];

interface YoloModalProps { onClose: () => void; }

export function YoloModal({ onClose }: YoloModalProps) {
  const { activate } = useBypass();

  async function select(sec: number, session = false) {
    await activate(sec, session);
    onClose();
  }

  return (
    <Modal title="⚡ Unsafe Mode" onClose={onClose}>
      <p style={{ color: "var(--color-text-dim)", fontSize: "var(--font-size-sm)", margin: "0 0 16px" }}>
        Select how long bypass mode stays active. Tool permissions will not be prompted.
      </p>
      <div style={{ display: "flex", gap: "10px", flexWrap: "wrap", marginBottom: "16px" }}>
        {DURATIONS.map((d) => (
          <Button key={d.label} variant="ghost" onClick={() => select(d.sec, d.session ?? false)}
            style={{ borderColor: "var(--color-yolo)", color: "var(--color-yolo)" }}>
            {d.label}
          </Button>
        ))}
      </div>
      <div style={{ textAlign: "right" }}>
        <Button variant="ghost" onClick={onClose}>Cancel</Button>
      </div>
    </Modal>
  );
}
