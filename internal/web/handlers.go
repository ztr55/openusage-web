package web

import (
	"context"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
	"github.com/janekbaraniewski/openusage/internal/detect"
	"github.com/janekbaraniewski/openusage/internal/tui"
	"github.com/janekbaraniewski/openusage/internal/version"
)

func (s *Server) serveAPI(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/bootstrap":
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		s.handleBootstrap(w, r)
	case "/api/v1/snapshots":
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		s.handleSnapshots(w, r)
	case "/api/v1/health":
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		s.handleHealth(w, r)
	case "/api/v1/browsers":
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		s.handleBrowsers(w, r)
	case "/api/v1/integrations":
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		s.handleIntegrations(w, r)
	case "/api/v1/settings/dashboard":
		if !allowMethod(w, r, http.MethodPatch) {
			return
		}
		s.handleDashboardSettings(w, r)
	case "/api/v1/settings/time-window":
		if !allowMethod(w, r, http.MethodPatch) {
			return
		}
		s.handleTimeWindowSettings(w, r)
	case "/api/v1/settings/theme":
		if !allowMethod(w, r, http.MethodPatch) {
			return
		}
		s.handleThemeSettings(w, r)
	case "/api/v1/settings/ui":
		if !allowMethod(w, r, http.MethodPatch) {
			return
		}
		s.handleUISettings(w, r)
	case "/api/v1/settings/providers":
		if !allowMethod(w, r, http.MethodPatch) {
			return
		}
		s.handleProviderSettings(w, r)
	case "/api/v1/settings/sections":
		if !allowMethod(w, r, http.MethodPatch) {
			return
		}
		s.handleSectionSettings(w, r)
	case "/api/v1/settings/telemetry-links":
		if !allowMethod(w, r, http.MethodPatch) {
			return
		}
		s.handleTelemetryLinkSettings(w, r)
	case "/api/v1/daemon/install":
		if !allowMethod(w, r, http.MethodPost) {
			return
		}
		s.handleDaemonInstall(w, r)
	default:
		if strings.HasPrefix(r.URL.Path, "/api/v1/accounts/") {
			s.serveAccountAPI(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/integrations/") {
			s.serveIntegrationAPI(w, r)
			return
		}
		writeJSONError(w, http.StatusNotFound, "not found")
	}
}

func allowMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.configLoader()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "configuration unavailable")
		return
	}

	// The web process does not render the TUI, but loading the same catalog keeps
	// custom theme names and palettes visible to the dashboard.
	_ = tui.LoadThemes(config.ConfigDir())
	providers, providerSpecs := s.providerDTOs()
	credentials, _ := s.credentialsLoader()
	detected := s.detectAccounts(cfg)

	writeJSON(w, http.StatusOK, BootstrapResponse{
		APIVersion:   APIVersion,
		ServedAt:     s.nowUTC(),
		RequestToken: s.requestToken,
		App: AppDTO{
			Version:    strings.TrimSpace(version.Version),
			CommitHash: strings.TrimSpace(version.CommitHash),
			BuildDate:  strings.TrimSpace(version.BuildDate),
		},
		Daemon:       s.daemonStateDTO(r.Context()),
		TimeWindows:  timeWindowDTOs(),
		Providers:    providers,
		Accounts:     s.accountDTOs(cfg, detected, credentials, providerSpecs),
		Settings:     settingsDTO(cfg),
		Themes:       themeDTOs(tui.AvailableThemes()),
		Integrations: s.integrationDTOs(cfg),
	})
}

