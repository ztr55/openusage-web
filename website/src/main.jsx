import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import { initAnalytics } from "./analytics";
import "./styles.css";

const isDashboard = window.location.pathname.startsWith("/app");
const Dashboard = React.lazy(() => import("./dashboard/DashboardApp"));

if (!isDashboard) {
  initAnalytics();
}

const root = document.getElementById("root");
const app = isDashboard ? (
  <React.StrictMode>
    <DashboardApp />
  </React.StrictMode>
) : (
  <React.StrictMode>
    <App />
  </React.StrictMode>
);

// Only the marketing route is prerendered. The dashboard must mount fresh even
// though it shares the same index.html fallback.
if (!isDashboard && root.childNodes.length > 0) {
  ReactDOM.hydrateRoot(root, app);
} else {
  ReactDOM.createRoot(root).render(app);
}

// Keep the marketing bundle's analytics and hydration path separate from the
// local dashboard. The dynamic import is still statically analyzable by Vite.
function DashboardApp() {
  return (
    <React.Suspense fallback={<div className="dashboard-loading-shell">Loading OpenUsage...</div>}>
      <Dashboard />
    </React.Suspense>
  );
}
