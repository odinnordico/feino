import { NavLink } from "react-router-dom";
import { useSessionStore } from "../../store/sessionStore";
import { Tooltip } from "../shared/Tooltip";
import { useWindowWidth } from "../../hooks/useWindowWidth";
import { NAV_ITEMS, type NavItem } from "../../constants/navItems";

function NavItemEl({ item, collapsed }: { item: NavItem; collapsed: boolean }) {
  return (
    <Tooltip text={item.label} position="right">
      <NavLink
        to={item.to}
        end={item.to === "/"}
        style={({ isActive }) => ({
          display: "flex",
          alignItems: "center",
          gap: "10px",
          padding: collapsed ? "10px 14px" : "9px 16px",
          borderRadius: "var(--radius-md)",
          textDecoration: "none",
          fontFamily: "var(--font-sans)",
          fontSize: "var(--font-size-sm)",
          transition: "background var(--transition-fast), color var(--transition-fast)",
          color: isActive ? "var(--color-primary)" : "var(--color-text-dim)",
          background: isActive ? "var(--color-primary-muted)" : "transparent",
        })}
      >
        <span style={{ fontFamily: "var(--font-mono)", fontSize: "1rem" }}>{item.icon}</span>
        {!collapsed && <span>{item.label}</span>}
      </NavLink>
    </Tooltip>
  );
}

export function Sidebar() {
  const toggleMetrics = useSessionStore((s) => s.toggleMetrics);
  const metricsOpen   = useSessionStore((s) => s.metricsOpen);
  const width         = useWindowWidth();
  // Collapse to icon rail on tablet (768–1199px)
  const collapsed     = width >= 768 && width < 1200;

  return (
    <nav
      aria-label="Main navigation"
      style={{
        width: collapsed ? "56px" : "220px",
        transition: "width var(--transition-base)",
        flexShrink: 0,
        display: "flex",
        flexDirection: "column",
        background: "var(--color-surface-1)",
        borderRight: "1px solid var(--color-border)",
        padding: "12px 8px",
        gap: "2px",
      }}
    >
      {!collapsed && (
        <div style={{ padding: "8px 12px 16px", color: "var(--color-primary)", fontFamily: "var(--font-mono)", fontWeight: 700, fontSize: "1.1rem", letterSpacing: "0.05em" }}>
          FEINO
        </div>
      )}
      {collapsed && (
        <div style={{ padding: "12px 0 16px", textAlign: "center", color: "var(--color-primary)", fontFamily: "var(--font-mono)", fontWeight: 700, fontSize: "0.7rem" }}>
          F
        </div>
      )}

      {NAV_ITEMS.map((item) => (
        <NavItemEl key={item.to} item={item} collapsed={collapsed} />
      ))}

      <div style={{ flex: 1 }} />

      <Tooltip text="Metrics" position="right">
        <button
          onClick={toggleMetrics}
          aria-label="Toggle metrics panel"
          aria-pressed={metricsOpen}
          style={{
            display: "flex",
            alignItems: "center",
            gap: "10px",
            padding: collapsed ? "10px 14px" : "9px 16px",
            borderRadius: "var(--radius-md)",
            border: "none",
            cursor: "pointer",
            fontFamily: "var(--font-sans)",
            fontSize: "var(--font-size-sm)",
            transition: "background var(--transition-fast)",
            color: metricsOpen ? "var(--color-accent)" : "var(--color-text-dim)",
            background: metricsOpen ? "var(--color-tool-muted)" : "transparent",
            width: "100%",
          }}
        >
          <span style={{ fontFamily: "var(--font-mono)" }}>⊞</span>
          {!collapsed && <span>Metrics</span>}
        </button>
      </Tooltip>
    </nav>
  );
}
