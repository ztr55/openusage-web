const COST_KEY_RE = /(cost|spend|credit|balance|price|burn_rate)/i;
const TOKEN_KEY_RE = /(token|tokens)/i;
const REQUEST_KEY_RE = /(request|requests|completion)/i;

export const providerIconNames = {
  alibaba_cloud: "alibabacloud",
  anthropic: "anthropic",
  antigravity: "gemini",
  azure_openai: "openai",
  claude_code: "claudecode",
  codebuff: "codex",
  codex: "codex",
  copilot: "copilot",
  crush: "codex",
  cursor: "cursor",
  deepseek: "deepseek",
  droid: "droid",
  gemini_api: "gemini",
  gemini_cli: "geminicli",
  goose: "goose",
  groq: "groq",
  hermes: "hermes",
  kimi_cli: "kimi",
  kilocode: "kilocode",
  kiro: "kiro",
  mistral: "mistral",
  moonshot: "moonshot",
  mux: "mux",
  ollama: "ollama",
  openai: "openai",
  openclaw: "openclaw",
  opencode: "opencode",
  openrouter: "openrouter",
  perplexity: "perplexity",
  pi: "pi",
  qwen_cli: "qwen",
  roocode: "roocode",
  xai: "grok",
  zai: "zai",
  zed: "zed",
};

export function providerIconURL(providerID, base = "/") {
  const name = providerIconNames[providerID] || providerID;
  return `${base.replace(/\/$/, "")}/icons/${name}.svg`;
}

export function providerName(providerID, providers = []) {
  return providers.find((provider) => provider.id === providerID)?.name || providerID || "Unknown provider";
}

