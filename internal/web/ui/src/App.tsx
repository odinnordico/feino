import { useRoutes } from "react-router-dom";
import { ThemeProvider } from "./components/layout/ThemeProvider";
import { ToastContainer } from "./components/shared/ToastContainer";
import { OfflineBanner } from "./components/shared/OfflineBanner";
import { routes } from "./routes";

export default function App() {
  const element = useRoutes(routes);
  return (
    <ThemeProvider>
      <OfflineBanner />
      {element}
      <ToastContainer />
    </ThemeProvider>
  );
}
