# Web Dashboard Design

Date: 2026-09-11
Status: Proposed
Author: OpenUsage

## 1. Problem Statement

OpenUsage has a rich daemon-backed read model, but users can only inspect it through the terminal UI and cannot manage the application from a browser.

## 2. Goals

1. Add a polished, local-first browser dashboard for the existing usage and quota data.
2. Reuse the daemon read model and provider metadata without exposing provider credentials, SQLite, or the Unix socket to JavaScript.
3. Provide browser equivalents for the existing dashboard, analytics, settings, credential, browser-session, and integration workflows.
4. Keep the existing TUI, CLI commands, provider interface, and stored telemetry data backward-compatible.

## 3. Non-Goals

1. A hosted SaaS product, user accounts, or cloud storage.
2. A web interface for the remote hub in the first release.
3. New provider adapters or a second telemetry collection pipeline.
4. Direct browser access to provider APIs, the telemetry database, or raw credentials.

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | none | Existing UsageSnapshot and provider contracts remain the canonical data model. |
| providers | minor | Expose existing ProviderSpec metadata through safe web DTOs; Fetch behavior is unchanged. |
| TUI | none | Existing terminal dashboard remains supported and is not used as a web renderer. |
| config | minor | Reuse existing settings and credential helpers; add only web CLI options, not persisted secrets. |
| detect | none | Existing account discovery is reused by the daemon/read model. |
| daemon | minor | Web server calls the existing Unix-socket API and daemon lifecycle helpers. |
| telemetry | none | Existing read model supplies historical aggregates and daily series. |
| CLI | major | Add `openusage web` with loopback binding, browser launch, and static asset serving. |
| website | major | Add a dashboard route and split dashboard components from the marketing page. |
| integrations | minor | Expose existing status/install/uninstall operations through guarded local endpoints. |

### Existing Design Doc Overlap

- `DETAIL_PAGE_REDESIGN_DESIGN.md` supplies the provider detail information hierarchy and chart inventory.
- `UNIFIED_AGENT_USAGE_TRACKING_DESIGN.md` defines the telemetry dimensions already present in the read model.
- `MULTI_ACCOUNT_DESIGN.md`, `DATA_TIME_FRAMES_DESIGN.md`, and `PROVIDER_WIDGET_SECTION_SETTINGS_DESIGN.md` define account, window, and settings semantics.
- `BROWSER_SESSION_AUTH_DESIGN.md` defines the explicit-consent browser cookie workflow.
- `INTEGRATION_LIFECYCLE_DESIGN.md` defines integration status and installation behavior.

This is a new serving and presentation layer. It complements those designs and does not replace them.

## 5. Detailed Design

### 5.1 Runtime Boundary

`openusage web` starts a foreground HTTP server on `127.0.0.1` and serves the built dashboard. The web server owns a `daemon.ViewRuntime` and reads snapshots through `daemon.Client` over the existing Unix socket. It may use the existing service-manager path to start the daemon, but it never calls a provider or opens SQLite itself.

The default server is local-only. A non-loopback listen address requires the
explicit `--allow-public` flag and `OPENUSAGE_WEB_TOKEN`; deployments outside a
trusted local machine should still put TLS in front of the process.

### 5.2 HTTP API

All browser endpoints use `/api/v1` and return JSON. The API is same-origin in production and is proxied by Vite during development.

Initial read endpoints:

- `GET /api/v1/bootstrap` returns the safe app version, daemon state, time windows, provider metadata, accounts, settings, themes, and integration statuses.
- `GET /api/v1/snapshots?window=30d` returns a response envelope containing sanitized snapshots, effective cost-visibility flags, the requested window, and server timestamps.
- `GET /api/v1/health` returns web and daemon health without credentials.

Settings endpoints reuse the existing config read-modify-write helpers:

- `PATCH /api/v1/settings/dashboard`
- `PATCH /api/v1/settings/time-window`
- `PATCH /api/v1/settings/theme`
- `PATCH /api/v1/settings/ui`
- `PATCH /api/v1/settings/providers`
- `PATCH /api/v1/settings/sections`
- `PATCH /api/v1/settings/telemetry-links`

