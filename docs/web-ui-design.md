# FEINO Web UI — Comprehensive Design Document

> Version 1.4 · April 2026  
> Status: Complete — All phases implemented

---

## Implementation Status

> Updated each iteration. Legend: ✅ done · 🔄 partial · ⬜ pending

### Go Backend (`internal/web/`)

| File                         | Status | Notes                                                   |
| ---------------------------- | ------ | ------------------------------------------------------- |
| `server.go`                  | ✅     | h2c, Connect handler, SPA, CORS, graceful shutdown      |
| `handler.go`                 | ✅     | All RPCs implemented                                    |
| `session_manager.go`         | ✅     | Fan-out, permission bridge, no channel-close race       |
| `metrics_hub.go`             | ✅     | Broadcast hub with stateMu for concurrent state changes |
| `config_mapper.go`           | ✅     | ConfigToProto / ProtoToConfig (API keys write-only)     |
| `history_mapper.go`          | ✅     | messagesToProto / partToHistoryPart                     |
| `event_mapper.go`            | ✅     | eventToProto for all event kinds                        |
| `file_service.go`            | ✅     | Upload, ListFiles, UUID token store                     |
| `embed.go` / `embed_stub.go` | ✅     | `//go:build web` + stub                                 |
| `build_session.go`           | ✅     | Session + SessionManager + metricsHub + fileService     |
| `spa_handler.go`             | ✅     | Falls back to index.html for SPA routing                |
| `handler_test.go`            | ✅     | 14 integration tests                                    |
| `atref.go`                   | ✅     | `@path` / `@token` expansion; wired into `SendMessage`  |

### RPC Methods

| RPC                       | Status |
| ------------------------- | ------ |
| `SendMessage` (streaming) | ✅     |
| `CancelTurn`              | ✅     |
| `ResolvePermission`       | ✅     |
| `GetSessionState`         | ✅     |
| `GetHistory`              | ✅     |
| `ResetSession`            | ✅     |
| `GetConfig`               | ✅     |
| `UpdateConfig`            | ✅     |
| `GetConfigYAML`           | ✅     |
| `ListMemories`            | ✅     |
| `WriteMemory`             | ✅     |
| `UpdateMemory`            | ✅     |
| `DeleteMemory`            | ✅     |
| `UploadFile`              | ✅     |
| `ListFiles`               | ✅     |
| `ReloadPlugins`           | ✅     |
| `SetBypassMode`           | ✅     |
| `ClearBypassMode`         | ✅     |
| `GetBypassState`          | ✅     |
| `SetLanguage`             | ✅     |
| `SetTheme`                | ✅     |
| `StreamMetrics`           | ✅     |

### React Frontend (`internal/web/ui/src/`)

#### Entry points & infrastructure

| File         | Status | Notes                                                |
| ------------ | ------ | ---------------------------------------------------- |
| `main.tsx`   | ✅     | BrowserRouter, fonts, highlight css                  |
| `App.tsx`    | ✅     | ThemeProvider + useRoutes                            |
| `client.ts`  | ✅     | Connect transport + feinoClient singleton            |
| `routes.tsx` | ✅     | All routes: `/`, `/history`, `/settings`, `/profile` |

#### Styles

| File                         | Status | Notes                                               |
| ---------------------------- | ------ | --------------------------------------------------- |
| `styles/neural-terminal.css` | ✅     | Full design-spec color tokens, transitions          |
| `styles/globals.css`         | ✅     | Tailwind + prose + animation keyframes + responsive |

#### Types

| File               | Status |
| ------------------ | ------ |
| `types/chat.ts`    | ✅     |
| `types/config.ts`  | ✅     |
| `types/metrics.ts` | ✅     |

#### Lib

| File               | Status |
| ------------------ | ------ |
| `lib/utils.ts`     | ✅     |
| `lib/markdown.ts`  | ✅     |
| `lib/highlight.ts` | ✅     |

#### Stores

| File                    | Status | Notes                                                 |
| ----------------------- | ------ | ----------------------------------------------------- |
| `store/chatStore.ts`    | ✅     | messages, busy, reactState, pendingPermission, tokens |
| `store/sessionStore.ts` | ✅     | metricsOpen, bypassExpiry, bypassSession, modelName   |
| `store/metricsStore.ts` | ✅     | latency/token rolling history                         |
| `store/configStore.ts`  | ✅     | config snapshot + dirty flag                          |

#### Hooks

| File                     | Status |
| ------------------------ | ------ |
| `hooks/useChatStream.ts` | ✅     |
| `hooks/useMetrics.ts`    | ✅     |
| `hooks/useConfig.ts`     | ✅     |
| `hooks/useMemory.ts`     | ✅     |
| `hooks/useHistory.ts`    | ✅     |
| `hooks/useSession.ts`    | ✅     |
| `hooks/useBypass.ts`     | ✅     |
| `hooks/useFiles.ts`      | ✅     |

#### Layout components

| File                                  | Status |
| ------------------------------------- | ------ |
| `components/layout/ThemeProvider.tsx` | ✅     |
| `components/layout/Header.tsx`        | ✅     |
| `components/layout/AppShell.tsx`      | ✅     |
| `components/layout/Sidebar.tsx`       | ✅     |
| `components/layout/MobileNav.tsx`     | ✅     |

#### Shared components

| File                                | Status |
| ----------------------------------- | ------ |
| `components/shared/Button.tsx`      | ✅     |
| `components/shared/Spinner.tsx`     | ✅     |
| `components/shared/Modal.tsx`       | ✅     |
| `components/shared/Badge.tsx`       | ✅     |
| `components/shared/Tooltip.tsx`     | ✅     |
| `components/shared/CodeBlock.tsx`   | ✅     |
| `components/shared/Collapsible.tsx` | ✅     |

#### Chat components

| File                                   | Status | Notes                                            |
| -------------------------------------- | ------ | ------------------------------------------------ |
| `components/chat/ChatView.tsx`         | ✅     | MetricsPanel toggle wired                        |
| `components/chat/MessageList.tsx`      | ✅     | streaming cursor passed to last assistant bubble |
| `components/chat/MessageBubble.tsx`    | ✅     | ThoughtBlock + ToolCallCard + MarkdownRenderer   |
| `components/chat/ThoughtBlock.tsx`     | ✅     | collapsible purple block                         |
| `components/chat/ToolCallCard.tsx`     | ✅     | collapsible with status badge                    |
| `components/chat/StreamingCursor.tsx`  | ✅     | blinking caret                                   |
| `components/chat/MarkdownRenderer.tsx` | ✅     | react-markdown wrapper                           |
| `components/chat/InputBar.tsx`         | ✅     | slash completions, file attach                   |
| `components/chat/SlashCommandMenu.tsx` | ✅     |                                                  |
| `components/chat/AtPathMenu.tsx`       | ✅     | @path autocomplete with live file listing        |
| `components/chat/MessageQueue.tsx`     | ✅     | queue position badge with pulse animation        |
| `components/chat/PermissionModal.tsx`  | ✅     |                                                  |
| `components/chat/StatusBar.tsx`        | ✅     | YOLO countdown, latency, token counts            |

#### Metrics components

| File                                      | Status |
| ----------------------------------------- | ------ |
| `components/metrics/MetricsPanel.tsx`     | ✅     |
| `components/metrics/LatencySparkline.tsx` | ✅     |
| `components/metrics/TokenBarChart.tsx`    | ✅     |

#### Settings components

| File                                        | Status | Notes                                                        |
| ------------------------------------------- | ------ | ------------------------------------------------------------ |
| `components/settings/SettingsPanel.tsx`     | ✅     | 6 tabs: Providers, Security, Agent, Context, Email, Advanced |
| `components/settings/ProviderSection.tsx`   | ✅     | All 5 providers extracted to own component                   |
| `components/settings/SecuritySection.tsx`   | ✅     | Permission level + allowed paths extracted                   |
| `components/settings/AgentSection.tsx`      | ✅     | Max retries + complexity thresholds extracted                |
| `components/settings/EmailSetupSection.tsx` | ✅     | IMAP/SMTP form with enabled toggle                           |

#### Profile components

| File                                   | Status |
| -------------------------------------- | ------ |
| `components/profile/ProfilePage.tsx`   | ✅     |
| `components/profile/MemoryManager.tsx` | ✅     |
| `components/profile/MemoryRow.tsx`     | ✅     |

#### History components

| File                                 | Status |
| ------------------------------------ | ------ |
| `components/history/HistoryView.tsx` | ✅     |

#### Modal components

