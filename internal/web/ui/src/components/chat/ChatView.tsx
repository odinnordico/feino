import { useState } from "react";
import { MessageList } from "./MessageList";
import { InputBar } from "./InputBar";
import { StatusBar } from "./StatusBar";
import { PermissionModal } from "./PermissionModal";
import { MetricsPanel } from "../metrics/MetricsPanel";
import { ChatModals, type ModalKind } from "./ChatModals";

export function ChatView() {
  const [modal, setModal] = useState<ModalKind>(null);

  return (
    <div style={{ display: "flex", height: "100%", overflow: "hidden" }}>
      {/* Main chat column */}
      <div style={{ display: "flex", flexDirection: "column", flex: 1, minWidth: 0 }}>
        <MessageList />
        <InputBar onOpenModal={setModal} />
        <StatusBar />
      </div>

      {/* Metrics sidebar */}
      <MetricsPanel />

      {/* Overlays */}
      <PermissionModal />
      <ChatModals open={modal} onClose={() => setModal(null)} />
    </div>
  );
}