Local control endpoints:

- `POST /api/v1/daemon/install`
- `PUT /api/v1/accounts/{id}/credential`
- `DELETE /api/v1/accounts/{id}/credential`
- `GET /api/v1/browsers`
- `POST /api/v1/accounts/{id}/browser-session`
- `DELETE /api/v1/accounts/{id}/browser-session`
- `GET /api/v1/integrations`
- `POST /api/v1/integrations/{id}/install`
- `POST /api/v1/integrations/{id}/uninstall`

API keys and cookies are accepted only by the Go server. They are never returned, persisted in browser storage, or written to logs. The server computes effective `hide_costs` before removing `UsageSnapshot.Raw`, because the existing automatic policy uses provider plan signals from that field.

### 5.3 Local API Security

- Bind to loopback by default.
- Issue a per-process request token to the served UI and require it on all mutating requests.
- Validate `Origin` and `Host` against the local server origin.
- Limit JSON body sizes and reject unknown credential fields.
- Mark credential responses `Cache-Control: no-store`.
- Do not enable wildcard CORS.
- Allow opening only provider-declared console URLs, never arbitrary server-provided URLs.
- Require an explicit confirmation in the browser before reading a browser cookie store.

The request token is a browser CSRF guard, not OS-level authentication. A
same-user local process can already read the user's configuration and
credential files, so remote access remains intentionally unsupported.

### 5.4 Browser Information Architecture

The dashboard uses three top-level surfaces:

1. **Overview**: summary metrics, attention queue, spend/token trends, model leaders, and responsive provider cards.
2. **Analytics**: window-aware charts and ranked breakdowns for providers, models, clients, projects, tools, MCP servers, and languages.
3. **Settings**: appearance, provider visibility/order, sections, time windows, telemetry mappings, credentials, browser sessions, and integrations.

Provider detail is a route or desktop side panel with usage, spending, models, tokens, activity, trends, timers, and safe diagnostics. Mobile uses a full-screen detail view rather than a permanently open split pane.

The visual language is modern dark analytics: restrained panels, high information density, provider logos from the existing SVG assets, sans-serif interface text, and JetBrains Mono for values/model identifiers. Gruvbox-inspired green, aqua, amber, red, and lavender remain semantic accents without turning the page into a terminal emulator.

Charts use small accessible SVG primitives because the payloads are short daily series and the project has no chart dependency today. Every chart has a text summary or table fallback.

### 5.5 Frontend Data Flow

The React app loads bootstrap and snapshots in parallel, polls snapshots on the configured refresh cadence, and keeps the last successful response visible while showing stale status. Window changes are user events that update the URL and request a new snapshot; derived totals are calculated during render from the normalized response rather than synchronized through effects.

The marketing page remains the root route. The dashboard is mounted under `/app`, and dashboard code is kept in its own source directory so marketing CSS and analytics do not leak into the local application. PostHog is disabled on the local dashboard route.

`make website-build` copies the dashboard runtime assets into an ignored embed directory and `make build` packages them into the binary. An explicit `--static-dir` remains available for development and for serving the full source-checkout website.

### 5.6 Backward Compatibility

- Existing settings files continue to load without a `web` block.
- Existing TUI and CLI behavior is unchanged.
- No provider interface or telemetry schema changes are required.
- Existing `UsageSnapshot` JSON remains the daemon wire format; the web API adds a redacted envelope rather than changing it.
- Existing API-key and browser-session storage continues to use `credentials.json` with `0600` permissions.

## 6. Alternatives Considered

### Add TCP Routes to the Telemetry Daemon

Rejected for the first implementation. The daemon is deliberately Unix-socket-only and runs as a service with provider credentials. A separate loopback web process keeps the existing trust boundary and lifecycle stable while still reusing the daemon client.

### Have React Read SQLite Directly

Rejected. It would require shipping database access to the browser, duplicate read-model logic, expose local paths, and make credential boundaries unclear.

### Reuse TUI Renderers

Rejected. TUI renderers produce ANSI/lipgloss strings and encode terminal layout constraints. The web layer should share core data semantics but use native responsive HTML and SVG.

