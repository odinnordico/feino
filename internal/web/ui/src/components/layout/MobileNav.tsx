import { NavLink } from "react-router-dom";
import { NAV_ITEMS } from "../../constants/navItems";

export function MobileNav() {
  return (
    <nav
      style={{
        display: "flex",
        borderTop: "1px solid var(--color-border)",
        background: "var(--color-surface-1)",
      }}
    >
      {NAV_ITEMS.map((item) => (
        <NavLink
          key={item.to}
          to={item.to}
          end={item.to === "/"}
          style={({ isActive }) => ({
            flex: 1,
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            padding: "8px 0",
            textDecoration: "none",
            color: isActive ? "var(--color-primary)" : "var(--color-text-dim)",
            fontSize: "var(--font-size-xs)",
            fontFamily: "var(--font-sans)",
            gap: "2px",
          })}
        >
          <span style={{ fontSize: "1.1rem", fontFamily: "var(--font-mono)" }}>{item.icon}</span>
          <span>{item.label}</span>
        </NavLink>
      ))}
    </nav>
  );
}
