package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type Credentials struct {
	Keys     map[string]string               `json:"keys"`               // account ID → API key
	Sessions map[string]BrowserSession       `json:"sessions,omitempty"` // account ID → browser-session credential
	OAuth    map[string]core.OAuthCredential `json:"oauth,omitempty"`    // account ID → provider OAuth login
}

// BrowserSession stores a single account's browser-session credential. Used
// by providers whose dashboard data is gated by session cookies — see
// docs/BROWSER_SESSION_AUTH_DESIGN.md. The cookie value lives only in this
// file (not in settings.json), and the file is written with 0o600 perms;
// that's the same filesystem-permission posture as the existing API-key store.
type BrowserSession struct {
	// Domain and CookieName are mirrors of the AccountConfig.BrowserCookie
	// reference, persisted here so the credential is self-contained
	// (re-extraction works even if settings.json is regenerated).
	Domain     string `json:"domain"`
	CookieName string `json:"cookie_name"`

	// Value is the cookie value. Treated as a high-sensitivity credential.
	Value string `json:"value"`

	// SourceBrowser is the canonical browser name the cookie was last
	// extracted from ("chrome", "firefox", etc.). Used as a hint to the
	// extractor so it tries that browser first on the next refresh and
	// avoids triggering keychain prompts on others.
	SourceBrowser string `json:"source_browser,omitempty"`

	// CapturedAt is when openusage last successfully extracted this cookie
	// from the browser. ExpiresAt is the cookie's own Set-Cookie expiry —
	// zero for session-only cookies. Both are RFC3339 strings on the wire
	// for human readability.
	CapturedAt string `json:"captured_at,omitempty"`
	ExpiresAt  string `json:"expires_at,omitempty"`
}

// credMu guards read-modify-write cycles on the credentials file.
var credMu sync.Mutex

func CredentialsPath() string {
	return filepath.Join(ConfigDir(), "credentials.json")
}

func LoadCredentials() (Credentials, error) {
	return LoadCredentialsFrom(CredentialsPath())
}

func LoadCredentialsFrom(path string) (Credentials, error) {
	creds := emptyCredentials()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return creds, nil
		}
		return creds, fmt.Errorf("reading credentials: %w", err)
	}

	if err := json.Unmarshal(data, &creds); err != nil {
		return emptyCredentials(), fmt.Errorf("parsing credentials %s: %w", path, err)
	}

	if creds.Keys == nil {
		creds.Keys = make(map[string]string)
	}
	if creds.Sessions == nil {
		creds.Sessions = make(map[string]BrowserSession)
	}
	if creds.OAuth == nil {
		creds.OAuth = make(map[string]core.OAuthCredential)
	}
	if len(creds.Keys) > 0 {
		normalized := make(map[string]string, len(creds.Keys))
		for accountID, key := range creds.Keys {
			id := normalizeAccountID(accountID)
			if id == "" {
				continue
			}
			if _, exists := normalized[id]; !exists || accountID == id {
				normalized[id] = key
			}
		}
		creds.Keys = normalized
	}
	if len(creds.Sessions) > 0 {
		normalized := make(map[string]BrowserSession, len(creds.Sessions))
		for accountID, session := range creds.Sessions {
			id := normalizeAccountID(accountID)
			if id == "" {
				continue
			}
			if _, exists := normalized[id]; !exists || accountID == id {
				normalized[id] = session
			}
		}
		creds.Sessions = normalized
	}
	if len(creds.OAuth) > 0 {
		normalized := make(map[string]core.OAuthCredential, len(creds.OAuth))
		for accountID, credential := range creds.OAuth {
			id := normalizeAccountID(accountID)
			if id == "" {
				continue
			}
			credential.AccessToken = strings.TrimSpace(credential.AccessToken)
			credential.RefreshToken = strings.TrimSpace(credential.RefreshToken)
			credential.ProviderAccountID = strings.TrimSpace(credential.ProviderAccountID)
			if credential.AccessToken == "" {
				continue
			}
			if _, exists := normalized[id]; !exists || accountID == id {
				normalized[id] = credential
			}
		}
		creds.OAuth = normalized
	}

	return creds, nil
}

