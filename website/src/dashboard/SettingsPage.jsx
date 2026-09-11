import { useMemo, useState } from "react";
import {
  connectBrowserSession,
  deleteBrowserSession,
  deleteCredential,
  getBrowsers,
  installIntegration,
  patchDashboardSettings,
  patchProviderSettings,
  patchSectionSettings,
  patchTelemetryLink,
  patchTheme,
  patchTimeWindow,
  patchUISettings,
  uninstallIntegration,
  saveCredential,
} from "./api";
import { accountName, providerName } from "./data";
import { Icon, ProviderIcon } from "./icons";

const settingsSections = [
  { id: "display", label: "Display", icon: "monitor" },
  { id: "providers", label: "Providers", icon: "server" },
  { id: "sections", label: "Sections", icon: "spark" },
  { id: "credentials", label: "Credentials", icon: "key" },
  { id: "integrations", label: "Integrations", icon: "plug" },
  { id: "telemetry", label: "Telemetry", icon: "activity" },
];

export default function SettingsPage({ bootstrap, onChanged }) {
  const [activeSection, setActiveSection] = useState("display");
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState("");

  async function update(label, operation) {
    setBusy(label);
    setMessage("");
    try {
      await operation();
      setMessage("Saved");
      await onChanged();
    } catch (error) {
      setMessage(error.message || "Could not save changes.");
    } finally {
      setBusy("");
    }
  }

  return (
    <section className="settings-layout">
      <div className="settings-intro">
        <div><span className="eyebrow eyebrow--accent">CONTROL PLANE</span><h2>Make the dashboard fit the way you work.</h2><p>These changes are stored locally in your OpenUsage configuration. Credentials never come back to this page.</p></div>
        {message ? <div className={`settings-message${message === "Saved" ? " settings-message--ok" : ""}`} role="status"><Icon name={message === "Saved" ? "check" : "warning"} size={15} />{message}</div> : null}
      </div>
      <div className="settings-shell panel">
        <nav className="settings-nav" aria-label="Settings sections">
          {settingsSections.map((section) => <button aria-label={section.label} className={`settings-nav__item${activeSection === section.id ? " settings-nav__item--active" : ""}`} key={section.id} onClick={() => setActiveSection(section.id)} type="button"><Icon name={section.icon} size={17} /><span>{section.label}</span></button>)}
        </nav>
        <div className="settings-body">
          {activeSection === "display" ? <DisplaySettings bootstrap={bootstrap} busy={busy} update={update} /> : null}
          {activeSection === "providers" ? <ProviderSettings bootstrap={bootstrap} busy={busy} update={update} /> : null}
          {activeSection === "sections" ? <SectionSettings bootstrap={bootstrap} busy={busy} update={update} /> : null}
          {activeSection === "credentials" ? <CredentialSettings bootstrap={bootstrap} busy={busy} update={update} /> : null}
          {activeSection === "integrations" ? <IntegrationSettings bootstrap={bootstrap} busy={busy} update={update} /> : null}
          {activeSection === "telemetry" ? <TelemetrySettings bootstrap={bootstrap} busy={busy} update={update} /> : null}
        </div>
      </div>
    </section>
  );
}

