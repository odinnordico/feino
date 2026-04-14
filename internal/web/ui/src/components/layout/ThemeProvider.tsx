import { useEffect } from "react";
import { useSessionStore } from "../../store/sessionStore";

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const theme = useSessionStore((s) => s.theme);

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", theme);
  }, [theme]);

  return <>{children}</>;
}
