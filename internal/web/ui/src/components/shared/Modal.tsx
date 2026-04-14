import { type ReactNode, useEffect, useId, useRef } from "react";
import ReactDOM from "react-dom";

interface ModalProps {
  title: string;
  children: ReactNode;
  onClose?: () => void;
}

const FOCUSABLE = [
  "a[href]",
  "button:not([disabled])",
  "textarea:not([disabled])",
  "input:not([disabled])",
  "select:not([disabled])",
  "[tabindex]:not([tabindex='-1'])",
].join(",");

export function Modal({ title, children, onClose }: ModalProps) {
  const titleId   = useId();
  const dialogRef = useRef<HTMLDivElement>(null);

  // Close on Escape + focus trap on Tab/Shift+Tab
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") { onClose?.(); return; }
      if (e.key !== "Tab") return;
      const el = dialogRef.current;
      if (!el) return;
      const focusable = Array.from(el.querySelectorAll<HTMLElement>(FOCUSABLE));
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last  = focusable[focusable.length - 1];
      if (e.shiftKey) {
        if (document.activeElement === first) { e.preventDefault(); last.focus(); }
      } else {
        if (document.activeElement === last) { e.preventDefault(); first.focus(); }
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [onClose]);

  // Focus first focusable element on mount
  useEffect(() => {
    const el = dialogRef.current;
    if (!el) return;
    const first = el.querySelector<HTMLElement>(FOCUSABLE);
    first?.focus();
  }, []);

  return ReactDOM.createPortal(
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      style={{
        position: "fixed",
        inset: 0,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        background: "rgba(0,0,0,0.7)",
        zIndex: 100,
      }}
      onClick={onClose}
    >
      <div
        ref={dialogRef}
        style={{
          background: "var(--color-surface-2)",
          border: "1px solid var(--color-border)",
          borderRadius: "var(--radius-md)",
          padding: "24px",
          minWidth: "340px",
          maxWidth: "90vw",
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <h2
          id={titleId}
          style={{
            margin: "0 0 16px",
            color: "var(--color-primary)",
            fontFamily: "var(--font-mono)",
            fontSize: "1rem",
          }}
        >
          {title}
        </h2>
        {children}
      </div>
    </div>,
    document.body
  );
}