function DisplaySettings({ bootstrap, busy, update }) {
  const settings = bootstrap?.settings || {};
  const dashboard = settings.dashboard || {};
  const data = settings.data || {};
  const ui = settings.ui || {};
  const themes = bootstrap?.themes || [];
  const [draftUI, setDraftUI] = useState(() => ({
    refresh_interval_seconds: ui.refresh_interval_seconds || 30,
    warn_threshold: ui.warn_threshold || 0.2,
    crit_threshold: ui.crit_threshold || 0.05,
  }));

  function saveUIField(field, value) {
    const parsed = Number(value);
    setDraftUI((current) => ({ ...current, [field]: value }));
    if (!Number.isFinite(parsed)) return;
    update(`ui:${field}`, () => patchUISettings({ [field]: parsed }));
  }

  return <div className="settings-section"><SettingsHeading kicker="DISPLAY" title="The quiet details matter" detail="Choose the window, theme, and density that should greet you." />
    <div className="settings-form-grid">
      <SettingField label="Default time window" detail="Used by the overview and analytics pages."><select value={data.time_window || "30d"} onChange={(event) => update("window", () => patchTimeWindow(event.target.value))}><option value="1d">Today</option><option value="3d">3 Days</option><option value="7d">7 Days</option><option value="30d">30 Days</option><option value="all">All Time</option></select></SettingField>
      <SettingField label="Dashboard layout" detail="The terminal layout preference remains available to the TUI."><select value={dashboard.view || "grid"} onChange={(event) => update("view", () => patchDashboardSettings({ view: event.target.value }))}><option value="grid">Grid</option><option value="stacked">Stacked</option><option value="tabs">Tabs</option><option value="split">Split</option><option value="compare">Compare</option></select></SettingField>
      <SettingField label="Web theme" detail="Themes are shared with the existing OpenUsage palette catalog."><select value={settings.theme || "Deep Space"} onChange={(event) => update("theme", () => patchTheme(event.target.value))}>{themes.map((theme) => <option key={theme.name} value={theme.name}>{theme.name}</option>)}</select></SettingField>
      <SettingField label="Refresh cadence" detail="How often the browser asks the daemon for fresh data."><div className="setting-input-suffix"><input min="1" max="3600" onBlur={(event) => saveUIField("refresh_interval_seconds", event.target.value)} onChange={(event) => setDraftUI((current) => ({ ...current, refresh_interval_seconds: event.target.value }))} type="number" value={draftUI.refresh_interval_seconds} /><span>seconds</span></div></SettingField>
      <SettingField label="Warning threshold" detail="Warn when less than this fraction remains."><div className="setting-input-suffix"><input max="1" min="0.01" onBlur={(event) => saveUIField("warn_threshold", event.target.value)} onChange={(event) => setDraftUI((current) => ({ ...current, warn_threshold: event.target.value }))} step="0.01" type="number" value={draftUI.warn_threshold} /><span>ratio</span></div></SettingField>
      <SettingField label="Critical threshold" detail="Critical state when less than this fraction remains."><div className="setting-input-suffix"><input max="1" min="0.01" onBlur={(event) => saveUIField("crit_threshold", event.target.value)} onChange={(event) => setDraftUI((current) => ({ ...current, crit_threshold: event.target.value }))} step="0.01" type="number" value={draftUI.crit_threshold} /><span>ratio</span></div></SettingField>
      <SettingField label="Automatic discovery" detail="Scan for installed tools and API keys on refresh."><Toggle label="Automatic discovery" checked={settings.auto_detect !== false} disabled={busy === "auto-detect"} onChange={(checked) => update("auto-detect", () => patchUISettings({ auto_detect: checked }))} /></SettingField>
    </div>
    <div className="settings-divider" />
    <div className="setting-toggle-row"><div><strong>Hide sections with no data</strong><span>Keep sparse provider cards focused.</span></div><Toggle label="Hide sections with no data" checked={Boolean(dashboard.hide_sections_with_no_data)} disabled={busy === "empty"} onChange={(checked) => update("empty", () => patchSectionSettings({ hide_sections_with_no_data: checked }))} /></div>
    <div className="setting-toggle-row"><div><strong>Hide monetary values</strong><span>Useful for subscription plans where API-equivalent cost is misleading.</span></div><TriStateToggle label="Monetary value visibility" value={dashboard.hide_costs} disabled={busy === "costs"} onChange={(value) => update("costs", () => patchDashboardSettings({ hide_costs: value }))} /></div>
  </div>;
}

