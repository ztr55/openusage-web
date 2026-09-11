package web

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
	"github.com/janekbaraniewski/openusage/internal/detect"
	"github.com/janekbaraniewski/openusage/internal/integrations"
)

func TestSanitizeSnapshotOmitsRawAndDoesNotMutate(t *testing.T) {
	snap := core.NewUsageSnapshot("claude_code", "claude")
	snap.Raw["subscription"] = "active"
	snap.Raw["secret"] = "do-not-return"
	used := 7.0
	snap.Metrics["requests"] = core.Metric{Used: &used, Unit: "requests", Window: "1d"}

	dto := SanitizeSnapshot(snap, nil, nil)
	if !dto.HideCosts {
		t.Fatal("HideCosts = false, want true from the raw subscription signal")
	}
	dto.Metrics["requests"] = core.Metric{}
	if snap.Metrics["requests"].Used == nil || *snap.Metrics["requests"].Used != used {
		t.Fatal("sanitizing snapshot mutated the source metrics")
	}

	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal sanitized snapshot: %v", err)
	}
	if strings.Contains(string(data), "do-not-return") || strings.Contains(string(data), `"raw"`) {
		t.Fatalf("sanitized snapshot leaked raw data: %s", data)
	}
	if snap.Raw["secret"] != "do-not-return" {
		t.Fatal("sanitizing snapshot mutated the source raw map")
	}
}

func TestSanitizeSnapshotOmitsSensitiveMetadataCopiedFromRaw(t *testing.T) {
	snap := core.NewUsageSnapshot("openrouter", "router")
	snap.Attributes["account_email"] = "dev@example.com"
	snap.Attributes["api_key_hint"] = "should-not-return"
	snap.Diagnostics["session_token"] = "should-not-return"

	dto := SanitizeSnapshot(snap, nil, nil)
	if dto.Attributes["account_email"] != "dev@example.com" {
		t.Fatalf("safe attribute missing: %#v", dto.Attributes)
	}
	for _, values := range []map[string]string{dto.Attributes, dto.Diagnostics} {
		for key, value := range values {
			if strings.Contains(key, "key") || strings.Contains(key, "token") || strings.Contains(value, "should-not-return") {
				t.Fatalf("sensitive metadata leaked: %#v", values)
			}
		}
	}
}

func TestSnapshotEndpointUsesRuntimeAndRedactsRaw(t *testing.T) {
	snap := core.NewUsageSnapshot("claude_code", "claude")
	snap.Raw["subscription"] = "active"
	snap.Raw["api_key"] = "secret-api-key"
	snap.Status = core.StatusOK

	runtimeView, closeDaemon := fakeDaemonRuntime(t, map[string]core.UsageSnapshot{"claude": snap})
	defer closeDaemon()

	server, err := NewServer(Options{
		Runtime: runtimeView,
		ConfigLoader: func() (config.Config, error) {
			return config.Config{Data: config.DataConfig{TimeWindow: "30d"}}, nil
		},
		CredentialsLoader: func() (config.Credentials, error) {
			return config.Credentials{Keys: map[string]string{}}, nil
		},
		ApplyCredentials: func(*detect.Result) {},
		Now:              fixedNow,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots?window=7d", nil)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.Code, resp.Body)
	}

	var payload SnapshotsResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode snapshots response: %v", err)
	}
	if payload.Window != "7d" {
		t.Fatalf("window = %q, want 7d", payload.Window)
	}
	got, ok := payload.Snapshots["claude"]
	if !ok {
		t.Fatalf("snapshot missing from response: %#v", payload.Snapshots)
	}
	if !got.HideCosts {
		t.Fatal("snapshot hide_costs = false, want true")
	}
	data := resp.Body.String()
	if strings.Contains(data, "secret-api-key") || strings.Contains(data, `"raw"`) {
		t.Fatalf("snapshot response leaked raw data: %s", data)
	}
}

