package web

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/auth"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/detect"
)

func TestCodexOAuthDeviceFlowPersistsRefreshableCredential(t *testing.T) {
	isolateWebConfig(t)
	if err := config.Save(config.Config{AutoDetect: false}); err != nil {
		t.Fatal(err)
	}
	var polls atomic.Int32
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			_, _ = w.Write([]byte(`{"device_auth_id":"private-device-id","user_code":"CODE-1234","interval":"1"}`))
		case "/api/accounts/deviceauth/token":
			if polls.Add(1) == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"authorization_code":"private-auth-code","code_verifier":"private-verifier"}`))
		case "/oauth/token":
			accountToken := webTestJWT(t, map[string]any{"chatgpt_account_id": "chatgpt-account"})
			_, _ = w.Write([]byte(`{"access_token":"private-access","refresh_token":"private-refresh","id_token":` + jsonString(accountToken) + `,"expires_in":3600}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	oauthClient := auth.NewOAuthClient(upstream.Client())
	oauthClient.CodexBaseURL = upstream.URL
	oauthClient.Now = fixedNow
	provider := testProvider{spec: core.ProviderSpec{
		ID: "codex",
		Auth: core.ProviderAuthSpec{
			Type:             core.ProviderAuthTypeOAuth,
			DefaultAccountID: "codex-cli",
		},
	}}
	server, err := NewServer(Options{
		ConfigLoader:     config.Load,
		ConfigSaver:      config.Save,
		ProviderLister:   func() []core.UsageProvider { return []core.UsageProvider{provider} },
		ApplyCredentials: func(*detect.Result) {},
		OAuthClient:      oauthClient,
		Now:              fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}

	response := doControlRequest(server, http.MethodPost, "/api/v1/accounts/codex-cli/oauth/start", `{"provider_id":"codex"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("start status = %d: %s", response.Code, response.Body)
	}
	var started OAuthStartResponse
	if err := json.Unmarshal(response.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if started.FlowID == "" || started.UserCode != "CODE-1234" || started.VerificationURL != upstream.URL+"/codex/device" || started.IntervalSeconds != 1 {
		t.Fatalf("start response = %#v", started)
	}
	if strings.Contains(response.Body.String(), "private-device-id") {
		t.Fatalf("start response leaked device auth ID: %s", response.Body)
	}

	completeBody := `{"flow_id":` + jsonString(started.FlowID) + `,"authorization_response":""}`
	response = doControlRequest(server, http.MethodPost, "/api/v1/accounts/codex-cli/oauth/complete", completeBody)
	if response.Code != http.StatusAccepted {
		t.Fatalf("pending status = %d: %s", response.Code, response.Body)
	}
	response = doControlRequest(server, http.MethodPost, "/api/v1/accounts/codex-cli/oauth/complete", completeBody)
	if response.Code != http.StatusOK {
		t.Fatalf("complete status = %d: %s", response.Code, response.Body)
	}
	for _, secret := range []string{"private-auth-code", "private-verifier", "private-access", "private-refresh", "chatgpt-account"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("complete response leaked %q: %s", secret, response.Body)
		}
	}
	credential, ok, err := config.LoadOAuthCredential("codex-cli")
	if err != nil || !ok {
		t.Fatalf("stored credential found=%v err=%v", ok, err)
	}
	if credential.AccessToken != "private-access" || credential.RefreshToken != "private-refresh" || credential.ProviderAccountID != "chatgpt-account" {
		t.Fatalf("stored credential = %#v", credential)
	}
	cfg, err := config.Load()
	if err != nil || len(cfg.Accounts) != 1 || cfg.Accounts[0].Auth != "oauth" || cfg.Accounts[0].Provider != "codex" {
		t.Fatalf("stored account = %#v, err=%v", cfg.Accounts, err)
	}
}

func TestClaudeOAuthFlowValidatesStateAndPersistsCredential(t *testing.T) {
	isolateWebConfig(t)
	if err := config.Save(config.Config{AutoDetect: false}); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["code"] != "returned-code" || body["state"] == "" || body["code_verifier"] == "" {
			t.Fatalf("token request = %#v", body)
		}
		_, _ = w.Write([]byte(`{"access_token":"private-claude-access","refresh_token":"private-claude-refresh","expires_in":3600}`))
	}))
	defer upstream.Close()

	oauthClient := auth.NewOAuthClient(upstream.Client())
	oauthClient.ClaudeAuthorizeURL = upstream.URL + "/authorize"
	oauthClient.ClaudeTokenURL = upstream.URL + "/token"
	oauthClient.Now = fixedNow
	provider := testProvider{spec: core.ProviderSpec{
		ID: "claude_code",
		Auth: core.ProviderAuthSpec{
			Type:              core.ProviderAuthTypeLocal,
			SupplementalTypes: []core.ProviderAuthType{core.ProviderAuthTypeOAuth},
			DefaultAccountID:  "claude-code",
		},
	}}
	server, err := NewServer(Options{
		ConfigLoader:     config.Load,
		ConfigSaver:      config.Save,
		ProviderLister:   func() []core.UsageProvider { return []core.UsageProvider{provider} },
		ApplyCredentials: func(*detect.Result) {},
		OAuthClient:      oauthClient,
		Now:              fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}

	response := doControlRequest(server, http.MethodPost, "/api/v1/accounts/claude-code/oauth/start", `{"provider_id":"claude_code"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("start status = %d: %s", response.Code, response.Body)
	}
	var started OAuthStartResponse
	if err := json.Unmarshal(response.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	authorizeURL, err := url.Parse(started.AuthorizationURL)
	if err != nil || authorizeURL.Query().Get("state") == "" {
		t.Fatalf("authorization URL = %q, err=%v", started.AuthorizationURL, err)
	}
	returnedCode := "returned-code#" + authorizeURL.Query().Get("state")
	completeBody := `{"flow_id":` + jsonString(started.FlowID) + `,"authorization_response":` + jsonString(returnedCode) + `}`
	response = doControlRequest(server, http.MethodPost, "/api/v1/accounts/claude-code/oauth/complete", completeBody)
	if response.Code != http.StatusOK {
		t.Fatalf("complete status = %d: %s", response.Code, response.Body)
	}
	if strings.Contains(response.Body.String(), "private-claude") {
		t.Fatalf("complete response leaked credential: %s", response.Body)
	}
	credential, ok, err := config.LoadOAuthCredential("claude-code")
	if err != nil || !ok || credential.AccessToken != "private-claude-access" || credential.RefreshToken != "private-claude-refresh" {
		t.Fatalf("stored credential = %#v, found=%v err=%v", credential, ok, err)
	}
}

func TestBootstrapMarksRejectedOAuthRefreshAsExpired(t *testing.T) {
	isolateWebConfig(t)
	if err := config.Save(config.Config{Accounts: []core.AccountConfig{{
		ID: "codex-cli", Provider: "codex", Auth: "oauth",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveOAuthCredential("codex-cli", core.OAuthCredential{
		AccessToken:           "access",
		RefreshToken:          "refresh",
		ExpiresAt:             fixedNow().Add(time.Hour).UnixMilli(),
		RefreshTokenExpiresAt: fixedNow().Add(-time.Minute).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	provider := testProvider{spec: core.ProviderSpec{ID: "codex", Auth: core.ProviderAuthSpec{Type: core.ProviderAuthTypeOAuth}}}
	server, err := NewServer(Options{
		ProviderLister:   func() []core.UsageProvider { return []core.UsageProvider{provider} },
		ApplyCredentials: func(*detect.Result) {},
		Now:              fixedNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("bootstrap status = %d: %s", response.Code, response.Body)
	}
	var bootstrap BootstrapResponse
	if err := json.Unmarshal(response.Body.Bytes(), &bootstrap); err != nil {
		t.Fatal(err)
	}
	if len(bootstrap.Accounts) != 1 || !bootstrap.Accounts[0].Credential.Expired || bootstrap.Accounts[0].Credential.Refreshable {
		t.Fatalf("credential status = %#v", bootstrap.Accounts)
	}
}

func webTestJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}