function ProviderSettings({ bootstrap, busy, update }) {
  const accounts = bootstrap?.accounts || [];
  const configured = bootstrap?.settings?.dashboard?.providers || [];
  const preferences = new Map(configured.map((entry) => [entry.account_id, entry]));
  const ordered = useMemo(() => {
    const order = new Map(configured.map((entry, index) => [entry.account_id, index]));
    return [...accounts].sort((left, right) => (order.get(left.id) ?? 9999) - (order.get(right.id) ?? 9999) || left.id.localeCompare(right.id));
  }, [accounts, configured]);

  function preferenceList(nextOrder = ordered) {
    return nextOrder.map((account) => {
      const preference = preferences.get(account.id);
      return { account_id: account.id, enabled: preference?.enabled !== false, ...(preference?.hide_costs === undefined ? {} : { hide_costs: preference.hide_costs }) };
    });
  }

  function move(index, delta) {
    const next = [...ordered];
    const target = index + delta;
    if (target < 0 || target >= next.length) return;
    [next[index], next[target]] = [next[target], next[index]];
    update("order", () => patchProviderSettings(preferenceList(next)));
  }

  return <div className="settings-section"><SettingsHeading kicker="PROVIDERS" title="Choose what gets a seat at the table" detail="Reorder sources, hide noise, and keep the overview honest." />
    {ordered.length ? <div className="settings-provider-list">{ordered.map((account, index) => { const preference = preferences.get(account.id); const provider = bootstrap?.providers?.find((item) => item.id === account.provider_id); return <div className="settings-provider-row" key={account.id}><ProviderIcon name={provider?.name || providerName(account.provider_id, bootstrap?.providers)} providerID={account.provider_id} size={32} /><div className="settings-provider-row__identity"><strong>{accountName(account.id)}</strong><small>{provider?.name || providerName(account.provider_id, bootstrap?.providers)}{account.discovered ? " · discovered" : ""}</small></div><TriStateToggle label={`Cost visibility for ${accountName(account.id)}`} value={preference?.hide_costs} disabled={Boolean(busy)} onChange={(value) => update(`costs:${account.id}`, () => patchProviderSettings(preferenceList(ordered).map((entry) => entry.account_id === account.id ? { ...entry, hide_costs: value } : entry)))} /><Toggle label={`Show ${accountName(account.id)} in dashboard`} checked={preference?.enabled !== false} disabled={busy === account.id} onChange={(checked) => update(account.id, () => patchProviderSettings(preferenceList(ordered).map((entry) => entry.account_id === account.id ? { ...entry, enabled: checked } : entry)))} /><div className="reorder-actions"><button aria-label={`Move ${accountName(account.id)} up`} disabled={index === 0 || Boolean(busy)} onClick={() => move(index, -1)} type="button"><Icon name="chevron" size={14} className="rotate-180" /></button><button aria-label={`Move ${accountName(account.id)} down`} disabled={index === ordered.length - 1 || Boolean(busy)} onClick={() => move(index, 1)} type="button"><Icon name="chevron" size={14} /></button></div></div>; })}</div> : <div className="missing-data">No accounts discovered yet.</div>}
    <div className="settings-footnote"><Icon name="shield" size={15} />Account discovery remains automatic. Disabling a row only changes dashboard visibility.</div>
  </div>;
}

const widgetSectionLabels = {
  top_usage_progress: "Top usage progress",
  model_burn: "Model usage",
  client_burn: "Client usage",
  project_breakdown: "Project breakdown",
  tool_usage: "Tool usage",
  mcp_usage: "MCP usage",
  language_burn: "Language usage",
  code_stats: "Code statistics",
  daily_usage: "Daily usage",
  provider_burn: "Provider usage",
  upstream_providers: "Upstream providers",
  other_data: "Other data",
};

const detailSectionLabels = {
  usage: "Usage",
  spending: "Spending",
  models: "Models",
  clients: "Clients",
  projects: "Projects",
  tools: "Tools",
  mcp: "MCP usage",
  languages: "Languages",
  code_stats: "Code statistics",
  trends: "Trends",
  activity_heatmap: "Activity heatmap",
  cost_requests: "Cost and requests",
  forecast: "Forecast",
  upstream: "Hosting",
  provider_burn: "Provider usage",
  other_data: "Other data",
  timers: "Timers",
  info: "Info",
};

const defaultWidgetSections = Object.keys(widgetSectionLabels);
const defaultDetailSections = Object.keys(detailSectionLabels);

