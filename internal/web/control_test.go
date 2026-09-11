package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/browsercookies"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/dashboardapp"
	"github.com/janekbaraniewski/openusage/internal/detect"
	"github.com/janekbaraniewski/openusage/internal/integrations"
)

func TestSettingsEndpointsPersistValidatedValues(t *testing.T) {
	isolateWebConfig(t)
	if err := config.Save(config.DefaultConfig()); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	server, err := NewServer(Options{
		ConfigLoader:     config.Load,
		ApplyCredentials: func(*detect.Result) {},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	resp := doControlRequest(server, http.MethodPatch, "/api/v1/settings/dashboard", `{"hide_costs":null,"hide_sections_with_no_data":true,"view":"split"}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("dashboard settings status = %d: %s", resp.Code, resp.Body)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load dashboard config: %v", err)
	}
	if cfg.Dashboard.HideCosts != nil || !cfg.Dashboard.HideSectionsWithNoData || cfg.Dashboard.View != config.DashboardViewSplit {
		t.Fatalf("dashboard settings = %#v", cfg.Dashboard)
	}

	resp = doControlRequest(server, http.MethodPatch, "/api/v1/settings/time-window", `{"window":"7d"}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("time window status = %d: %s", resp.Code, resp.Body)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("load time window config: %v", err)
	}
	if cfg.Data.TimeWindow != "7d" {
		t.Fatalf("time window = %q, want 7d", cfg.Data.TimeWindow)
	}

	if resp = doControlRequest(server, http.MethodPatch, "/api/v1/settings/ui", `{"refresh_interval_seconds":45,"warn_threshold":0.30,"crit_threshold":0.08,"auto_detect":false}`); resp.Code != http.StatusOK {
		t.Fatalf("ui settings status = %d: %s", resp.Code, resp.Body)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("load ui config: %v", err)
	}
	if cfg.UI.RefreshIntervalSeconds != 45 || cfg.UI.WarnThreshold != 0.30 || cfg.UI.CritThreshold != 0.08 || cfg.AutoDetect {
		t.Fatalf("ui settings = %#v auto_detect=%v", cfg.UI, cfg.AutoDetect)
	}
	if resp = doControlRequest(server, http.MethodPatch, "/api/v1/settings/ui", `{"warn_threshold":2}`); resp.Code != http.StatusBadRequest {
		t.Fatalf("invalid ui settings status = %d, want 400", resp.Code)
	}

	if resp = doControlRequest(server, http.MethodPatch, "/api/v1/settings/theme", `{"theme":"Nord"}`); resp.Code != http.StatusOK {
		t.Fatalf("theme status = %d: %s", resp.Code, resp.Body)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("load theme config: %v", err)
	}
	if cfg.Theme != "Nord" {
		t.Fatalf("theme = %q, want Nord", cfg.Theme)
	}
	if resp = doControlRequest(server, http.MethodPatch, "/api/v1/settings/theme", `{"theme":"not-a-theme"}`); resp.Code != http.StatusBadRequest {
		t.Fatalf("unknown theme status = %d, want 400", resp.Code)
	}

	providersBody := `{"providers":[{"account_id":"openai","enabled":false,"hide_costs":true},{"account_id":"claude","enabled":true,"hide_costs":null}]}`
	if resp = doControlRequest(server, http.MethodPatch, "/api/v1/settings/providers", providersBody); resp.Code != http.StatusOK {
		t.Fatalf("provider settings status = %d: %s", resp.Code, resp.Body)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("load provider config: %v", err)
	}
	if len(cfg.Dashboard.Providers) != 2 || cfg.Dashboard.Providers[0].AccountID != "openai" || cfg.Dashboard.Providers[0].Enabled || cfg.Dashboard.Providers[0].HideCosts == nil || !*cfg.Dashboard.Providers[0].HideCosts {
		t.Fatalf("provider settings = %#v", cfg.Dashboard.Providers)
	}

	sectionsBody := `{"widget_sections":[{"id":"top_usage_progress","enabled":false}],"detail_sections":[{"id":"usage","enabled":true}],"hide_sections_with_no_data":false}`
	if resp = doControlRequest(server, http.MethodPatch, "/api/v1/settings/sections", sectionsBody); resp.Code != http.StatusOK {
		t.Fatalf("section settings status = %d: %s", resp.Code, resp.Body)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("load section config: %v", err)
	}
	if len(cfg.Dashboard.WidgetSections) != 1 || cfg.Dashboard.WidgetSections[0].Enabled || len(cfg.Dashboard.DetailSections) != 1 || cfg.Dashboard.DetailSections[0].ID != core.DetailSectionUsage || cfg.Dashboard.HideSectionsWithNoData {
		t.Fatalf("section settings = %#v / %#v / hide=%v", cfg.Dashboard.WidgetSections, cfg.Dashboard.DetailSections, cfg.Dashboard.HideSectionsWithNoData)
	}

	if resp = doControlRequest(server, http.MethodPatch, "/api/v1/settings/telemetry-links", `{"source":"my-tool","target":"codex"}`); resp.Code != http.StatusOK {
		t.Fatalf("telemetry link save status = %d: %s", resp.Code, resp.Body)
	}
	if resp = doControlRequest(server, http.MethodPatch, "/api/v1/settings/telemetry-links", `{"source":"my-tool","delete":true}`); resp.Code != http.StatusOK {
		t.Fatalf("telemetry link delete status = %d: %s", resp.Code, resp.Body)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("load telemetry config: %v", err)
	}
	if _, ok := cfg.Telemetry.ProviderLinks["my-tool"]; ok {
		t.Fatalf("deleted telemetry link remains: %#v", cfg.Telemetry.ProviderLinks)
	}
}

func TestCredentialEndpointsValidateAndNeverReturnKey(t *testing.T) {
	isolateWebConfig(t)
	var validated atomic.Int32
	provider := testProvider{spec: core.ProviderSpec{
		ID:   "test-provider",
		Info: core.ProviderInfo{Name: "Test Provider"},
		Auth: core.ProviderAuthSpec{
			Type:             core.ProviderAuthTypeAPIKey,
			APIKeyEnv:        "TEST_PROVIDER_API_KEY",
			DefaultAccountID: "test-account",
		},
	}}
	server, err := NewServer(Options{
		ConfigLoader:   config.Load,
		ConfigSaver:    config.Save,
		ProviderLister: func() []core.UsageProvider { return []core.UsageProvider{provider} },
		ValidateAPIKey: func(accountID, providerID, apiKey string) (bool, string) {
			if accountID != "test-account" || providerID != "test-provider" || apiKey != "valid-secret-key" {
				return false, "invalid"
			}
			validated.Add(1)
			return true, ""
		},
		ApplyCredentials: func(*detect.Result) {},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	resp := doControlRequest(server, http.MethodPut, "/api/v1/accounts/test-account/credential", `{"provider_id":"test-provider","api_key":"valid-secret-key"}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("credential save status = %d: %s", resp.Code, resp.Body)
	}
	if strings.Contains(resp.Body.String(), "valid-secret-key") {
		t.Fatalf("credential response leaked API key: %s", resp.Body)
	}
	if validated.Load() != 1 {
		t.Fatalf("validator calls = %d, want 1", validated.Load())
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.Accounts) != 1 || cfg.Accounts[0].ID != "test-account" || cfg.Accounts[0].Provider != "test-provider" || cfg.Accounts[0].Auth != "api_key" {
		t.Fatalf("persisted account = %#v", cfg.Accounts)
	}
	creds, err := config.LoadCredentials()
	if err != nil {
		t.Fatalf("load credentials: %v", err)
	}
	if creds.Keys["test-account"] != "valid-secret-key" {
		t.Fatalf("saved credential missing or changed")
	}

	resp = doControlRequest(server, http.MethodDelete, "/api/v1/accounts/test-account/credential", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("credential delete status = %d: %s", resp.Code, resp.Body)
	}
	creds, err = config.LoadCredentials()
	if err != nil {
		t.Fatalf("load credentials after delete: %v", err)
	}
	if _, ok := creds.Keys["test-account"]; ok {
		t.Fatal("credential remains after delete")
	}
}

func TestCredentialValidationPreservesCustomAccountFields(t *testing.T) {
	isolateWebConfig(t)
	if err := config.Save(config.Config{
		Accounts: []core.AccountConfig{{
			ID:         "custom-account",
			Provider:   "test-provider",
			Auth:       "api_key",
			BaseURL:    "https://example.test",
			ProbeModel: "custom-model",
		}},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	provider := testProvider{spec: core.ProviderSpec{
		ID:   "test-provider",
		Info: core.ProviderInfo{Name: "Test Provider"},
		Auth: core.ProviderAuthSpec{Type: core.ProviderAuthTypeAPIKey, APIKeyEnv: "TEST_PROVIDER_KEY"},
	}}
	server, err := NewServer(Options{
		ConfigLoader:   config.Load,
		ConfigSaver:    config.Save,
		ProviderLister: func() []core.UsageProvider { return []core.UsageProvider{provider} },
		ValidateAPIKeyForAccount: func(account core.AccountConfig, apiKey string) (bool, string) {
			if account.BaseURL != "https://example.test" || account.ProbeModel != "custom-model" || apiKey != "valid" {
				return false, "account fields were dropped"
			}
			return true, ""
		},
		ApplyCredentials: func(*detect.Result) {},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	resp := doControlRequest(server, http.MethodPut, "/api/v1/accounts/custom-account/credential", `{"provider_id":"test-provider","api_key":"valid"}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("credential status = %d: %s", resp.Code, resp.Body)
	}
}

func TestBrowserSessionEndpointsUseDeclaredCookieAndHideValue(t *testing.T) {
	isolateWebConfig(t)
	service := dashboardapp.NewService(context.Background())
	service.SetCookieReader(&browsercookies.FakeReader{Cookies: []browsercookies.Cookie{{
		Name:    "auth",
		Value:   "browser-cookie-secret",
		Domain:  ".example.com",
		Source:  "firefox",
		Expires: fixedNow().Add(time.Hour),
	}}})
	provider := testProvider{spec: core.ProviderSpec{
		ID:   "browser-provider",
		Info: core.ProviderInfo{Name: "Browser Provider"},
		Auth: core.ProviderAuthSpec{
			Type:                core.ProviderAuthTypeAPIKey,
			SupplementalTypes:   []core.ProviderAuthType{core.ProviderAuthTypeBrowserSession},
			BrowserCookieDomain: ".example.com",
			BrowserCookieName:   "auth",
		},
	}}
	server, err := NewServer(Options{
		DashboardService: service,
		ConfigLoader:     config.Load,
		ConfigSaver:      config.Save,
		ProviderLister:   func() []core.UsageProvider { return []core.UsageProvider{provider} },
		ApplyCredentials: func(*detect.Result) {},
		Now:              fixedNow,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	resp := doControlRequest(server, http.MethodPost, "/api/v1/accounts/browser-account/browser-session", `{"provider_id":"browser-provider","browser":"firefox"}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("browser session status = %d: %s", resp.Code, resp.Body)
	}
	if strings.Contains(resp.Body.String(), "browser-cookie-secret") {
		t.Fatalf("browser session response leaked cookie: %s", resp.Body)
	}
	var sessionResponse BrowserSessionResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &sessionResponse); err != nil {
		t.Fatalf("decode browser response: %v", err)
	}
	if !sessionResponse.BrowserSession.Connected || sessionResponse.BrowserSession.CookieName != "auth" {
		t.Fatalf("browser session response = %#v", sessionResponse)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load browser config: %v", err)
	}
	if len(cfg.Accounts) != 1 || cfg.Accounts[0].Provider != "browser-provider" || cfg.Accounts[0].Auth != "browser_session" || cfg.Accounts[0].BrowserCookie == nil {
		t.Fatalf("persisted browser account = %#v", cfg.Accounts)
	}
	if cfg.Accounts[0].BrowserCookie.Domain != ".example.com" || cfg.Accounts[0].BrowserCookie.CookieName != "auth" {
		t.Fatalf("persisted browser cookie reference = %#v", cfg.Accounts[0].BrowserCookie)
	}
	creds, err := config.LoadCredentials()
	if err != nil {
		t.Fatalf("load browser credentials: %v", err)
	}
	if creds.Sessions["browser-account"].Value != "browser-cookie-secret" {
		t.Fatal("browser session was not stored")
	}

	resp = doControlRequest(server, http.MethodDelete, "/api/v1/accounts/browser-account/browser-session", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("browser session delete status = %d: %s", resp.Code, resp.Body)
	}
	creds, err = config.LoadCredentials()
	if err != nil {
		t.Fatalf("load browser credentials after delete: %v", err)
	}
	if _, ok := creds.Sessions["browser-account"]; ok {
		t.Fatal("browser session remains after delete")
	}
}

func TestIntegrationEndpointsOperateAndPersistState(t *testing.T) {
	isolateWebConfig(t)
	var installs, uninstalls atomic.Int32
	server, err := NewServer(Options{
		ConfigLoader: config.Load,
		IntegrationLister: func() []integrations.Status {
			return []integrations.Status{{
				ID:         integrations.OpenCodeID,
				Name:       "OpenCode",
				Installed:  installs.Load() > uninstalls.Load(),
				Configured: installs.Load() > uninstalls.Load(),
				State:      "ready",
			}}
		},
		InstallIntegration: func(id integrations.ID) ([]integrations.Status, error) {
			if id != integrations.OpenCodeID {
				return nil, errors.New("unexpected integration")
			}
			installs.Add(1)
			return nil, nil
		},
		UninstallIntegration: func(id integrations.ID) error {
			if id != integrations.OpenCodeID {
				return errors.New("unexpected integration")
			}
			uninstalls.Add(1)
			return nil
		},
		ApplyCredentials: func(*detect.Result) {},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	resp := doControlRequest(server, http.MethodPost, "/api/v1/integrations/opencode/install", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("integration install status = %d: %s", resp.Code, resp.Body)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load integration config: %v", err)
	}
	if state := cfg.Integrations["opencode"]; !state.Installed {
		t.Fatalf("integration state after install = %#v", state)
	}

	resp = doControlRequest(server, http.MethodPost, "/api/v1/integrations/opencode/uninstall", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("integration uninstall status = %d: %s", resp.Code, resp.Body)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("load integration config after uninstall: %v", err)
	}
	if state := cfg.Integrations["opencode"]; state.Installed {
		t.Fatalf("integration state after uninstall = %#v", state)
	}
}

func TestControlEndpointsRejectOversizedBody(t *testing.T) {
	server, err := NewServer(Options{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	body := `{"theme":"` + strings.Repeat("x", int(maxControlBodyBytes)) + `"}`
	resp := doControlRequest(server, http.MethodPatch, "/api/v1/settings/theme", body)
	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status = %d, want 413", resp.Code)
	}
}

func isolateWebConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", home)
	}
	t.Setenv("OPENAI_API_KEY", "")
}

func doControlRequest(server *Server, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Host = "127.0.0.1:8787"
	request.Header.Set("Origin", "http://127.0.0.1:8787")
	request.Header.Set(RequestTokenHeader, server.RequestToken())
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}
