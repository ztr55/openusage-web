package claude_code

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/auth"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

// These are Anthropic's Claude Code OAuth endpoints and public client ID. No
// separate OpenUsage OAuth application or authorization flow is registered.
var claudeOAuthTokenURL = "https://platform.claude.com/v1/oauth/token"

const claudeOAuthClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

var errClaudeRefreshRejected = errors.New("Claude Code OAuth refresh token rejected")

func readClaudeCodeOAuthToken() (string, error) {
	credential, err := readClaudeCodeOAuthCredential(context.Background(), core.AccountConfig{})
	if err != nil {
		return "", err
	}
	return credential.AccessToken, nil
}

func readClaudeCodeOAuthCredential(ctx context.Context, acct core.AccountConfig) (core.OAuthCredential, error) {
	if acct.OAuth != nil && strings.TrimSpace(acct.OAuth.AccessToken) != "" {
		credential := *acct.OAuth
		if strings.TrimSpace(credential.RefreshToken) != "" && credential.RefreshTokenExpiresAt > 0 && time.Now().UnixMilli() >= credential.RefreshTokenExpiresAt {
			return core.OAuthCredential{}, fmt.Errorf("Claude Code OAuth refresh token expired; sign in again")
		}
		if !credential.IsExpired(time.Now().Add(5 * time.Minute)) {
			return credential, nil
		}
		if strings.TrimSpace(credential.RefreshToken) == "" {
			return core.OAuthCredential{}, fmt.Errorf("Claude Code OAuth token expired and has no refresh token")
		}
		return refreshClaudeCodeOAuth(ctx, credential, acct)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return core.OAuthCredential{}, fmt.Errorf("resolving home directory: %w", err)
	}
	credsPath := filepath.Join(home, ".claude", ".credentials.json")
	credsData, err := os.ReadFile(credsPath)
	if err != nil {
		return core.OAuthCredential{}, fmt.Errorf("reading Claude Code credentials: %w", err)
	}
	imported, err := auth.ParseLocalCredential("claude_code", credsData)
	if err != nil {
		return core.OAuthCredential{}, err
	}
	if imported.Credential.IsExpired(time.Now()) {
		return core.OAuthCredential{}, fmt.Errorf("Claude Code OAuth token expired (run `claude login`)")
	}
	return imported.Credential, nil
}