function SectionSettings({ bootstrap, busy, update }) {
  const configured = bootstrap?.settings?.dashboard || {};
  const widgetSections = configured.widget_sections?.length ? configured.widget_sections : defaultWidgetSections.map((id) => ({ id, enabled: true }));
  const detailSections = configured.detail_sections?.length ? configured.detail_sections : defaultDetailSections.map((id) => ({ id, enabled: true }));
  function toggle(kind, entries, id) {
    const next = entries.map((entry) => entry.id === id ? { ...entry, enabled: !entry.enabled } : entry);
    update(`section:${id}`, () => patchSectionSettings({ [kind]: next }));
  }
  return <div className="settings-section"><SettingsHeading kicker="SECTIONS" title="Keep the useful signal in frame" detail="The same section controls apply to the TUI and the local dashboard." /><div className="section-settings-grid"><SectionList entries={widgetSections} labelMap={widgetSectionLabels} title="Overview sections" onToggle={(id) => toggle("widget_sections", widgetSections, id)} busy={Boolean(busy)} /><SectionList entries={detailSections} labelMap={detailSectionLabels} title="Provider detail sections" onToggle={(id) => toggle("detail_sections", detailSections, id)} busy={Boolean(busy)} /></div><div className="settings-footnote"><Icon name="spark" size={15} />A disabled section disappears from its corresponding view. Provider-specific sections still appear only when data exists.</div></div>;
}

function SectionList({ busy, entries, labelMap, onToggle, title }) {
  return <div className="section-list"><div className="settings-subheading"><span className="panel-kicker">LAYOUT</span><h3>{title}</h3></div>{entries.map((entry) => <div className="section-setting-row" key={entry.id}><span>{labelMap[entry.id] || entry.id}</span><Toggle label={`Show ${labelMap[entry.id] || entry.id}`} checked={entry.enabled} disabled={busy} onChange={() => onToggle(entry.id)} /></div>)}</div>;
}

