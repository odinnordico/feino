export type NavItem = {
  to: string;
  label: string;
  icon: string;
}

export const NAV_ITEMS: NavItem[] = [
  { to: "/",        label: "Chat",     icon: "◈" },
  { to: "/history", label: "History",  icon: "⊟" },
  { to: "/profile", label: "Profile",  icon: "◉" },
  { to: "/settings",label: "Settings", icon: "⚙" },
];
