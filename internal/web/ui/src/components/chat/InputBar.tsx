import { useState, useRef, useCallback, useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { useChatStore } from "../../store/chatStore";
import { useChatStream } from "../../hooks/useChatStream";
import { useFiles } from "../../hooks/useFiles";
import { Button } from "../shared/Button";
import { Spinner } from "../shared/Spinner";
import { SlashCommandMenu } from "./SlashCommandMenu";
import { AtPathMenu } from "./AtPathMenu";
import type { ModalKind } from "./ChatModals";

const SLASH_NAVIGATE: Record<string, string> = {
  "/settings": "/settings",
  "/history":  "/history",
  "/profile":  "/profile",
};

const SLASH_MODAL: Record<string, ModalKind> = {
  "/yolo":   "yolo",
  "/theme":  "theme",
  "/lang":   "lang",
  "/config": "config-yaml",
};

type InputBarProps = {
  onOpenModal: (kind: ModalKind) => void;
}

export function InputBar({ onOpenModal }: InputBarProps) {
  const [text, setText]             = useState("");
  const [slashOpen, setSlashOpen]   = useState(false);
  const [slashIdx, setSlashIdx]     = useState(0);
  const [atOpen, setAtOpen]         = useState(false);
  const [atIdx, setAtIdx]           = useState(0);
  const [atToken, setAtToken]       = useState(""); // the @word at cursor
  const [atTokenStart, setAtTokenStart] = useState(0); // index in text where @word begins

  const busy            = useChatStore((s) => s.busy);
  const { sendMessage, cancel } = useChatStream();
  const { uploadFile, listFiles, entries: fileEntries } = useFiles();
  const textareaRef     = useRef<HTMLTextAreaElement>(null);
  const navigate        = useNavigate();

  // Detect slash query (entire input starts with /)
  const isSlash   = text.startsWith("/") && !text.includes(" ");
  const slashQuery = isSlash ? text.slice(1) : "";

  useEffect(() => {
    setSlashOpen(isSlash);
    setSlashIdx(0);
  }, [isSlash, text]);

  // Detect @word at cursor position
  function detectAtToken(val: string, cursor: number) {
    const before = val.slice(0, cursor);
    const match  = before.match(/@([\w./-]*)$/);
    if (match) {
      const word  = match[1];
      const start = cursor - match[0].length;
      setAtToken(word);
      setAtTokenStart(start);
      setAtOpen(true);
      setAtIdx(0);
      listFiles(word);
    } else {
      setAtOpen(false);
    }
  }

  const submit = useCallback(async () => {
    const msg = text.trim();
    if (!msg || busy) {return;}
    setText("");
    if (textareaRef.current) {textareaRef.current.style.height = "auto";}
    await sendMessage(msg);
  }, [text, busy, sendMessage]);

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (slashOpen) {
      if (e.key === "ArrowDown")              { e.preventDefault(); setSlashIdx((i) => i + 1); return; }
      if (e.key === "ArrowUp")                { e.preventDefault(); setSlashIdx((i) => Math.max(0, i - 1)); return; }
      if (e.key === "Escape")                 { setSlashOpen(false); return; }
      if (e.key === "Tab" || e.key === "Enter") { e.preventDefault(); return; } // handled by menu
    }
    if (atOpen) {
      if (e.key === "ArrowDown")              { e.preventDefault(); setAtIdx((i) => i + 1); return; }
      if (e.key === "ArrowUp")                { e.preventDefault(); setAtIdx((i) => Math.max(0, i - 1)); return; }
      if (e.key === "Escape")                 { setAtOpen(false); return; }
      if (e.key === "Tab" || e.key === "Enter") { e.preventDefault(); return; }
    }
    if (e.key === "Enter" && !e.shiftKey && !slashOpen && !atOpen) {
      e.preventDefault();
      submit().catch(console.error);
    }
  }

  function handleInput(e: React.ChangeEvent<HTMLTextAreaElement>) {
    const val    = e.target.value;
    const cursor = e.target.selectionStart ?? val.length;
    setText(val);
    const el = e.target;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 160)  }px`;
    if (!val.startsWith("/")) {detectAtToken(val, cursor);}
  }

  function handleSlashSelect(command: string) {
    setSlashOpen(false);
    setText("");
    const nav   = SLASH_NAVIGATE[command];
    const modal = SLASH_MODAL[command];
    if (nav)   { navigate(nav); return; }
    if (modal) { onOpenModal(modal); return; }
    if (command === "/reset") {
      // handled as a message so the backend processes it
      sendMessage(command);
      return;
    }
    setText(`${command  } `);
    textareaRef.current?.focus();
  }

  function handleAtSelect(entry: string) {
    // Replace the @token in text with the chosen path
    const before = text.slice(0, atTokenStart);
    const after  = text.slice(atTokenStart + atToken.length + 1); // +1 for @
    const newText = `${before  }@${entry}${  after}`;
    setText(newText);
    setAtOpen(false);
    textareaRef.current?.focus();
  }

  async function handleFileAttach(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) {return;}
    e.target.value = "";
    try {
      const token = await uploadFile(file);
      setText((t) => `${t  }@${token} `);
      textareaRef.current?.focus();
    } catch { /* ignore */ }
  }

  return (
    <div
      style={{
        padding: "10px 16px",
        borderTop: "1px solid var(--color-border-dim)",
        background: "var(--color-surface-1)",
        position: "relative",
      }}
    >
      {slashOpen && !atOpen && (
        <SlashCommandMenu
          query={slashQuery}
          onSelect={handleSlashSelect}
          selectedIdx={slashIdx}
        />
      )}

      {atOpen && fileEntries.length > 0 && (
        <AtPathMenu
          entries={fileEntries}
          onSelect={handleAtSelect}
          selectedIdx={atIdx % fileEntries.length}
        />
      )}

      <div style={{ display: "flex", gap: "10px", alignItems: "flex-end" }}>
        <label style={{ cursor: "pointer", color: "var(--color-text-dim)", fontSize: "1rem", alignSelf: "center", display: "flex" }} title="Attach file" aria-label="Attach file">
          📎
          <input type="file" hidden onChange={handleFileAttach} aria-hidden="true" />
        </label>

        <textarea
          ref={textareaRef}
          value={text}
          onChange={handleInput}
          onKeyDown={handleKeyDown}
          disabled={busy}
          aria-label="Message input"
          placeholder="Ask anything… (Shift+Enter for newline, / for commands, @path for files)"
          rows={1}
          style={{
            flex: 1,
            resize: "none",
            background: "var(--color-surface-2)",
            color: "var(--color-text)",
            border: "1px solid var(--color-border)",
            borderRadius: "var(--radius-md)",
            padding: "9px 12px",
            fontFamily: "var(--font-sans)",
            fontSize: "var(--font-size-base)",
            outline: "none",
            lineHeight: 1.5,
            overflow: "hidden",
            transition: "border-color var(--transition-fast), box-shadow var(--transition-fast)",
          }}
          onFocus={(e) => {
            e.currentTarget.style.borderColor = "var(--color-primary)";
            e.currentTarget.style.boxShadow   = "var(--glow-focus)";
          }}
          onBlur={(e) => {
            e.currentTarget.style.borderColor = "var(--color-border)";
            e.currentTarget.style.boxShadow   = "none";
          }}
        />

        {busy ? (
          <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
            <Spinner size={14} />
            <Button variant="danger" onClick={cancel} aria-label="Stop generation" style={{ whiteSpace: "nowrap" }}>Stop</Button>
          </div>
        ) : (
          <Button
            variant="primary"
            onClick={submit}
            disabled={!text.trim()}
            aria-label="Send message"
            style={{ whiteSpace: "nowrap", opacity: text.trim() ? 1 : 0.4 }}
          >
            Send ↵
          </Button>
        )}
      </div>
    </div>
  );
}
