package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/version"
)

const (
	claudeOAuthAuthorizeURL = "https://claude.com/cai/oauth/authorize"
	claudeOAuthTokenURL     = "https://platform.claude.com/v1/oauth/token"
	claudeOAuthClientID     = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	claudeOAuthRedirectURI  = "https://platform.claude.com/oauth/code/callback"

	codexOAuthBaseURL     = "https://auth.openai.com"
	codexOAuthClientID    = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexDeviceFlowExpiry = 15 * time.Minute

	maxOAuthResponseBytes = 1 << 20
)

var (
	ErrAuthorizationPending = errors.New("authorization pending")
	ErrAuthorizationExpired = errors.New("authorization expired")
	ErrDeviceAuthDisabled   = errors.New("device code login is not enabled")
	ErrRefreshTokenRejected = errors.New("refresh token rejected")
)

var claudeOAuthScopes = []string{
	"org:create_api_key",
	"user:profile",
	"user:inference",
	"user:sessions:claude_code",
	"user:mcp_servers",
	"user:file_upload",
}

// OAuthClient implements the public OAuth flows used by Claude Code and Codex.
// Endpoint fields are configurable so the protocol can be tested without live services.
type OAuthClient struct {
	HTTPClient         *http.Client
	ClaudeAuthorizeURL string
	ClaudeTokenURL     string
	CodexBaseURL       string
	Now                func() time.Time
}

func NewOAuthClient(client *http.Client) *OAuthClient {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &OAuthClient{
		HTTPClient:         client,
		ClaudeAuthorizeURL: claudeOAuthAuthorizeURL,
		ClaudeTokenURL:     claudeOAuthTokenURL,
		CodexBaseURL:       codexOAuthBaseURL,
		Now:                time.Now,
	}
}

type ClaudeAuthorization struct {
	AuthorizationURL string
	CodeVerifier     string
	State            string
}

func (c *OAuthClient) StartClaudeAuthorization() (ClaudeAuthorization, error) {
	verifier, err := randomOAuthValue(32)
	if err != nil {
		return ClaudeAuthorization{}, fmt.Errorf("create Claude PKCE verifier: %w", err)
	}
	state, err := randomOAuthValue(32)
	if err != nil {
		return ClaudeAuthorization{}, fmt.Errorf("create Claude OAuth state: %w", err)
	}
	challenge := sha256.Sum256([]byte(verifier))
	authorizeURL, err := url.Parse(c.claudeAuthorizeURL())
	if err != nil {
		return ClaudeAuthorization{}, fmt.Errorf("parse Claude authorization endpoint: %w", err)
	}
	query := authorizeURL.Query()
	query.Set("code", "true")
	query.Set("client_id", claudeOAuthClientID)
	query.Set("response_type", "code")
	query.Set("redirect_uri", claudeOAuthRedirectURI)
	query.Set("scope", strings.Join(claudeOAuthScopes, " "))
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	query.Set("code_challenge_method", "S256")
	query.Set("state", state)
	authorizeURL.RawQuery = query.Encode()
	return ClaudeAuthorization{
		AuthorizationURL: authorizeURL.String(),
		CodeVerifier:     verifier,
		State:            state,
	}, nil
}

