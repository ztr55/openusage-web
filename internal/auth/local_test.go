package auth

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestParseLocalCredential(t *testing.T) {
	claims, err := json.Marshal(map[string]any{"exp": int64(1_900_000_000)})
	if err != nil {
		t.Fatal(err)
	}
	jwt := "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"

	tests := []struct {
		name        string
		provider    string
		data        string
		wantKind    core.ProviderAuthType
		wantAccess  string
		wantRefresh string
		wantExpiry  int64
	}{
		{
			name:        "Claude Code nested credentials",
			provider:    "claude_code",
			data:        `{"claudeAiOauth":{"accessToken":"claude-access","refreshToken":"claude-refresh","expiresAt":1900000000000}}`,
			wantKind:    core.ProviderAuthTypeOAuth,
			wantAccess:  "claude-access",
			wantRefresh: "claude-refresh",
			wantExpiry:  1900000000000,
		},
		{
			name:        "Claude Code legacy credentials",
			provider:    "claude_code",
			data:        `{"accessToken":"legacy-access","refreshToken":"legacy-refresh","expiresAt":1900000000000}`,
			wantKind:    core.ProviderAuthTypeOAuth,
			wantAccess:  "legacy-access",
			wantRefresh: "legacy-refresh",
			wantExpiry:  1900000000000,
		},
		{
			name:        "Codex credentials",
			provider:    "codex",
			data:        `{"account_id":"chatgpt-account","tokens":{"access_token":"` + jwt + `","refresh_token":"codex-refresh"}}`,
			wantKind:    core.ProviderAuthTypeOAuth,
			wantAccess:  jwt,
			wantRefresh: "codex-refresh",
			wantExpiry:  1900000000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLocalCredential(tt.provider, []byte(tt.data))
			if err != nil {
				t.Fatalf("ParseLocalCredential: %v", err)
			}
			if got.AuthType != tt.wantKind || got.Credential.AccessToken != tt.wantAccess || got.Credential.RefreshToken != tt.wantRefresh || got.Credential.ExpiresAt != tt.wantExpiry {
				t.Fatalf("credential = %#v, want kind=%q access=%q expiry=%d", got, tt.wantKind, tt.wantAccess, tt.wantExpiry)
			}
			if tt.provider == "codex" && got.Credential.ProviderAccountID != "chatgpt-account" {
				t.Fatalf("Codex provider account ID = %q", got.Credential.ProviderAccountID)
			}
		})
	}
}

func TestParseLocalCredentialRejectsUnsupportedOrMalformedInput(t *testing.T) {
	for _, tc := range []struct {
		provider string
		data     string
	}{
		{provider: "unknown", data: `{}`},
		{provider: "claude_code", data: `{not-json`},
		{provider: "claude_code", data: `{"claudeAiOauth":{}}`},
		{provider: "codex", data: `{"tokens":{}}`},
	} {
		if _, err := ParseLocalCredential(tc.provider, []byte(tc.data)); err == nil {
			t.Fatalf("ParseLocalCredential(%q) accepted invalid input", tc.provider)
		}
	}
}
