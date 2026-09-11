package detect

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestProbeClaudeCodeCredentialsFileReadsNestedOAuthSchema(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS uses the keychain probe instead of this file path")
	}
	home := withCleanCredentialEnv(t)
	path := filepath.Join(home, ".claude", ".credentials.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().Add(time.Hour).UnixMilli()
	data := []byte(`{"claudeAiOauth":{"accessToken":"claude-access","refreshToken":"claude-refresh","expiresAt":` + strconv.FormatInt(expiresAt, 10) + `}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var result Result
	probeClaudeCodeCredentialsFile(&result)
	if len(result.Accounts) != 1 {
		t.Fatalf("accounts = %+v", result.Accounts)
	}
	account := result.Accounts[0]
	if account.Hint("credential_source", "") == "" || account.Hint("credential_expires_at", "") != strconv.FormatInt(expiresAt, 10) || account.Hint("credential_refreshable", "") != "true" {
		t.Fatalf("credential hints = %#v", account.RuntimeHints)
	}
	if account.Hint("access_token", "") != "" {
		t.Fatal("access token was copied into runtime hints")
	}
}

func TestDetectCodexMarksAccessTokenWithoutExposingIt(t *testing.T) {
	expires := int64(1_900_000_000)
	accessToken := makeFakeIDToken(t, map[string]interface{}{"exp": expires})
	withFakeCodexAuth(t, `{"tokens":{"access_token":"`+accessToken+`"}}`)

	var result Result
	detectCodex(&result)
	var found bool
	for _, account := range result.Accounts {
		if account.ID != "codex-cli" {
			continue
		}
		found = true
		if account.Hint("credential_source", "") == "" || account.Hint("credential_expires_at", "") != strconv.FormatInt(expires*1000, 10) {
			t.Fatalf("credential hints = %#v", account.RuntimeHints)
		}
		if account.Hint("access_token", "") != "" {
			t.Fatal("access token was copied into runtime hints")
		}
	}
	if !found {
		t.Fatalf("Codex account not found: %+v", result.Accounts)
	}
}

func TestApplyCredentialsAppliesStoredOAuth(t *testing.T) {
	withCleanCredentialEnv(t)
	credential := core.OAuthCredential{AccessToken: "stored-access", RefreshToken: "stored-refresh"}
	if err := config.SaveOAuthCredential("claude-code", credential); err != nil {
		t.Fatalf("SaveOAuthCredential: %v", err)
	}
	result := Result{Accounts: []core.AccountConfig{{ID: "claude-code", Provider: "claude_code", Auth: "local"}}}
	ApplyCredentials(&result)
	if len(result.Accounts) != 1 || result.Accounts[0].OAuth == nil || result.Accounts[0].OAuth.AccessToken != credential.AccessToken || result.Accounts[0].Token != credential.AccessToken {
		t.Fatalf("applied account = %#v", result.Accounts)
	}
	if result.Accounts[0].Hint("oauth_source", "") != "stored" {
		t.Fatalf("oauth source = %#v", result.Accounts[0].RuntimeHints)
	}
}