func (c *OAuthClient) ExchangeClaudeAuthorization(ctx context.Context, authorization ClaudeAuthorization, input string) (core.OAuthCredential, error) {
	code, returnedState := parseAuthorizationCodeInput(input)
	if code == "" {
		return core.OAuthCredential{}, errors.New("Claude authorization response does not contain a code")
	}
	if returnedState == "" || returnedState != authorization.State {
		return core.OAuthCredential{}, errors.New("Claude authorization state mismatch")
	}

	payload, err := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     claudeOAuthClientID,
		"code":          code,
		"redirect_uri":  claudeOAuthRedirectURI,
		"code_verifier": authorization.CodeVerifier,
		"state":         authorization.State,
	})
	if err != nil {
		return core.OAuthCredential{}, fmt.Errorf("encode Claude token exchange: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.claudeTokenURL(), strings.NewReader(string(payload)))
	if err != nil {
		return core.OAuthCredential{}, fmt.Errorf("create Claude token exchange: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	var response oauthTokenResponse
	if err := c.doTokenRequest(req, "Claude token exchange", &response); err != nil {
		return core.OAuthCredential{}, err
	}
	if strings.TrimSpace(response.AccessToken) == "" || strings.TrimSpace(response.RefreshToken) == "" {
		return core.OAuthCredential{}, errors.New("Claude token exchange returned incomplete credentials")
	}
	now := c.now()
	expiresAt := oauthExpiryMillis(response.ExpiresAt, response.ExpiresIn, now)
	if expiresAt == 0 {
		expiresAt = now.Add(8 * time.Hour).UnixMilli()
	}
	return core.OAuthCredential{
		AccessToken:           strings.TrimSpace(response.AccessToken),
		RefreshToken:          strings.TrimSpace(response.RefreshToken),
		ExpiresAt:             expiresAt,
		RefreshTokenExpiresAt: oauthExpiryMillis(response.RefreshTokenExpiresAt, response.RefreshTokenExpiresIn, now),
	}, nil
}

type CodexDeviceAuthorization struct {
	DeviceAuthID    string
	UserCode        string
	VerificationURL string
	Interval        time.Duration
	ExpiresAt       time.Time
}

func (c *OAuthClient) StartCodexDeviceAuthorization(ctx context.Context) (CodexDeviceAuthorization, error) {
	payload, err := json.Marshal(map[string]string{"client_id": codexOAuthClientID})
	if err != nil {
		return CodexDeviceAuthorization{}, fmt.Errorf("encode Codex device authorization: %w", err)
	}
	endpoint := strings.TrimRight(c.codexBaseURL(), "/") + "/api/accounts/deviceauth/usercode"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return CodexDeviceAuthorization{}, fmt.Errorf("create Codex device authorization: %w", err)
	}
	setCodexHeaders(req, "application/json")

	resp, err := c.client().Do(req)
	if err != nil {
		return CodexDeviceAuthorization{}, fmt.Errorf("Codex device authorization request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return CodexDeviceAuthorization{}, ErrDeviceAuthDisabled
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CodexDeviceAuthorization{}, fmt.Errorf("Codex device authorization returned HTTP %d", resp.StatusCode)
	}
	var response struct {
		DeviceAuthID string        `json:"device_auth_id"`
		UserCode     string        `json:"user_code"`
		UserCodeAlt  string        `json:"usercode"`
		Interval     secondsString `json:"interval"`
	}
	if err := decodeLimitedJSON(resp.Body, &response); err != nil {
		return CodexDeviceAuthorization{}, fmt.Errorf("parse Codex device authorization: %w", err)
	}
	if response.UserCode == "" {
		response.UserCode = response.UserCodeAlt
	}
	response.DeviceAuthID = strings.TrimSpace(response.DeviceAuthID)
	response.UserCode = strings.TrimSpace(response.UserCode)
	if response.DeviceAuthID == "" || response.UserCode == "" {
		return CodexDeviceAuthorization{}, errors.New("Codex device authorization returned incomplete data")
	}
	intervalSeconds := int64(response.Interval)
	if intervalSeconds < 1 {
		intervalSeconds = 5
	}
	if intervalSeconds > 60 {
		intervalSeconds = 60
	}
	interval := time.Duration(intervalSeconds) * time.Second
	now := c.now()
	return CodexDeviceAuthorization{
		DeviceAuthID:    response.DeviceAuthID,
		UserCode:        response.UserCode,
		VerificationURL: strings.TrimRight(c.codexBaseURL(), "/") + "/codex/device",
		Interval:        interval,
		ExpiresAt:       now.Add(codexDeviceFlowExpiry),
	}, nil
}

func (c *OAuthClient) CompleteCodexDeviceAuthorization(ctx context.Context, authorization CodexDeviceAuthorization) (core.OAuthCredential, error) {
	if !c.now().Before(authorization.ExpiresAt) {
		return core.OAuthCredential{}, ErrAuthorizationExpired
	}
	payload, err := json.Marshal(map[string]string{
		"device_auth_id": authorization.DeviceAuthID,
		"user_code":      authorization.UserCode,
	})
	if err != nil {
		return core.OAuthCredential{}, fmt.Errorf("encode Codex device poll: %w", err)
	}
	endpoint := strings.TrimRight(c.codexBaseURL(), "/") + "/api/accounts/deviceauth/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return core.OAuthCredential{}, fmt.Errorf("create Codex device poll: %w", err)
	}
	setCodexHeaders(req, "application/json")

	resp, err := c.client().Do(req)
	if err != nil {
		return core.OAuthCredential{}, fmt.Errorf("Codex device poll failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxOAuthResponseBytes))
		return core.OAuthCredential{}, ErrAuthorizationPending
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return core.OAuthCredential{}, fmt.Errorf("Codex device authorization returned HTTP %d", resp.StatusCode)
	}
	var approved struct {
		AuthorizationCode string `json:"authorization_code"`
		CodeVerifier      string `json:"code_verifier"`
	}
	if err := decodeLimitedJSON(resp.Body, &approved); err != nil {
		return core.OAuthCredential{}, fmt.Errorf("parse Codex device approval: %w", err)
	}
	approved.AuthorizationCode = strings.TrimSpace(approved.AuthorizationCode)
	approved.CodeVerifier = strings.TrimSpace(approved.CodeVerifier)
	if approved.AuthorizationCode == "" || approved.CodeVerifier == "" {
		return core.OAuthCredential{}, errors.New("Codex device approval returned incomplete data")
	}
	return c.exchangeCodexAuthorizationCode(ctx, approved.AuthorizationCode, approved.CodeVerifier)
}

func (c *OAuthClient) RefreshCodexCredential(ctx context.Context, credential core.OAuthCredential) (core.OAuthCredential, error) {
	refreshToken := strings.TrimSpace(credential.RefreshToken)
	if refreshToken == "" {
		return core.OAuthCredential{}, errors.New("Codex credential has no refresh token")
	}
	payload, err := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     codexOAuthClientID,
	})
	if err != nil {
		return core.OAuthCredential{}, fmt.Errorf("encode Codex token refresh: %w", err)
	}
	endpoint := strings.TrimRight(c.codexBaseURL(), "/") + "/oauth/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return core.OAuthCredential{}, fmt.Errorf("create Codex token refresh: %w", err)
	}
	setCodexHeaders(req, "application/json")
	var response oauthTokenResponse
	if err := c.doTokenRequest(req, "Codex token refresh", &response); err != nil {
		var statusError *oauthHTTPStatusError
		if errors.As(err, &statusError) && statusError.TerminalRefreshError {
			return core.OAuthCredential{}, fmt.Errorf("%w: %v", ErrRefreshTokenRejected, err)
		}
		return core.OAuthCredential{}, err
	}
	updated, err := c.codexCredential(response, refreshToken)
	if err != nil {
		return core.OAuthCredential{}, err
	}
	if updated.ProviderAccountID == "" {
		updated.ProviderAccountID = credential.ProviderAccountID
	}
	return updated, nil
}