func (s *Server) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.configLoader()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "configuration unavailable")
		return
	}

	window, err := requestedWindow(r, cfg, s.runtime)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	frame := daemon.SnapshotFrame{TimeWindow: window}
	if s.runtime != nil {
		frame = s.runtime.ReadWithFallbackForWindow(r.Context(), window)
	}
	if frame.Snapshots == nil {
		frame.Snapshots = make(map[string]core.UsageSnapshot)
	}

	perAccount := make(map[string]*bool, len(cfg.Dashboard.Providers))
	for _, preference := range cfg.Dashboard.Providers {
		accountID := strings.TrimSpace(preference.AccountID)
		if accountID != "" {
			perAccount[accountID] = preference.HideCosts
		}
	}

	redacted := make(map[string]SnapshotDTO, len(frame.Snapshots))
	for key, snap := range frame.Snapshots {
		accountID := strings.TrimSpace(snap.AccountID)
		if accountID == "" {
			accountID = strings.TrimSpace(key)
		}
		redacted[key] = SanitizeSnapshot(snap, perAccount[accountID], cfg.Dashboard.HideCosts)
	}

	writeJSON(w, http.StatusOK, SnapshotsResponse{
		Window:    string(window),
		ServedAt:  s.nowUTC(),
		Snapshots: redacted,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:   "ok",
		ServedAt: s.nowUTC(),
		Daemon:   s.daemonHealth(r.Context()),
	})
}

func (s *Server) handleBrowsers(w http.ResponseWriter, r *http.Request) {
	browsers, err := s.browserLister(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "browser discovery unavailable")
		return
	}

	seen := make(map[string]struct{}, len(browsers))
	clean := make([]string, 0, len(browsers))
	for _, browser := range browsers {
		browser = strings.TrimSpace(browser)
		if browser == "" {
			continue
		}
		if _, ok := seen[browser]; ok {
			continue
		}
		seen[browser] = struct{}{}
		clean = append(clean, browser)
	}
	sort.Strings(clean)

	writeJSON(w, http.StatusOK, BrowsersResponse{
		Browsers: clean,
		ServedAt: s.nowUTC(),
	})
}

func (s *Server) handleIntegrations(w http.ResponseWriter, _ *http.Request) {
	cfg, _ := s.configLoader()
	writeJSON(w, http.StatusOK, IntegrationsResponse{
		Integrations: s.integrationDTOs(cfg),
		ServedAt:     s.nowUTC(),
	})
}

func (s *Server) handleDaemonInstall(w http.ResponseWriter, r *http.Request) {
	if !s.requireMutation(w, r) {
		return
	}
	if err := s.installDaemon(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "daemon install failed")
		return
	}
	if s.runtime != nil {
		s.runtime.ResetEnsureThrottle()
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "installed"})
}

func (s *Server) daemonHealth(ctx context.Context) DaemonHealthDTO {
	state := daemon.DaemonState{Status: daemon.DaemonStatusUnknown}
	if s.runtime == nil {
		return DaemonHealthDTO{Status: daemonStatusName(state.Status)}
	}
	state = s.runtime.State()
	client := s.runtime.CurrentClient()
	if client == nil {
		ensureCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		client = s.runtime.EnsureClient(ensureCtx)
		cancel()
	}
	if client == nil {
		return DaemonHealthDTO{Status: daemonStatusName(s.runtime.State().Status)}
	}

	healthCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	health, err := client.HealthInfo(healthCtx)
	cancel()
	if err != nil {
		return DaemonHealthDTO{Status: daemonStatusName(state.Status)}
	}

	return DaemonHealthDTO{
		Status:             "running",
		Healthy:            true,
		Version:            strings.TrimSpace(health.DaemonVersion),
		APIVersion:         strings.TrimSpace(health.APIVersion),
		IntegrationVersion: strings.TrimSpace(health.IntegrationVersion),
		ProviderRegistry:   strings.TrimSpace(health.ProviderRegistry),
	}
}