export function accountName(accountID) {
  if (!accountID) return "Unknown account";
  return accountID
    .replace(/[_-]+/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

export function numeric(value) {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

export function metricValue(metric, field = "used") {
  if (!metric || metric[field] === undefined || metric[field] === null) return null;
  return numeric(metric[field]);
}

export function metricIsCost(key, metric = {}) {
  return metric.unit && /usd|cny|eur|gbp|credits|credit/i.test(metric.unit)
    ? true
    : COST_KEY_RE.test(key);
}

export function usedPercent(key, metric) {
  if (!metric) return -1;
  const used = metricValue(metric, "used");
  const limit = metricValue(metric, "limit");
  const remaining = metricValue(metric, "remaining");
  if (metric.unit === "%" || /percent_used|usage_percent/i.test(key)) {
    return used === null ? -1 : clamp(used, 0, 100);
  }
  if (limit !== null && limit > 0 && remaining !== null) {
    return clamp((1 - remaining / limit) * 100, 0, 100);
  }
  if (limit !== null && limit > 0 && used !== null) {
    return clamp((used / limit) * 100, 0, 100);
  }
  return -1;
}

export function remainingPercent(key, metric) {
  const used = usedPercent(key, metric);
  return used < 0 ? -1 : 100 - used;
}

export function formatCompact(value, digits = 1) {
  const amount = numeric(value);
  const absolute = Math.abs(amount);
  if (absolute >= 1_000_000_000) return `${trimNumber(amount / 1_000_000_000, digits)}B`;
  if (absolute >= 1_000_000) return `${trimNumber(amount / 1_000_000, digits)}M`;
  if (absolute >= 1_000) return `${trimNumber(amount / 1_000, digits)}K`;
  return trimNumber(amount, absolute < 10 ? 2 : 0);
}

export function formatMoney(value, unit = "USD") {
  const amount = numeric(value);
  const symbol = { USD: "$", CNY: "¥", EUR: "€", GBP: "£" }[unit] || `${unit} `;
  return `${symbol}${amount.toFixed(2)}`;
}

export function formatMetric(value, unit = "") {
  if (unit && /usd|cny|eur|gbp/i.test(unit)) return formatMoney(value, unit.toUpperCase());
  if (unit === "%") return `${numeric(value).toFixed(0)}%`;
  return `${formatCompact(value)}${unit ? ` ${unit}` : ""}`;
}

export function firstMetric(snapshot, keys, hideCosts = false) {
  for (const key of keys) {
    const metric = snapshot?.metrics?.[key];
    if (!metric || (hideCosts && metricIsCost(key, metric))) continue;
    if (metricValue(metric, "used") !== null || metricValue(metric, "remaining") !== null || metricValue(metric, "limit") !== null) {
      return { key, metric };
    }
  }
  return null;
}

export function primaryGauge(snapshot) {
  const keys = [
    "spend_limit",
    "plan_spend",
    "available_balance",
    "credits",
    "credit_balance",
    "usage_five_hour",
    "usage_seven_day",
    "quota_pro",
    "quota",
    "quota_flash",
    "context_window",
  ];
  const candidate = firstMetric(snapshot, keys, snapshot.hide_costs);
  if (candidate && usedPercent(candidate.key, candidate.metric) >= 0) return candidate;
  const entries = Object.entries(snapshot?.metrics || {});
  for (const [key, metric] of entries) {
    if (snapshot.hide_costs && metricIsCost(key, metric)) continue;
    if (usedPercent(key, metric) >= 0) return { key, metric };
  }
  return null;
}

export function resetFor(snapshot, key) {
  const candidates = [key, `${key}_reset`, "usage_reset", "quota_reset", "reset"];
  for (const candidate of candidates) {
    const value = snapshot?.resets?.[candidate];
    if (value) return value;
  }
  const future = Object.values(snapshot?.resets || {})
    .map((value) => new Date(value))
    .filter((value) => !Number.isNaN(value.valueOf()) && value.valueOf() > Date.now())
    .sort((a, b) => a - b);
  return future[0]?.toISOString() || "";
}

export function formatRelativeTime(value, now = Date.now()) {
  if (!value) return "No reset scheduled";
  const delta = new Date(value).valueOf() - now;
  if (!Number.isFinite(delta)) return "Unknown reset";
  if (delta <= 0) return "Resetting soon";
  const minutes = Math.round(delta / 60_000);
  if (minutes < 60) return `in ${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const remainder = minutes % 60;
  if (hours < 24) return `in ${hours}h ${remainder}m`;
  return `in ${Math.floor(hours / 24)}d`;
}

export function timeAgo(value, now = Date.now()) {
  const delta = Math.max(0, now - new Date(value).valueOf());
  if (!Number.isFinite(delta)) return "unknown";
  const seconds = Math.round(delta / 1000);
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  return `${Math.round(minutes / 60)}h ago`;
}

export function costSummary(snapshot) {
  if (!snapshot || snapshot.hide_costs) return { value: 0, unit: "USD", hidden: true };
  const modelCost = (snapshot.model_usage || []).reduce((total, record) => total + numeric(record.cost_usd), 0);
  const keys = [
    "window_credit_spend",
    "window_cost",
    "total_cost_usd",
    "all_time_api_cost",
    "billing_total_cost",
    "composer_cost",
    "total_cost",
    "cli_cost",
    "plan_total_spend_usd",
    "individual_spend",
    "monthly_cost",
    "today_api_cost",
    "daily_cost_usd",
    "today_cost",
  ];
  let metricCost = 0;
  let unit = "USD";
  for (const key of keys) {
    const metric = snapshot.metrics?.[key];
    const value = metricValue(metric, "used");
    if (value !== null && value > 0) {
      metricCost = Math.max(metricCost, value);
      unit = metric.unit || unit;
    }
  }
  return { value: Math.max(modelCost, metricCost), unit, hidden: false };
}

export function tokenSummary(snapshot) {
  const records = snapshot?.model_usage || [];
  const recordTokens = records.reduce((total, record) => {
    const explicit = numeric(record.total_tokens);
    return total + (explicit || numeric(record.input_tokens) + numeric(record.output_tokens));
  }, 0);
  if (recordTokens > 0) return recordTokens;
  for (const key of ["window_tokens", "analytics_tokens", "tokens_total", "total_tokens", "today_tokens"]) {
    const value = metricValue(snapshot?.metrics?.[key]);
    if (value !== null && value > 0) return value;
  }
  return Object.entries(snapshot?.metrics || {}).reduce((total, [key, metric]) => {
    if (!TOKEN_KEY_RE.test(key) || metricIsCost(key, metric)) return total;
    return total + numeric(metricValue(metric) || 0);
  }, 0);
}

export function requestSummary(snapshot) {
  const records = snapshot?.model_usage || [];
  const recordRequests = records.reduce((total, record) => total + numeric(record.requests), 0);
  if (recordRequests > 0) return recordRequests;
  for (const key of ["window_requests", "analytics_requests", "requests", "requests_today", "recent_requests"]) {
    const value = metricValue(snapshot?.metrics?.[key]);
    if (value !== null && value > 0) return value;
  }
  return Object.entries(snapshot?.metrics || {}).reduce((total, [key, metric]) => {
    if (!REQUEST_KEY_RE.test(key) || metricIsCost(key, metric)) return total;
    return total + numeric(metricValue(metric) || 0);
  }, 0);
}

export function aggregateTotals(snapshots) {
  const rows = Object.values(snapshots || {});
  return rows.reduce((total, snapshot) => {
    const cost = costSummary(snapshot);
    return {
      cost: total.cost + (cost.hidden ? 0 : cost.value),
      costVisible: total.costVisible || !cost.hidden,
      tokens: total.tokens + tokenSummary(snapshot),
      requests: total.requests + requestSummary(snapshot),
      active: total.active + (snapshot.status === "OK" || snapshot.status === "NEAR_LIMIT" ? 1 : 0),
      providers: total.providers + 1,
      attention: total.attention + (snapshot.status === "OK" ? 0 : 1),
    };
  }, { cost: 0, costVisible: false, tokens: 0, requests: 0, active: 0, providers: 0, attention: 0 });
}

export function aggregateSeries(snapshots, keys, hideCosts = false) {
  const values = new Map();
  for (const snapshot of Object.values(snapshots || {})) {
    const isCostSeries = keys.some((key) => /cost/i.test(key));
    if (isCostSeries && (hideCosts || snapshot.hide_costs)) continue;
    let points = null;
    for (const key of keys) {
      if (snapshot.daily_series?.[key]?.length) {
        points = snapshot.daily_series[key];
        break;
      }
    }
    for (const point of points || []) {
      values.set(point.date, (values.get(point.date) || 0) + numeric(point.value));
    }
  }
  return [...values.entries()]
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([date, value]) => ({ date, value }));
}

export function modelRows(snapshots, hideCosts = false) {
  const rows = new Map();
  for (const snapshot of Object.values(snapshots || {})) {
    for (const record of snapshot.model_usage || []) {
      const name = record.canonical || record.canonical_lineage_id || record.raw_model_id || "Unknown model";
      const row = rows.get(name) || { name, cost: 0, input: 0, output: 0, total: 0, requests: 0, providers: new Set() };
      row.cost += hideCosts || snapshot.hide_costs ? 0 : numeric(record.cost_usd);
      row.input += numeric(record.input_tokens);
      row.output += numeric(record.output_tokens);
      row.total += numeric(record.total_tokens) || numeric(record.input_tokens) + numeric(record.output_tokens);
      row.requests += numeric(record.requests);
      row.providers.add(snapshot.provider_id);
      rows.set(name, row);
    }
  }
  return [...rows.values()]
    .map((row) => ({ ...row, providers: [...row.providers] }))
    .filter((row) => row.cost > 0 || row.total > 0 || row.requests > 0)
    .sort((left, right) => (right.cost || right.total || right.requests) - (left.cost || left.total || left.requests));
}

export function dimensionRows(snapshots, dimension) {
  const rows = new Map();
  const prefixes = {
    client: ["usage_client_", "client_"],
    project: ["usage_project_", "project_"],
    tool: ["usage_tool_", "tool_"],
    mcp: ["usage_mcp_", "mcp_"],
    language: ["lang_"],
  }[dimension] || [];

  for (const snapshot of Object.values(snapshots || {})) {
    for (const [key, metric] of Object.entries(snapshot.metrics || {})) {
      if (metric?.used === undefined || metric?.used === null) continue;
      const match = prefixes.find((prefix) => key.startsWith(prefix));
      if (!match) continue;
      let name = key.slice(match.length);
      for (const suffix of ["_requests_today", "_requests", "_calls_today", "_calls", "_total", "_tokens"]) {
        if (name.endsWith(suffix)) name = name.slice(0, -suffix.length);
      }
      if (!name || ["total", "calls", "servers_active"].includes(name)) continue;
      const row = rows.get(name) || { name, value: 0 };
      row.value += numeric(metric.used);
      rows.set(name, row);
    }

    const seriesPrefix = `usage_${dimension}_`;
    for (const [key, points] of Object.entries(snapshot.daily_series || {})) {
      if (!key.startsWith(seriesPrefix)) continue;
      const name = key.slice(seriesPrefix.length);
      if (!name || !points?.length) continue;
      const row = rows.get(name) || { name, value: 0 };
      row.value = Math.max(row.value, points.reduce((total, point) => total + numeric(point.value), 0));
      rows.set(name, row);
    }
  }
  return [...rows.values()].filter((row) => row.value > 0).sort((left, right) => right.value - left.value);
}

export function metricRows(snapshot) {
  return Object.entries(snapshot?.metrics || {})
    .filter(([key, metric]) => !(snapshot.hide_costs && metricIsCost(key, metric)))
    .map(([key, metric]) => ({ key, metric, value: metricValue(metric) }))
    .filter((row) => row.value !== null || metricValue(row.metric, "remaining") !== null || metricValue(row.metric, "limit") !== null)
    .sort((left, right) => left.key.localeCompare(right.key));
}

export function visibleAccounts(bootstrap, snapshots, query = "") {
  const accounts = new Map((bootstrap?.accounts || []).map((account) => [account.id, account]));
  for (const snapshot of Object.values(snapshots || {})) {
    if (!accounts.has(snapshot.account_id)) {
      accounts.set(snapshot.account_id, {
        id: snapshot.account_id,
        provider_id: snapshot.provider_id,
        configured: false,
        discovered: true,
        credential: { present: true, kind: "detected", source: "detected" },
        browser_session: {},
      });
    }
  }
  const configured = bootstrap?.settings?.dashboard?.providers || [];
  const preferences = new Map(configured.map((preference, index) => [preference.account_id, { ...preference, index }]));
  const normalized = query.trim().toLowerCase();
  return [...accounts.values()].filter((account) => {
    const preference = preferences.get(account.id);
    if (preference && preference.enabled === false) return false;
    if (!normalized) return true;
    return `${account.id} ${account.provider_id}`.toLowerCase().includes(normalized);
  }).sort((left, right) => {
    const leftIndex = preferences.get(left.id)?.index ?? Number.MAX_SAFE_INTEGER;
    const rightIndex = preferences.get(right.id)?.index ?? Number.MAX_SAFE_INTEGER;
    return leftIndex - rightIndex || left.id.localeCompare(right.id);
  });
}

export function clamp(value, min, max) {
  return Math.min(max, Math.max(min, numeric(value)));
}

function trimNumber(value, digits) {
  return new Intl.NumberFormat("en-US", {
    maximumFractionDigits: digits,
    minimumFractionDigits: 0,
  }).format(value);
}