func (c *OAuthClient) exchangeCodexAuthorizationCode(ctx context.Context, code, verifier string) (core.OAuthCredential, error) {
	baseURL := strings.TrimRight(c.codexBaseURL(), "/")
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {baseURL + "/deviceauth/callback"},
		"client_id":     {codexOAuthClientID},
		"code_verifier": {verifier},
	}
	response, err := c.requestCodexTokens(ctx, form, "Codex token exchange")
	if err != nil {
		return core.OAuthCredential{}, err
	}
	return c.codexCredential(response, "")
}

func (c *OAuthClient) requestCodexTokens(ctx context.Context, form url.Values, operation string) (oauthTokenResponse, error) {
	endpoint := strings.TrimRight(c.codexBaseURL(), "/") + "/oauth/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return oauthTokenResponse{}, fmt.Errorf("create %s: %w", strings.ToLower(operation), err)
	}
	setCodexHeaders(req, "application/x-www-form-urlencoded")
	var response oauthTokenResponse
	if err := c.doTokenRequest(req, operation, &response); err != nil {
		return oauthTokenResponse{}, err
	}
	return response, nil
}

func (c *OAuthClient) codexCredential(response oauthTokenResponse, existingRefreshToken string) (core.OAuthCredential, error) {
	accessToken := strings.TrimSpace(response.AccessToken)
	refreshToken := strings.TrimSpace(response.RefreshToken)
	if refreshToken == "" {
		refreshToken = existingRefreshToken
	}
	if accessToken == "" || refreshToken == "" {
		return core.OAuthCredential{}, errors.New("Codex token endpoint returned incomplete credentials")
	}
	expiresAt := oauthExpiryMillis(response.ExpiresAt, response.ExpiresIn, c.now())
	if expiresAt == 0 {
		expiresAt = jwtExpiryMillis(accessToken)
	}
	if expiresAt == 0 {
		expiresAt = c.now().Add(time.Hour).UnixMilli()
	}
	providerAccountID := accountIDFromJWT(response.IDToken)
	if providerAccountID == "" {
		providerAccountID = accountIDFromJWT(accessToken)
	}
	return core.OAuthCredential{
		AccessToken:       accessToken,
		RefreshToken:      refreshToken,
		ProviderAccountID: providerAccountID,
		ExpiresAt:         expiresAt,
	}, nil
}

