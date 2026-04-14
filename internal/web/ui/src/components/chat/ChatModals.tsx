import { YoloModal } from "../modals/YoloModal";
import { ThemeModal } from "../modals/ThemeModal";
import { LangModal } from "../modals/LangModal";
import { ConfigYamlModal } from "../modals/ConfigYamlModal";

export type ModalKind = "yolo" | "theme" | "lang" | "config-yaml" | null;

interface ChatModalsProps {
  open: ModalKind;
  onClose: () => void;
}

export function ChatModals({ open, onClose }: ChatModalsProps) {
  if (!open) return null;

  switch (open) {
    case "yolo":       return <YoloModal onClose={onClose} />;
    case "theme":      return <ThemeModal onClose={onClose} />;
    case "lang":       return <LangModal onClose={onClose} />;
    case "config-yaml":return <ConfigYamlModal onClose={onClose} />;
    default:           return null;
  }
}