function CredentialSettings({ bootstrap, busy, update }) {
  const providers = (bootstrap?.providers || []).filter((provider) => provider.auth_type === "api_key" || provider.auth_types?.includes("api_key"));
  const accounts = bootstrap?.accounts || [];
  const [providerID, setProviderID] = useState(providers[0]?.id || "");
  const [accountID, setAccountID] = useState(providers[0]?.default_account_id || "");
  const [apiKey, setAPIKey] = useState("");
  const [selectedBrowserAccount, setSelectedBrowserAccount] = useState("");
  const [browsers, setBrowsers] = useState([]);
  const [browser, setBrowser] = useState("");
  const [browserError, setBrowserError] = useState("");
  const browserProviders = (bootstrap?.providers || []).filter((provider) => provider.auth_type === "browser_session" || provider.auth_types?.includes("browser_session"));
  const browserAccounts = browserProviders.map((provider) => accounts.find((account) => account.provider_id === provider.id) || ({
    id: provider.default_account_id || provider.id,
    provider_id: provider.id,
    browser_session: {},
  }));

  function selectProvider(next) {
    setProviderID(next);
    setAccountID(providers.find((provider) => provider.id === next)?.default_account_id || next);
  }

  async function connect(account, provider) {
    setSelectedBrowserAccount(account.id);
    setBrowserError("");
    try {
      const result = await getBrowsers();
      setBrowsers(result.browsers || []);
      setBrowser(result.browsers?.[0] || "");
    } catch (error) {
      setBrowsers([]);
      setBrowser("");
      setBrowserError(error.message || "Could not find browser cookie stores.");
    }
  }

  async function connectSelected(account, provider) {
    await update(`browser:${account.id}`, () => connectBrowserSession(account.id, provider.id, browser));
    setSelectedBrowserAccount("");
    setBrowsers([]);
  }

  return <div className="settings-section"><SettingsHeading kicker="CREDENTIALS" title="Keep the keys close, never in the browser" detail="The Go helper validates and stores credentials locally. This page only receives a status." />
    <div className="credential-form"><div className="form-grid form-grid--credential"><SettingField label="Provider"><select value={providerID} onChange={(event) => selectProvider(event.target.value)}>{providers.map((provider) => <option key={provider.id} value={provider.id}>{provider.name}</option>)}</select></SettingField><SettingField label="Account ID"><input value={accountID} onChange={(event) => setAccountID(event.target.value)} placeholder="my-account" /></SettingField></div><SettingField label="API key" detail="It is sent over loopback for validation, then cleared from this form."><div className="credential-input-row"><input autoComplete="off" onChange={(event) => setAPIKey(event.target.value)} placeholder="Paste a provider key" type="password" value={apiKey} /><button className="button button--primary" disabled={!providerID || !accountID.trim() || !apiKey.trim() || Boolean(busy)} onClick={() => update("credential", async () => { await saveCredential(accountID.trim(), providerID, apiKey); setAPIKey(""); })} type="button">{busy === "credential" ? "Checking..." : "Validate & save"}<Icon name="arrow" size={15} /></button></div></SettingField></div>
    <div className="settings-subheading"><span className="panel-kicker">LOCAL ACCOUNTS</span><h3>Configured and discovered sources</h3></div>
    <div className="credential-list">{accounts.map((account) => <CredentialRow account={account} bootstrap={bootstrap} busy={busy} key={account.id} onDelete={() => update(`delete:${account.id}`, () => deleteCredential(account.id))} />)}</div>
    {browserProviders.length ? <><div className="settings-subheading"><span className="panel-kicker">BROWSER SESSIONS</span><h3>Connect to dashboard-only data</h3></div>{browserError ? <div className="settings-inline-error" role="alert"><Icon name="warning" size={15} />{browserError}</div> : null}<div className="browser-session-list">{browserProviders.map((provider, index) => { const account = browserAccounts[index]; const session = account.browser_session || {}; return <div className="browser-session-row" key={account.id}><ProviderIcon name={provider.name} providerID={provider.id} size={30} /><div className="browser-session-row__copy"><strong>{accountName(account.id)}</strong><span>{session.connected ? `Connected via ${session.source_browser || "browser"}` : "Not connected"}{session.expired ? " · expired" : ""}</span></div><div className="browser-session-row__actions">{provider.browser_console_url ? <button className="text-button" onClick={() => window.open(provider.browser_console_url, "_blank", "noopener,noreferrer")} type="button">Open console <Icon name="external" size={14} /></button> : null}{session.connected ? <button className="text-button text-button--danger" disabled={Boolean(busy)} onClick={() => update(`disconnect:${account.id}`, () => deleteBrowserSession(account.id))} type="button">Disconnect</button> : <button className="button button--secondary" disabled={Boolean(busy)} onClick={() => connect(account, provider)} type="button">Choose browser <Icon name="arrow" size={14} /></button>}</div>{selectedBrowserAccount === account.id ? <div className="browser-picker"><select value={browser} onChange={(event) => setBrowser(event.target.value)}>{browsers.map((item) => <option key={item} value={item}>{item}</option>)}</select><button className="button button--primary" disabled={!browser || Boolean(busy)} onClick={() => connectSelected(account, provider)} type="button">Read selected cookie</button></div> : null}</div>; })}</div><div className="settings-footnote"><Icon name="shield" size={15} />OpenUsage reads only the browser and cookie you choose. Cookie values never reach this page.</div></> : null}
  </div>;
}

function CredentialRow({ account, bootstrap, busy, onDelete }) {
  const provider = bootstrap?.providers?.find((item) => item.id === account.provider_id);
  return <div className="credential-row"><ProviderIcon name={provider?.name || account.provider_id} providerID={account.provider_id} size={28} /><div className="credential-row__copy"><strong>{accountName(account.id)}</strong><span>{provider?.name || providerName(account.provider_id, bootstrap?.providers)} · {account.credential?.present ? `${account.credential.source || "configured"} credential` : "missing credential"}</span></div><span className={`credential-state credential-state--${account.credential?.present ? "ready" : "missing"}`}>{account.credential?.present ? "Ready" : "Missing"}</span>{account.credential?.present && account.credential?.source === "stored" ? <button className="icon-button icon-button--danger" aria-label={`Delete credential for ${accountName(account.id)}`} disabled={busy === `delete:${account.id}`} onClick={onDelete} type="button"><Icon name="close" size={15} /></button> : null}</div>;
}

