import {
  startTransition,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  getBootstrap,
  getSnapshots,
  installDaemon,
  patchTimeWindow,
} from "./api";
import {
  accountName,
  aggregateSeries,
  aggregateTotals,
  clamp,
  costSummary,
  dimensionRows,
  formatCompact,
  formatMetric,
  formatMoney,
  formatRelativeTime,
  metricRows,
  modelRows,
  primaryGauge,
  providerName,
  requestSummary,
  resetFor,
  timeAgo,
  tokenSummary,
  usedPercent,
  visibleAccounts,
} from "./data";
import { BrandMark, Icon, ProviderIcon } from "./icons";
import SettingsPage from "./SettingsPage";
import "../dashboard.css";

const defaultWindow = "30d";

function readRoute() {
  const path = window.location.pathname.replace(/\/+$/, "") || "/app";
  if (path === "/app/settings") return { page: "settings", accountID: "" };
  if (path === "/app/analytics") return { page: "analytics", accountID: "" };
  if (path.startsWith("/app/provider/")) {
    let accountID;
    try {
      accountID = decodeURIComponent(path.slice("/app/provider/".length));
    } catch {
      return { page: "overview", accountID: "" };
    }
    return {
      page: "provider",
      accountID,
    };
  }
  return { page: "overview", accountID: "" };
}

function navigateTo(page, accountID = "") {
  const path = accountID
    ? `/app/provider/${encodeURIComponent(accountID)}`
    : `/app/${page === "overview" ? "" : `${page}/`}`;
  window.history.pushState({}, "", path);
  window.dispatchEvent(new PopStateEvent("popstate"));
}