type oauthTokenResponse struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	IDToken               string `json:"id_token"`
	ExpiresIn             int64  `json:"expires_in"`
	ExpiresAt             int64  `json:"expires_at"`
	RefreshTokenExpiresAt int64  `json:"refresh_token_expires_at"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
}

func (c *OAuthClient) doTokenRequest(req *http.Request, operation string, dst any) error {
	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("%s request failed: %w", operation, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return &oauthHTTPStatusError{
			Operation:            operation,
			StatusCode:           resp.StatusCode,
			TerminalRefreshError: hasTerminalRefreshErrorCode(body),
		}
	}
	if err := decodeLimitedJSON(resp.Body, dst); err != nil {
		return fmt.Errorf("parse %s response: %w", strings.ToLower(operation), err)
	}
	return nil
}

type oauthHTTPStatusError struct {
	Operation            string
	StatusCode           int
	TerminalRefreshError bool
}

func hasTerminalRefreshErrorCode(body []byte) bool {
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

func (e *oauthHTTPStatusError) Error() string {
	return fmt.Sprintf("%s returned HTTP %d", e.Operation, e.StatusCode)
}

func (c *OAuthClient) client() *http.Client {
	if c != nil && c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (c *OAuthClient) now() time.Time {
	if c != nil && c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *OAuthClient) claudeAuthorizeURL() string {
	if c != nil && strings.TrimSpace(c.ClaudeAuthorizeURL) != "" {
		return strings.TrimSpace(c.ClaudeAuthorizeURL)
	}
	return claudeOAuthAuthorizeURL
}

func (c *OAuthClient) claudeTokenURL() string {
	if c != nil && strings.TrimSpace(c.ClaudeTokenURL) != "" {
		return strings.TrimSpace(c.ClaudeTokenURL)
	}
	return claudeOAuthTokenURL
}

func (c *OAuthClient) codexBaseURL() string {
	if c != nil && strings.TrimSpace(c.CodexBaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(c.CodexBaseURL), "/")
	}
	return codexOAuthBaseURL
}

func setCodexHeaders(req *http.Request, contentType string) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Originator", "openusage")
	userAgent := "openusage"
	if value := strings.TrimSpace(version.Version); value != "" {
		userAgent += "/" + value
		req.Header.Set("Version", value)
	}
	req.Header.Set("User-Agent", userAgent)
}

func randomOAuthValue(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func parseAuthorizationCodeInput(input string) (code, state string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ""
	}
	if parsed, err := url.Parse(input); err == nil && parsed.IsAbs() {
		query := parsed.Query()
		fragment, _ := url.ParseQuery(strings.TrimPrefix(parsed.Fragment, "#"))
		fragmentState := fragment.Get("state")
		if fragmentState == "" && parsed.Fragment != "" && !strings.Contains(parsed.Fragment, "=") {
			fragmentState = parsed.Fragment
		}
		return strings.TrimSpace(firstNonEmpty(query.Get("code"), fragment.Get("code"))),
			strings.TrimSpace(firstNonEmpty(query.Get("state"), fragmentState))
	}
	if strings.HasPrefix(input, "code=") {
		values, _ := url.ParseQuery(input)
		return strings.TrimSpace(values.Get("code")), strings.TrimSpace(values.Get("state"))
	}

	codePart, fragmentState, _ := strings.Cut(input, "#")
	code, rawQuery, hasQuery := strings.Cut(codePart, "&")
	if hasQuery {
		values, _ := url.ParseQuery(rawQuery)
		state = values.Get("state")
	}
	if state == "" {
		state = fragmentState
	}
	decodedCode, err := url.QueryUnescape(strings.TrimSpace(code))
	if err == nil {
		code = decodedCode
	}
	decodedState, err := url.QueryUnescape(strings.TrimSpace(state))
	if err == nil {
		state = decodedState
	}
	return strings.TrimSpace(code), strings.TrimSpace(state)
}

func accountIDFromJWT(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		AccountID string `json:"chatgpt_account_id"`
		Auth      struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
		Organizations []struct {
			ID string `json:"id"`
		} `json:"organizations"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	if value := strings.TrimSpace(claims.AccountID); value != "" {
		return value
	}
	if value := strings.TrimSpace(claims.Auth.AccountID); value != "" {
		return value
	}
	if len(claims.Organizations) > 0 {
		return strings.TrimSpace(claims.Organizations[0].ID)
	}
	return ""
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

func decodeLimitedJSON(reader io.Reader, dst any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, maxOAuthResponseBytes))
	return decoder.Decode(dst)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type secondsString int64

func (s *secondsString) UnmarshalJSON(data []byte) error {
	value := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if value == "" || value == "null" {
		return nil
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return err
	}
	*s = secondsString(seconds)
	return nil
}