function IntegrationSettings({ bootstrap, busy, update }) {
  const integrations = bootstrap?.integrations || [];
  return <div className="settings-section"><SettingsHeading kicker="INTEGRATIONS" title="Give the collector richer signals" detail="Install hooks and plugins for the tools that can report per-turn activity." /><div className="integration-list">{integrations.map((integration) => <div className="integration-row" key={integration.id}><div className={`integration-state integration-state--${integration.state}`}><Icon name={integration.state === "ready" ? "check" : integration.state === "outdated" ? "refresh" : "plug"} size={16} /></div><div className="integration-row__copy"><strong>{integration.name}</strong><span>{integration.summary || integration.state}{integration.installed_version ? ` · v${integration.installed_version}` : ""}</span></div><span className={`integration-badge integration-badge--${integration.state}`}>{integration.state}</span>{integration.state === "ready" ? <button className="text-button text-button--danger" disabled={Boolean(busy)} onClick={() => { if (window.confirm(`Remove ${integration.name}?`)) update(`integration:${integration.id}`, () => uninstallIntegration(integration.id)); }} type="button">Remove</button> : <button className="button button--secondary" disabled={Boolean(busy)} onClick={() => update(`integration:${integration.id}`, () => installIntegration(integration.id))} type="button">{integration.needs_upgrade ? "Upgrade" : "Install"}<Icon name="arrow" size={14} /></button>}</div>)}</div><div className="settings-footnote"><Icon name="warning" size={15} />Installing an integration writes a hook or plugin and patches the tool's local configuration. Existing files are backed up by OpenUsage.</div></div>;
}

function TelemetrySettings({ bootstrap, busy, update }) {
  const links = bootstrap?.settings?.telemetry?.provider_links || {};
  const [source, setSource] = useState("");
  const [target, setTarget] = useState("");
  const providerOptions = bootstrap?.providers || [];
  return <div className="settings-section"><SettingsHeading kicker="TELEMETRY" title="Make event data land in the right place" detail="Map source provider names from integrations to configured dashboard providers." /><div className="telemetry-link-form"><SettingField label="Source provider"><input value={source} onChange={(event) => setSource(event.target.value)} placeholder="e.g. google" /></SettingField><SettingField label="Dashboard provider"><select value={target} onChange={(event) => setTarget(event.target.value)}><option value="">Choose target</option>{providerOptions.map((provider) => <option key={provider.id} value={provider.id}>{provider.name}</option>)}</select></SettingField><button className="button button--primary" disabled={!source.trim() || !target || Boolean(busy)} onClick={() => update("link", async () => { await patchTelemetryLink({ source: source.trim(), target }); setSource(""); setTarget(""); })} type="button">Save mapping <Icon name="arrow" size={14} /></button></div><div className="telemetry-link-list">{Object.entries(links).map(([from, to]) => <div className="telemetry-link-row" key={from}><code>{from}</code><Icon name="arrow" size={14} /><strong>{providerName(to, providerOptions)}</strong><button className="icon-button icon-button--danger" aria-label={`Remove ${from} mapping`} disabled={Boolean(busy)} onClick={() => update(`unlink:${from}`, () => patchTelemetryLink({ source: from, delete: true }))} type="button"><Icon name="close" size={15} /></button></div>)}</div>{!Object.keys(links).length ? <div className="missing-data">No custom mappings. Built-in provider links are applied automatically.</div> : null}</div>;
}

function SettingsHeading({ detail, kicker, title }) {
  return <div className="settings-heading"><span className="panel-kicker">{kicker}</span><h3>{title}</h3><p>{detail}</p></div>;
}

function SettingField({ children, detail, label }) {
  return <label className="setting-field"><span>{label}</span>{children}{detail ? <small>{detail}</small> : null}</label>;
}

function Toggle({ checked, disabled, label = "Toggle setting", onChange }) {
  return <button aria-checked={checked} aria-label={label} className={`toggle${checked ? " toggle--on" : ""}`} disabled={disabled} onClick={() => onChange(!checked)} role="switch" type="button"><span /></button>;
}

function TriStateToggle({ disabled, label: ariaLabel = "Setting", onChange, value }) {
  const stateLabel = value === true ? "On" : value === false ? "Off" : "Auto";
  const next = value === null || value === undefined ? true : value === true ? false : null;
  return <button aria-label={ariaLabel} className={`tri-state tri-state--${stateLabel.toLowerCase()}`} disabled={disabled} onClick={() => onChange(next)} type="button"><span>{stateLabel}</span><Icon name="chevron" size={13} /></button>;
}