| File                                    | Status |
| --------------------------------------- | ------ |
| `components/modals/YoloModal.tsx`       | ✅     |
| `components/modals/LangModal.tsx`       | ✅     |
| `components/modals/ThemeModal.tsx`      | ✅     |
| `components/modals/ConfigYamlModal.tsx` | ✅     |

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Design Principles](#2-design-principles)
3. [Visual Design System — Neural Terminal](#3-visual-design-system--neural-terminal)
4. [Architecture Overview](#4-architecture-overview)
5. [Transport Layer — Connect RPC](#5-transport-layer--connect-rpc)
6. [Proto Service Contract](#6-proto-service-contract)
7. [Go Server Layer (`internal/web/`)](#7-go-server-layer-internalweb)
8. [React Application Architecture (`web/`)](#8-react-application-architecture-web)
9. [Component Specifications](#9-component-specifications)
10. [State Management](#10-state-management)
11. [Routing and Navigation](#11-routing-and-navigation)
12. [Build Pipeline](#12-build-pipeline)
13. [Complete File Inventory](#13-complete-file-inventory)
14. [Dependency Manifest](#14-dependency-manifest)
15. [`cmd/feino/main.go` Changes](#15-cmdfeinomainogo-changes)
16. [Implementation Phases](#16-implementation-phases)
17. [Security Considerations](#17-security-considerations)
18. [Deployment Guide](#18-deployment-guide)
19. [TUI Feature Parity Checklist](#19-tui-feature-parity-checklist)

---

## 1. Executive Summary

The web UI adds a third runtime mode to the existing FEINO binary, alongside the Bubble Tea TUI (`tui.Run`) and the plain REPL (`repl.Run`). It is activated with the `--web` flag:

```bash
feino --web                          # localhost:3000
feino --web --web-port 8080          # custom port
feino --web --web-host 0.0.0.0       # expose on LAN / cloud
```

The React application is embedded directly into the compiled binary via `go:embed`, so no separate deployment of frontend assets is needed. A single binary can serve both the API and the UI.

**What is new:**

| Layer     | Technology                                                  |
| --------- | ----------------------------------------------------------- |
| Frontend  | React 18 + TypeScript + Vite + Tailwind CSS v4              |
| Transport | Connect RPC (bufbuild) — gRPC-compatible, browser-native    |
| Serving   | Embedded Go HTTP server, `h2c` (HTTP/2 cleartext)           |
| Streaming | Server-streaming RPC — one open stream per active chat turn |
| Schema    | Protobuf 3 (`proto/feino/v1/feino.proto`)                   |

**What is unchanged:**

- `internal/app/Session` — the core is driven identically to the TUI
- `internal/config/` — same config file, same load/merge/save pipeline
- `internal/memory/` — same memory store
- `internal/tools/` — same tools, same permission gate
- `internal/security/` — same gate levels, same callbacks

---

## 2. Design Principles

**1. Single binary, zero external deps for the UI.**  
`go:embed` packages the compiled React app. `feino --web` is enough; no Node.js runtime, no separate static file server.

**2. The UI is a thin adapter over `app.Session`.**  
Every feature the TUI supports works through the same `sess.Send` / `sess.Subscribe` / `sess.Cancel` API. The web layer adds no agent logic — it only translates between HTTP and the session.

**3. gRPC-compatible transport, no proxy.**  
Connect RPC speaks gRPC, gRPC-Web, and the Connect protocol from the same Go handler. Browsers use the Connect protocol over HTTP/1.1 + HTTP/2. No Envoy or grpcwebproxy sidecar is required. Native gRPC clients (e.g. a future mobile app) can connect directly.

**4. Streaming is first-class.**  
All agent events are delivered over a single long-lived server-streaming RPC (`SendMessage`). No polling. No SSE workarounds. The stream stays open for exactly one turn, then closes cleanly.

**5. Full feature parity with the TUI.**  
Every slash command, every overlay (yolo picker, language picker, theme picker), permission prompts, tool call display, thinking blocks, metrics sidebar, profile page, memory manager — all must be present in the web UI.

**6. Accessible to both non-technical and technical users.**  
The design uses clear iconography, readable labels, and friendly language. Nothing is hidden. Technical details (tool calls, metrics, ReAct state) are visible but collapsed by default, discoverable on demand.

**7. Responsive from mobile to widescreen.**  
Single layout that adapts: sidebar collapses to a bottom navigation bar on narrow screens; metrics panel becomes a drawer; input grows to fill available width.

---

## 3. Visual Design System — Neural Terminal

### 3.1 Design Philosophy

The aesthetic bridges two audiences: the non-technical user who appreciates a polished, dark-glass application, and the technical user who is drawn to the phosphor-green terminal heritage of the TUI. The result is called **Neural Terminal** — a clean, modern dark UI with neon-green life.

Key visual ideas:

- Near-black background with a subtle blue-black tint (not pure #000000, which feels flat)
- Neon green as the sole accent — reserved for interactive elements and active states
- Cyan as a secondary accent for links and informational highlights
- All text is rendered on surfaces rather than directly on the background
- Subtle glow (`box-shadow`) on interactive elements to reinforce the neon theme
- JetBrains Mono for all code, terminal output, and command-like elements; Inter for all prose
- Rounded corners (8px standard, 4px for small elements) — more approachable than sharp corners
- Micro-animations on state transitions (fade-in for new messages, pulse for streaming cursor)

### 3.2 Color Tokens

All values are defined as CSS custom properties in `web/src/styles/neural-terminal.css` and consumed by Tailwind via `@theme`.

```css
/* web/src/styles/neural-terminal.css */
:root {
  /* ── Backgrounds ─────────────────────────────────────────── */
  --color-bg: #050508; /* page background */
  --color-surface-1: #0d0d14; /* card / panel background */
  --color-surface-2: #14141f; /* elevated surfaces, dropdowns */
  --color-surface-3: #1a1a2e; /* highest elevation, tooltips */
  --color-border: #1e1e2e; /* subtle separators */
  --color-border-dim: #131320; /* ultra-subtle borders */

  /* ── Neon green (primary) ────────────────────────────────── */
  --color-primary: #00ff88; /* buttons, active states, cursor */
  --color-primary-dim: #00cc6a; /* hover states */
  --color-primary-muted: #00ff8833; /* transparent tint */
  --color-glow: rgba(0, 255, 136, 0.18); /* box-shadow glow */
  --color-glow-strong: rgba(0, 255, 136, 0.35); /* focused glow */

  /* ── Cyan (secondary accent) ────────────────────────────── */
  --color-accent: #00e5ff; /* links, info, secondary actions */
  --color-accent-dim: #00b8cc; /* hover for accent elements */

  /* ── Text ────────────────────────────────────────────────── */
  --color-text: #e8e8f0; /* primary text */
  --color-text-dim: #8888aa; /* secondary / muted text */
  --color-text-faint: #4a4a66; /* placeholder, disabled */
  --color-text-bright: #ffffff; /* headings, labels */
  --color-text-code: #00ff88; /* inline code */

  /* ── Semantic ────────────────────────────────────────────── */
  --color-error: #ff4466;
  --color-error-muted: rgba(255, 68, 102, 0.15);
  --color-warning: #ffaa00;
  --color-warning-muted: rgba(255, 170, 0, 0.15);
  --color-success: #00ff88; /* same as primary */
  --color-info: #00e5ff; /* same as accent */

  /* ── State colors ────────────────────────────────────────── */
  --color-yolo: #ff6b00; /* bypass/yolo mode indicator */
  --color-yolo-muted: rgba(255, 107, 0, 0.15);
  --color-thinking: #9b59ff; /* thought block accent */
  --color-thinking-muted: rgba(155, 89, 255, 0.12);
  --color-tool: #00e5ff; /* tool call card accent */
  --color-tool-muted: rgba(0, 229, 255, 0.1);

  /* ── Typography ──────────────────────────────────────────── */
  --font-mono:
    "JetBrains Mono", "Fira Code", "Cascadia Code", ui-monospace, monospace;
  --font-sans: "Inter", "Helvetica Neue", system-ui, sans-serif;
  --font-size-xs: 0.75rem; /* 12px */
  --font-size-sm: 0.875rem; /* 14px */
  --font-size-base: 1rem; /* 16px */
  --font-size-lg: 1.125rem; /* 18px */
  --font-size-xl: 1.25rem; /* 20px */

  /* ── Spacing ─────────────────────────────────────────────── */
  --radius-sm: 4px;
  --radius-md: 8px;
  --radius-lg: 12px;
  --radius-pill: 999px;

  /* ── Shadows / Glow ──────────────────────────────────────── */
  --shadow-sm: 0 1px 3px rgba(0, 0, 0, 0.4);
  --shadow-md: 0 4px 12px rgba(0, 0, 0, 0.5);
  --glow-primary: 0 0 12px var(--color-glow), 0 0 24px var(--color-glow);
  --glow-focus:
    0 0 0 2px var(--color-primary), 0 0 12px var(--color-glow-strong);
}

/* Light theme override */
[data-theme="light"] {
  --color-bg: #f4f4f8;
  --color-surface-1: #ffffff;
  --color-surface-2: #f0f0f5;
  --color-surface-3: #e8e8f0;
  --color-border: #d8d8e8;
  --color-primary: #008844;
  --color-primary-dim: #006633;
  --color-glow: rgba(0, 136, 68, 0.15);
  --color-text: #1a1a2e;
  --color-text-dim: #6666aa;
  --color-text-faint: #aaaacc;
  --color-text-bright: #000000;
  --color-text-code: #008844;
}
```

### 3.3 Typography Rules

| Use                  | Font           | Size        | Weight |
| -------------------- | -------------- | ----------- | ------ |
| Page headings        | Inter          | xl (20px)   | 600    |
| Section labels       | Inter          | sm (14px)   | 500    |
| Body text            | Inter          | base (16px) | 400    |
| Assistant messages   | Inter          | base        | 400    |
| User messages        | Inter          | base        | 400    |
| Command names, paths | JetBrains Mono | sm          | 400    |
| Tool names, IDs      | JetBrains Mono | xs          | 400    |
| Code blocks          | JetBrains Mono | sm          | 400    |
| Status bar           | JetBrains Mono | xs          | 400    |
| ReAct state          | JetBrains Mono | xs          | 500    |

### 3.4 Interactive States

Every clickable element follows a consistent state progression:

```
default  → hover     → active      → focus
#0d0d14  → #14141f   → #1a1a2e     → outline: 2px solid #00ff88
                                     box-shadow: glow-focus
```

Primary buttons (e.g. Send, Allow):

```
default  → hover          → active        → disabled
bg:primary → bg:primary-dim → bg:primary×0.8 → opacity: 0.35
text:bg    → text:bg        → text:bg        → cursor: not-allowed
glow-primary on hover and focus
```

### 3.5 Animation Tokens

```css
--transition-fast: 120ms cubic-bezier(0.4, 0, 0.2, 1);
--transition-base: 200ms cubic-bezier(0.4, 0, 0.2, 1);
--transition-slow: 350ms cubic-bezier(0.4, 0, 0.2, 1);
--transition-spring: 400ms cubic-bezier(0.34, 1.56, 0.64, 1); /* overshoot */
```

Streaming cursor: `@keyframes blink { 0%,100% { opacity:1 } 50% { opacity:0 } }` at 900ms.

New message fade-in: `@keyframes fadeSlideUp { from { opacity:0; transform:translateY(6px) } }` at 200ms.

---

## 4. Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────┐
│  feino binary (single process)                                          │
│                                                                         │
│  ┌──────────────────────────────────────────────────────────────────┐   │
│  │  cmd/feino/main.go                                               │   │
│  │  flag: --web, --web-port, --web-host                             │   │
│  └────────────────┬─────────────────────────────────────────────────┘   │
│                   │ web.Start(ctx, cfg, addr)                           │
│  ┌────────────────▼─────────────────────────────────────────────────┐   │
│  │  internal/web/server.go                                          │   │
│  │                                                                  │   │
│  │  http.ServeMux                                                   │   │
│  │   /feino.v1.FeinoService/*  → Connect handler                   │   │
│  │   /                         → go:embed static SPA               │   │
│  │                                                                  │   │
│  │  h2c.NewHandler (HTTP/2 cleartext)                               │   │
│  └──────┬──────────────────────┬────────────────────────────────────┘   │
│         │                      │                                        │
│  ┌──────▼──────┐    ┌──────────▼──────────────────────────────────┐     │
│  │ embed.FS    │    │  internal/web/handler.go                    │     │
│  │ web/dist    │    │  FeinoServiceHandler                        │     │
│  │ (React SPA) │    │                                             │     │
│  └─────────────┘    │  ┌─────────────────────────────────────┐   │     │
│                     │  │ SessionManager                      │   │     │
│                     │  │  • subscriber fan-out               │   │     │
│                     │  │  • permission bridge (chan bool)    │   │     │
│                     │  │  • metrics hub                      │   │     │
│                     │  └────────────┬────────────────────────┘   │     │
│                     └──────────────┼────────────────────────────┘     │
│                                    │                                   │
│  ┌─────────────────────────────────▼────────────────────────────────┐  │
│  │  internal/app/Session   (unchanged)                              │  │
│  │   sess.Send / Subscribe / Cancel / History / Reset / ...        │  │
│  └──────────────────────────────────────────────────────────────────┘  │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
          ▲
          │  Connect RPC (HTTP/2 or HTTP/1.1)
          │  Protobuf binary encoding
          ▼
┌─────────────────────────────────────────────────────┐
│  Browser (React SPA)                                │
│                                                     │
│  @connectrpc/connect-web transport                  │
│  Generated TypeScript client (feinoClient)          │
│                                                     │
│  Zustand stores ← useChatStream hook                │
│  ChatView, SettingsPanel, ProfilePage, ...          │
└─────────────────────────────────────────────────────┘
```

### 4.1 Runtime Modes Side-by-Side

| Aspect      | TUI                          | REPL               | Web                                      |
| ----------- | ---------------------------- | ------------------ | ---------------------------------------- |
| Entry       | `tui.Run(cfg)`               | `repl.Run(sess,…)` | `web.Start(ctx, cfg, addr)`              |
| Session     | `app.Session`                | `app.Session`      | `app.Session`                            |
| Events      | `prog.Send(SessionEventMsg)` | blocking loop      | `stream.Send(AgentEvent)`                |
| Permissions | inline y/n                   | inline y/n         | modal dialog via `ResolvePermission` RPC |
| Streaming   | Bubble Tea viewport          | line-by-line       | server-stream chunks                     |
| Config save | `config.Save`                | manual             | `config.Save` via `UpdateConfig` RPC     |

---

## 5. Transport Layer — Connect RPC

### 5.1 Why Connect, Not Raw gRPC-Web

Browsers cannot speak raw gRPC (HTTP/2 binary framing requires trailer support that browser Fetch API does not provide). Options were:

| Option           | Pros                                                       | Cons                                            |
| ---------------- | ---------------------------------------------------------- | ----------------------------------------------- |
| WebSocket        | Simple, bidirectional                                      | Custom framing, no type safety, manual TS types |
| SSE + REST       | HTTP-native, simple                                        | No backpressure, polling for bidirectional      |
| gRPC-Web + Envoy | True gRPC on the wire                                      | Requires Envoy proxy sidecar                    |
| **Connect RPC**  | gRPC-compatible, no proxy, browser-native, typed TS client | Minor wire format difference from raw gRPC      |

**Connect** (bufbuild) was chosen because:

1. The Go handler `connectrpc.com/connect` serves all three protocols from the same `http.Handler`: Connect (browser-friendly), gRPC, and gRPC-Web. No proxy.
2. `buf generate` produces both Go server interfaces and TypeScript client code from the same `.proto` file.
3. Server streaming (the primary need) is fully supported in browsers via the Connect protocol over HTTP/1.1 fetch with `ReadableStream`.
4. Native gRPC clients can connect directly when HTTP/2 is available (e.g. a future mobile app, Go integration tests, `grpcurl`).

### 5.2 HTTP/2 Cleartext (`h2c`)

TLS would add friction on localhost (self-signed cert browser warnings). The server uses `golang.org/x/net/http2/h2c` which upgrades HTTP/1.1 connections to HTTP/2 via the `h2c` mechanism. This is transparent to Connect clients.

For cloud/on-premise deployment, a reverse proxy (nginx, Caddy) terminates TLS and proxies to the h2c backend:

```nginx
# nginx example
location / {
    grpc_pass grpc://127.0.0.1:3000;
    # OR for HTTP:
    proxy_pass http://127.0.0.1:3000;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
}
```

### 5.3 CORS

When `--web-host` is `127.0.0.1` (default), CORS is not needed — browser and server share the same origin. When `--web-host 0.0.0.0` is used (LAN or cloud), the server adds permissive CORS headers. The Connect Go handler requires explicit CORS configuration via `connectrpc.com/connect/cors` helper:

```go
corsHandler := cors.NewHandler(mux, cors.Options{
    AllowedOrigins: origins, // ["*"] when --web-host != 127.0.0.1
    AllowedMethods: cors.AllowedMethods(),
    AllowedHeaders: cors.AllowedHeaders(),
    ExposedHeaders: cors.ExposedHeaders(),
})
```

---

## 6. Proto Service Contract

The complete protobuf schema lives at `proto/feino/v1/feino.proto`. Below is the full definition with all messages, grouped by feature area.

```protobuf
syntax = "proto3";
package feino.v1;
option go_package = "github.com/odinnordico/feino/gen/feino/v1;feinov1";

import "google/protobuf/timestamp.proto";
import "google/protobuf/struct.proto";

// ════════════════════════════════════════════════════════════════════
// Service
// ════════════════════════════════════════════════════════════════════

service FeinoService {
  // ── Chat ──────────────────────────────────────────────────────────
  // Send a user message; receive the agent turn as a stream of events.
  // The stream stays open until the turn completes (CompleteEvent or
  // ErrorEvent) or the client cancels. The server closes the stream
  // on completion; the client may cancel at any time.
  rpc SendMessage(SendMessageRequest) returns (stream AgentEvent);

  // Abort the currently in-flight agent turn.
  rpc CancelTurn(CancelTurnRequest) returns (CancelTurnResponse);

  // Unblock a pending permission prompt (blocking in the agent goroutine).
  // Must be called while a SendMessage stream is open and has emitted a
  // PermissionRequestEvent. The matching request_id ties the two calls.
  rpc ResolvePermission(ResolvePermissionRequest) returns (ResolvePermissionResponse);

  // Snapshot of session state (busy, queue depth, ReAct state, bypass).
  rpc GetSessionState(GetSessionStateRequest) returns (SessionStateResponse);

  // ── History ────────────────────────────────────────────────────────
  rpc GetHistory(GetHistoryRequest) returns (GetHistoryResponse);
  rpc ResetSession(ResetSessionRequest) returns (ResetSessionResponse);

  // ── Config ─────────────────────────────────────────────────────────
  rpc GetConfig(GetConfigRequest) returns (GetConfigResponse);
  rpc UpdateConfig(UpdateConfigRequest) returns (UpdateConfigResponse);
  rpc GetConfigYAML(GetConfigYAMLRequest) returns (GetConfigYAMLResponse);

  // ── Memory ─────────────────────────────────────────────────────────
  rpc ListMemories(ListMemoriesRequest) returns (ListMemoriesResponse);
  rpc WriteMemory(WriteMemoryRequest) returns (WriteMemoryResponse);
  rpc UpdateMemory(UpdateMemoryRequest) returns (UpdateMemoryResponse);
  rpc DeleteMemory(DeleteMemoryRequest) returns (DeleteMemoryResponse);

  // ── Metrics ────────────────────────────────────────────────────────
  // Long-lived server stream that pushes a MetricsEvent after each turn.
  rpc StreamMetrics(StreamMetricsRequest) returns (stream MetricsEvent);

  // ── File uploads ───────────────────────────────────────────────────
  // Upload a file from the browser; get back a server-side token that
  // can be passed as an @<token> reference in a subsequent SendMessage.
  rpc UploadFile(UploadFileRequest) returns (UploadFileResponse);

  // List files/dirs in the working directory (for @path autocomplete).
  rpc ListFiles(ListFilesRequest) returns (ListFilesResponse);

  // ── Commands ───────────────────────────────────────────────────────
  // Stateless commands that do not require a new agent turn.
  rpc ExecuteCommand(ExecuteCommandRequest) returns (ExecuteCommandResponse);

  // ── Plugins ────────────────────────────────────────────────────────
  rpc ReloadPlugins(ReloadPluginsRequest) returns (ReloadPluginsResponse);

  // ── Bypass (yolo) mode ─────────────────────────────────────────────
  rpc SetBypassMode(SetBypassModeRequest) returns (SetBypassModeResponse);
  rpc ClearBypassMode(ClearBypassModeRequest) returns (ClearBypassModeResponse);
  rpc GetBypassState(GetBypassStateRequest) returns (BypassStateResponse);

  // ── Language / Theme ───────────────────────────────────────────────
  rpc SetLanguage(SetLanguageRequest) returns (SetLanguageResponse);
  rpc SetTheme(SetThemeRequest) returns (SetThemeResponse);
}

// ════════════════════════════════════════════════════════════════════
// Chat messages
// ════════════════════════════════════════════════════════════════════

message SendMessageRequest {
  string text = 1;
  // Pre-resolved attachments. For @path references (typed by the user),
  // the server expands them server-side, so attachments can be empty.
  // For drag-and-drop uploads, the client sends the upload token from
  // UploadFileResponse; the server resolves it before calling sess.Send.
  repeated FileAttachment attachments = 2;
}

message FileAttachment {
  // Exactly one of path or upload_token must be set.
  string path         = 1; // server-side absolute path
  string upload_token = 2; // token from UploadFileResponse
}

message CancelTurnRequest  {}
message CancelTurnResponse {}

message ResolvePermissionRequest {
  string request_id = 1; // from PermissionRequestEvent.request_id
  bool   approved   = 2;
}
message ResolvePermissionResponse {}

message GetSessionStateRequest {}
message SessionStateResponse {
  bool   busy          = 1;
  int32  queue_length  = 2;
  string react_state   = 3; // "init"|"gather"|"act"|"verify"|"complete"|"failed"
  bool   bypass_active = 4;
}

// ════════════════════════════════════════════════════════════════════
// Agent event stream
// ════════════════════════════════════════════════════════════════════

// AgentEvent is the single message type delivered by SendMessage's server
// stream. One oneof ensures the client can switch on a single field.
message AgentEvent {
  oneof event {
    PartReceivedEvent      part_received      = 1;
    ThoughtReceivedEvent   thought_received   = 2;
    ToolCallEvent          tool_call          = 3;
    ToolResultEvent        tool_result        = 4;
    StateChangedEvent      state_changed      = 5;
    UsageUpdatedEvent      usage_updated      = 6;
    CompleteEvent          complete           = 7;
    ErrorEvent             error              = 8;
    PermissionRequestEvent permission_request = 9;
    QueuePositionEvent     queue_position     = 10;
  }
}

// A streaming text token from the model.
message PartReceivedEvent {
  string text = 1;
}

// A streaming token that belongs inside a <thought>…</thought> block.
// The client renders it in the ThoughtBlock component separately.
message ThoughtReceivedEvent {
  string text = 1;
}

// The agent invoked a tool. Arrives before the tool executes.
message ToolCallEvent {
  string call_id   = 1; // correlates with ToolResultEvent.call_id
  string name      = 2; // tool name, e.g. "file_read"
  string arguments = 3; // JSON-encoded tool arguments
}

// The tool has finished executing.
message ToolResultEvent {
  string call_id  = 1;
  string name     = 2;
  string content  = 3; // tool output (may be large)
  bool   is_error = 4; // true when the tool returned an error
}

// The ReAct state machine transitioned.
// Values: "init" | "gather" | "act" | "verify" | "complete" | "failed"
message StateChangedEvent {
  string state = 1;
}

// Token usage updated (may arrive multiple times per turn during streaming).
message UsageUpdatedEvent {
  UsageMetadata usage = 1;
}

// The turn completed successfully. final_text is the assembled text of the
// complete assistant message (same as the last contiguous text from
// PartReceivedEvents). Useful for clients that buffer the whole turn.
message CompleteEvent {
  string final_text = 1;
}

// A non-fatal error occurred during the turn. The stream closes after this.
message ErrorEvent {
  string message = 1;
  string code    = 2; // e.g. "context_cancelled", "provider_error"
}

// The agent is requesting permission to use a tool that exceeds the
// current permission level. The agent goroutine blocks until
// ResolvePermission is called with this request_id.
message PermissionRequestEvent {
  string request_id = 1; // opaque UUID; must be passed to ResolvePermission
  string tool_name  = 2; // e.g. "file_write"
  string required   = 3; // required permission level, e.g. "write"
  string allowed    = 4; // current permission level, e.g. "read"
}

// The client sent a message when the session was busy. This event
// reports the queue position. The turn will execute in order.
message QueuePositionEvent {
  int32 position  = 1; // 1-based position in the queue
  int32 queue_max = 2; // maximum queue depth (always 10)
}

// ════════════════════════════════════════════════════════════════════
// History
// ════════════════════════════════════════════════════════════════════

message GetHistoryRequest {}
message GetHistoryResponse {
  repeated HistoryMessage messages = 1;
}

message ResetSessionRequest  {}
message ResetSessionResponse {}

message HistoryMessage {
  string role = 1; // "user" | "assistant" | "tool"
  repeated HistoryPart parts = 2;
  google.protobuf.Timestamp created_at = 3;
}

message HistoryPart {
  oneof content {
    string     text        = 1;
    ToolCall   tool_call   = 2;
    ToolResult tool_result = 3;
    string     thought     = 4;
  }
}

message ToolCall {
  string id        = 1;
  string name      = 2;
  string arguments = 3;
}

message ToolResult {
  string call_id  = 1;
  string name     = 2;
  string content  = 3;
  bool   is_error = 4;
}

// ════════════════════════════════════════════════════════════════════
// Config
// ════════════════════════════════════════════════════════════════════

message GetConfigRequest    {}
message GetConfigResponse   { ConfigProto config = 1; }
message UpdateConfigRequest { ConfigProto config = 1; }
message UpdateConfigResponse { ConfigProto config = 1; string message = 2; }
message GetConfigYAMLRequest {}
message GetConfigYAMLResponse { string yaml = 1; }

// ConfigProto mirrors config.Config. API keys are WRITE-ONLY: they are
// accepted in UpdateConfigRequest but always returned as an empty string
// in GetConfigResponse (never sent back to the browser).
message ConfigProto {
  ProvidersConfigProto   providers = 1;
  AgentConfigProto       agent     = 2;
  SecurityConfigProto    security  = 3;
  ContextConfigProto     context   = 4;
  UIConfigProto          ui        = 5;
  UserProfileConfigProto user      = 6;
  ServicesConfigProto    services  = 7;
}

message ProvidersConfigProto {
  AnthropicConfigProto    anthropic     = 1;
  OpenAIConfigProto       openai        = 2;
  GeminiConfigProto       gemini        = 3;
  OllamaConfigProto       ollama        = 4;
  OpenAICompatConfigProto openai_compat = 5;
}

message AnthropicConfigProto {
  string api_key       = 1; // write-only; empty in responses
  string default_model = 2;
  bool   has_api_key   = 3; // true when a key is stored (read-only)
}

message OpenAIConfigProto {
  string api_key       = 1; // write-only
  string base_url      = 2;
  string default_model = 3;
  bool   has_api_key   = 4;
}

message GeminiConfigProto {
  string api_key       = 1; // write-only
  string default_model = 2;
  bool   vertex        = 3;
  string project_id    = 4;
  string location      = 5;
  bool   has_api_key   = 6;
}

message OllamaConfigProto {
  string host          = 1;
  string default_model = 2;
}

message OpenAICompatConfigProto {
  string base_url      = 1;
  string api_key       = 2; // write-only
  string name          = 3;
  string default_model = 4;
  bool   disable_tools = 5;
  bool   has_api_key   = 6;
}

message AgentConfigProto {
  int32  max_retries               = 1;
  int32  high_complexity_threshold = 2;
  int32  low_complexity_threshold  = 3;
  string metrics_path              = 4;
}

message SecurityConfigProto {
  string          permission_level     = 1; // read|write|bash|danger_zone
  repeated string allowed_paths        = 2;
  bool            enable_ast_blacklist = 3;
}

message ContextConfigProto {
  string working_dir         = 1;
  string global_config_path  = 2;
  int32  max_budget          = 3;
  string plugins_dir         = 4;
}

message UIConfigProto {
  string theme     = 1; // dark|light|auto|neo
  string log_level = 2; // debug|info|warn|error
  string language  = 3; // BCP 47: en, es-419, pt-BR, …
}

message UserProfileConfigProto {
  string name                = 1;
  string timezone            = 2;
  string communication_style = 3; // concise|detailed|technical|friendly
}

message ServicesConfigProto {
  EmailServiceConfigProto email = 1;
}

message EmailServiceConfigProto {
  bool   enabled   = 1;
  string address   = 2;
  string imap_host = 3;
  int32  imap_port = 4;
  string smtp_host = 5;
  int32  smtp_port = 6;
  bool   has_password = 7; // write-only indicator
}

// ════════════════════════════════════════════════════════════════════
// Memory
// ════════════════════════════════════════════════════════════════════

message ListMemoriesRequest  {
  string category = 1; // empty = all; valid: profile|preference|fact|note
  string query    = 2; // optional full-text search
}
message ListMemoriesResponse { repeated MemoryEntryProto entries = 1; }

message WriteMemoryRequest  { string category = 1; string content = 2; }
message WriteMemoryResponse { MemoryEntryProto entry = 1; }

message UpdateMemoryRequest  { string id = 1; string content = 2; }
message UpdateMemoryResponse { MemoryEntryProto entry = 1; }

message DeleteMemoryRequest  { string id = 1; }
message DeleteMemoryResponse {}

message MemoryEntryProto {
  string   id         = 1;
  string   category   = 2;
  string   content    = 3;
  google.protobuf.Timestamp created_at = 4;
  google.protobuf.Timestamp updated_at = 5;
}

// ════════════════════════════════════════════════════════════════════
// Metrics
// ════════════════════════════════════════════════════════════════════

message StreamMetricsRequest {}

message MetricsEvent {
  UsageMetadata             usage       = 1;
  double                    latency_ms  = 2;
  string                    react_state = 3;
  google.protobuf.Timestamp timestamp   = 4;
}

message UsageMetadata {
  int32  prompt_tokens         = 1;
  int32  completion_tokens     = 2;
  int32  total_tokens          = 3;
  int32  cache_creation_tokens = 4;
  int32  cache_read_tokens     = 5;
  double duration_ms           = 6;
}

// ════════════════════════════════════════════════════════════════════
// Files
// ════════════════════════════════════════════════════════════════════

message UploadFileRequest {
  string filename = 1;
  bytes  content  = 2; // raw file bytes (max 10 MB enforced by server)
}

message UploadFileResponse {
  string token       = 1; // opaque UUID; use as @<token> in SendMessage
  string server_path = 2; // for display only
  int64  size_bytes  = 3;
}

message ListFilesRequest {
  string path   = 1; // relative to working_dir; empty = working_dir root
  bool   dirs_only = 2; // true = return directories only
}

message ListFilesResponse {
  repeated FileEntry entries = 1;
  string             base    = 2; // absolute path of the listed directory
}

message FileEntry {
  string name     = 1;
  bool   is_dir   = 2;
  int64  size     = 3;
  string rel_path = 4; // relative to working_dir
}

// ════════════════════════════════════════════════════════════════════
// Commands (stateless, no new agent turn)
// ════════════════════════════════════════════════════════════════════

// ExecuteCommand handles slash commands that do not start an agent turn.
// For commands that need structured output (history, config, profile),
// dedicated RPCs are preferred. ExecuteCommand is for simple actions.
message ExecuteCommandRequest {
  // Valid values: "clear" | "reset" | "history" | "config" | "profile"
  // "reload-plugins" | "email-setup" | "exit"
  string command = 1;
  // Command-specific parameters as a JSON object.
  // "yolo": {"duration_sec": 300}
  // "lang": {"code": "es-419"}
  // "theme": {"name": "dark"}
  google.protobuf.Struct args = 2;
}

message ExecuteCommandResponse {
  string          message  = 1; // human-readable confirmation
  bool            success  = 2;
  // Structured output for commands that return data.
  google.protobuf.Struct data = 3;
}

// ════════════════════════════════════════════════════════════════════
// Plugins
// ════════════════════════════════════════════════════════════════════

message ReloadPluginsRequest  {}
message ReloadPluginsResponse {
  int32           count   = 1;
  repeated string plugins = 2; // plugin names loaded
}

// ════════════════════════════════════════════════════════════════════
// Bypass (yolo) mode
// ════════════════════════════════════════════════════════════════════

message SetBypassModeRequest {
  bool  session_long = 1; // true = until process exit; overrides duration_sec
  int64 duration_sec = 2; // 300|600|1800 match TUI options
}
message SetBypassModeResponse {
  google.protobuf.Timestamp expires_at   = 1; // null when session_long=true
  bool                      session_long = 2;
}

message ClearBypassModeRequest  {}
message ClearBypassModeResponse {}

message GetBypassStateRequest {}
message BypassStateResponse {
  bool                      active       = 1;
  bool                      session_long = 2;
  google.protobuf.Timestamp expires_at   = 3;
}

// ════════════════════════════════════════════════════════════════════
// Language / Theme
// ════════════════════════════════════════════════════════════════════

message SetLanguageRequest  { string code  = 1; } // BCP 47
message SetLanguageResponse { string code  = 1; string label = 2; }

message SetThemeRequest     { string theme = 1; } // dark|light|auto|neo
message SetThemeResponse    { string theme = 1; }
```

---

## 7. Go Server Layer (`internal/web/`)

### 7.1 Package Structure

```
internal/web/
├── server.go           HTTP server setup, routing, lifecycle
├── handler.go          FeinoServiceHandler — all RPC method implementations
├── session_manager.go  Session event fan-out and permission bridge
├── metrics_hub.go      Broadcast hub for StreamMetrics subscribers
├── config_mapper.go    config.Config ↔ ConfigProto bidirectional conversion
├── file_service.go     UploadFile, ListFiles, upload token store
├── atref.go            @path and @token expansion (mirrors tui/chat/expand.go)
├── embed.go            go:embed directive for web/dist
└── build_session.go    Session construction (mirrors tui.Run's session setup)
```

### 7.2 `server.go` — HTTP Server

```go
// internal/web/server.go

package web

import (
    "context"
    "fmt"
    "net/http"

    "connectrpc.com/connect"
    "connectrpc.com/connect/cors"
    "golang.org/x/net/http2"
    "golang.org/x/net/http2/h2c"

    feinov1connect "github.com/odinnordico/feino/gen/feino/v1/feinov1connect"
    "github.com/odinnordico/feino/internal/app"
    "github.com/odinnordico/feino/internal/config"
    "github.com/odinnordico/feino/internal/credentials"
    "github.com/odinnordico/feino/internal/memory"
)

// Options controls server behaviour.
type Options struct {
    Host      string        // bind address; default "127.0.0.1"
    Port      int           // TCP port; default 3000
    AllowCORS bool          // true when host is not 127.0.0.1
}

// Start constructs the session, registers Connect handlers, and blocks
// until ctx is cancelled or the listener fails.
func Start(ctx context.Context, cfg config.Config, opts Options) error {
    sess, store, memStore, cfgPath, err := BuildSession(cfg)
    if err != nil {
        return fmt.Errorf("web: build session: %w", err)
    }

    sm  := NewSessionManager(sess)
    hub := NewMetricsHub()

    // Subscribe the metrics hub to session events once.
    sess.Subscribe(func(e app.Event) { hub.Publish(e) })

    h := &FeinoServiceHandler{
        sess:     sess,
        sm:       sm,
        hub:      hub,
        mem:      memStore,
        store:    store,
        cfg:      cfg,
        cfgPath:  cfgPath,
        fileSvc:  NewFileService(cfg.Context.WorkingDir),
    }

    mux := http.NewServeMux()

    // Connect handler — path prefix from generated code.
    connectPath, connectHandler := feinov1connect.NewFeinoServiceHandler(h,
        connect.WithInterceptors(loggingInterceptor()),
    )
    if opts.AllowCORS {
        connectHandler = cors.NewHandler(connectHandler, cors.Options{
            AllowedOrigins: []string{"*"},
            AllowedMethods: cors.AllowedMethods(),
            AllowedHeaders: cors.AllowedHeaders(),
            ExposedHeaders: cors.ExposedHeaders(),
        })
    }
    mux.Handle(connectPath, connectHandler)

    // Static SPA — any path not matching Connect is served index.html.
    mux.Handle("/", spaHandler(EmbeddedFS()))

    addr := fmt.Sprintf("%s:%d", opts.Host, opts.Port)
    srv := &http.Server{
        Addr:    addr,
        Handler: h2c.NewHandler(mux, &http2.Server{}),
    }

    // Graceful shutdown on context cancellation.
    go func() {
        <-ctx.Done()
        _ = srv.Shutdown(context.Background())
    }()

    return srv.ListenAndServe()
}
```

### 7.3 `session_manager.go` — Fan-out and Permission Bridge

The `SessionManager` is the critical piece that decouples the global `app.Session` subscriber from individual HTTP connections.

**Problem:** `sess.Subscribe(handler)` is append-only — handlers are never removed. If we registered one handler per HTTP connection, they would accumulate and fire long after connections are closed.

**Solution:** Register one global handler in `SessionManager` that forwards to a `sync.Map` of per-stream channels. When a connection closes, its channel is removed from the map. No handlers leak.

```
                         global handler (registered once)
                              │
         ┌────────────────────▼────────────────────────┐
         │  SessionManager                             │
         │                                             │
         │  streamSubs:  map[streamID] chan app.Event  │
         │  pendingPerms: map[requestID] chan bool     │
         └──────┬──────────────────────┬───────────────┘
                │ fan-out              │ permission bridge
         ┌──────▼──────┐        ┌──────▼──────┐
         │ stream A    │        │ ReAct goroutine │
         │ (HTTP conn) │        │ blocks on       │
         └─────────────┘        │ <-ch            │
         ┌─────────────┐        └─────────────────┘
         │ stream B    │
         │ (HTTP conn) │
         └─────────────┘
```

Key methods:

- `Subscribe(streamID string) (<-chan app.Event, cancel func())` — creates channel, registers it. `cancel()` deregisters it.
- `SetPermissionCallback()` — called once in `BuildSession`; installs the callback that stores the `chan bool` and emits `PermissionRequestEvent` to the active stream.
- `ResolvePermission(requestID string, approved bool) error` — finds the channel, sends the decision.
- `ActiveStreamID() string` — returns the stream ID of the currently active `SendMessage` call (only one turn runs at a time).

### 7.4 `handler.go` — `SendMessage` RPC

This is the most complex RPC:

```
Client                        Server (FeinoServiceHandler)
  │                                │
  │── SendMessage(text) ──────────>│
  │                                │ sm.Subscribe(streamID) → eventCh
  │                                │ sess.Send(ctx, expandedText)  ←── goroutine
  │<── PartReceivedEvent ──────────│     ↑ sess events forwarded to eventCh
  │<── ToolCallEvent ──────────────│
  │<── PermissionRequestEvent ─────│ (goroutine blocks on chan bool)
  │                                │
  │── ResolvePermission(id, true) ─┤ (separate RPC call, same HTTP connection)
  │                                │ pendingPerms[id] <- true
  │                                │ goroutine unblocks
  │<── ToolResultEvent ────────────│
  │<── CompleteEvent ──────────────│
  │                                │ sm.Unsubscribe(streamID)
  │                                │ stream.Close()
```

Pseudocode:

```go
func (h *FeinoServiceHandler) SendMessage(
    ctx context.Context,
    req *connect.Request[SendMessageRequest],
    stream *connect.ServerStream[AgentEvent],
) error {
    streamID := uuid.New().String()
    eventCh, cancel := h.sm.Subscribe(streamID)
    defer cancel()

    expanded, err := h.fileSvc.ExpandRefs(ctx, req.Msg.Text, req.Msg.Attachments)
    if err != nil {
        return connect.NewError(connect.CodeInvalidArgument, err)
    }

    go func() { _ = h.sess.Send(ctx, expanded) }()

    for {
        select {
        case <-ctx.Done():
            h.sess.Cancel()
            return nil
        case e, ok := <-eventCh:
            if !ok {
                return nil
            }
            msg, done, err := eventToProto(e, streamID)
            if err != nil {
                continue
            }
            if err := stream.Send(msg); err != nil {
                return err
            }
            if done {
                return nil
            }
        }
    }
}
```

### 7.5 `config_mapper.go` — Config ↔ Proto Conversion

Two pure functions:

- `ConfigToProto(cfg config.Config) *ConfigProto` — copies all fields; sets `has_api_key = cfg.Providers.Anthropic.APIKey != ""`; sets `api_key = ""` in the output (never sent to browser).
- `ProtoToConfig(p *ConfigProto, existing config.Config) config.Config` — applies only non-empty proto fields onto the existing config. API keys are only applied when the proto field is non-empty (empty string = "leave unchanged").

### 7.6 `embed.go`

```go
//go:build web

package web

import (
    "embed"
    "io/fs"
)

//go:embed all:../../web/dist
var webDist embed.FS

func EmbeddedFS() fs.FS {
    sub, _ := fs.Sub(webDist, "web/dist")
    return sub
}
```

The `//go:build web` tag means the embed is only included when building with `-tags web`. Without it, `EmbeddedFS()` returns an empty FS and `--web` prints a notice. This prevents `go build` failures when `web/dist` has not been compiled yet.

A companion `embed_stub.go` provides the fallback:

```go
//go:build !web

package web

import "io/fs"

func EmbeddedFS() fs.FS { return emptyFS{} }
```

### 7.7 `build_session.go` — Session Construction

Mirrors `internal/tui/run.go`'s session setup logic, extracted into a shared helper so both TUI and web use the same construction path:

```go
func BuildSession(cfg config.Config) (
    *app.Session, credentials.Store, memory.Store, string, error,
) {
    cfgPath, _ := config.DefaultConfigPath()
    store := credentialStore()

    opts := ollamaSessionOpts(cfg)
    if cfg.Services.Email.Enabled {
        emailToolList := emailtools.NewEmailTools(cfg.Services.Email, store, slog.Default())
        opts = append(opts, app.WithExtraTools(emailToolList...))
    }

    memStore := openMemoryStore()
    if memStore != nil {
        opts = append(opts, app.WithMemoryStore(memStore))
        opts = append(opts, app.WithExtraTools(tools.NewMemoryTools(memStore, slog.Default())...))
    }

    sess, err := app.New(cfg, opts...)
    if err != nil {
        return nil, nil, nil, "", err
    }
    return sess, store, memStore, cfgPath, nil
}
```

The refactoring of shared logic into `build_session.go` also updates `internal/tui/run.go` to call `web.BuildSession` instead of duplicating the construction.

---

## 8. React Application Architecture (`web/`)

### 8.1 Directory Structure

```
web/
├── index.html              root HTML shell (only loads main.tsx)
├── package.json
├── vite.config.ts
├── tsconfig.json
├── tailwind.config.ts      (Tailwind v4 uses CSS-first config, but ts file for IDE)
└── src/
    ├── main.tsx            React root, router, providers
    ├── App.tsx             top-level layout shell
    ├── client.ts           Connect transport + feinoClient singleton
    ├── routes.tsx          React Router route definitions

    ├── gen/                generated by buf — DO NOT EDIT
    │   └── feino/v1/
    │       ├── feino_pb.ts
    │       └── feino_connect.ts

    ├── components/
    │   ├── chat/
    │   │   ├── ChatView.tsx          main chat layout (3-column)
    │   │   ├── MessageList.tsx       virtualized scrollable log
    │   │   ├── MessageBubble.tsx     user / assistant / system message
    │   │   ├── ThoughtBlock.tsx      collapsible thought section
    │   │   ├── ToolCallCard.tsx      collapsible tool call + result
    │   │   ├── StreamingCursor.tsx   blinking caret
    │   │   ├── MarkdownRenderer.tsx  react-markdown + syntax highlight
    │   │   ├── InputBar.tsx          textarea, slash completions, @path, send
    │   │   ├── SlashCommandMenu.tsx  floating dropdown for / commands
    │   │   ├── AtPathMenu.tsx        floating dropdown for @path
    │   │   ├── MessageQueue.tsx      queue position badge
    │   │   ├── PermissionModal.tsx   blocking permission approval dialog
    │   │   └── StatusBar.tsx         state · latency · tokens
    │   ├── metrics/
    │   │   ├── MetricsPanel.tsx      toggleable side panel
    │   │   ├── LatencySparkline.tsx  last 20 turns line chart
    │   │   └── TokenBarChart.tsx     per-turn prompt vs completion bars
    │   ├── settings/
    │   │   ├── SettingsPanel.tsx     full-screen overlay, tabbed
    │   │   ├── ProviderSection.tsx   per-provider form
    │   │   ├── EmailSetupSection.tsx IMAP/SMTP form
    │   │   ├── SecuritySection.tsx   permission level + paths
    │   │   ├── AgentSection.tsx      max retries, budget
    │   │   └── MCPSection.tsx        MCP server list
    │   ├── profile/
    │   │   ├── ProfilePage.tsx       name / tz / style form
    │   │   ├── MemoryManager.tsx     list + CRUD for memories
    │   │   └── MemoryRow.tsx         single memory entry with inline edit
    │   ├── history/
    │   │   └── HistoryView.tsx       read-only conversation replay
    │   ├── layout/
    │   │   ├── AppShell.tsx          outer shell, sidebar, main content
    │   │   ├── Sidebar.tsx           nav links + status indicators
    │   │   ├── Header.tsx            app bar: FEINO pill, model, status
    │   │   ├── MobileNav.tsx         bottom tab bar (≤768px)
    │   │   └── ThemeProvider.tsx     CSS variable injection
    │   ├── modals/
    │   │   ├── YoloModal.tsx         duration picker
    │   │   ├── LangModal.tsx         language picker
    │   │   ├── ThemeModal.tsx        theme picker
    │   │   └── ConfigYamlModal.tsx   read-only YAML view
    │   └── shared/
    │       ├── Button.tsx
    │       ├── Badge.tsx             status/state pill
    │       ├── Spinner.tsx           animated loading indicator
    │       ├── Tooltip.tsx
    │       ├── CodeBlock.tsx         syntax-highlighted code
    │       ├── Modal.tsx             generic modal shell
    │       └── Collapsible.tsx       animated expand/collapse

    ├── hooks/
    │   ├── useChatStream.ts    stream subscription + message dispatch
    │   ├── usePermission.ts    permission prompt state
    │   ├── useMetrics.ts       StreamMetrics subscription
    │   ├── useConfig.ts        config CRUD
    │   ├── useMemory.ts        memory CRUD
    │   ├── useHistory.ts       history fetch + reset
    │   ├── useSession.ts       session state polling (GetSessionState)
    │   ├── useBypass.ts        yolo mode state
    │   └── useFiles.ts         file listing for @path autocomplete

    ├── store/
    │   ├── chatStore.ts        messages, busy, queue, pending permission
    │   ├── configStore.ts      config snapshot + dirty flag
    │   ├── metricsStore.ts     rolling latency + token history
    │   └── sessionStore.ts     theme, language, bypass state

    ├── types/
    │   ├── chat.ts             Message, ToolCall, Thought, RenderedMessage
    │   ├── config.ts           typed wrappers over proto Config
    │   └── metrics.ts          UsageMetadata, LatencyPoint, TokenPoint

    ├── lib/
    │   ├── markdown.ts         react-markdown config (plugins, components)
    │   ├── highlight.ts        highlight.js language registration
    │   └── utils.ts            cn(), formatMs(), truncate(), etc.

    └── styles/
        ├── globals.css         Tailwind directives + @import
        └── neural-terminal.css CSS custom properties (section 3.2)
```

### 8.2 `client.ts` — Connect Client Singleton

```typescript
// web/src/client.ts
import { createConnectTransport } from "@connectrpc/connect-web";
import { createClient } from "@connectrpc/connect";
import { FeinoService } from "./gen/feino/v1/feino_connect";

export const transport = createConnectTransport({
  baseUrl: window.location.origin,
  useBinaryFormat: true, // protobuf binary over Connect protocol
});

export const feinoClient = createClient(FeinoService, transport);
```

### 8.3 `main.tsx` — React Entry Point

```tsx
// web/src/main.tsx
import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import App from "./App";
import { ThemeProvider } from "./components/layout/ThemeProvider";
import "./styles/globals.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <BrowserRouter>
      <ThemeProvider>
        <App />
      </ThemeProvider>
    </BrowserRouter>
  </React.StrictMode>,
);
```

### 8.4 `routes.tsx` — Route Definitions

```tsx
// web/src/routes.tsx
import { Routes, Route, Navigate } from "react-router-dom";
import { AppShell } from "./components/layout/AppShell";
import { ChatView } from "./components/chat/ChatView";
import { HistoryView } from "./components/history/HistoryView";
import { SettingsPanel } from "./components/settings/SettingsPanel";
import { ProfilePage } from "./components/profile/ProfilePage";

export function AppRoutes() {
  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<ChatView />} />
        <Route path="history" element={<HistoryView />} />
        <Route path="settings" element={<SettingsPanel />} />
        <Route path="profile" element={<ProfilePage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}
```

---

## 9. Component Specifications

### 9.1 `AppShell` — Outer Layout

```
Desktop (≥1024px):
┌────────────────────────────────────────────────────────────┐
│  Header (56px)                                             │
├────────┬───────────────────────────────────────────────────┤
│        │                                                   │
│Sidebar │         <Outlet />  (route content)               │
│ 220px  │                                                   │
│        │                                                   │
└────────┴───────────────────────────────────────────────────┘

Tablet (768px–1023px):
Sidebar collapses to icon-only rail (48px wide).
Hover expands with labels (tooltip style).

Mobile (< 768px):
Sidebar hidden.
Header remains.
Bottom tab bar (MobileNav) provides navigation.
```

### 9.2 `ChatView` — Main Chat Layout

```
┌─ Header (inside ChatView, not AppShell Header) ─────────────┐
│  FEINO  │  model-name badge  │  state pill  │  [⊞ metrics]  │
└─────────────────────────────────────────────────────────────┘
┌─ MessageList ──────────────────────┐ ┌─ MetricsPanel (280px) ─┐
│                                    │ │                        │
│  MessageBubble (user)              │ │  Latency sparkline     │
│                                    │ │  20-point line chart   │
│  MessageBubble (assistant)         │ │                        │
│    ▾ ThoughtBlock                  │ │  Token barchart        │
│    ▾ ToolCallCard  pending         │ │  per-turn p/c bars     │
│    ▾ ToolCallCard  resolved        │ │                        │
│                                    │ │  Current turn:         │
│  StreamingCursor                   │ │  127p / 43c / 170 tot  │
│                                    │ │                        │
└─ (auto-scrolled to bottom) ────────┘ └────────────────────────┘
┌─ StatusBar ─────────────────────────────────────────────────┐
│  ● act  │  Latency: 842ms  │  Turn: 127p / 43c  │  170 tok  │
└─────────────────────────────────────────────────────────────┘
┌─ InputBar ──────────────────────────────────────────────────┐
│  [📎]  textarea (auto-grow)  [spinner?]  [Send / ✕ Cancel]  │
│                                                             │
│  SlashCommandMenu (floating above, when text starts with /) │
│  AtPathMenu (floating above, when @word detected)           │
└─────────────────────────────────────────────────────────────┘
```

Props: none (reads from `chatStore`).

### 9.3 `MessageBubble` — Message Rendering

```tsx
interface Props {
  message: RenderedMessage; // role, parts[], timestamp
  isStreaming?: boolean;
}
```

**User bubble:** right-aligned label "You" in primary color; text wrapped in surface-2 background with rounded corners. No markdown rendering (verbatim text).

**Assistant bubble:** left-aligned label "FEINO" in accent color; text rendered through `MarkdownRenderer`. Contains zero or more nested `ThoughtBlock` and `ToolCallCard` components.

**System/info bubble:** full-width, dimmed background, italic text. Used for `/config`, `/history`, `/profile` output.

**Error bubble:** full-width, `--color-error-muted` background, red left border, error icon.

### 9.4 `ThoughtBlock` — Reasoning Display

```tsx
interface Props {
  text: string; // accumulated thought text
  isStreaming: boolean; // true while more thought chunks may arrive
}
```

Renders as a collapsible section with a purple (`--color-thinking`) accent border on the left. Default state: collapsed, showing "💭 Thinking…" while streaming, "💭 Thought" when complete. Expand reveals the full thought text rendered in a dim `pre` block (monospace, smaller font).

### 9.5 `ToolCallCard` — Tool Call Display

```tsx
interface Props {
  call: {
    callId: string;
    name: string;
    arguments: string; // JSON
    result?: { content: string; isError: boolean };
    status: "pending" | "running" | "resolved" | "error";
  };
}
```

**Collapsed header:** tool icon (wrench) + tool name in monospace + status badge.

Status badge colors:

- `pending` → dim gray
- `running` → pulsing cyan dot
- `resolved` → green checkmark
- `error` → red X

**Expanded body:**

```
▼ file_read  ✓
  Arguments
  ┌──────────────────────────────────────────┐
  │ {                                        │
  │   "path": "/home/user/project/main.go"   │
  │ }                                        │
  └──────────────────────────────────────────┘
  Result
  ┌──────────────────────────────────────────┐
  │ package main                             │
  │ ...                                      │
  └──────────────────────────────────────────┘
```

Large results (>2000 chars) are truncated with a "Show full output" link.

### 9.6 `PermissionModal` — Blocking Permission Prompt

This modal renders when `chatStore.pendingPermission !== null`. It blocks all input until resolved.

```
┌─────────────────────────────────────────────────┐
│                                                 │
│  ⚠  Permission Required                         │
│                                                 │
│  "file_write" needs  write  access.             │
│  Current permission level:  read                │
│                                                 │
│  The agent will write to the filesystem.        │
│  Review the tool arguments before allowing.     │
│                                                 │
│  ┌─────────────────────────────────────────┐    │
│  │ path: /home/user/project/output.txt     │    │
│  └─────────────────────────────────────────┘    │
│                                                 │
│  [ Deny ]                       [ Allow ]       │
│                                                 │
└─────────────────────────────────────────────────┘
```

- Backdrop: semi-transparent black, `backdropFilter: blur(4px)`
- Allow button: primary green with glow
- Deny button: surface-2 background, error-colored border
- Keyboard: `Enter` = Allow, `Escape` = Deny

On selection: calls `feinoClient.resolvePermission({ requestId, approved })` then clears `chatStore.pendingPermission`.

### 9.7 `InputBar` — Message Input

**Textarea behavior:**

- Auto-grows vertically from 1 to 8 lines (`resize: none`, CSS `field-sizing: content` with JS fallback).
- `Enter` submits (when no completion menu open).
- `Ctrl+Enter` or `Shift+Enter` inserts newline.
- `Escape` dismisses any open completion menu.

**Slash command completions:**

- Triggered when the entire textarea value starts with `/` and contains no spaces.
- `SlashCommandMenu` renders floating above the textarea, positioned to the left edge.
- `↑`/`↓` arrows navigate; `Tab` or `Enter` accepts.
- Full command list matches TUI: `/clear`, `/config`, `/email-setup`, `/exit`, `/history`, `/lang`, `/profile`, `/quit`, `/reload-plugins`, `/reset`, `/setup`, `/theme`, `/yolo`.

**`@path` completions:**

- Triggered when the current word (space-delimited token at cursor) starts with `@`.
- Calls `feinoClient.listFiles({ path: partialPath })` with 300ms debounce.
- `AtPathMenu` renders floating above the cursor; same keyboard navigation.
- Drag-and-drop anywhere in `ChatView` calls `feinoClient.uploadFile(...)` and inserts `@<token>` at the cursor.

**Send / Cancel button:**

- When not busy: green "Send" button with arrow icon. Submits on click.
- When busy: red "Cancel" button with X icon. Calls `feinoClient.cancelTurn()`.
- File attach button (📎): opens browser file picker; selected file is uploaded via `uploadFile` RPC and token inserted at cursor.

### 9.8 `StatusBar` — Turn Status

```
● gather  │  Latency: —  │  Turn: —  │  ⚡ YOLO 04:32
```

- State indicator: colored dot + state name in monospace
  - `init`/`gather`: dim gray
  - `act`/`verify`: cyan pulsing
  - `complete`: brief green, fades to dim
  - `failed`: red
- Latency: shows last turn's latency in ms after `CompleteEvent`
- Token counts: `127p / 43c / 170 total`
- Yolo indicator: amber `⚡ YOLO` with countdown when bypass active

### 9.9 `MetricsPanel` — Metrics Sidebar

Toggleable panel (right side of `ChatView`). State persisted in `sessionStore.metricsOpen`.

**LatencySparkline:**

- Line chart, 20-point rolling window
- X-axis: turn number (no labels, too small)
- Y-axis: milliseconds (auto-scale)
- Area fill in primary green with low opacity
- Line in primary green
- Last point dot emphasized

**TokenBarChart:**

- Grouped bar chart, 10-turn rolling window
- Two bars per turn: prompt (dim green) + completion (bright green)
- Y-axis: token count
- Hover tooltip: exact counts + turn number

Both charts use `recharts` with custom styling to match the Neural Terminal palette.

### 9.10 `SettingsPanel` — Configuration

Full-screen overlay (not a route, a modal) opened via the ⚙ icon in the header.

**Tabs:**

1. **Provider** — active provider badge + per-provider sections:
   - Anthropic: API key (masked input), default model (text input), "Test connection" button
   - OpenAI: API key, base URL, default model
   - Gemini: toggle API key vs Vertex; API key or project+location fields
   - Ollama: host URL, default model
   - OpenAI-Compatible: base URL, API key (optional), display name, model
2. **Security** — permission level selector (radio: read/write/bash/danger_zone), allowed paths list, AST blacklist toggle
3. **Agent** — max retries, complexity thresholds, metrics path
4. **Context** — working directory, global config path, max budget, plugins directory
5. **Email** — IMAP/SMTP form (mirrors TUI `/email-setup` wizard)
6. **Advanced** — log level, config YAML viewer

**Save behavior:** "Save" button calls `UpdateConfig`. On success: `config_updated` toast. On failure: `config_save_failed` error message inline.

**API key masking:** Input shows `••••••••••` for stored keys. A "Replace" button clears the mask and allows typing a new key. An empty submission leaves the existing key unchanged (server-side logic via `has_api_key` flag in proto).

### 9.11 `ProfilePage` — User Profile and Memories

Two sections on this page.

**Profile section:**

- Name: text input, placeholder "e.g. Diego"
- Timezone: text input with autocomplete from a static IANA list
- Communication style: radio group with icons (none / concise / detailed / technical / friendly)
- "Save" calls `UpdateConfig` with the `user` field only

**Memory manager:**

- List of all memories grouped by category (profile, preference, fact, note)
- Each `MemoryRow`:
  - Category badge (colored per category)
  - Content text (truncated, expands on click)
  - Edit button: inline textarea to modify content, "Save" calls `UpdateMemory`
  - Delete button: confirmation popover, then calls `DeleteMemory`
- "Add memory" button: inline form at top of category → calls `WriteMemory`
- Search box: client-side filter + server-side `query` param for `ListMemories`
- Empty state: "No memories yet. FEINO will remember things you tell it."

### 9.12 `YoloModal`, `LangModal`, `ThemeModal`

These three modals are triggered by the corresponding slash commands and follow the same pattern as the TUI's picker overlays.

**YoloModal:**

```
⚡ UNSAFE MODE
Select how long bypass mode stays active:
[ 5 min ]  [ 10 min ]  [ 30 min ]  [ Session ]
              [ Cancel ]
```

Selection calls `feinoClient.setBypassMode(...)`. Confirmation toast appears. The amber YOLO indicator activates in the status bar.

**LangModal:**

```
🌐 Select UI language:
● English  ○ Español (Latin America)  ○ Español (España)
○ Português (Brasil)  ○ Português (Portugal)  ○ 中文 (简体)
○ 日本語  ○ Русский
         [ Cancel ]
```

Calls `feinoClient.setLanguage(...)`. The language change takes effect after page refresh (Connect client uses the `Accept-Language` header; the server returns localized error messages and strings).

**ThemeModal:**

```
🎨 Select theme:
○ Neo — phosphor green   ● Dark — Catppuccin Mocha
○ Light — Catppuccin Latte  ○ Auto (detect background)
              [ Cancel ]
```

Calls `feinoClient.setTheme(...)`. `ThemeProvider` reacts immediately by updating `data-theme` on `<html>`. No page refresh needed.

---

## 10. State Management

### 10.1 `chatStore.ts` — Chat State

```typescript
interface ChatState {
  messages: RenderedMessage[];
  busy: boolean;
  queueLength: number;
  reactState: string; // ReAct state name
  pendingPermission: PermissionRequest | null;
  streamingText: string; // accumulates during stream
  streamingThought: string; // accumulates thought
  activeToolCalls: Map<string, ToolCallState>;

  // Actions
  addUserMessage: (text: string) => void;
  appendStreamChunk: (text: string) => void;
  appendThoughtChunk: (text: string) => void;
  addToolCall: (call: ToolCallEvent) => void;
  resolveToolCall: (result: ToolResultEvent) => void;
  flushStream: () => void; // called on CompleteEvent
  setPermission: (req: PermissionRequest | null) => void;
  setReactState: (state: string) => void;
  setBusy: (busy: boolean, queueLen?: number) => void;
  clear: () => void;
  reset: () => void;
}
```

### 10.2 `metricsStore.ts` — Metrics State

```typescript
interface MetricsState {
  latencyHistory: number[]; // rolling 20 values (ms per turn)
  tokenHistory: TokenPoint[]; // rolling 10 values {prompt, completion}
  currentUsage: UsageMetadata | null;

  pushMetric: (event: MetricsEvent) => void;
}
```

### 10.3 `configStore.ts` — Config State

```typescript
interface ConfigState {
  config: ConfigProto | null;
  dirty: boolean;

  setConfig: (cfg: ConfigProto) => void;
  updateField: <K extends keyof ConfigProto>(
    key: K,
    value: ConfigProto[K],
  ) => void;
  markClean: () => void;
}
```

### 10.4 `sessionStore.ts` — Session-wide State

```typescript
interface SessionState {
  theme: string;
  language: string;
  bypassActive: boolean;
  bypassExpiry: Date | null;
  bypassSession: boolean;
  metricsOpen: boolean;

  setTheme: (t: string) => void;
  setLang: (l: string) => void;
  setBypass: (state: BypassStateResponse) => void;
  clearBypass: () => void;
  toggleMetrics: () => void;
}
```

### 10.5 Data Flow: Sending a Message

```
User types text → presses Enter
        │
        ▼
InputBar.onSubmit(text)
        │
        ▼
useChatStream.send(text)
        │  chatStore.addUserMessage(text)
        │  chatStore.setBusy(true)
        │
        ▼
feinoClient.sendMessage({ text }) → server stream opens
        │
        ├── PartReceivedEvent → chatStore.appendStreamChunk(text)
        │                       MessageList re-renders last bubble
        │
        ├── ThoughtReceivedEvent → chatStore.appendThoughtChunk(text)
        │                          ThoughtBlock streams live
        │
        ├── ToolCallEvent → chatStore.addToolCall(call)
        │                   ToolCallCard renders in "pending" state
        │
        ├── PermissionRequestEvent → chatStore.setPermission(req)
        │                            PermissionModal renders
        │                            (stream is blocked server-side)
        │                            user clicks Allow/Deny
        │                            feinoClient.resolvePermission(...)
        │                            chatStore.setPermission(null)
        │
        ├── ToolResultEvent → chatStore.resolveToolCall(result)
        │                     ToolCallCard shows result
        │
        ├── StateChangedEvent → chatStore.setReactState(state)
        │                       StatusBar dot updates
        │
        ├── UsageUpdatedEvent → metricsStore.pushMetric(event)
        │                       MetricsPanel charts update
        │
        ├── QueuePositionEvent → chatStore.setBusy(true, queuePos)
        │                        MessageQueue badge shows position
        │
        └── CompleteEvent → chatStore.flushStream()
                            chatStore.setBusy(false)
                            stream closes
```

---

## 11. Routing and Navigation

### 11.1 Sidebar Navigation

```
┌──────────────┐
│  FEINO  ●    │  ← FEINO logo/wordmark + status dot
├──────────────┤
│ 💬 Chat      │  ← active: primary green left border + background
│ 📋 History   │
│ 👤 Profile   │
├──────────────┤
│              │  (spacer — grows)
├──────────────┤
│ ⚙  Settings  │  ← bottom-pinned
└──────────────┘
```

Status dot colors:

- Green: session idle
- Cyan pulsing: agent working
- Amber: bypass mode active
- Red: error

### 11.2 Mobile Bottom Navigation

```
┌────────────────────────────────────────────────────────┐
│  💬 Chat  │  📋 History  │  👤 Profile  │  ⚙ Settings  │
└────────────────────────────────────────────────────────┘
```

Active tab: primary green dot above the icon.

### 11.3 Slash Command to Route/Action Mapping

| Command           | Action                                                                |
| ----------------- | --------------------------------------------------------------------- |
| `/setup`          | Opens `SettingsPanel` overlay                                         |
| `/email-setup`    | Opens `SettingsPanel` on Email tab                                    |
| `/yolo`           | Opens `YoloModal`                                                     |
| `/lang`           | Opens `LangModal`                                                     |
| `/theme`          | Opens `ThemeModal`                                                    |
| `/reset`          | Calls `feinoClient.resetSession()`, clears `chatStore`                |
| `/history`        | Calls `feinoClient.getHistory()`, appends result as a system message  |
| `/clear`          | Clears `chatStore.messages` without resetting session                 |
| `/config`         | Calls `feinoClient.getConfigYaml()`, shows `ConfigYamlModal`          |
| `/profile`        | Navigates to `/profile` route                                         |
| `/reload-plugins` | Calls `feinoClient.reloadPlugins()`, shows confirmation toast         |
| `/exit`, `/quit`  | Not applicable in web UI; shows toast "Close the browser tab to exit" |

---

## 12. Build Pipeline

### 12.1 Step-by-Step Order

```
1. buf generate             → proto/feino/v1/feino.proto
                              → gen/feino/v1/feino.pb.go
                              → gen/feino/v1/feinov1connect/feino.connect.go
                              → web/src/gen/feino/v1/feino_pb.ts
                              → web/src/gen/feino/v1/feino_connect.ts

2. cd web && npm ci         → installs node_modules

3. cd web && npm run build  → vite build → web/dist/
                               web/dist/index.html
                               web/dist/assets/index-[hash].js
                               web/dist/assets/index-[hash].css
                               web/dist/assets/[vendor chunks]

4. go build -tags web ./cmd/feino
                            → embeds web/dist via go:embed
                            → produces ./feino binary
```

### 12.2 `Makefile`

```makefile
.PHONY: proto web build dev clean test

# ── Proto generation ───────────────────────────────────────────────
proto:
	cd proto && buf generate

# ── Frontend build ─────────────────────────────────────────────────
web:
	cd web && npm ci && npm run build

# ── Full production build ──────────────────────────────────────────
build: proto web
	go build -tags web -o feino ./cmd/feino

# ── Development: run Go server + Vite dev server in parallel ───────
dev-go:
	ANTHROPIC_API_KEY=$$ANTHROPIC_API_KEY \
	go run ./cmd/feino --web --web-port 3000

dev-web:
	cd web && npm run dev

# ── Tests ──────────────────────────────────────────────────────────
test:
	go test ./...
	cd web && npm test -- --run --passWithNoTests

# ── Clean ──────────────────────────────────────────────────────────
clean:
	rm -f feino
	rm -rf web/dist
	rm -rf gen

# ── CI pipeline target ─────────────────────────────────────────────
ci: proto web
	go test -race ./...
	go build -tags web ./cmd/feino
```

### 12.3 `vite.config.ts`

```typescript
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],

  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
    rollupOptions: {
      output: {
        manualChunks: {
          vendor: ["react", "react-dom", "react-router-dom"],
          connect: [
            "@connectrpc/connect",
            "@connectrpc/connect-web",
            "@bufbuild/protobuf",
          ],
          charts: ["recharts"],
          md: ["react-markdown", "rehype-highlight", "remark-gfm"],
        },
      },
    },
  },

  // Dev server: proxy Connect calls to the running Go server.
  server: {
    port: 5173,
    proxy: {
      "/feino.v1.FeinoService": {
        target: "http://localhost:3000",
        changeOrigin: true,
      },
    },
  },
});
```

### 12.4 `buf.gen.yaml`

```yaml
version: v2
plugins:
  # Go server stubs
  - plugin: buf.build/protocolbuffers/go:v1.34.2
    out: gen
    opt: paths=source_relative

  # Connect-go handler interfaces
  - plugin: buf.build/connectrpc/go:v1.17.0
    out: gen
    opt: paths=source_relative

  # TypeScript protobuf messages
  - plugin: buf.build/bufbuild/es:v2.2.0
    out: web/src/gen
    opt: target=ts,import_extension=none

  # TypeScript Connect client
  - plugin: buf.build/connectrpc/es:v2.0.0
    out: web/src/gen
    opt: target=ts,import_extension=none
```

### 12.5 `buf.yaml`

```yaml
version: v2
modules:
  - path: proto
deps:
  - buf.build/googleapis/googleapis
lint:
  use: [STANDARD]
breaking:
  use: [FILE]
```

---

## 13. Complete File Inventory

### New Go files

| File                                           | Purpose                                           |
| ---------------------------------------------- | ------------------------------------------------- |
| `proto/feino/v1/feino.proto`                   | Complete service definition                       |
| `buf.yaml`                                     | Buf module configuration                          |
| `buf.gen.yaml`                                 | Buf code generation targets                       |
| `Makefile`                                     | Build orchestration                               |
| `gen/feino/v1/feino.pb.go`                     | Generated — do not edit                           |
| `gen/feino/v1/feinov1connect/feino.connect.go` | Generated — do not edit                           |
| `internal/web/server.go`                       | HTTP server, routing, lifecycle                   |
| `internal/web/handler.go`                      | FeinoServiceHandler, all RPC methods              |
| `internal/web/session_manager.go`              | Event fan-out, permission bridge                  |
| `internal/web/metrics_hub.go`                  | Broadcast hub for StreamMetrics                   |
| `internal/web/config_mapper.go`                | config.Config ↔ ConfigProto conversion            |
| `internal/web/file_service.go`                 | Upload handling, ListFiles, @-token resolution    |
| `internal/web/atref.go`                        | @path and @token expansion for web context        |
| `internal/web/embed.go`                        | `//go:build web` embed directive                  |
| `internal/web/embed_stub.go`                   | `//go:build !web` empty FS fallback               |
| `internal/web/build_session.go`                | Shared session construction logic                 |
| `internal/web/spa_handler.go`                  | SPA fallback (serve index.html for unknown paths) |

### Modified Go files

| File                  | Change                                                                                 |
| --------------------- | -------------------------------------------------------------------------------------- |
| `cmd/feino/main.go`   | Add `--web`, `--web-port`, `--web-host` flags + new runtime branch                     |
| `internal/tui/run.go` | Refactor session construction to call `web.BuildSession`                               |
| `go.mod`              | Add `connectrpc.com/connect`, promote `golang.org/x/net`, `google.golang.org/protobuf` |

### New frontend files

All files under `web/src/` as listed in section 8.1, plus:

| File                 | Purpose                        |
| -------------------- | ------------------------------ |
| `web/index.html`     | HTML shell                     |
| `web/package.json`   | npm manifest                   |
| `web/vite.config.ts` | Vite config                    |
| `web/tsconfig.json`  | TypeScript config              |
| `web/src/gen/**`     | Generated by buf — do not edit |

---

## 14. Dependency Manifest

### Go modules to add

```
connectrpc.com/connect           v1.17.0   Connect RPC server
google.golang.org/protobuf       v1.34.2   Protobuf runtime (promote from indirect)
golang.org/x/net                 latest    h2c HTTP/2 cleartext (promote from indirect)
github.com/google/uuid           v1.6.0    UUID generation for stream/permission IDs
```

Install via:

```bash
go get connectrpc.com/connect@v1.17.0
go get google.golang.org/protobuf@v1.34.2
go get github.com/google/uuid@v1.6.0
go mod tidy
```

### npm packages

```json
{
  "dependencies": {
    "@bufbuild/protobuf": "^2.2.0",
    "@connectrpc/connect": "^2.0.0",
    "@connectrpc/connect-web": "^2.0.0",
    "@fontsource/inter": "^5.1.0",
    "@fontsource/jetbrains-mono": "^5.1.0",
    "clsx": "^2.1.0",
    "react": "^18.3.0",
    "react-dom": "^18.3.0",
    "react-dropzone": "^14.2.0",
    "react-markdown": "^9.0.0",
    "react-router-dom": "^6.26.0",
    "recharts": "^2.12.0",
    "rehype-highlight": "^7.0.0",
    "rehype-raw": "^7.0.0",
    "remark-gfm": "^4.0.0",
    "tailwind-merge": "^2.5.0",
    "zustand": "^5.0.0"
  },
  "devDependencies": {
    "@bufbuild/buf": "^1.45.0",
    "@connectrpc/protoc-gen-connect-es": "^2.0.0",
    "@bufbuild/protoc-gen-es": "^2.2.0",
    "@tailwindcss/vite": "^4.0.0",
    "@types/react": "^18.3.0",
    "@types/react-dom": "^18.3.0",
    "@vitejs/plugin-react": "^4.3.0",
    "highlight.js": "^11.10.0",
    "tailwindcss": "^4.0.0",
    "typescript": "^5.5.0",
    "vite": "^6.0.0",
    "vitest": "^2.0.0"
  }
}
```

---

## 15. `cmd/feino/main.go` Changes

```go
// Add after existing flag declarations:
webMode    := flag.Bool("web", false, "start the web UI server (serves embedded React app)")
webPort    := flag.Int("web-port", 3000, "port for the web UI server")
webHost    := flag.String("web-host", "127.0.0.1",
    "bind address for the web UI (0.0.0.0 exposes on all interfaces)")

// Add new branch after the existing --no-tui branch:
if *webMode {
    // Redirect slog to file — identical to TUI behaviour.
    if logFile, err := openLogFile(); err == nil {
        defer logFile.Close()
        slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{
            Level: config.ParseLogLevel(cfg.UI.LogLevel),
        })))
    }

    addr := fmt.Sprintf("%s:%d", *webHost, *webPort)
    fmt.Fprintf(os.Stderr, "FEINO web UI → http://%s\n", addr)

    if err := web.Start(ctx, cfg, web.Options{
        Host:      *webHost,
        Port:      *webPort,
        AllowCORS: *webHost != "127.0.0.1",
    }); err != nil && !errors.Is(err, http.ErrServerClosed) {
        slog.Error("web server failed", "error", err)
        os.Exit(1)
    }
    return
}
```

---

## 16. Implementation Phases

### Phase 1 — Go Skeleton (week 1)

**Goal:** `go build -tags web ./cmd/feino` succeeds; `./feino --web` starts and serves a placeholder page.

- [ ] Install `buf` CLI and write `buf.yaml`, `buf.gen.yaml`
- [ ] Write complete `proto/feino/v1/feino.proto`
- [ ] Run `buf generate`; verify Go files in `gen/` compile
- [ ] Add Go dependencies (`connectrpc.com/connect`, `uuid`)
- [ ] Write `internal/web/embed.go` + `embed_stub.go`
- [ ] Create placeholder `web/dist/index.html` (single HTML line)
- [ ] Write `internal/web/server.go` (skeleton, registers handler path)
- [ ] Write `internal/web/handler.go` (all methods return `Unimplemented`)
- [ ] Modify `cmd/feino/main.go` — `--web` flag and branch
- [ ] Verify `go build -tags web ./...` and `go test ./...` pass

### Phase 2 — Session Bridge (week 1–2)

**Goal:** `SendMessage` works end-to-end; events stream to `grpcurl` or a test client.

- [ ] Write `internal/web/session_manager.go`
- [ ] Write `internal/web/build_session.go`; refactor `tui/run.go` to use it
- [ ] Implement `SendMessage` RPC (event fan-out loop)
- [ ] Implement `CancelTurn` RPC
- [ ] Implement `ResolvePermission` RPC (permission bridge)
- [ ] Implement `GetSessionState` RPC
- [ ] Write integration test: `TestSendMessage_StreamsEvents` using a mock provider
- [ ] Write integration test: `TestResolvePermission_UnblocksAgent`

### Phase 3 — Supporting RPCs (week 2–3)

**Goal:** All non-streaming RPCs implemented and tested.

- [ ] `GetHistory` / `ResetSession`
- [ ] `GetConfig` / `UpdateConfig` / `GetConfigYAML` + `config_mapper.go`
- [ ] `ListMemories` / `WriteMemory` / `UpdateMemory` / `DeleteMemory`
- [ ] `UploadFile` / `ListFiles` + `file_service.go` + `atref.go`
- [ ] `ReloadPlugins`
- [ ] `SetBypassMode` / `ClearBypassMode` / `GetBypassState`
- [ ] `SetLanguage` / `SetTheme`
- [ ] `StreamMetrics` + `metrics_hub.go`
- [ ] Integration tests for each RPC

### Phase 4 — React Scaffold (week 3)

**Goal:** Browser renders a functional (unstyled) chat that streams real responses.

- [ ] `npm create vite@latest web -- --template react-ts`
- [ ] Install all npm dependencies
- [ ] Run `buf generate` for TypeScript targets
- [ ] Write `client.ts`, `App.tsx`, `routes.tsx`, `main.tsx`
- [ ] Write all Zustand stores
- [ ] Write `useChatStream` hook
- [ ] Write `ChatView`, `MessageList`, `MessageBubble` (bare HTML)
- [ ] Write `InputBar` (textarea + send button)
- [ ] Verify end-to-end: browser sends message, assistant response streams in

### Phase 5 — TUI Parity Features (week 4)

**Goal:** All TUI features available in the web UI.

- [ ] `ThoughtBlock` — streaming thought display
- [ ] `ToolCallCard` — collapsible with pending/resolved states
- [ ] `PermissionModal` — blocking approval dialog
- [ ] `SlashCommandMenu` — floating dropdown
- [ ] `AtPathMenu` — file-path autocomplete
- [ ] `MetricsPanel` + charts (`LatencySparkline`, `TokenBarChart`)
- [ ] `SettingsPanel` — all provider sections + email + security + agent
- [ ] `ProfilePage` + `MemoryManager`
- [ ] `HistoryView`
- [ ] `YoloModal`, `LangModal`, `ThemeModal`
- [ ] `ConfigYamlModal`
- [ ] All slash command handlers
- [ ] Drag-and-drop file upload

### Phase 6 — Neural Terminal Styling (week 5)

**Goal:** Full visual design applied; responsive on mobile.

- [ ] Write `neural-terminal.css` with all CSS custom properties
- [ ] Configure Tailwind v4 to consume CSS custom properties
- [ ] Apply styling to all components systematically
- [ ] Import and apply Inter + JetBrains Mono fonts
- [ ] Implement `ThemeProvider` and theme switching
- [ ] Add glow effects to interactive elements
- [ ] Add micro-animations (fade-in messages, streaming cursor, transition states)
- [ ] Responsive breakpoints: desktop → tablet → mobile
- [ ] Sidebar collapse → icon rail on tablet
- [ ] Bottom navigation bar on mobile
- [ ] MetricsPanel as drawer on mobile

### Phase 7 — Polish and Hardening (week 6)

**Goal:** Production-ready; passes all tests; documented.

- [ ] Accessibility audit: keyboard navigation, ARIA roles, focus management
- [ ] Error boundary in React: catches render errors, shows friendly message
- [ ] Empty states: first-run page, empty history, no memories
- [ ] Loading skeletons for initial config/history fetch
- [ ] Toast notification system (success, error, info)
- [ ] Connection-lost state (when the Go server is unreachable)
- [ ] Load test: 10 concurrent streaming sessions
- [ ] `go test -race ./internal/web/...`
- [ ] `cd web && npm test -- --run --passWithNoTests`
- [ ] Update `CLAUDE.md` with web development commands
- [ ] Add `--web` section to usage documentation

---

## 17. Security Considerations

### API Keys Never Leave the Server

`ConfigToProto` always sets `api_key = ""` in `GetConfigResponse`. The `has_api_key bool` field tells the browser whether a key is stored without revealing it. This is enforced in `config_mapper.go`.

### Upload Token Isolation

Uploaded files are stored in `$TMPDIR/feino-uploads/<uuid>/` with mode `0600`. Tokens are UUIDs generated server-side. Files are cleaned up after the send turn that references them completes. Tokens cannot be guessed or enumerated.

### `@path` Expansion Scoped to Working Directory

Server-side `@path` expansion in `atref.go` validates that the resolved absolute path is under `cfg.Context.WorkingDir` (same check as the TUI's `expandAtRefs`). Paths attempting directory traversal (`../../etc/passwd`) are rejected with an error.

### CORS Policy

When `--web-host 127.0.0.1` (default), no CORS headers are set — the browser enforces same-origin. When `--web-host 0.0.0.0` (`AllowCORS: true`), CORS `AllowedOrigins: ["*"]` is set. For production deployments, users should configure a specific `AllowedOrigins` list. Future enhancement: `--web-origin` flag.

### No Authentication by Default

The web server does not include authentication. For personal local use, this is acceptable. For cloud/on-premise deployment, users are expected to:

1. Put the server behind a reverse proxy with HTTP Basic Auth or a VPN
2. Or expose only on a trusted LAN interface

Future enhancement: optional `--web-token <token>` flag for bearer token auth as a simple single-user auth mechanism.

### Bypass (Yolo) Mode

`SetBypassMode` records the duration in memory only. The session permission gate ignores the bypass state if the server is restarted (it does not persist). The TUI follows the same pattern.

### Request Size Limits

`UploadFile` enforces a 10 MB maximum on file content. The server rejects larger uploads with `connect.CodeResourceExhausted`. This limit can be tuned via `--web-max-upload` (future flag).

---

## 18. Deployment Guide

### Local Use (Default)

```bash
# Build the binary with web UI embedded
make build

# Run
ANTHROPIC_API_KEY=sk-ant-... ./feino --web
# → FEINO web UI → http://127.0.0.1:3000
```

Open `http://localhost:3000` in a browser.

### Home Network (LAN)

```bash
./feino --web --web-host 0.0.0.0 --web-port 8080
# → accessible on http://<your-machine-ip>:8080
```

Recommendation: enable firewall rules to restrict access to trusted IPs.

### Cloud / VPS (with TLS via Caddy)

```caddyfile
# /etc/caddy/Caddyfile
feino.example.com {
    reverse_proxy localhost:3000 {
        transport http {
            versions h2c
        }
    }
    basicauth {
        user $2a$14$hashed_password_here
    }
}
```

```bash
./feino --web --web-host 127.0.0.1 --web-port 3000
```

### Docker

```dockerfile
FROM golang:1.23 AS builder
WORKDIR /app
COPY . .
RUN make build

FROM debian:bookworm-slim
COPY --from=builder /app/feino /usr/local/bin/feino
EXPOSE 3000
ENV ANTHROPIC_API_KEY=""
CMD ["feino", "--web", "--web-host", "0.0.0.0", "--web-port", "3000"]
```

```bash
docker run -e ANTHROPIC_API_KEY=sk-ant-... -p 3000:3000 feino
```

### systemd Service

```ini
# /etc/systemd/system/feino-web.service
[Unit]
Description=FEINO Web UI
After=network.target

[Service]
User=feino
ExecStart=/usr/local/bin/feino --web --web-host 127.0.0.1 --web-port 3000
EnvironmentFile=/etc/feino/env
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

---

## 19. TUI Feature Parity Checklist

| TUI Feature                | Web equivalent                | Component / RPC                                 |
| -------------------------- | ----------------------------- | ----------------------------------------------- |
| Streaming chat             | ✅                            | `useChatStream` + `PartReceivedEvent`           |
| Markdown rendering         | ✅                            | `MarkdownRenderer` (react-markdown)             |
| Tool call display          | ✅                            | `ToolCallCard` (collapsible)                    |
| Thought/reasoning blocks   | ✅                            | `ThoughtBlock` (collapsible, purple)            |
| Permission prompt          | ✅                            | `PermissionModal` (modal dialog)                |
| `/setup` — provider config | ✅                            | `SettingsPanel` (Provider tab)                  |
| `/email-setup`             | ✅                            | `SettingsPanel` (Email tab)                     |
| `/yolo` — bypass mode      | ✅                            | `YoloModal` + `SetBypassMode` RPC               |
| `/lang` — language switch  | ✅                            | `LangModal` + `SetLanguage` RPC                 |
| `/theme` — theme switch    | ✅                            | `ThemeModal` + `SetTheme` RPC                   |
| `/reset` — clear session   | ✅                            | `ResetSession` RPC                              |
| `/history` — view history  | ✅                            | `HistoryView` + `GetHistory` RPC                |
| `/clear` — clear view      | ✅                            | `chatStore.clear()`                             |
| `/config` — view YAML      | ✅                            | `ConfigYamlModal` + `GetConfigYAML` RPC         |
| `/profile` — view profile  | ✅                            | `ProfilePage` route                             |
| `/reload-plugins`          | ✅                            | `ReloadPlugins` RPC                             |
| `/quit` / `/exit`          | ✅ (toast: close browser tab) | n/a                                             |
| `@path` file references    | ✅                            | `AtPathMenu` + `ListFiles` + server expand      |
| Drag-and-drop file upload  | ✅ (web-only bonus)           | `react-dropzone` + `UploadFile` RPC             |
| Slash command autocomplete | ✅                            | `SlashCommandMenu`                              |
| Metrics sidebar (latency)  | ✅                            | `LatencySparkline` + `StreamMetrics`            |
| Metrics sidebar (tokens)   | ✅                            | `TokenBarChart`                                 |
| ReAct state display        | ✅                            | `StatusBar` + `StateChangedEvent`               |
| Message queue indicator    | ✅                            | `QueuePositionEvent` → `MessageQueue`           |
| Session busy / cancel      | ✅                            | `CancelTurn` RPC + Cancel button                |
| Memory manager             | ✅                            | `MemoryManager` + memory RPCs                   |
| User profile form          | ✅                            | `ProfilePage` + `UpdateConfig` RPC              |
| Theme: dark/light/auto/neo | ✅                            | `ThemeProvider` + CSS custom properties         |
| Bypass mode indicator      | ✅                            | Amber `⚡ YOLO` + countdown in status bar       |
| Yolo active badge          | ✅                            | `sessionStore.bypassActive`                     |
| Error display              | ✅                            | `appendErrorText` equivalent in `MessageBubble` |
| Config YAML view           | ✅                            | `ConfigYamlModal`                               |
| API key masking            | ✅                            | `has_api_key` proto flag + masked input         |

---

_End of Document_