func TestBootstrapContainsSafeStateAndMetadata(t *testing.T) {
	provider := testProvider{
		spec: core.ProviderSpec{
			ID: "test-provider",
			Info: core.ProviderInfo{
				Name:         "Test Provider",
				Capabilities: []string{"usage_endpoint"},
				DocURL:       "https://example.com/docs",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeAPIKey,
				APIKeyEnv:        "TEST_PROVIDER_KEY",
				DefaultAccountID: "test",
			},
		},
	}
	storedSession := config.BrowserSession{
		Domain:        ".example.com",
		CookieName:    "session",
		Value:         "cookie-secret",
		SourceBrowser: "firefox",
		CapturedAt:    "2026-09-11T10:00:00Z",
		ExpiresAt:     "2026-09-12T10:00:00Z",
	}
	cfg := config.Config{
		AutoDetect: false,
		Theme:      "Deep Space",
		Data:       config.DataConfig{TimeWindow: "7d", RetentionDays: 90},
		Accounts: []core.AccountConfig{
			{
				ID:        "test",
				Provider:  "test-provider",
				Auth:      "api_key",
				APIKeyEnv: "TEST_PROVIDER_KEY",
				Token:     "api-key-secret",
				BrowserCookie: &core.BrowserCookieRef{
					Domain:     ".example.com",
					CookieName: "session",
				},
			},
		},
		Dashboard: config.DashboardConfig{
			HideCosts: boolPtr(true),
		},
	}

	server, err := NewServer(Options{
		ConfigLoader: func() (config.Config, error) { return cfg, nil },
		CredentialsLoader: func() (config.Credentials, error) {
			return config.Credentials{
				Keys:     map[string]string{"test": "stored-key-secret"},
				Sessions: map[string]config.BrowserSession{"test": storedSession},
			}, nil
		},
		Discover:         func() detect.Result { return detect.Result{} },
		ApplyCredentials: func(*detect.Result) {},
		ProviderLister:   func() []core.UsageProvider { return []core.UsageProvider{provider} },
		IntegrationLister: func() []integrations.Status {
			return []integrations.Status{{ID: integrations.OpenCodeID, Name: "OpenCode", State: "missing"}}
		},
		Now: fixedNow,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.Code, resp.Body)
	}
	var payload BootstrapResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode bootstrap response: %v", err)
	}
	if payload.RequestToken == "" || payload.APIVersion != APIVersion {
		t.Fatalf("bootstrap protocol fields are incomplete: %#v", payload)
	}
	if len(payload.TimeWindows) != len(core.ValidTimeWindows) {
		t.Fatalf("time windows = %d, want %d", len(payload.TimeWindows), len(core.ValidTimeWindows))
	}
	if len(payload.Providers) != 1 || payload.Providers[0].AuthType != "api_key" {
		t.Fatalf("provider metadata = %#v", payload.Providers)
	}
	if len(payload.Accounts) != 1 {
		t.Fatalf("accounts = %#v, want one account", payload.Accounts)
	}
	account := payload.Accounts[0]
	if !account.Configured || !account.Credential.Present || !account.BrowserSession.Connected {
		t.Fatalf("safe account status is incomplete: %#v", account)
	}
	if account.BrowserSession.ExpiresAt != storedSession.ExpiresAt || account.BrowserSession.Expired {
		t.Fatalf("browser session status = %#v", account.BrowserSession)
	}
	data := resp.Body.String()
	for _, secret := range []string{"api-key-secret", "stored-key-secret", "cookie-secret"} {
		if strings.Contains(data, secret) {
			t.Fatalf("bootstrap response leaked %q: %s", secret, data)
		}
	}
}

func TestHealthReportsUnavailableDaemonWithoutFailingWebHealth(t *testing.T) {
	server, err := NewServer(Options{
		Runtime: daemon.NewViewRuntime(nil, "", false),
		Now:     fixedNow,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.Code, resp.Body)
	}
	var payload HealthResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if payload.Status != "ok" || payload.Daemon.Healthy {
		t.Fatalf("health = %#v, want live web and unhealthy daemon", payload)
	}
}

func TestSnapshotsRejectInvalidWindow(t *testing.T) {
	server, err := NewServer(Options{
		ConfigLoader: func() (config.Config, error) { return config.DefaultConfig(), nil },
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshots?window=2d", nil)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", resp.Code, resp.Body)
	}
}

