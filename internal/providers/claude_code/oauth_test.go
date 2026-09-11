package claude_code

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestRefreshClaudeCodeOAuthUsesOfficialClientShape(t *testing.T) {
	var requestBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("refresh request = %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode refresh request: %v", err)
		}
		_, _ = w.Write([]byte(`{"access_token":"fresh-access","refresh_token":"fresh-refresh","expires_in":3600}`))
	}))
	defer server.Close()

	oldURL := claudeOAuthTokenURL
	claudeOAuthTokenURL = server.URL
	defer func() { claudeOAuthTokenURL = oldURL }()

	got, err := refreshClaudeCodeOAuth(context.Background(), core.OAuthCredential{RefreshToken: "old-refresh"}, core.AccountConfig{})
	if err != nil {
		t.Fatalf("refreshClaudeCodeOAuth: %v", err)
	}
	if got.AccessToken != "fresh-access" || got.RefreshToken != "fresh-refresh" || got.ExpiresAt <= time.Now().UnixMilli() {
		t.Fatalf("refreshed credential = %#v", got)
	}
	if requestBody["grant_type"] != "refresh_token" || requestBody["client_id"] != claudeOAuthClientID || requestBody["refresh_token"] != "old-refresh" {
		t.Fatalf("refresh request body = %#v", requestBody)
	}
	if _, exists := requestBody["client_secret"]; exists {
		t.Fatal("refresh request must not use an unofficial client secret")
	}
}

func TestClaudeRefreshOnlyClassifiesTerminalOAuthCodes(t *testing.T) {
	for _, test := range []struct {
		name     string
		response string
		terminal bool
	}{
		{name: "invalid grant", response: `{"error":"invalid_grant"}`, terminal: true},
		{name: "invalid request", response: `{"error":"invalid_request"}`, terminal: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(test.response))
			}))
			defer server.Close()
			oldURL := claudeOAuthTokenURL
			claudeOAuthTokenURL = server.URL
			defer func() { claudeOAuthTokenURL = oldURL }()

			_, err := refreshClaudeCodeOAuth(context.Background(), core.OAuthCredential{RefreshToken: "refresh"}, core.AccountConfig{})
			if errors.Is(err, errClaudeRefreshRejected) != test.terminal {
				t.Fatalf("refresh error = %v, terminal=%v", err, test.terminal)
			}
		})
	}
}

func TestClaudeCodeFetchUsesImportedOAuthWithoutOrganizationFile(t *testing.T) {
	setTempHome(t)
	var gotAuthorization, gotBeta string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":23,"resets_at":"2999-01-01T00:00:00Z"}}`))
	}))
	defer server.Close()

	oldURL := oauthUsageURL
	oauthUsageURL = server.URL
	defer func() { oauthUsageURL = oldURL }()

	provider := New()
	snap, err := provider.Fetch(context.Background(), core.AccountConfig{
		ID:       "claude-code",
		Provider: "claude_code",
		OAuth:    &core.OAuthCredential{AccessToken: "imported-access"},
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %q, want OK: %s", snap.Status, snap.Message)
	}
	if gotAuthorization != "Bearer imported-access" || gotBeta != "oauth-2025-04-20" {
		t.Fatalf("OAuth headers = %q / %q", gotAuthorization, gotBeta)
	}
	metric, ok := snap.Metrics["usage_five_hour"]
	if !ok || metric.Used == nil || *metric.Used != 23 {
		t.Fatalf("usage metric = %#v", metric)
	}
	if strings.Contains(snap.Message, "imported-access") {
		t.Fatal("snapshot message leaked access token")
	}
}

func TestReadClaudeCodeOAuthCredentialRefreshesAndPersists(t *testing.T) {
	setTempHome(t)
	if err := config.SaveOAuthCredential("claude-code", core.OAuthCredential{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Minute).UnixMilli(),
	}); err != nil {
		t.Fatalf("seed OAuth credential: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"fresh-access","expires_in":3600}`))
	}))
	defer server.Close()

	oldURL := claudeOAuthTokenURL
	claudeOAuthTokenURL = server.URL
	defer func() { claudeOAuthTokenURL = oldURL }()

	got, err := readClaudeCodeOAuthCredential(context.Background(), core.AccountConfig{
		ID:    "claude-code",
		OAuth: &core.OAuthCredential{AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: time.Now().Add(-time.Minute).UnixMilli()},
	})
	if err != nil || got.AccessToken != "fresh-access" {
		t.Fatalf("refreshed OAuth credential = %#v, err=%v", got, err)
	}
	persisted, ok, err := config.LoadOAuthCredential("claude-code")
	if err != nil || !ok || persisted.AccessToken != "fresh-access" {
		t.Fatalf("persisted OAuth credential = %#v, found=%v, err=%v", persisted, ok, err)
	}
}

func TestClaudeCodeFetchRefreshesOAuthAfterUnauthorized(t *testing.T) {
	setTempHome(t)
	credential := core.OAuthCredential{
		AccessToken:  "current-access",
		RefreshToken: "current-refresh",
		ExpiresAt:    time.Now().Add(time.Hour).UnixMilli(),
	}
	if err := config.SaveOAuthCredential("claude-code", credential); err != nil {
		t.Fatalf("seed OAuth credential: %v", err)
	}
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["refresh_token"] != "current-refresh" {
			t.Fatalf("refresh body = %#v", body)
		}
		_, _ = w.Write([]byte(`{"access_token":"fresh-access","refresh_token":"fresh-refresh","expires_in":3600}`))
	}))
	defer tokenServer.Close()
	oldTokenURL := claudeOAuthTokenURL
	claudeOAuthTokenURL = tokenServer.URL
	defer func() { claudeOAuthTokenURL = oldTokenURL }()

	var calls atomic.Int32
	usageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			if r.Header.Get("Authorization") != "Bearer current-access" {
				t.Fatalf("initial authorization = %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fresh-access" {
			t.Fatalf("retry authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":31,"resets_at":"2999-01-01T00:00:00Z"}}`))
	}))
	defer usageServer.Close()
	oldUsageURL := oauthUsageURL
	oauthUsageURL = usageServer.URL
	defer func() { oauthUsageURL = oldUsageURL }()

	snapshot, err := New().Fetch(context.Background(), core.AccountConfig{
		ID:       "claude-code",
		Provider: "claude_code",
		Auth:     "oauth",
		OAuth:    &credential,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	metric := snapshot.Metrics["usage_five_hour"]
	if snapshot.Status != core.StatusOK || metric.Used == nil || *metric.Used != 31 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	stored, ok, err := config.LoadOAuthCredential("claude-code")
	if err != nil || !ok || stored.AccessToken != "fresh-access" || stored.RefreshToken != "fresh-refresh" {
		t.Fatalf("stored credential = %#v, found=%v err=%v", stored, ok, err)
	}
}
