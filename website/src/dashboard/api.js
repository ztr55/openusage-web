const API_ROOT = "/api/v1";

let requestToken = "";
let accessToken = new URLSearchParams(window.location.search).get("access_token") || "";

class APIError extends Error {
  constructor(message, status, payload) {
    super(message);
    this.name = "APIError";
    this.status = status;
    this.payload = payload;
  }
}

async function request(path, options = {}) {
  const method = (options.method || "GET").toUpperCase();
  const headers = new Headers(options.headers || {});
  headers.set("Accept", "application/json");

  if (options.body !== undefined && options.body !== null) {
    headers.set("Content-Type", "application/json");
  }
  if (method !== "GET" && requestToken) {
    headers.set("X-OpenUsage-Request-Token", requestToken);
  }
  if (accessToken) {
    headers.set("Authorization", `Bearer ${accessToken}`);
  }

  const response = await fetch(`${API_ROOT}${path}`, {
    ...options,
    method,
    headers,
    cache: "no-store",
  });

  let payload = null;
  try {
    payload = await response.json();
  } catch {
    payload = null;
  }

  if (!response.ok) {
    const message = payload?.error || `Request failed (${response.status})`;
    throw new APIError(message, response.status, payload);
  }
  return payload;
}

export async function getBootstrap() {
  const payload = await request("/bootstrap");
  requestToken = payload?.request_token || requestToken;
  if (accessToken) {
    const url = new URL(window.location.href);
    url.searchParams.delete("access_token");
    window.history.replaceState({}, "", url);
  }
  return payload;
}

export function getRequestToken() {
  return requestToken;
}

export function getSnapshots(windowID) {
  const query = windowID ? `?window=${encodeURIComponent(windowID)}` : "";
  return request(`/snapshots${query}`);
}

export function getHealth() {
  return request("/health");
}

export function getBrowsers() {
  return request("/browsers");
}

export function getIntegrations() {
  return request("/integrations");
}

function mutate(path, method, value) {
  return request(path, {
    method,
    body: value === undefined ? undefined : JSON.stringify(value),
  });
}

export function patchDashboardSettings(value) {
  return mutate("/settings/dashboard", "PATCH", value);
}

export function patchTimeWindow(windowID) {
  return mutate("/settings/time-window", "PATCH", { window: windowID });
}

export function patchTheme(theme) {
  return mutate("/settings/theme", "PATCH", { theme });
}

export function patchUISettings(value) {
  return mutate("/settings/ui", "PATCH", value);
}

export function patchProviderSettings(providers) {
  return mutate("/settings/providers", "PATCH", { providers });
}

export function patchSectionSettings(value) {
  return mutate("/settings/sections", "PATCH", value);
}

export function patchTelemetryLink(value) {
  return mutate("/settings/telemetry-links", "PATCH", value);
}

export function installDaemon() {
  return mutate("/daemon/install", "POST");
}

export function saveCredential(accountID, providerID, apiKey) {
  return mutate(`/accounts/${encodeURIComponent(accountID)}/credential`, "PUT", {
    provider_id: providerID,
    api_key: apiKey,
  });
}

export function deleteCredential(accountID) {
  return mutate(`/accounts/${encodeURIComponent(accountID)}/credential`, "DELETE");
}

export function connectBrowserSession(accountID, providerID, browser) {
  return mutate(`/accounts/${encodeURIComponent(accountID)}/browser-session`, "POST", {
    provider_id: providerID,
    browser,
  });
}

export function deleteBrowserSession(accountID) {
  return mutate(`/accounts/${encodeURIComponent(accountID)}/browser-session`, "DELETE");
}

export function installIntegration(id) {
  return mutate(`/integrations/${encodeURIComponent(id)}/install`, "POST");
}

export function uninstallIntegration(id) {
  return mutate(`/integrations/${encodeURIComponent(id)}/uninstall`, "POST");
}

export { APIError };
