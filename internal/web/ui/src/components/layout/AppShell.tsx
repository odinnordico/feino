import { Outlet } from "react-router-dom";
import { Header } from "./Header";
import { Sidebar } from "./Sidebar";
import { MobileNav } from "./MobileNav";
import { useSession } from "../../hooks/useSession";
import { useMetrics } from "../../hooks/useMetrics";

export function AppShell() {
  // Wire global polling / streaming hooks at the shell level.
  useSession();
  useMetrics();

  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%", background: "var(--color-bg)" }}>
      <Header />

      <div style={{ display: "flex", flex: 1, overflow: "hidden" }}>
        {/* Sidebar — hidden on narrow screens via inline media isn't possible, but we'll
            rely on the parent container being >= 768px for visibility. */}
        <div
          style={{ display: "flex" }}
          className="sidebar-container"
        >
          <Sidebar />
        </div>

        <main style={{ flex: 1, overflow: "hidden", display: "flex", flexDirection: "column" }}>
          <Outlet />
        </main>
      </div>

      {/* Mobile bottom nav */}
      <div className="mobile-nav-container">
        <MobileNav />
      </div>
    </div>
  );
}
