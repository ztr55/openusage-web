import { useState } from "react";
import { providerIconURL } from "./data";

const paths = {
  activity: <><path d="M3 12h4l2-8 4 16 2-8h6" /></>,
  arrow: <><path d="M5 12h14" /><path d="m13 6 6 6-6 6" /></>,
  chart: <><path d="M4 19V5" /><path d="M4 19h16" /><path d="m7 15 3-3 3 2 5-6" /></>,
  check: <><path d="m5 12 4 4L19 6" /></>,
  chevron: <><path d="m7 10 5 5 5-5" /></>,
  close: <><path d="m6 6 12 12" /><path d="m18 6-12 12" /></>,
  database: <><ellipse cx="12" cy="5" rx="7" ry="3" /><path d="M5 5v7c0 1.7 3.1 3 7 3s7-1.3 7-3V5" /><path d="M5 12v7c0 1.7 3.1 3 7 3s7-1.3 7-3v-7" /></>,
  external: <><path d="M14 4h6v6" /><path d="m20 4-9 9" /><path d="M18 13v5a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h5" /></>,
  filter: <><path d="M4 5h16" /><path d="M7 12h10" /><path d="M10 19h4" /></>,
  gear: <><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-1.8 1.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6v.2h-2.6V20a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1-1.8-1.8.1-.1a1.7 1.7 0 0 0 .3-1.9 1.7 1.7 0 0 0-1.6-1H6v-2.6h.2a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9l-.1-.1 1.8-1.8.1.1a1.7 1.7 0 0 0 1.9.3 1.7 1.7 0 0 0 1-1.6V4h2.6v.2a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1 1.8 1.8-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.2v2.6H20a1.7 1.7 0 0 0-1.6 1Z" /></>,
  home: <><path d="m3 11 9-7 9 7" /><path d="M5 10v10h14V10" /><path d="M10 20v-6h4v6" /></>,
  key: <><circle cx="8" cy="15" r="4" /><path d="m11 12 8-8" /><path d="m15 7 2 2" /><path d="m17 5 2 2" /></>,
  menu: <><path d="M4 6h16" /><path d="M4 12h16" /><path d="M4 18h16" /></>,
  monitor: <><rect x="3" y="4" width="18" height="13" rx="2" /><path d="M8 21h8" /><path d="M12 17v4" /></>,
  plug: <><path d="M8 12h8" /><path d="M12 8v8" /><path d="M7 4v4" /><path d="M17 4v4" /><path d="M5 8h14v3a7 7 0 0 1-14 0V8Z" /></>,
  refresh: <><path d="M20 11a8 8 0 0 0-14.7-3L3 11" /><path d="M3 5v6h6" /><path d="M4 13a8 8 0 0 0 14.7 3L21 13" /><path d="M21 19v-6h-6" /></>,
  search: <><circle cx="10.8" cy="10.8" r="6.8" /><path d="m16 16 5 5" /></>,
  server: <><rect x="3" y="4" width="18" height="6" rx="1" /><rect x="3" y="14" width="18" height="6" rx="1" /><path d="M7 7h.01M7 17h.01" /></>,
  shield: <><path d="M12 3 19 6v5c0 4.5-3 8-7 10-4-2-7-5.5-7-10V6l7-3Z" /><path d="m9 12 2 2 4-4" /></>,
  spark: <><path d="m12 3 1.6 5.4L19 10l-5.4 1.6L12 17l-1.6-5.4L5 10l5.4-1.6L12 3Z" /><path d="m19 16 .7 2.3L22 19l-2.3.7L19 22l-.7-2.3L16 19l2.3-.7L19 16Z" /></>,
  warning: <><path d="m12 3 9 16H3L12 3Z" /><path d="M12 9v4" /><path d="M12 16h.01" /></>,
  window: <><rect x="3" y="4" width="18" height="16" rx="2" /><path d="M3 9h18" /><path d="M8 2v4M16 2v4" /></>,
  x: <><path d="m6 6 12 12" /><path d="m18 6-12 12" /></>,
};

export function Icon({ name, size = 18, strokeWidth = 1.8, className = "" }) {
  return (
    <svg
      aria-hidden="true"
      className={`ui-icon ${className}`}
      fill="none"
      height={size}
      viewBox="0 0 24 24"
      width={size}
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      strokeWidth={strokeWidth}
    >
      {paths[name] || paths.activity}
    </svg>
  );
}

export function ProviderIcon({ providerID, name, size = 32 }) {
  const [failed, setFailed] = useState(false);
  const initial = (name || providerID || "?").slice(0, 1).toUpperCase();
  if (failed) {
    return <span className="provider-monogram" style={{ width: size, height: size }}>{initial}</span>;
  }
  return (
    <img
      alt=""
      aria-hidden="true"
      className="provider-icon"
      height={size}
      onError={() => setFailed(true)}
      src={providerIconURL(providerID, import.meta.env.BASE_URL || "/")}
      width={size}
    />
  );
}

export function BrandMark({ compact = false }) {
  return (
    <div className={`brand-mark${compact ? " brand-mark--compact" : ""}`}>
      <span className="brand-mark__orb" aria-hidden="true"><span /></span>
      <span className="brand-mark__word">OpenUsage</span>
    </div>
  );
}