func SaveCredential(accountID, apiKey string) error {
	return SaveCredentialTo(CredentialsPath(), accountID, apiKey)
}

func SaveCredentialTo(path, accountID, apiKey string) error {
	accountID = normalizeAccountID(accountID)
	if accountID == "" {
		return fmt.Errorf("account ID is empty")
	}

	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("api key is empty")
	}

	return updateCredentials(path, func(creds *Credentials) {
		creds.Keys[accountID] = apiKey
	})
}

func DeleteCredential(accountID string) error {
	return DeleteCredentialFrom(CredentialsPath(), accountID)
}

func DeleteCredentialFrom(path, accountID string) error {
	accountID = normalizeAccountID(accountID)
	if accountID == "" {
		return fmt.Errorf("account ID is empty")
	}

	return updateCredentials(path, func(creds *Credentials) {
		delete(creds.Keys, accountID)
	})
}

// SaveOAuthCredential persists the minimum token set needed by a provider's
// runtime OAuth path. Imported CLI auth files are never stored verbatim.
func SaveOAuthCredential(accountID string, credential core.OAuthCredential) error {
	return SaveOAuthCredentialTo(CredentialsPath(), accountID, credential)
}

func SaveOAuthCredentialTo(path, accountID string, credential core.OAuthCredential) error {
	accountID = normalizeAccountID(accountID)
	if accountID == "" {
		return fmt.Errorf("account ID is empty")
	}
	credential.AccessToken = strings.TrimSpace(credential.AccessToken)
	credential.RefreshToken = strings.TrimSpace(credential.RefreshToken)
	credential.ProviderAccountID = strings.TrimSpace(credential.ProviderAccountID)
	if credential.AccessToken == "" {
		return fmt.Errorf("access token is empty")
	}

	return updateCredentials(path, func(creds *Credentials) {
		creds.OAuth[accountID] = credential
	})
}

// DeleteOAuthCredential removes an imported OAuth credential while leaving API
// keys, browser sessions, and the account configuration untouched.
func DeleteOAuthCredential(accountID string) error {
	return DeleteOAuthCredentialFrom(CredentialsPath(), accountID)
}

func DeleteOAuthCredentialFrom(path, accountID string) error {
	accountID = normalizeAccountID(accountID)
	if accountID == "" {
		return fmt.Errorf("account ID is empty")
	}

	return updateCredentials(path, func(creds *Credentials) {
		delete(creds.OAuth, accountID)
	})
}

func LoadOAuthCredential(accountID string) (core.OAuthCredential, bool, error) {
	return LoadOAuthCredentialFrom(CredentialsPath(), accountID)
}

func LoadOAuthCredentialFrom(path, accountID string) (core.OAuthCredential, bool, error) {
	accountID = normalizeAccountID(accountID)
	if accountID == "" {
		return core.OAuthCredential{}, false, fmt.Errorf("account ID is empty")
	}
	creds, err := LoadCredentialsFrom(path)
	if err != nil {
		return core.OAuthCredential{}, false, err
	}
	credential, ok := creds.OAuth[accountID]
	return credential, ok, nil
}

// RefreshOAuthCredential serializes a rotating-token exchange across processes.
// The refresh callback runs only if the stored credential still matches the
// caller's copy, and it runs while the credentials lock is held.
func RefreshOAuthCredential(accountID string, previous core.OAuthCredential, refresh func(core.OAuthCredential) (core.OAuthCredential, error)) (core.OAuthCredential, bool, error) {
	return RefreshOAuthCredentialFrom(CredentialsPath(), accountID, previous, refresh)
}

