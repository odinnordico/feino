import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import "@fontsource/inter";
import "@fontsource/jetbrains-mono";
import "highlight.js/styles/github-dark.css";
import "./styles/globals.css";
import App from "./App";
import { ErrorBoundary } from "./components/shared/ErrorBoundary";

const rootElement = document.getElementById("root");
if (!rootElement) {
  throw new Error("Root element #root not found in document");
}
createRoot(rootElement).render(
  <StrictMode>
    <BrowserRouter>
      <ErrorBoundary>
        <App />
      </ErrorBoundary>
    </BrowserRouter>
  </StrictMode>
);
