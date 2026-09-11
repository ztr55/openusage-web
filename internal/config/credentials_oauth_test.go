package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestSaveAndLoadOAuthCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	in := core.OAuthCredential{
		AccessToken:           "access-token",
		RefreshToken:          "refresh-token",
		ProviderAccountID:     "provider-account",
		ExpiresAt:             1900000000000,
		RefreshTokenExpiresAt: 1901000000000,
	}
	if err := SaveOAuthCredentialTo(path, " claude-code ", in); err != nil {
		t.Fatalf("SaveOAuthCredentialTo: %v", err)
	}

	got, ok, err := LoadOAuthCredentialFrom(path, "claude-code")
	if err != nil {
		t.Fatalf("LoadOAuthCredentialFrom: %v", err)
	}
	if !ok || got != in {
		t.Fatalf("credential = %#v, found=%v, want %#v", got, ok, in)
	}
}

func TestOAuthCredentialCoexistsWithKeysAndSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := SaveCredentialTo(path, "openai", "api-key"); err != nil {
		t.Fatal(err)
	}
	if err := SaveSessionTo(path, "console", BrowserSession{Domain: ".example.com", CookieName: "auth", Value: "cookie"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveOAuthCredentialTo(path, "codex-cli", core.OAuthCredential{AccessToken: "codex-token"}); err != nil {
		t.Fatal(err)
	}

	creds, err := LoadCredentialsFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Keys["openai"] != "api-key" || creds.Sessions["console"].Value != "cookie" || creds.OAuth["codex-cli"].AccessToken != "codex-token" {
		t.Fatalf("credentials did not coexist: %#v", creds)
	}
}

func TestDeleteOAuthCredentialLeavesOtherCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := SaveCredentialTo(path, "openai", "api-key"); err != nil {
		t.Fatal(err)
	}
	if err := SaveOAuthCredentialTo(path, "claude-code", core.OAuthCredential{AccessToken: "token"}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteOAuthCredentialFrom(path, "claude-code"); err != nil {
		t.Fatal(err)
	}

	creds, err := LoadCredentialsFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Keys["openai"] != "api-key" {
		t.Fatal("API key was removed with OAuth credential")
	}
	if _, ok := creds.OAuth["claude-code"]; ok {
		t.Fatal("OAuth credential remains after delete")
	}
}

func TestSaveOAuthCredentialRejectsEmptyAccessToken(t *testing.T) {
	if err := SaveOAuthCredentialTo(filepath.Join(t.TempDir(), "credentials.json"), "x", core.OAuthCredential{}); err == nil {
		t.Fatal("empty access token was accepted")
	}
}

func TestSaveOAuthCredentialDoesNotOverwriteMalformedStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	original := []byte(`{"keys":`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveOAuthCredentialTo(path, "claude-code", core.OAuthCredential{AccessToken: "token"}); err == nil {
		t.Fatal("malformed credentials store was accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("malformed store changed from %q to %q", original, got)
	}
}

func TestSaveCredentialDoesNotOverwriteMalformedStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	original := []byte(`{"keys":`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredentialTo(path, "openai", "secret"); err == nil {
		t.Fatal("malformed credentials store was accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("malformed store changed from %q to %q", original, got)
	}
}

func TestOAuthCredentialFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file permission bits are not represented on Windows")
	}
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := SaveOAuthCredentialTo(path, "claude-code", core.OAuthCredential{AccessToken: "token"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("permissions = %o, want 0600", got)
	}
}

func TestLoadCredentialsNormalizesOAuthAccountIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	data, err := json.Marshal(map[string]any{
		"oauth": map[string]core.OAuthCredential{"  claude-code  ": {AccessToken: "token"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	credential, ok, err := LoadOAuthCredentialFrom(path, "claude-code")
	if err != nil || !ok || credential.AccessToken != "token" {
		t.Fatalf("normalized OAuth credential = %#v, found=%v, err=%v", credential, ok, err)
	}
}

func TestRefreshOAuthCredentialSkipsStaleCaller(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	current := core.OAuthCredential{AccessToken: "current-access", RefreshToken: "current-refresh"}
	if err := SaveOAuthCredentialTo(path, "codex-cli", current); err != nil {
		t.Fatal(err)
	}
	called := false
	got, refreshed, err := RefreshOAuthCredentialFrom(path, "codex-cli", core.OAuthCredential{
		AccessToken:  "stale-access",
		RefreshToken: "stale-refresh",
	}, func(core.OAuthCredential) (core.OAuthCredential, error) {
		called = true
		return core.OAuthCredential{AccessToken: "wrong"}, nil
	})
	if err != nil || refreshed || called || got != current {
		t.Fatalf("stale refresh = %#v refreshed=%v called=%v err=%v", got, refreshed, called, err)
	}
}