func (s *Server) daemonStateDTO(ctx context.Context) DaemonStateDTO {
	if s.runtime == nil {
		return DaemonStateDTO{Status: daemonStatusName(daemon.DaemonStatusUnknown)}
	}
	state := s.runtime.State()
	if state.Status == daemon.DaemonStatusConnecting || state.Status == daemon.DaemonStatusUnknown {
		ensureCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = s.runtime.EnsureClient(ensureCtx)
		cancel()
		state = s.runtime.State()
	}
	result := DaemonStateDTO{Status: daemonStatusName(state.Status)}
	switch state.Status {
	case daemon.DaemonStatusConnecting:
		result.Message = "Connecting to the telemetry daemon."
	case daemon.DaemonStatusNotInstalled:
		result.Message = "The telemetry daemon is not installed."
		result.InstallHint = "openusage telemetry daemon install"
	case daemon.DaemonStatusStarting:
		result.Message = "Starting the telemetry daemon."
	case daemon.DaemonStatusRunning:
		result.Message = "Telemetry daemon is running."
	case daemon.DaemonStatusOutdated:
		result.Message = "The telemetry daemon needs an update."
	case daemon.DaemonStatusError:
		result.Message = "The telemetry daemon is unavailable."
	}
	return result
}

func daemonStatusName(status daemon.DaemonStatus) string {
	switch status {
	case daemon.DaemonStatusConnecting:
		return "connecting"
	case daemon.DaemonStatusNotInstalled:
		return "not_installed"
	case daemon.DaemonStatusStarting:
		return "starting"
	case daemon.DaemonStatusRunning:
		return "running"
	case daemon.DaemonStatusOutdated:
		return "outdated"
	case daemon.DaemonStatusError:
		return "error"
	default:
		return "unknown"
	}
}

func (s *Server) nowUTC() time.Time {
	now := s.now()
	if now.IsZero() {
		now = time.Now()
	}
	return now.UTC()
}

func requestedWindow(r *http.Request, cfg config.Config, runtime *daemon.ViewRuntime) (core.TimeWindow, error) {
	values, present := r.URL.Query()["window"]
	if present && len(values) > 1 {
		return "", &invalidWindowError{}
	}
	if present && strings.TrimSpace(values[0]) != "" {
		value := strings.TrimSpace(values[0])
		for _, valid := range core.ValidTimeWindows {
			if string(valid) == value {
				return valid, nil
			}
		}
		return "", &invalidWindowError{}
	}

	if strings.TrimSpace(cfg.Data.TimeWindow) != "" {
		return core.ParseTimeWindow(strings.TrimSpace(cfg.Data.TimeWindow)), nil
	}
	if runtime != nil {
		return runtime.TimeWindow(), nil
	}
	return core.TimeWindow30d, nil
}

type invalidWindowError struct{}

func (e *invalidWindowError) Error() string {
	return "invalid window; use one of 1d, 3d, 7d, 30d, all"
}

func (s *Server) detectAccounts(cfg config.Config) []core.AccountConfig {
	result := detect.Result{}
	if cfg.AutoDetect && s.discover != nil {
		result = s.discover()
	}
	if s.applyCredentials != nil {
		s.applyCredentials(&result)
	}
	return result.Accounts
}