## 7. Implementation Tasks

### Task 1: Web API DTOs and redaction
Files: `internal/web/types.go`, `internal/web/redact.go`, `internal/web/types_test.go`
Depends on: none
Description: Define the bootstrap, snapshot, settings, account, provider, theme, integration, and daemon-state DTOs. Build a defensive snapshot sanitizer that omits raw metadata and exposes effective cost visibility without mutating cached snapshots.
Tests: JSON shape tests, raw-data omission tests, hide-cost resolution tests, and credential-field omission tests.

### Task 2: Loopback web server and read endpoints
Files: `internal/web/server.go`, `internal/web/handlers.go`, `internal/web/server_test.go`
Depends on: Task 1
Description: Implement the local HTTP server, daemon runtime bridge, bootstrap endpoint, snapshot endpoint, health endpoint, request-token validation, origin checks, and static-file fallback. Use the existing daemon client and config/provider registries.
Tests: `httptest` coverage for successful reads, daemon unavailable state, invalid windows, request-token enforcement, origin rejection, and no raw snapshot fields.

### Task 3: Web CLI and asset serving
Files: `cmd/openusage/web.go`, `cmd/openusage/main.go`, `Makefile`, `website/vite.config.js`
Depends on: Task 2
Description: Add `openusage web` with loopback listen, browser-open, and static-directory options. Add development API proxying and a website build target that copies the small runtime asset set into the embedded web filesystem without changing the default TUI command.
Tests: Cobra command registration and flag tests; startup smoke test with a temporary static directory.

### Task 4: Dashboard frontend shell and overview
Files: `website/src/main.jsx`, `website/src/dashboard/`, `website/src/dashboard.css`, `website/src/App.jsx`
Depends on: Task 1 and the read contract from Task 2
Description: Add the `/app` route, dashboard data client, responsive shell, sidebar, window selector, status bar, overview metrics, attention queue, provider cards, empty/error/loading states, and accessible SVG charts. Keep the marketing page behavior intact.
Tests: Production build plus Puppeteer smoke coverage at 375px, 768px, and 1440px with fixture API responses.

### Task 5: Analytics and provider detail
Files: `website/src/dashboard/analytics/`, `website/src/dashboard/providers/`, `website/src/dashboard/selectors.js`
Depends on: Task 4
Description: Implement window-aware analytics panels and provider detail views using snapshots and core-compatible selectors. Include model/token, client, project, tool, MCP, language, timer, and diagnostics sections with table fallbacks.
Tests: Fixture selector tests for cost visibility, missing data, mixed currencies, model ranking, and daily series; browser navigation smoke tests.

### Task 6: Settings and local controls
Files: `internal/web/settings.go`, `internal/web/credentials.go`, `internal/web/integrations.go`, `internal/dashboardapp/service.go`, `website/src/dashboard/settings/`
Depends on: Tasks 2 and 4
Description: Add safe settings mutations, account credential validation/save/delete, browser-session consent and connection flows, daemon install, and integration status/install/uninstall actions. Reuse existing config, dashboardapp, browsercookies, and integrations code.
Tests: Config persistence tests, credential redaction tests, browser-session status tests, integration action tests, and browser confirmation/error flows.

### Task 7: Documentation and release verification
Files: `README.md`, `docs/site/docs/getting-started/ways-to-use.md`, `docs/site/docs/concepts/architecture.md`, `docs/site/docs/concepts/snapshots.md`, `docs/site/docs/reference/cli.md`, `docs/site/docs/reference/configuration.md`
Depends on: Tasks 3 and 6
Description: Document local web startup, security posture, browser-session behavior, development proxying, and release asset packaging. Verify the Go binary, website build, docs build, and local smoke path.
Tests: `make build`, `make vet`, `make fmt`, `go test ./internal/web/... ./internal/daemon/... ./internal/config/... ./internal/integrations/...`, and website production build.

### Dependency Graph

```text
Task 1 -> Task 2 -> Task 3
   |       |         |
   |       +-------> Task 4 -> Task 5
   |                 |
   +---------------> Task 6

Task 7 depends on Tasks 3, 5, and 6.
```