func TestMutationRequiresRequestTokenAndSameOrigin(t *testing.T) {
	var installs atomic.Int32
	server, err := NewServer(Options{
		InstallDaemon: func() error {
			installs.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	request := func(token, host, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/daemon/install", nil)
		req.Host = host
		req.Header.Set("Origin", origin)
		if token != "" {
			req.Header.Set(RequestTokenHeader, token)
		}
		resp := httptest.NewRecorder()
		server.Handler().ServeHTTP(resp, req)
		return resp
	}

	if resp := request("", "127.0.0.1:8787", "http://127.0.0.1:8787"); resp.Code != http.StatusForbidden {
		t.Fatalf("missing token status = %d, want 403", resp.Code)
	}
	if resp := request(server.RequestToken(), "127.0.0.1:8787", "http://localhost:8787"); resp.Code != http.StatusForbidden {
		t.Fatalf("wrong origin status = %d, want 403", resp.Code)
	}
	if resp := request(server.RequestToken(), "127.0.0.1:8787", "http://127.0.0.1:8787"); resp.Code != http.StatusOK {
		t.Fatalf("valid mutation status = %d, want 200: %s", resp.Code, resp.Body)
	}
	if installs.Load() != 1 {
		t.Fatalf("install calls = %d, want 1", installs.Load())
	}
}

func TestPublicAPIRequiresAccessTokenAndLeavesHealthOpen(t *testing.T) {
	server, err := NewServer(Options{
		AllowPublic:  true,
		AuthToken:    "web-secret",
		ConfigLoader: func() (config.Config, error) { return config.DefaultConfig(), nil },
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/api/v1/bootstrap", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing access token status = %d, want 401", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/api/v1/bootstrap?access_token=web-secret", nil)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Set-Cookie") == "" {
		t.Fatalf("query access token response = %d headers=%#v", response.Code, response.Header())
	}

	request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/api/v1/health", nil)
	request.Header.Set("Cookie", response.Header().Get("Set-Cookie"))
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("cookie access token status = %d, want 200", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/healthz", nil)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", response.Code)
	}

	request = httptest.NewRequest(http.MethodHead, "http://127.0.0.1:8787/healthz", nil)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("healthz HEAD status = %d, want 200", response.Code)
	}
}

func TestStaticServingUsesSPAFallbackAndRejectsTraversal(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("dashboard shell"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	assetsDir := filepath.Join(staticDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatalf("create assets dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "app.js"), []byte("asset"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	docsDir := filepath.Join(staticDir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "index.html"), []byte("docs shell"), 0o644); err != nil {
		t.Fatalf("write docs index: %v", err)
	}

	server, err := NewServer(Options{StaticDir: staticDir})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	get := func(requestPath string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787"+requestPath, nil)
		resp := httptest.NewRecorder()
		server.Handler().ServeHTTP(resp, req)
		return resp
	}

	if resp := get("/app/providers"); resp.Code != http.StatusOK || resp.Body.String() != "dashboard shell" {
		t.Fatalf("SPA fallback = %d %q", resp.Code, resp.Body.String())
	}
	if resp := get("/app/providers"); resp.Header().Get("X-Frame-Options") != "DENY" || resp.Header().Get("Content-Security-Policy") != "frame-ancestors 'none'" {
		t.Fatalf("frame protection headers = %#v", resp.Header())
	}
	if resp := get("/assets/app.js"); resp.Code != http.StatusOK || resp.Body.String() != "asset" {
		t.Fatalf("asset response = %d %q", resp.Code, resp.Body.String())
	}
	if resp := get("/docs/"); resp.Code != http.StatusOK || resp.Body.String() != "docs shell" {
		t.Fatalf("nested index response = %d %q", resp.Code, resp.Body.String())
	}
	if resp := get("/../index.html"); resp.Code != http.StatusNotFound {
		t.Fatalf("traversal status = %d, want 404", resp.Code)
	}
}

func TestValidateListenAddrOnlyAllowsLoopback(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{addr: "127.0.0.1:8787", want: true},
		{addr: "localhost:8787", want: true},
		{addr: "[::1]:8787", want: true},
		{addr: ":8787", want: false},
		{addr: "0.0.0.0:8787", want: false},
		{addr: "10.0.0.5:8787", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.addr, func(t *testing.T) {
			err := ValidateListenAddr(tc.addr)
			if (err == nil) != tc.want {
				t.Fatalf("ValidateListenAddr(%q) error = %v, want success=%v", tc.addr, err, tc.want)
			}
		})
	}
}

func fakeDaemonRuntime(t *testing.T, snapshots map[string]core.UsageSnapshot) (*daemon.ViewRuntime, func()) {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "daemon.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen fake daemon socket: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","api_version":"v1"}`))
		case "/v1/read-model":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(daemon.ReadModelResponse{Snapshots: snapshots})
		default:
			http.NotFound(w, r)
		}
	})}
	go func() { _ = server.Serve(listener) }()

	runtimeView := daemon.NewViewRuntime(daemon.NewClient(socketPath), socketPath, false)
	closeFn := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
	return runtimeView, closeFn
}

type testProvider struct {
	spec core.ProviderSpec
}

func (p testProvider) ID() string                            { return p.spec.ID }
func (p testProvider) Describe() core.ProviderInfo           { return p.spec.Info }
func (p testProvider) Spec() core.ProviderSpec               { return p.spec }
func (p testProvider) DashboardWidget() core.DashboardWidget { return p.spec.Dashboard }
func (p testProvider) DetailWidget() core.DetailWidget       { return p.spec.Detail }
func (p testProvider) Fetch(context.Context, core.AccountConfig) (core.UsageSnapshot, error) {
	return core.UsageSnapshot{}, nil
}

func fixedNow() time.Time {
	return time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
}

func boolPtr(value bool) *bool {
	return &value
}
