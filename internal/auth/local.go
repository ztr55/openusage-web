package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// ImportedCredential is the provider-neutral result of parsing a supported
// local auth file. The raw file is intentionally not retained.
type ImportedCredential struct {
	Credential core.OAuthCredential
	AuthType   core.ProviderAuthType
}

// ParseLocalCredential accepts the contents of a supported CLI credential file
// and extracts only the fields OpenUsage needs for provider requests.
func ParseLocalCredential(providerID string, data []byte) (ImportedCredential, error) {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "claude_code":
		return parseClaudeCodeCredential(data)
	case "codex":
		return parseCodexCredential(data)
	default:
		return ImportedCredential{}, fmt.Errorf("provider does not support local credential import")
	}
}

type claudeOAuthFields struct {
	AccessToken           string `json:"accessToken"`
	RefreshToken          string `json:"refreshToken"`
	ExpiresAt             int64  `json:"expiresAt"`
	RefreshTokenExpiresAt int64  `json:"refreshTokenExpiresAt"`
}

type claudeCredentialsFile struct {
	ClaudeAIOAuth         claudeOAuthFields `json:"claudeAiOauth"`
	AccessToken           string            `json:"accessToken"`
	RefreshToken          string            `json:"refreshToken"`
	ExpiresAt             int64             `json:"expiresAt"`
	RefreshTokenExpiresAt int64             `json:"refreshTokenExpiresAt"`
}

func parseClaudeCodeCredential(data []byte) (ImportedCredential, error) {
	var raw claudeCredentialsFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return ImportedCredential{}, fmt.Errorf("invalid Claude Code credentials JSON")
	}

	oauth := raw.ClaudeAIOAuth
	if strings.TrimSpace(oauth.AccessToken) == "" {
		oauth = claudeOAuthFields{
			AccessToken:           raw.AccessToken,
			RefreshToken:          raw.RefreshToken,
			ExpiresAt:             raw.ExpiresAt,
			RefreshTokenExpiresAt: raw.RefreshTokenExpiresAt,
		}
	}
	oauth.AccessToken = strings.TrimSpace(oauth.AccessToken)
	oauth.RefreshToken = strings.TrimSpace(oauth.RefreshToken)
	if oauth.AccessToken == "" {
		return ImportedCredential{}, fmt.Errorf("Claude Code credentials do not contain an access token")
	}

	return ImportedCredential{
		Credential: core.OAuthCredential{
			AccessToken:           oauth.AccessToken,
			RefreshToken:          oauth.RefreshToken,
			ExpiresAt:             oauth.ExpiresAt,
			RefreshTokenExpiresAt: oauth.RefreshTokenExpiresAt,
		},
		AuthType: core.ProviderAuthTypeOAuth,
	}, nil
}

type codexCredentialsFile struct {
	AccountID string `json:"account_id"`
	Tokens    struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
}

func parseCodexCredential(data []byte) (ImportedCredential, error) {
	var raw codexCredentialsFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return ImportedCredential{}, fmt.Errorf("invalid Codex credentials JSON")
	}
	accessToken := strings.TrimSpace(raw.Tokens.AccessToken)
	if accessToken == "" {
		return ImportedCredential{}, fmt.Errorf("Codex credentials do not contain an access token")
	}

	expiresAt := jwtExpiryMillis(accessToken)
	if expiresAt == 0 {
		expiresAt = jwtExpiryMillis(raw.Tokens.IDToken)
	}
	return ImportedCredential{
		Credential: core.OAuthCredential{
			AccessToken:  accessToken,
			RefreshToken: strings.TrimSpace(raw.Tokens.RefreshToken),
			ProviderAccountID: firstNonEmpty(
				strings.TrimSpace(raw.Tokens.AccountID),
				strings.TrimSpace(raw.AccountID),
				accountIDFromJWT(raw.Tokens.IDToken),
				accountIDFromJWT(accessToken),
			),
			ExpiresAt: expiresAt,
		},
		AuthType: core.ProviderAuthTypeOAuth,
	}, nil
}

func jwtExpiryMillis(token string) int64 {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return 0
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return 0
		}
	}
	var claims struct {
		ExpiresAt json.Number `json:"exp"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	if err := decoder.Decode(&claims); err != nil {
		return 0
	}
	seconds, err := claims.ExpiresAt.Int64()
	if err != nil || seconds <= 0 {
		return 0
	}
	return seconds * 1000
}