func (s *Server) providerDTOs() ([]ProviderDTO, map[string]core.ProviderSpec) {
	providers := make([]ProviderDTO, 0)
	specs := make(map[string]core.ProviderSpec)
	seen := make(map[string]struct{})
	for _, provider := range s.providerLister() {
		if provider == nil {
			continue
		}
		spec := provider.Spec()
		info := provider.Describe()
		id := strings.TrimSpace(provider.ID())
		if id == "" {
			id = strings.TrimSpace(spec.ID)
		}
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		name := strings.TrimSpace(info.Name)
		if name == "" {
			name = strings.TrimSpace(spec.Info.Name)
		}
		if name == "" {
			name = id
		}
		capabilities := append([]string(nil), info.Capabilities...)
		if len(capabilities) == 0 {
			capabilities = append([]string(nil), spec.Info.Capabilities...)
		}
		docsURL := strings.TrimSpace(info.DocURL)
		if docsURL == "" {
			docsURL = strings.TrimSpace(spec.Setup.DocsURL)
		}
		authType := strings.TrimSpace(string(spec.Auth.Type))
		if authType == "" {
			authType = "unknown"
		}
		authTypes := make([]string, 0, len(spec.Auth.SupplementalTypes)+1)
		if spec.Auth.Type != core.ProviderAuthTypeUnknown {
			authTypes = append(authTypes, string(spec.Auth.Type))
		}
		for _, supplemental := range spec.Auth.SupplementalTypes {
			value := strings.TrimSpace(string(supplemental))
			if value == "" || containsString(authTypes, value) {
				continue
			}
			authTypes = append(authTypes, value)
		}

		specs[id] = spec
		providers = append(providers, ProviderDTO{
			ID:                  id,
			Name:                name,
			Capabilities:        capabilities,
			DocsURL:             docsURL,
			Quickstart:          append([]string(nil), spec.Setup.Quickstart...),
			AuthType:            authType,
			AuthTypes:           authTypes,
			APIKeyEnv:           strings.TrimSpace(spec.Auth.APIKeyEnv),
			DefaultAccountID:    strings.TrimSpace(spec.Auth.DefaultAccountID),
			BrowserCookieDomain: strings.TrimSpace(spec.Auth.BrowserCookieDomain),
			BrowserCookieName:   strings.TrimSpace(spec.Auth.BrowserCookieName),
			BrowserConsoleURL:   strings.TrimSpace(spec.Auth.BrowserConsoleURL),
		})
	}
	return providers, specs
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

type accountAggregate struct {
	account    core.AccountConfig
	configured bool
	discovered bool
}

func (s *Server) accountDTOs(
	cfg config.Config,
	detected []core.AccountConfig,
	credentials config.Credentials,
	providerSpecs map[string]core.ProviderSpec,
) []AccountDTO {
	accounts := make(map[string]*accountAggregate)
	add := func(account core.AccountConfig, configured, discovered bool) {
		account.ID = strings.TrimSpace(account.ID)
		if account.ID == "" {
			return
		}
		entry, ok := accounts[account.ID]
		if !ok {
			accounts[account.ID] = &accountAggregate{
				account:    account,
				configured: configured,
				discovered: discovered,
			}
			return
		}
		entry.configured = entry.configured || configured
		entry.discovered = entry.discovered || discovered
		if strings.TrimSpace(entry.account.Provider) == "" {
			entry.account.Provider = account.Provider
		}
		if strings.TrimSpace(entry.account.Auth) == "" {
			entry.account.Auth = account.Auth
		}
		if strings.TrimSpace(entry.account.APIKeyEnv) == "" {
			entry.account.APIKeyEnv = account.APIKeyEnv
		}
		if entry.account.BrowserCookie == nil && account.BrowserCookie != nil {
			cookie := *account.BrowserCookie
			entry.account.BrowserCookie = &cookie
		}
		if entry.account.Token == "" {
			entry.account.Token = account.Token
		}
	}

	for _, account := range cfg.Accounts {
		add(account, true, false)
	}
	for _, account := range cfg.AutoDetectedAccounts {
		add(account, false, true)
	}
	for _, account := range detected {
		add(account, false, true)
	}

	out := make([]AccountDTO, 0, len(accounts))
	for _, entry := range accounts {
		providerID := strings.TrimSpace(entry.account.Provider)
		spec := providerSpecs[providerID]
		authType := strings.TrimSpace(entry.account.Auth)
		if authType == "" {
			authType = strings.TrimSpace(string(spec.Auth.Type))
		}
		if authType == "" {
			authType = "unknown"
		}
		envVar := strings.TrimSpace(entry.account.APIKeyEnv)
		if envVar == "" {
			envVar = strings.TrimSpace(spec.Auth.APIKeyEnv)
		}
		storedKey, stored := credentials.Keys[entry.account.ID]
		envPresent := envVar != "" && strings.TrimSpace(os.Getenv(envVar)) != ""
		detectedCredential := strings.TrimSpace(entry.account.Hint("credential_source", "")) != ""
		present := strings.TrimSpace(entry.account.Token) != "" ||
			(stored && strings.TrimSpace(storedKey) != "") || envPresent || detectedCredential
		kind := authType
		if kind == "unknown" && present {
			kind = string(core.ProviderAuthTypeAPIKey)
		}
		source := ""
		switch {
		case envPresent:
			source = "environment"
		case stored && strings.TrimSpace(storedKey) != "":
			source = "stored"
		case present:
			source = "detected"
		}

		session := browserSessionDTO(entry.account, credentials.Sessions[entry.account.ID], credentials.Sessions != nil, s.nowUTC())
		if session.Connected && !present && authType == string(core.ProviderAuthTypeBrowserSession) {
			present = true
			kind = string(core.ProviderAuthTypeBrowserSession)
			source = "stored"
		}
		out = append(out, AccountDTO{
			ID:         entry.account.ID,
			ProviderID: providerID,
			AuthType:   authType,
			Configured: entry.configured,
			Discovered: entry.discovered,
			Credential: CredentialStatusDTO{
				Present: present,
				Kind:    kind,
				Source:  source,
				EnvVar:  envVar,
			},
			BrowserSession: session,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProviderID != out[j].ProviderID {
			return out[i].ProviderID < out[j].ProviderID
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func browserSessionDTO(account core.AccountConfig, session config.BrowserSession, hasStored bool, now time.Time) BrowserSessionDTO {
	result := BrowserSessionDTO{Configured: account.BrowserCookie != nil}
	if account.BrowserCookie != nil {
		result.Domain = strings.TrimSpace(account.BrowserCookie.Domain)
		result.CookieName = strings.TrimSpace(account.BrowserCookie.CookieName)
		result.SourceBrowser = strings.TrimSpace(account.BrowserCookie.SourceBrowser)
	}
	if !hasStored {
		return result
	}
	result.Connected = strings.TrimSpace(session.Value) != ""
	if value := strings.TrimSpace(session.Domain); value != "" {
		result.Domain = value
	}
	if value := strings.TrimSpace(session.CookieName); value != "" {
		result.CookieName = value
	}
	if value := strings.TrimSpace(session.SourceBrowser); value != "" {
		result.SourceBrowser = value
	}
	result.CapturedAt = strings.TrimSpace(session.CapturedAt)
	result.ExpiresAt = strings.TrimSpace(session.ExpiresAt)
	if result.ExpiresAt != "" {
		if expires, err := time.Parse(time.RFC3339, result.ExpiresAt); err == nil {
			result.Expired = now.After(expires)
		}
	}
	return result
}

func timeWindowDTOs() []TimeWindowDTO {
	out := make([]TimeWindowDTO, 0, len(core.ValidTimeWindows))
	for _, window := range core.ValidTimeWindows {
		out = append(out, TimeWindowDTO{
			ID:    string(window),
			Label: window.Label(),
			Days:  window.Days(),
		})
	}
	return out
}

func settingsDTO(cfg config.Config) SettingsDTO {
	dashboard := DashboardSettingsDTO{
		View:                   strings.TrimSpace(cfg.Dashboard.View),
		HideSectionsWithNoData: cfg.Dashboard.HideSectionsWithNoData,
		HideCosts:              cloneBool(cfg.Dashboard.HideCosts),
	}
	for _, provider := range cfg.Dashboard.Providers {
		dashboard.Providers = append(dashboard.Providers, DashboardProviderDTO{
			AccountID: strings.TrimSpace(provider.AccountID),
			Enabled:   provider.Enabled,
			HideCosts: cloneBool(provider.HideCosts),
		})
	}
	for _, section := range cfg.Dashboard.WidgetSections {
		dashboard.WidgetSections = append(dashboard.WidgetSections, WidgetSectionDTO{
			ID:      strings.TrimSpace(string(section.ID)),
			Enabled: section.Enabled,
		})
	}
	for _, section := range cfg.Dashboard.DetailSections {
		dashboard.DetailSections = append(dashboard.DetailSections, WidgetSectionDTO{
			ID:      strings.TrimSpace(string(section.ID)),
			Enabled: section.Enabled,
		})
	}

	links := make(map[string]string, len(cfg.Telemetry.ProviderLinks))
	for source, target := range cfg.Telemetry.ProviderLinks {
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if source != "" && target != "" {
			links[source] = target
		}
	}
	return SettingsDTO{
		Theme:      strings.TrimSpace(cfg.Theme),
		AutoDetect: cfg.AutoDetect,
		UI: UISettingsDTO{
			RefreshIntervalSeconds: cfg.UI.RefreshIntervalSeconds,
			WarnThreshold:          cfg.UI.WarnThreshold,
			CritThreshold:          cfg.UI.CritThreshold,
		},
		Data: DataSettingsDTO{
			TimeWindow:    strings.TrimSpace(cfg.Data.TimeWindow),
			RetentionDays: cfg.Data.RetentionDays,
		},
		Dashboard: dashboard,
		Telemetry: TelemetrySettingsDTO{ProviderLinks: links},
		ModelNormalization: core.ModelNormalizationConfig{
			Enabled:       cfg.ModelNormalization.Enabled,
			GroupBy:       strings.TrimSpace(cfg.ModelNormalization.GroupBy),
			MinConfidence: cfg.ModelNormalization.MinConfidence,
			Overrides:     append([]core.ModelNormalizationOverride(nil), cfg.ModelNormalization.Overrides...),
		},
	}
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func themeDTOs(themes []tui.Theme) []ThemeDTO {
	out := make([]ThemeDTO, 0, len(themes))
	for _, theme := range themes {
		out = append(out, ThemeDTO{
			Name: theme.Name,
			Icon: theme.Icon,
			Colors: map[string]string{
				"base":      string(theme.Base),
				"mantle":    string(theme.Mantle),
				"surface0":  string(theme.Surface0),
				"surface1":  string(theme.Surface1),
				"surface2":  string(theme.Surface2),
				"overlay":   string(theme.Overlay),
				"text":      string(theme.Text),
				"subtext":   string(theme.Subtext),
				"dim":       string(theme.Dim),
				"accent":    string(theme.Accent),
				"blue":      string(theme.Blue),
				"sapphire":  string(theme.Sapphire),
				"green":     string(theme.Green),
				"yellow":    string(theme.Yellow),
				"red":       string(theme.Red),
				"peach":     string(theme.Peach),
				"teal":      string(theme.Teal),
				"flamingo":  string(theme.Flamingo),
				"rosewater": string(theme.Rosewater),
				"lavender":  string(theme.Lavender),
				"sky":       string(theme.Sky),
				"maroon":    string(theme.Maroon),
				"mauve":     string(theme.Mauve),
			},
		})
	}
	return out
}

func (s *Server) integrationDTOs(cfg config.Config) []IntegrationDTO {
	declined := make(map[string]bool, len(cfg.Integrations))
	for id, state := range cfg.Integrations {
		declined[strings.TrimSpace(id)] = state.Declined
	}
	statuses := s.integrationLister()
	out := make([]IntegrationDTO, 0, len(statuses))
	for _, status := range statuses {
		id := strings.TrimSpace(string(status.ID))
		if id == "" {
			continue
		}
		out = append(out, IntegrationDTO{
			ID:               id,
			Name:             strings.TrimSpace(status.Name),
			Installed:        status.Installed,
			Configured:       status.Configured,
			InstalledVersion: strings.TrimSpace(status.InstalledVersion),
			DesiredVersion:   strings.TrimSpace(status.DesiredVersion),
			NeedsUpgrade:     status.NeedsUpgrade,
			State:            strings.TrimSpace(status.State),
			Summary:          strings.TrimSpace(status.Summary),
			Declined:         declined[id],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