func RefreshOAuthCredentialFrom(path, accountID string, previous core.OAuthCredential, refresh func(core.OAuthCredential) (core.OAuthCredential, error)) (core.OAuthCredential, bool, error) {
	accountID = normalizeAccountID(accountID)
	if accountID == "" {
		return core.OAuthCredential{}, false, fmt.Errorf("account ID is empty")
	}
	if refresh == nil {
		return core.OAuthCredential{}, false, fmt.Errorf("refresh callback is nil")
	}

	var result core.OAuthCredential
	refreshed := false
	err := withLockedCredentials(path, func(creds *Credentials) error {
		current, ok := creds.OAuth[accountID]
		if !ok {
			return nil
		}
		result = current
		if current.AccessToken != previous.AccessToken || current.RefreshToken != previous.RefreshToken {
			return nil
		}
		replacement, err := refresh(current)
		if err != nil {
			return err
		}
		replacement.AccessToken = strings.TrimSpace(replacement.AccessToken)
		replacement.RefreshToken = strings.TrimSpace(replacement.RefreshToken)
		replacement.ProviderAccountID = strings.TrimSpace(replacement.ProviderAccountID)
		if replacement.AccessToken == "" {
			return fmt.Errorf("access token is empty")
		}
		creds.OAuth[accountID] = replacement
		result = replacement
		refreshed = true
		return nil
	})
	return result, refreshed, err
}

// SaveSession persists a browser-session credential under the given account.
// The credential is protected only via filesystem perms (0o600) — the
// same posture as API keys in this store. Cookie values must never travel
// outside this file or the runtime memory of the daemon.
func SaveSession(accountID string, session BrowserSession) error {
	return SaveSessionTo(CredentialsPath(), accountID, session)
}

func SaveSessionTo(path, accountID string, session BrowserSession) error {
	accountID = normalizeAccountID(accountID)
	if accountID == "" {
		return fmt.Errorf("account ID is empty")
	}
	if strings.TrimSpace(session.Value) == "" {
		return fmt.Errorf("session value is empty")
	}
	if strings.TrimSpace(session.Domain) == "" || strings.TrimSpace(session.CookieName) == "" {
		return fmt.Errorf("session domain and cookie_name are required")
	}

	return updateCredentials(path, func(creds *Credentials) {
		creds.Sessions[accountID] = session
	})
}

// DeleteSession removes a browser-session credential. Safe to call when no
// entry exists.
func DeleteSession(accountID string) error {
	return DeleteSessionFrom(CredentialsPath(), accountID)
}

func DeleteSessionFrom(path, accountID string) error {
	accountID = normalizeAccountID(accountID)
	if accountID == "" {
		return fmt.Errorf("account ID is empty")
	}

	return updateCredentials(path, func(creds *Credentials) {
		delete(creds.Sessions, accountID)
	})
}

// LoadSession returns the stored browser-session credential for an account
// along with a found flag. Use this rather than poking creds.Sessions
// directly so the normalization / lookup stays in one place.
func LoadSession(accountID string) (BrowserSession, bool, error) {
	return LoadSessionFrom(CredentialsPath(), accountID)
}

func LoadSessionFrom(path, accountID string) (BrowserSession, bool, error) {
	accountID = normalizeAccountID(accountID)
	if accountID == "" {
		return BrowserSession{}, false, fmt.Errorf("account ID is empty")
	}
	creds, err := LoadCredentialsFrom(path)
	if err != nil {
		return BrowserSession{}, false, err
	}
	s, ok := creds.Sessions[accountID]
	return s, ok, nil
}

func writeCredentials(path string, creds Credentials) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating credentials dir: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling credentials: %w", err)
	}
	data = append(data, '\n')

	temporary, err := os.CreateTemp(dir, ".credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary credentials file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("setting temporary credentials permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("writing credentials: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("syncing credentials: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("closing credentials: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replacing credentials: %w", err)
	}
	return nil
}

func updateCredentials(path string, update func(*Credentials)) error {
	return withLockedCredentials(path, func(creds *Credentials) error {
		update(creds)
		return nil
	})
}

func withLockedCredentials(path string, update func(*Credentials) error) error {
	credMu.Lock()
	defer credMu.Unlock()

	unlock, err := lockFile(path)
	if err != nil {
		return err
	}
	defer unlock()

	creds, err := LoadCredentialsFrom(path)
	if err != nil {
		return err
	}
	if err := update(&creds); err != nil {
		return err
	}
	return writeCredentials(path, creds)
}

func emptyCredentials() Credentials {
	return Credentials{
		Keys:     make(map[string]string),
		Sessions: make(map[string]BrowserSession),
		OAuth:    make(map[string]core.OAuthCredential),
	}
}