type claudeOAuthRefreshResponse struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresIn             int64  `json:"expires_in"`
	ExpiresAt             int64  `json:"expires_at"`
	RefreshTokenExpiresAt int64  `json:"refresh_token_expires_at"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
}

func refreshClaudeCodeOAuth(ctx context.Context, credential core.OAuthCredential, acct core.AccountConfig) (core.OAuthCredential, error) {
	var rejected error
	refresh := func(current core.OAuthCredential) (core.OAuthCredential, error) {
		if strings.TrimSpace(current.RefreshToken) == "" {
			return core.OAuthCredential{}, fmt.Errorf("Claude Code OAuth token has no refresh token")
		}
		if current.RefreshTokenExpiresAt > 0 && time.Now().UnixMilli() >= current.RefreshTokenExpiresAt {
			return core.OAuthCredential{}, fmt.Errorf("Claude Code OAuth refresh token expired")
		}
		updated, err := requestClaudeCodeOAuthRefresh(ctx, current)
		if errors.Is(err, errClaudeRefreshRejected) && strings.TrimSpace(acct.ID) != "" {
			current.RefreshTokenExpiresAt = time.Now().UnixMilli()
			rejected = err
			return current, nil
		}
		return updated, err
	}
	if strings.TrimSpace(acct.ID) == "" {
		return refresh(credential)
	}
	updated, refreshed, err := config.RefreshOAuthCredential(acct.ID, credential, refresh)
	if err != nil {
		return core.OAuthCredential{}, fmt.Errorf("refreshing Claude Code OAuth credential: %w", err)
	}
	if updated.AccessToken == "" {
		return core.OAuthCredential{}, fmt.Errorf("Claude Code OAuth credential was removed during refresh")
	}
	if rejected != nil {
		return core.OAuthCredential{}, fmt.Errorf("Claude Code OAuth refresh token rejected; sign in again")
	}
	if !refreshed && updated.IsExpired(time.Now().Add(5*time.Minute)) {
		return core.OAuthCredential{}, fmt.Errorf("Claude Code OAuth credential changed but is expired")
	}
	return updated, nil
}

func requestClaudeCodeOAuthRefresh(ctx context.Context, credential core.OAuthCredential) (core.OAuthCredential, error) {
	payload := map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     claudeOAuthClientID,
		"refresh_token": credential.RefreshToken,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return core.OAuthCredential{}, fmt.Errorf("encoding Claude Code OAuth refresh: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claudeOAuthTokenURL, strings.NewReader(string(body)))
	if err != nil {
		return core.OAuthCredential{}, fmt.Errorf("creating Claude Code OAuth refresh: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return core.OAuthCredential{}, fmt.Errorf("Claude Code OAuth refresh request failed: %w", err)
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		if hasTerminalClaudeRefreshErrorCode(responseBody) {
			return core.OAuthCredential{}, fmt.Errorf("%w (HTTP %d)", errClaudeRefreshRejected, resp.StatusCode)
		}
		return core.OAuthCredential{}, fmt.Errorf("Claude Code OAuth refresh returned HTTP %d", resp.StatusCode)
	}
	if readErr != nil {
		return core.OAuthCredential{}, fmt.Errorf("reading Claude Code OAuth refresh: %w", readErr)
	}
	var refreshed claudeOAuthRefreshResponse
	if err := json.Unmarshal(responseBody, &refreshed); err != nil {
		return core.OAuthCredential{}, fmt.Errorf("parsing Claude Code OAuth refresh: %w", err)
	}
	refreshed.AccessToken = strings.TrimSpace(refreshed.AccessToken)
	if refreshed.AccessToken == "" {
		return core.OAuthCredential{}, fmt.Errorf("Claude Code OAuth refresh returned no access token")
	}

	now := time.Now()
	updated := credential
	updated.AccessToken = refreshed.AccessToken
	if strings.TrimSpace(refreshed.RefreshToken) != "" {
		updated.RefreshToken = strings.TrimSpace(refreshed.RefreshToken)
	}
	expiresAt := oauthExpiryMillis(refreshed.ExpiresAt, refreshed.ExpiresIn, now)
	if expiresAt == 0 {
		expiresAt = now.Add(8 * time.Hour).UnixMilli()
	}
	updated.ExpiresAt = expiresAt
	if refreshExpiresAt := oauthExpiryMillis(refreshed.RefreshTokenExpiresAt, refreshed.RefreshTokenExpiresIn, now); refreshExpiresAt > 0 {
		updated.RefreshTokenExpiresAt = refreshExpiresAt
	}
	return updated, nil
}

func hasTerminalClaudeRefreshErrorCode(body []byte) bool {
	var payload struct {
		Error json.RawMessage `json:"error"`
		Code  string          `json:"code"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return false
	}
	code := strings.TrimSpace(payload.Code)
	if len(payload.Error) > 0 {
		var value string
		if json.Unmarshal(payload.Error, &value) == nil {
			code = value
		} else {
			var nested struct {
				Code string `json:"code"`
			}
			if json.Unmarshal(payload.Error, &nested) == nil && strings.TrimSpace(nested.Code) != "" {
				code = nested.Code
			}
		}
	}
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "invalid_grant", "invalid_refresh_token", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused", "revoked":
		return true
	default:
		return false
	}
}

func oauthExpiryMillis(expiresAt, expiresIn int64, now time.Time) int64 {
	if expiresAt > 0 {
		if expiresAt < 1_000_000_000_000 {
			return expiresAt * 1000
		}
		return expiresAt
	}
	if expiresIn > 0 {
		return now.Add(time.Duration(expiresIn) * time.Second).UnixMilli()
	}
	return 0
}
