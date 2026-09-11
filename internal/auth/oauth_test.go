package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestClaudeAuthorizationUsesPKCEAndExchangesReturnedCode(t *testing.T) {
	now := time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
	var tokenRequest map[string]string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" || r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("token request = %s %s %q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&tokenRequest); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"access_token":"claude-access","refresh_token":"claude-refresh","expires_in":3600}`))
	}))
	defer server.Close()

	client := NewOAuthClient(server.Client())
	client.ClaudeAuthorizeURL = server.URL + "/authorize"
	client.ClaudeTokenURL = server.URL + "/token"
	client.Now = func() time.Time { return now }
	authorization, err := client.StartClaudeAuthorization()
	if err != nil {
		t.Fatalf("StartClaudeAuthorization: %v", err)
	}
	parsed, err := url.Parse(authorization.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	challenge := sha256.Sum256([]byte(authorization.CodeVerifier))
	if parsed.Query().Get("state") != authorization.State || parsed.Query().Get("code_challenge") != base64.RawURLEncoding.EncodeToString(challenge[:]) {
		t.Fatalf("authorization query = %v", parsed.Query())
	}

	credential, err := client.ExchangeClaudeAuthorization(context.Background(), authorization, "claude-code#"+authorization.State)
	if err != nil {
		t.Fatalf("ExchangeClaudeAuthorization: %v", err)
	}
	if credential.AccessToken != "claude-access" || credential.RefreshToken != "claude-refresh" || credential.ExpiresAt != now.Add(time.Hour).UnixMilli() {
		t.Fatalf("credential = %#v", credential)
	}
	if tokenRequest["code"] != "claude-code" || tokenRequest["state"] != authorization.State || tokenRequest["code_verifier"] != authorization.CodeVerifier {
		t.Fatalf("token request = %#v", tokenRequest)
	}
}

func TestClaudeAuthorizationRejectsMismatchedState(t *testing.T) {
	client := NewOAuthClient(nil)
	authorization, err := client.StartClaudeAuthorization()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ExchangeClaudeAuthorization(context.Background(), authorization, "code#wrong-state"); err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("state mismatch error = %v", err)
	}
}

func TestClaudeAuthorizationRequiresReturnedState(t *testing.T) {
	client := NewOAuthClient(nil)
	authorization, err := client.StartClaudeAuthorization()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ExchangeClaudeAuthorization(context.Background(), authorization, "code-without-state"); err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("missing state error = %v", err)
	}
}

func TestCodexDeviceAuthorizationPollsAndExchangesTokens(t *testing.T) {
	now := time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
	idToken := testJWT(t, map[string]any{
		"exp":                now.Add(time.Hour).Unix(),
		"chatgpt_account_id": "chatgpt-account",
	})
	var polls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Originator") != "openusage" || !strings.HasPrefix(r.Header.Get("User-Agent"), "openusage") {
			t.Fatalf("Codex headers = %#v", r.Header)
		}
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			_, _ = w.Write([]byte(`{"device_auth_id":"device-id","user_code":"ABCD-EFGH","interval":"2"}`))
		case "/api/accounts/deviceauth/token":
			if polls.Add(1) == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"authorization_code":"authorization-code","code_verifier":"code-verifier"}`))
		case "/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("redirect_uri") != server.URL+"/deviceauth/callback" || r.Form.Get("code") != "authorization-code" || r.Form.Get("code_verifier") != "code-verifier" {
				t.Fatalf("token form = %v", r.Form)
			}
			_, _ = w.Write([]byte(`{"access_token":"codex-access","refresh_token":"codex-refresh","id_token":` + jsonStringForTest(idToken) + `,"expires_in":3600}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewOAuthClient(server.Client())
	client.CodexBaseURL = server.URL
	client.Now = func() time.Time { return now }
	authorization, err := client.StartCodexDeviceAuthorization(context.Background())
	if err != nil {
		t.Fatalf("StartCodexDeviceAuthorization: %v", err)
	}
	if authorization.UserCode != "ABCD-EFGH" || authorization.Interval != 2*time.Second || authorization.VerificationURL != server.URL+"/codex/device" {
		t.Fatalf("authorization = %#v", authorization)
	}
	if _, err := client.CompleteCodexDeviceAuthorization(context.Background(), authorization); err != ErrAuthorizationPending {
		t.Fatalf("first poll error = %v", err)
	}
	credential, err := client.CompleteCodexDeviceAuthorization(context.Background(), authorization)
	if err != nil {
		t.Fatalf("CompleteCodexDeviceAuthorization: %v", err)
	}
	if credential.AccessToken != "codex-access" || credential.RefreshToken != "codex-refresh" || credential.ProviderAccountID != "chatgpt-account" || credential.ExpiresAt != now.Add(time.Hour).UnixMilli() {
		t.Fatalf("credential = %#v", credential)
	}
}

func TestRefreshCodexCredentialKeepsRefreshTokenWhenNotRotated(t *testing.T) {
	now := time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("refresh content type = %q", r.Header.Get("Content-Type"))
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["grant_type"] != "refresh_token" || body["refresh_token"] != "old-refresh" {
			t.Fatalf("refresh body = %v", body)
		}
		_, _ = w.Write([]byte(`{"access_token":"fresh-access","expires_in":1800}`))
	}))
	defer server.Close()

	client := NewOAuthClient(server.Client())
	client.CodexBaseURL = server.URL
	client.Now = func() time.Time { return now }
	credential, err := client.RefreshCodexCredential(context.Background(), coreOAuthCredentialForTest())
	if err != nil {
		t.Fatalf("RefreshCodexCredential: %v", err)
	}
	if credential.AccessToken != "fresh-access" || credential.RefreshToken != "old-refresh" || credential.ProviderAccountID != "existing-account" || credential.ExpiresAt != now.Add(30*time.Minute).UnixMilli() {
		t.Fatalf("credential = %#v", credential)
	}
}

func TestRefreshCodexCredentialClassifiesRejectedToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","refresh_token":"must-not-leak"}`))
	}))
	defer server.Close()

	client := NewOAuthClient(server.Client())
	client.CodexBaseURL = server.URL
	_, err := client.RefreshCodexCredential(context.Background(), coreOAuthCredentialForTest())
	if !errors.Is(err, ErrRefreshTokenRejected) || strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("rejected refresh error = %v", err)
	}
}

func TestRefreshCodexCredentialDoesNotPoisonGenericBadRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_request"}`))
	}))
	defer server.Close()

	client := NewOAuthClient(server.Client())
	client.CodexBaseURL = server.URL
	_, err := client.RefreshCodexCredential(context.Background(), coreOAuthCredentialForTest())
	if err == nil || errors.Is(err, ErrRefreshTokenRejected) {
		t.Fatalf("generic bad request error = %v", err)
	}
}

func testJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func jsonStringForTest(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func coreOAuthCredentialForTest() core.OAuthCredential {
	return core.OAuthCredential{RefreshToken: "old-refresh", ProviderAccountID: "existing-account"}
}
