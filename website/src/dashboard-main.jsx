import React from "react";
import ReactDOM from "react-dom/client";
import DashboardApp from "./dashboard/DashboardApp";
import "./styles.css";

ReactDOM.createRoot(document.getElementById("root")).render(
  <React.StrictMode>
    <DashboardApp basePath="" />
  </React.StrictMode>,
);
