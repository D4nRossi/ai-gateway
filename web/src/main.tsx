import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import App from "./App";
import { ErrorBoundary } from "./components/ErrorBoundary";
import "./index.css";

// The Go server serves the SPA from /ui/, so React Router uses that as its basename.
// All client-side routes are written relative to /ui (e.g. <Route path="/applications" />).
const rootEl = document.getElementById("root");
if (!rootEl) {
  throw new Error("missing #root element");
}

createRoot(rootEl).render(
  <StrictMode>
    <ErrorBoundary>
      <BrowserRouter basename="/ui">
        <App />
      </BrowserRouter>
    </ErrorBoundary>
  </StrictMode>,
);