export default function DashboardApp() {
  const [route, setRoute] = useState(readRoute);
  const [bootstrap, setBootstrap] = useState(null);
  const [snapshots, setSnapshots] = useState({});
  const [windowID, setWindowID] = useState("");
  const [snapshotWindow, setSnapshotWindow] = useState("");
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const [lastUpdated, setLastUpdated] = useState(0);
  const requestInFlight = useRef(false);
  const deferredQuery = useDeferredValue(query);

  useEffect(() => {
    const previousTitle = document.title;
    document.title = "OpenUsage Dashboard";
    return () => { document.title = previousTitle; };
  }, []);

  useEffect(() => {
    const onPopState = () => {
      startTransition(() => setRoute(readRoute()));
      setMobileNavOpen(false);
    };
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  async function refreshSnapshots(silent = false, requestedWindow = windowID) {
    if (requestInFlight.current) return;
    requestInFlight.current = true;
    if (!silent) setRefreshing(true);
    try {
      const response = await getSnapshots(requestedWindow || undefined);
      setSnapshots(response?.snapshots || {});
      setSnapshotWindow(response?.window || requestedWindow || defaultWindow);
      setLastUpdated(Date.now());
      setError("");
    } catch (requestError) {
      setError(requestError.message || "Could not load usage data.");
    } finally {
      requestInFlight.current = false;
      if (!silent) setRefreshing(false);
    }
  }

  async function retryConnection() {
    if (bootstrap) {
      await refreshSnapshots(false, windowID || defaultWindow);
      return;
    }

    setLoading(true);
    try {
      const [loadedBootstrap, loadedSnapshots] = await Promise.all([
        getBootstrap(),
        getSnapshots(),
      ]);
      const configuredWindow = loadedBootstrap?.settings?.data?.time_window
        || loadedSnapshots?.window
        || defaultWindow;
      setBootstrap(loadedBootstrap);
      setWindowID(configuredWindow);
      setSnapshotWindow(loadedSnapshots?.window || configuredWindow);
      setSnapshots(loadedSnapshots?.snapshots || {});
      setLastUpdated(Date.now());
      setError("");
    } catch (requestError) {
      setError(requestError.message || "Could not connect to OpenUsage.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    let cancelled = false;
    async function loadInitialData() {
      setLoading(true);
      try {
        const [loadedBootstrap, loadedSnapshots] = await Promise.all([
          getBootstrap(),
          getSnapshots(),
        ]);
        if (cancelled) return;
        const configuredWindow = loadedBootstrap?.settings?.data?.time_window
          || loadedSnapshots?.window
          || defaultWindow;
        setBootstrap(loadedBootstrap);
        setWindowID(configuredWindow);
        setSnapshotWindow(loadedSnapshots?.window || configuredWindow);
        setSnapshots(loadedSnapshots?.snapshots || {});
        setLastUpdated(Date.now());
        setError("");
      } catch (requestError) {
        if (!cancelled) setError(requestError.message || "Could not connect to OpenUsage.");
      } finally {
        if (!cancelled) setLoading(false);
      }
    }
    loadInitialData();
    return () => { cancelled = true; };
  }, []);

  useEffect(() => {
    if (!bootstrap || !windowID) return undefined;
    const seconds = Number(bootstrap.settings?.ui?.refresh_interval_seconds) || 30;
    const interval = Math.max(5, seconds) * 1000;
    const timer = window.setInterval(() => refreshSnapshots(true, windowID), interval);
    return () => window.clearInterval(timer);
  }, [bootstrap, windowID]);

  useEffect(() => {
    if (!notice) return undefined;
    const timer = window.setTimeout(() => setNotice(""), 3600);
    return () => window.clearTimeout(timer);
  }, [notice]);

  const accounts = useMemo(
    () => visibleAccounts(bootstrap, snapshots, deferredQuery),
    [bootstrap, snapshots, deferredQuery],
  );
  const providerByID = useMemo(
    () => new Map((bootstrap?.providers || []).map((provider) => [provider.id, provider])),
    [bootstrap],
  );
  const filteredAccounts = useMemo(() => accounts.filter((account) => {
    if (statusFilter === "all") return true;
    return (snapshots[account.id]?.status || "UNKNOWN") === statusFilter;
  }), [accounts, snapshots, statusFilter]);
  const totals = useMemo(() => aggregateTotals(snapshots), [snapshots]);

  function navigate(page, accountID = "") {
    navigateTo(page, accountID);
  }

  async function handleWindowChange(nextWindow) {
    if (!nextWindow || nextWindow === windowID) return;
    const previousWindow = windowID;
    setWindowID(nextWindow);
    setNotice(`Loading ${windowLabel(bootstrap, nextWindow)}...`);
    try {
      await patchTimeWindow(nextWindow);
      setBootstrap((current) => current ? {
        ...current,
        settings: {
          ...current.settings,
          data: { ...current.settings.data, time_window: nextWindow },
        },
      } : current);
      await refreshSnapshots(false, nextWindow);
    } catch (requestError) {
      setWindowID(previousWindow);
      setNotice(requestError.message || "Could not change the time window.");
    }
  }

  async function handleInstallDaemon() {
    setNotice("Installing the telemetry daemon...");
    try {
      await installDaemon();
      setNotice("Daemon installed. Waiting for the first snapshot...");
      const refreshed = await getBootstrap();
      setBootstrap(refreshed);
      await refreshSnapshots(false, windowID || defaultWindow);
    } catch (requestError) {
      setNotice(requestError.message || "Daemon installation failed.");
    }
  }

  async function reloadBootstrap() {
    try {
      const refreshed = await getBootstrap();
      setBootstrap(refreshed);
      setWindowID(refreshed?.settings?.data?.time_window || windowID || defaultWindow);
    } catch (requestError) {
      setNotice(requestError.message || "Could not reload settings.");
    }
  }

  if (loading) return <LoadingScreen />;

  const pageTitle = route.page === "analytics"
    ? "Analytics"
    : route.page === "settings"
      ? "Settings"
      : route.page === "provider"
        ? "Provider detail"
        : "Overview";

  return (
    <div className="dashboard-app" style={themeStyle(bootstrap)}>
      <aside className={`dashboard-sidebar${mobileNavOpen ? " dashboard-sidebar--open" : ""}`}>
        <div className="sidebar-topline">
          <BrandMark />
          <button className="icon-button sidebar-close" onClick={() => setMobileNavOpen(false)} type="button" aria-label="Close navigation">
            <Icon name="close" />
          </button>
        </div>
        <div className="sidebar-context">
          <span className="eyebrow">LOCAL WORKSPACE</span>
          <span className={`connection-dot connection-dot--${bootstrap?.daemon?.status || "unknown"}`} />
          <span>{daemonLabel(bootstrap?.daemon?.status)}</span>
        </div>
        <nav className="dashboard-nav" aria-label="Dashboard navigation">
          <NavItem active={route.page === "overview"} icon="home" label="Overview" onClick={() => navigate("overview")} />
          <NavItem active={route.page === "analytics"} icon="chart" label="Analytics" onClick={() => navigate("analytics")} />
          <NavItem active={route.page === "settings"} icon="gear" label="Settings" onClick={() => navigate("settings")} />
        </nav>
        <div className="sidebar-section-heading">
          <span>Accounts</span>
          <span className="count-badge">{accounts.length}</span>
        </div>
        <div className="sidebar-accounts">
          {accounts.length === 0 ? (
            <div className="sidebar-empty">No accounts detected yet.</div>
          ) : accounts.map((account) => {
            const snapshot = snapshots[account.id];
            return (
              <button
                className={`sidebar-account${route.accountID === account.id ? " sidebar-account--active" : ""}`}
                key={account.id}
                onClick={() => navigate("provider", account.id)}
                type="button"
              >
                <ProviderIcon
                  name={providerName(account.provider_id, bootstrap?.providers)}
                  providerID={account.provider_id}
                  size={24}
                />
                <span className="sidebar-account__copy">
                  <strong>{accountName(account.id)}</strong>
                  <small>{providerName(account.provider_id, bootstrap?.providers)}</small>
                </span>
                <span className={`status-dot status-dot--${statusTone(snapshot?.status)}`} aria-label={snapshot?.status || "Unknown"} />
              </button>
            );
          })}
        </div>
        <div className="sidebar-footer">
          <span className="sidebar-footer__label">OpenUsage</span>
          <span className="sidebar-footer__version">v{bootstrap?.app?.version || "dev"}</span>
        </div>
      </aside>

      {mobileNavOpen ? <button className="sidebar-scrim" onClick={() => setMobileNavOpen(false)} type="button" aria-label="Close navigation" /> : null}

      <main className="dashboard-main">
        <header className="dashboard-topbar">
          <div className="topbar-title">
            <button className="icon-button mobile-menu-button" onClick={() => setMobileNavOpen(true)} type="button" aria-label="Open navigation">
              <Icon name="menu" />
            </button>
            <div>
              <span className="topbar-kicker">OPENUSAGE / {pageTitle.toUpperCase()}</span>
              <h1>{pageTitle}</h1>
            </div>
          </div>
          <div className="topbar-actions">
            <div className="live-status" title={bootstrap?.daemon?.message || "Telemetry daemon status"}>
              <span className={`connection-dot connection-dot--${bootstrap?.daemon?.status || "unknown"}`} />
              <span>{daemonLabel(bootstrap?.daemon?.status)}</span>
            </div>
            <label className="window-select">
              <Icon name="window" size={16} />
              <span className="sr-only">Time window</span>
              <select value={windowID || defaultWindow} onChange={(event) => handleWindowChange(event.target.value)}>
                {(bootstrap?.time_windows || []).map((windowOption) => (
                  <option key={windowOption.id} value={windowOption.id}>{windowOption.label}</option>
                ))}
              </select>
              <Icon name="chevron" size={14} />
            </label>
            <button aria-label="Refresh usage data" className={`toolbar-button${refreshing ? " toolbar-button--loading" : ""}`} onClick={() => refreshSnapshots(false)} type="button">
              <Icon name="refresh" size={16} />
              <span className="toolbar-button__label">Refresh</span>
            </button>
            <button className="toolbar-button toolbar-button--settings" onClick={() => navigate("settings")} type="button" aria-label="Open settings">
              <Icon name="gear" size={17} />
            </button>
          </div>
        </header>

        <div className="dashboard-content">
          <div className="dashboard-status-row">
            <div className="breadcrumb"><span>Workspace</span><Icon name="chevron" size={13} /><strong>{windowLabel(bootstrap, snapshotWindow || windowID)}</strong></div>
            <div className="freshness">{lastUpdated ? `Updated ${timeAgo(lastUpdated)}` : "Waiting for data"}</div>
          </div>

          {bootstrap?.daemon?.status !== "running" ? (
            <DaemonBanner daemon={bootstrap?.daemon} onInstall={handleInstallDaemon} />
          ) : null}
          {error ? <InlineError message={error} onRetry={retryConnection} /> : null}
          {notice ? <div className="toast-notice" role="status"><Icon name="check" size={15} />{notice}</div> : null}

          {route.page === "overview" ? (
            <OverviewPage
              accounts={filteredAccounts}
              bootstrap={bootstrap}
              onNavigate={navigate}
              query={query}
              setQuery={setQuery}
              setStatusFilter={setStatusFilter}
              snapshots={snapshots}
              statusFilter={statusFilter}
              totals={totals}
              windowID={snapshotWindow || windowID}
            />
          ) : null}
          {route.page === "analytics" ? (
            <AnalyticsPage bootstrap={bootstrap} snapshots={snapshots} totals={totals} windowID={snapshotWindow || windowID} />
          ) : null}
          {route.page === "provider" ? (
            <ProviderDetailPage
              account={accounts.find((item) => item.id === route.accountID)}
              bootstrap={bootstrap}
              onNavigate={navigate}
              snapshot={snapshots[route.accountID]}
              windowID={snapshotWindow || windowID}
            />
          ) : null}
          {route.page === "settings" ? (
            <SettingsPage bootstrap={bootstrap} onChanged={reloadBootstrap} snapshots={snapshots} />
          ) : null}
        </div>
      </main>
    </div>
  );
}

function OverviewPage({ accounts, bootstrap, onNavigate, query, setQuery, setStatusFilter, snapshots, statusFilter, totals, windowID }) {
  const costSeries = aggregateSeries(snapshots, ["analytics_cost", "cost"], !totals.costVisible);
  const tokenSeries = aggregateSeries(snapshots, ["analytics_tokens", "tokens_total", "tokens"], false);
  const hideEmpty = Boolean(bootstrap?.settings?.dashboard?.hide_sections_with_no_data);
  const attention = accounts
    .map((account) => ({ account, snapshot: snapshots[account.id] }))
    .filter(({ snapshot }) => snapshot && (snapshot.status !== "OK" || (primaryGauge(snapshot) && usedPercent(primaryGauge(snapshot).key, primaryGauge(snapshot).metric) >= 80)))
    .sort((left, right) => attentionScore(right.snapshot) - attentionScore(left.snapshot));

  return (
    <>
      <section className="page-intro">
        <div>
          <span className="eyebrow eyebrow--accent">USAGE CONTROL ROOM</span>
          <h2>Know what is burning before it becomes a surprise.</h2>
          <p>One calm view across the agents, APIs, and local runtimes on this machine.</p>
        </div>
        <div className="intro-meta"><Icon name="database" size={15} />{accounts.length} configured sources</div>
      </section>

      <section className="summary-grid" aria-label="Usage summary">
        <StatCard accent="green" label={totals.costVisible ? "Window spend" : "Window activity"} value={totals.costVisible ? formatMoney(totals.cost) : `${formatCompact(totals.tokens)} tok`} detail={totals.costVisible ? windowLabel(bootstrap, windowID) : "Cost visibility is off"} icon={totals.costVisible ? "activity" : "chart"} />
        <StatCard accent="aqua" label="Token volume" value={`${formatCompact(totals.tokens)} tok`} detail={`${formatCompact(totals.requests)} requests`} icon="spark" />
        <StatCard accent="amber" label="Active sources" value={`${totals.active}/${accounts.length}`} detail={totals.providers ? (totals.active === totals.providers ? "All reporting normally" : "Review source status") : "Waiting for first snapshot"} icon="server" />
        <StatCard accent={totals.attention ? "red" : "lavender"} label="Needs attention" value={`${totals.attention}`} detail={totals.attention ? "Open the queue below" : "Nothing urgent"} icon={totals.attention ? "warning" : "shield"} />
      </section>

      {attention.length > 0 ? (
        <section className="attention-panel panel panel--flush">
          <div className="panel-heading panel-heading--attention">
            <div><span className="panel-kicker">ATTENTION</span><h3>Sources that need a look</h3></div>
            <span className="panel-heading__count">{attention.length} flagged</span>
          </div>
          <div className="attention-list">
            {attention.slice(0, 5).map(({ account, snapshot }) => (
              <button className="attention-row" key={account.id} onClick={() => onNavigate("provider", account.id)} type="button">
                <ProviderIcon name={providerName(account.provider_id, bootstrap?.providers)} providerID={account.provider_id} size={30} />
                <span className="attention-row__copy"><strong>{accountName(account.id)}</strong><small>{providerName(account.provider_id, bootstrap?.providers)}{snapshot.message ? ` · ${snapshot.message}` : ""}</small></span>
                <AttentionValue snapshot={snapshot} />
                <Icon name="arrow" size={16} className="attention-row__arrow" />
              </button>
            ))}
          </div>
        </section>
      ) : null}

      {sectionEnabled(bootstrap, "daily_usage", "widget_sections") && (!hideEmpty || costSeries.length || tokenSeries.length) ? <section className="chart-grid">
        <ChartPanel
          color="green"
          empty={!costSeries.length}
          label={totals.costVisible ? "Window spend" : "Spend hidden"}
          points={costSeries}
          subtitle={totals.costVisible ? "Provider-reported and event-derived cost" : "Enable cost visibility in settings to see spend"}
          valueFormatter={(value) => totals.costVisible ? formatMoney(value) : ""}
        />
        <ChartPanel
          color="aqua"
          empty={!tokenSeries.length}
          label="Token volume"
          points={tokenSeries}
          subtitle="Daily input, output, and cached activity"
          valueFormatter={(value) => `${formatCompact(value)} tok`}
        />
      </section> : null}

      <section className="section-header-row">
        <div><span className="panel-kicker">PROVIDER PULSE</span><h3>Every source, at a glance</h3></div>
        <div className="provider-filters">
          <label className="search-field"><Icon name="search" size={15} /><span className="sr-only">Filter accounts</span><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Filter accounts" /></label>
          <label className="filter-select"><Icon name="filter" size={14} /><span className="sr-only">Filter status</span><select value={statusFilter} onChange={(event) => setStatusFilter(event.target.value)}><option value="all">All states</option><option value="OK">Healthy</option><option value="NEAR_LIMIT">Near limit</option><option value="LIMITED">Limited</option><option value="AUTH_REQUIRED">Auth required</option><option value="ERROR">Error</option></select></label>
        </div>
      </section>
      {accounts.length ? (
        <section className="provider-grid">
          {accounts.map((account) => <ProviderCard account={account} bootstrap={bootstrap} key={account.id} onClick={() => onNavigate("provider", account.id)} snapshot={snapshots[account.id]} />)}
        </section>
      ) : (
        <EmptyState title="No sources match that filter" detail="Try a different account name or status." icon="search" />
      )}
    </>
  );
}

function AnalyticsPage({ bootstrap, snapshots, totals, windowID }) {
  const hideEmpty = Boolean(bootstrap?.settings?.dashboard?.hide_sections_with_no_data);
  const models = modelRows(snapshots, !totals.costVisible);
  const providers = Object.values(snapshots).map((snapshot) => ({
    name: accountName(snapshot.account_id),
    value: totals.costVisible ? costSummary(snapshot).value : tokenSummary(snapshot),
    detail: totals.costVisible ? formatMoney(costSummary(snapshot).value, costSummary(snapshot).unit) : `${formatCompact(tokenSummary(snapshot))} tok`,
  })).sort((left, right) => right.value - left.value);
  const clients = dimensionRows(snapshots, "client");
  const projects = dimensionRows(snapshots, "project");
  const tools = dimensionRows(snapshots, "tool");
  const mcp = dimensionRows(snapshots, "mcp");
  const languages = dimensionRows(snapshots, "language");
  const series = aggregateSeries(snapshots, ["analytics_cost", "cost"], !totals.costVisible);

  return (
    <>
      <section className="page-intro page-intro--compact">
        <div><span className="eyebrow eyebrow--accent">WINDOW ANALYSIS</span><h2>Find the shape behind the number.</h2><p>{windowLabel(bootstrap, windowID)} across {totals.providers} sources. Rankings fall back to tokens where spend is unavailable.</p></div>
        <div className="intro-meta"><Icon name="chart" size={15} />{totals.costVisible ? "Cost-aware view" : "Token-only view"}</div>
      </section>
      <section className="analytics-hero panel">
        <div className="analytics-hero__head"><div><span className="panel-kicker">TOTAL ACTIVITY</span><h3>{totals.costVisible ? formatMoney(totals.cost) : `${formatCompact(totals.tokens)} tokens`}</h3></div><span className="analytics-hero__window">{windowLabel(bootstrap, windowID)}</span></div>
        <LineChart color="green" empty={!series.length} height={210} points={series} valueFormatter={(value) => totals.costVisible ? formatMoney(value) : formatCompact(value)} />
      </section>
      <section className="analytics-columns">
        <RankPanel accent="green" hideEmpty={hideEmpty} rows={providers.slice(0, 8)} title="Source leaders" subtitle={totals.costVisible ? "Spend by configured account" : "Token volume by configured account"} valueLabel={totals.costVisible ? "spend" : "tokens"} />
        <RankPanel accent="aqua" hideEmpty={hideEmpty} rows={models.slice(0, 8).map((row) => ({ name: row.name, value: totals.costVisible ? row.cost : row.total, detail: totals.costVisible ? formatMoney(row.cost) : `${formatCompact(row.total)} tok` }))} title="Model leaders" subtitle="Models driving the selected window" valueLabel={totals.costVisible ? "spend" : "tokens"} />
        <RankPanel accent="amber" hideEmpty={hideEmpty} rows={clients.slice(0, 8).map((row) => ({ ...row, detail: `${formatCompact(row.value)} requests` }))} title="Client hotspots" subtitle="Where activity originated" valueLabel="requests" />
      </section>
      <section className="analytics-columns analytics-columns--secondary">
        <RankPanel accent="lavender" hideEmpty={hideEmpty} rows={projects.slice(0, 8).map((row) => ({ ...row, detail: `${formatCompact(row.value)} requests` }))} title="Project hotspots" subtitle="Workspaces with the most usage" valueLabel="requests" />
        <RankPanel accent="peach" hideEmpty={hideEmpty} rows={tools.slice(0, 8).map((row) => ({ ...row, detail: `${formatCompact(row.value)} calls` }))} title="Tool activity" subtitle="Tool calls from agent telemetry" valueLabel="calls" />
        <RankPanel accent="rose" hideEmpty={hideEmpty} rows={[...mcp, ...languages].slice(0, 8).map((row) => ({ ...row, detail: `${formatCompact(row.value)} events` }))} title="MCP and language signals" subtitle="The extra context behind usage" valueLabel="events" />
      </section>
      <section className="analytics-note"><Icon name="shield" size={16} /><span>OpenUsage keeps this history on your machine. Values depend on what each provider and integration exposes.</span></section>
    </>
  );
}

function ProviderDetailPage({ account, bootstrap, onNavigate, snapshot, windowID }) {
  if (!snapshot) {
    return <EmptyState title={account ? `${accountName(account.id)} has no snapshot yet` : "Account not found"} detail={account ? "The daemon will keep trying on its next polling cycle." : "Choose an account from the navigation."} icon="database" action={account ? undefined : () => onNavigate("overview")} actionLabel="Back to overview" />;
  }
  const gauge = primaryGauge(snapshot);
  const gaugePercent = gauge ? usedPercent(gauge.key, gauge.metric) : -1;
  const cost = costSummary(snapshot);
  const models = modelRows({ [snapshot.account_id]: snapshot }, snapshot.hide_costs);
  const trends = aggregateSeries({ [snapshot.account_id]: snapshot }, ["analytics_cost", "cost"], snapshot.hide_costs);
  const tokens = aggregateSeries({ [snapshot.account_id]: snapshot }, ["analytics_tokens", "tokens_total", "tokens"], false);
  const clients = dimensionRows({ [snapshot.account_id]: snapshot }, "client");
  const projects = dimensionRows({ [snapshot.account_id]: snapshot }, "project");
  const mcp = dimensionRows({ [snapshot.account_id]: snapshot }, "mcp");
  const rows = metricRows(snapshot).slice(0, 18);
  const hideEmpty = Boolean(bootstrap?.settings?.dashboard?.hide_sections_with_no_data);
  const show = (section) => sectionEnabled(bootstrap, section, "detail_sections");

  return (
    <>
      <button className="back-link" onClick={() => onNavigate("overview")} type="button"><Icon name="arrow" size={15} className="back-link__icon" />Back to overview</button>
      <section className="detail-hero panel">
        <div className="detail-hero__identity"><ProviderIcon name={providerName(snapshot.provider_id, bootstrap?.providers)} providerID={snapshot.provider_id} size={54} /><div><div className="detail-hero__provider">{providerName(snapshot.provider_id, bootstrap?.providers)}</div><h2>{accountName(snapshot.account_id)}</h2><div className="detail-hero__meta"><StatusPill status={snapshot.status} />{snapshot.attributes?.plan_name ? <span>{snapshot.attributes.plan_name}</span> : null}{snapshot.attributes?.account_email ? <span>{snapshot.attributes.account_email}</span> : null}</div></div></div>
        <div className="detail-hero__side"><span className="panel-kicker">LAST SNAPSHOT</span><strong>{timeAgo(snapshot.timestamp)}</strong><small>{new Date(snapshot.timestamp).toLocaleString()}</small></div>
      </section>

      {snapshot.message && snapshot.status !== "OK" ? <div className={`detail-callout detail-callout--${statusTone(snapshot.status)}`}><Icon name={snapshot.status === "AUTH_REQUIRED" ? "key" : "warning"} size={17} /><span>{snapshot.message}</span></div> : null}

      <section className="detail-summary-grid">
        {show("usage") ? <div className="detail-gauge panel panel--accent-green"><span className="panel-kicker">PRIMARY PRESSURE</span>{gauge && gaugePercent >= 0 ? <><div className="detail-gauge__value">{gaugePercent.toFixed(0)}<small>% used</small></div><ProgressBar percent={gaugePercent} tone={progressTone(gaugePercent)} /><div className="detail-gauge__meta"><span>{metricLabel(gauge.key)}</span><span>{formatRelativeTime(resetFor(snapshot, gauge.key))}</span></div></> : <div className="missing-data">No fillable quota reported.</div>}</div> : null}
        {show("usage") ? <StatCard accent="aqua" label="Token volume" value={`${formatCompact(tokenSummary(snapshot))}`} detail={`${formatCompact(requestSummary(snapshot))} requests`} icon="spark" /> : null}
        {show("spending") ? <StatCard accent={snapshot.hide_costs ? "lavender" : "green"} label={snapshot.hide_costs ? "Spend visibility" : "Window spend"} value={snapshot.hide_costs ? "Hidden" : formatMoney(cost.value, cost.unit)} detail={snapshot.hide_costs ? "Configured by account policy" : windowLabel(bootstrap, windowID)} icon={snapshot.hide_costs ? "shield" : "activity"} /> : null}
      </section>

      {show("usage") || show("timers") ? <section className="detail-columns">
        {show("usage") ? <div className="panel"><PanelHeading title="Usage metrics" kicker="CURRENT STATE" />{!hideEmpty || rows.length ? <MetricTable rows={rows} /> : null}</div> : null}
        {show("timers") && (!hideEmpty || Object.keys(snapshot.resets || {}).length) ? <div className="panel"><PanelHeading title="Reset timers" kicker="NEXT WINDOWS" />{Object.keys(snapshot.resets || {}).length ? <div className="timer-list">{Object.entries(snapshot.resets).slice(0, 8).map(([key, value]) => <div className="timer-row" key={key}><span>{metricLabel(key)}</span><strong>{formatRelativeTime(value)}</strong></div>)}</div> : <div className="missing-data">No reset timers reported.</div>}</div> : null}
      </section> : null}

      {show("trends") && (!hideEmpty || trends.length || tokens.length) ? <section className="detail-columns">
        <ChartPanel color="green" empty={!trends.length} label="Spend trend" points={trends} subtitle="Daily cost reported for this account" valueFormatter={(value) => formatMoney(value, cost.unit)} />
        <ChartPanel color="aqua" empty={!tokens.length} label="Token trend" points={tokens} subtitle="Daily token volume" valueFormatter={(value) => `${formatCompact(value)} tok`} />
      </section> : null}

      {show("models") || show("clients") || show("projects") || show("mcp") ? <section className="detail-columns detail-columns--wide">
        {show("models") && (!hideEmpty || models.length) ? <div className="panel"><PanelHeading title="Models" kicker="MODEL MIX" />{models.length ? <ModelTable rows={models} hidden={snapshot.hide_costs} /> : <div className="missing-data">No per-model usage reported.</div>}</div> : null}
        {(show("clients") || show("projects") || show("mcp")) && (!hideEmpty || clients.length || projects.length || mcp.length) ? <div className="panel"><PanelHeading title="Activity hotspots" kicker="TELEMETRY" />{show("clients") ? <HotspotList title="Clients" rows={clients} unit="events" /> : null}{show("projects") ? <HotspotList title="Projects" rows={projects} unit="requests" /> : null}{show("mcp") ? <HotspotList title="MCP servers" rows={mcp} unit="calls" /> : null}</div> : null}
      </section> : null}

      {show("info") && Object.keys(snapshot.diagnostics || {}).length ? <section className="panel diagnostics-panel"><PanelHeading title="Diagnostics" kicker="PROVIDER NOTES" /><div className="diagnostic-list">{Object.entries(snapshot.diagnostics).map(([key, value]) => <div key={key}><span>{metricLabel(key)}</span><strong>{value}</strong></div>)}</div></section> : null}
    </>
  );
}

function NavItem({ active, icon, label, onClick }) {
  return <button className={`dashboard-nav__item${active ? " dashboard-nav__item--active" : ""}`} onClick={onClick} type="button"><Icon name={icon} size={17} /><span>{label}</span>{active ? <span className="dashboard-nav__active-mark" /> : null}</button>;
}

function StatCard({ accent, detail, icon, label, value }) {
  return <div className={`stat-card stat-card--${accent}`}><div className="stat-card__top"><span>{label}</span><Icon name={icon} size={17} /></div><strong>{value}</strong><small>{detail}</small></div>;
}

function ProviderCard({ account, bootstrap, onClick, snapshot }) {
  const gauge = snapshot ? primaryGauge(snapshot) : null;
  const percent = gauge ? usedPercent(gauge.key, gauge.metric) : -1;
  const cost = snapshot ? costSummary(snapshot) : null;
  return <button className={`provider-card panel${snapshot && snapshot.status !== "OK" ? " provider-card--attention" : ""}`} onClick={onClick} type="button">
    <div className="provider-card__head"><ProviderIcon name={providerName(account.provider_id, bootstrap?.providers)} providerID={account.provider_id} size={36} /><div className="provider-card__identity"><strong>{accountName(account.id)}</strong><span>{providerName(account.provider_id, bootstrap?.providers)}</span></div><StatusPill status={snapshot?.status || "UNKNOWN"} /></div>
    <div className="provider-card__main">{snapshot && gauge && percent >= 0 ? <><div className="provider-card__gauge-label"><span>{metricLabel(gauge.key)}</span><strong>{percent.toFixed(0)}%</strong></div><ProgressBar percent={percent} tone={progressTone(percent)} /><div className="provider-card__reset"><span>{formatMetric(metricValueForGauge(gauge.metric), gauge.metric.unit)}</span><span>{formatRelativeTime(resetFor(snapshot, gauge.key))}</span></div></> : <div className="provider-card__empty"><Icon name={snapshot?.status === "AUTH_REQUIRED" ? "key" : "database"} size={18} /><span>{snapshot?.message || "Waiting for first snapshot"}</span></div>}</div>
    <div className="provider-card__footer"><span>{snapshot && !snapshot.hide_costs ? formatMoney(cost.value, cost.unit) : snapshot ? `${formatCompact(tokenSummary(snapshot))} tok` : "No data"}</span><span className="provider-card__open">View detail <Icon name="arrow" size={14} /></span></div>
  </button>;
}

function ChartPanel({ color, empty, label, points, subtitle, valueFormatter }) {
  const latest = points?.[points.length - 1]?.value || 0;
  const previous = points?.[points.length - 2]?.value || 0;
  const delta = previous > 0 ? ((latest - previous) / previous) * 100 : 0;
  return <div className={`chart-panel panel panel--${color}`}><div className="chart-panel__head"><div><span className="panel-kicker">TREND</span><h3>{label}</h3><p>{subtitle}</p></div>{empty ? null : <div className={`chart-panel__delta${delta < 0 ? " chart-panel__delta--down" : ""}`}>{delta === 0 ? "steady" : `${delta > 0 ? "+" : ""}${delta.toFixed(0)}%`}<small>vs prior day</small></div>}</div>{empty ? <div className="chart-empty"><Icon name="chart" size={20} /><span>Not enough history for this view.</span></div> : <LineChart color={color} points={points} valueFormatter={valueFormatter} />}</div>;
}

function LineChart({ color, empty, height = 150, points, valueFormatter }) {
  if (empty || !points?.length) return <div className="chart-empty"><Icon name="chart" size={20} /><span>No series available.</span></div>;
  const width = 720;
  const padding = { top: 12, right: 10, bottom: 28, left: 10 };
  const values = points.map((point) => Math.max(0, Number(point.value) || 0));
  const maxValue = Math.max(...values, 1);
  const plotWidth = width - padding.left - padding.right;
  const plotHeight = height - padding.top - padding.bottom;
  const pointsString = values.map((value, index) => {
    const x = padding.left + (points.length === 1 ? plotWidth / 2 : (index / (points.length - 1)) * plotWidth);
    const y = padding.top + plotHeight - (value / maxValue) * plotHeight;
    return `${x.toFixed(2)},${y.toFixed(2)}`;
  }).join(" ");
  const areaPoints = `${padding.left},${padding.top + plotHeight} ${pointsString} ${width - padding.right},${padding.top + plotHeight}`;
  const accent = chartColor(color);
  const labels = [points[0]?.date, points[Math.floor((points.length - 1) / 2)]?.date, points[points.length - 1]?.date].filter(Boolean);
  return <div className="line-chart"><svg aria-label={`${points.length}-point ${color} trend chart`} className="line-chart__svg" role="img" viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none"><defs><linearGradient id={`chart-fill-${color}`} x1="0" x2="0" y1="0" y2="1"><stop offset="0%" stopColor={accent} stopOpacity=".28" /><stop offset="100%" stopColor={accent} stopOpacity="0" /></linearGradient></defs>{[0, .5, 1].map((ratio) => <line key={ratio} x1={padding.left} x2={width - padding.right} y1={padding.top + plotHeight * ratio} y2={padding.top + plotHeight * ratio} stroke="currentColor" strokeOpacity=".12" />)}<polygon fill={`url(#chart-fill-${color})`} points={areaPoints} /><polyline fill="none" points={pointsString} stroke={accent} strokeLinecap="round" strokeLinejoin="round" strokeWidth="3" />{values.map((value, index) => { const [x, y] = pointsString.split(" ")[index].split(","); return <circle className="line-chart__point" cx={x} cy={y} fill={accent} key={`${points[index].date}-${index}`} r="4"><title>{`${points[index].date}: ${valueFormatter ? valueFormatter(value) : value}`}</title></circle>; })}</svg><div className="line-chart__labels">{labels.map((date, index) => <span key={`${date}-${index}`}>{formatDate(date)}</span>)}</div></div>;
}

function RankPanel({ accent, hideEmpty = false, rows, subtitle, title, valueLabel }) {
  if (hideEmpty && !rows.length) return null;
  const maxValue = Math.max(...rows.map((row) => row.value), 1);
  return <div className={`rank-panel panel panel--${accent}`}><PanelHeading kicker="RANKING" title={title} /><p className="rank-panel__subtitle">{subtitle}</p>{rows.length ? <div className="rank-list">{rows.map((row, index) => <div className="rank-row" key={`${row.name}-${index}`}><div className="rank-row__top"><span><b>{String(index + 1).padStart(2, "0")}</b>{row.name}</span><strong>{row.detail || `${formatCompact(row.value)} ${valueLabel}`}</strong></div><div className="rank-track"><span style={{ width: `${clamp((row.value / maxValue) * 100, 4, 100)}%` }} /></div></div>)}</div> : <div className="missing-data">No activity in this window.</div>}</div>;
}

function MetricTable({ rows }) {
  return rows.length ? <div className="metric-table">{rows.map(({ key, metric, value }) => <div className="metric-row" key={key}><span>{metricLabel(key)}</span><strong>{metricDisplayValue(metric, value)}</strong><small>{metric.window || "current"}</small></div>)}</div> : <div className="missing-data">No metrics reported.</div>;
}

function ModelTable({ hidden, rows }) {
  return <div className="model-table"><div className="model-table__header"><span>Model</span><span>{hidden ? "Tokens" : "Spend"}</span></div>{rows.slice(0, 10).map((row) => <div className="model-table__row" key={row.name}><span><strong>{row.name}</strong><small>{row.providers?.join(", ") || ""}</small></span><strong>{hidden ? `${formatCompact(row.total)} tok` : formatMoney(row.cost)}</strong></div>)}</div>;
}

function HotspotList({ rows, title, unit }) {
  if (!rows?.length) return null;
  return <div className="hotspot-group"><div className="hotspot-group__title">{title}</div>{rows.slice(0, 4).map((row) => <div className="hotspot-row" key={row.name}><span>{accountName(row.name)}</span><strong>{formatCompact(row.value)} {unit}</strong></div>)}</div>;
}

function PanelHeading({ kicker, title }) {
  return <div className="panel-heading"><div><span className="panel-kicker">{kicker}</span><h3>{title}</h3></div></div>;
}

function DaemonBanner({ daemon, onInstall }) {
  const canInstall = daemon?.status === "not_installed" || daemon?.status === "outdated";
  return <section className="daemon-banner"><div className="daemon-banner__icon"><Icon name={canInstall ? "server" : "refresh"} size={20} /></div><div><strong>{daemon?.message || "Connecting to the telemetry daemon."}</strong><span>{canInstall ? "Install the background collector to start receiving usage data." : "The dashboard will retry automatically."}</span></div>{canInstall ? <button className="button button--primary" onClick={onInstall} type="button">Install collector <Icon name="arrow" size={15} /></button> : null}</section>;
}

function InlineError({ message, onRetry }) {
  return <div className="inline-error" role="alert"><Icon name="warning" size={16} /><span>{message}</span><button onClick={onRetry} type="button">Retry</button></div>;
}

function EmptyState({ action, actionLabel, detail, icon, title }) {
  return <div className="empty-state panel"><div className="empty-state__icon"><Icon name={icon || "database"} size={24} /></div><h3>{title}</h3><p>{detail}</p>{action ? <button className="button button--secondary" onClick={action} type="button">{actionLabel || "Continue"}<Icon name="arrow" size={15} /></button> : null}</div>;
}

function AttentionValue({ snapshot }) {
  if (snapshot.status !== "OK") return <StatusPill status={snapshot.status} />;
  const gauge = primaryGauge(snapshot);
  const percent = gauge ? usedPercent(gauge.key, gauge.metric) : -1;
  return <strong className="attention-row__value">{percent >= 0 ? `${percent.toFixed(0)}% used` : "Review usage"}</strong>;
}

function StatusPill({ status }) {
  return <span className={`status-pill status-pill--${statusTone(status)}`}>{statusLabel(status)}</span>;
}

function ProgressBar({ percent, tone }) {
  return <div className={`progress-bar progress-bar--${tone}`}><span style={{ width: `${clamp(percent, 0, 100)}%` }} /></div>;
}

function LoadingScreen() {
  return <div className="dashboard-loading-screen"><BrandMark /><div className="loading-pulse"><span /><span /><span /></div><p>Connecting to your local usage data...</p></div>;
}

function metricValueForGauge(metric) {
  return metricValue(metric, "used") ?? metricValue(metric, "remaining") ?? metricValue(metric, "limit") ?? 0;
}

function metricDisplayValue(metric, used) {
  if (used !== null) return formatMetric(used, metric.unit);
  const remaining = metricValue(metric, "remaining");
  const limit = metricValue(metric, "limit");
  if (remaining !== null && limit !== null) return `${formatMetric(remaining, metric.unit)} left / ${formatMetric(limit, metric.unit)}`;
  if (remaining !== null) return `${formatMetric(remaining, metric.unit)} left`;
  if (limit !== null) return `${formatMetric(limit, metric.unit)} limit`;
  return "-";
}

function metricValue(metric, field) {
  return metric?.[field] === undefined || metric?.[field] === null ? null : Number(metric[field]);
}

function windowLabel(bootstrap, id) {
  return bootstrap?.time_windows?.find((windowOption) => windowOption.id === id)?.label || id || "30 Days";
}

function daemonLabel(status) {
  return { running: "Collector live", starting: "Starting", connecting: "Connecting", not_installed: "Setup needed", outdated: "Update needed", error: "Collector error" }[status] || "Unknown state";
}

function statusTone(status) {
  return { OK: "ok", NEAR_LIMIT: "warn", LIMITED: "danger", AUTH_REQUIRED: "auth", ERROR: "danger", UNSUPPORTED: "muted", UNKNOWN: "muted" }[status] || "muted";
}

function statusLabel(status) {
  return { OK: "Healthy", NEAR_LIMIT: "Near limit", LIMITED: "Limited", AUTH_REQUIRED: "Auth needed", ERROR: "Error", UNSUPPORTED: "Unsupported", UNKNOWN: "Unknown" }[status] || "Unknown";
}

function progressTone(percent) {
  if (percent >= 90) return "danger";
  if (percent >= 75) return "warn";
  return "green";
}

function attentionScore(snapshot) {
  if (!snapshot) return 0;
  const gauge = primaryGauge(snapshot);
  return snapshot.status === "ERROR" || snapshot.status === "LIMITED" ? 100 : snapshot.status === "AUTH_REQUIRED" ? 90 : gauge ? usedPercent(gauge.key, gauge.metric) : 50;
}

function metricLabel(key) {
  return key
    .replace(/^(analytics_|usage_|model_|client_|project_|mcp_|lang_)/, "")
    .replace(/_today$/, " today")
    .replace(/_total$/, " total")
    .replace(/_/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function chartColor(color) {
  return { green: "#b8bb26", aqua: "#83a598", amber: "#fabd2f", lavender: "#d3869b", peach: "#fe8019", rose: "#d3869b" }[color] || "#b8bb26";
}

function formatDate(value) {
  if (!value) return "";
  const date = new Date(`${value}T00:00:00`);
  return Number.isNaN(date.valueOf()) ? value : date.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

function sectionEnabled(bootstrap, section, settingKey) {
  const configured = bootstrap?.settings?.dashboard?.[settingKey] || [];
  if (!configured.length) return true;
  const entry = configured.find((candidate) => candidate.id === section);
  return entry ? entry.enabled !== false : true;
}

function themeStyle(bootstrap) {
  const activeName = bootstrap?.settings?.theme;
  const theme = bootstrap?.themes?.find((candidate) => candidate.name === activeName);
  const colors = theme?.colors;
  if (!colors) return undefined;
  return {
    "--dash-bg": colors.base,
    "--dash-panel": colors.surface0,
    "--dash-panel-raised": colors.surface1,
    "--dash-panel-soft": colors.mantle,
    "--dash-border": colors.surface1,
    "--dash-border-strong": colors.surface2,
    "--dash-text": colors.text,
    "--dash-muted": colors.subtext,
    "--dash-dim": colors.dim,
    "--dash-green": colors.green,
    "--dash-aqua": colors.teal || colors.sapphire,
    "--dash-amber": colors.yellow,
    "--dash-red": colors.red,
    "--dash-peach": colors.peach,
    "--dash-lavender": colors.lavender,
  };
}
