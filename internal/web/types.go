package web

import (
	"context"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
	"github.com/janekbaraniewski/openusage/internal/dashboardapp"
	"github.com/janekbaraniewski/openusage/internal/detect"
	"github.com/janekbaraniewski/openusage/internal/integrations"
)

const (
	APIVersion         = "v1"
	RequestTokenHeader = "X-OpenUsage-Request-Token"
)

// Options wires the web process to the existing local runtime. Snapshot reads
// always go through Runtime; provider validation is delegated to dashboardapp.
type Options struct {
	Runtime     *daemon.ViewRuntime
	SocketPath  string
	StaticDir   string
	AllowPublic bool
	AuthToken   string

	ConfigLoader             func() (config.Config, error)
	ConfigSaver              func(config.Config) error
	CredentialsLoader        func() (config.Credentials, error)
	DashboardService         *dashboardapp.Service
	Discover                 func() detect.Result
	ApplyCredentials         func(*detect.Result)
	ProviderLister           func() []core.UsageProvider
	IntegrationLister        func() []integrations.Status
	BrowserLister            func(context.Context) ([]string, error)
	ValidateAPIKey           func(accountID, providerID, apiKey string) (bool, string)
	ValidateAPIKeyForAccount func(core.AccountConfig, string) (bool, string)
	ConnectBrowserSession    func(accountID, domain, cookieName, browser string) (core.BrowserSessionInfo, error)
	DisconnectBrowserSession func(accountID string) error
	InstallIntegration       func(integrations.ID) ([]integrations.Status, error)
	UninstallIntegration     func(integrations.ID) error
	SaveIntegrationState     func(string, config.IntegrationState) error
	InstallDaemon            func() error

	Now     func() time.Time
	OnReady func(string)
}

type AppDTO struct {
	Version    string `json:"version"`
	CommitHash string `json:"commit_hash,omitempty"`
	BuildDate  string `json:"build_date,omitempty"`
}

type DaemonStateDTO struct {
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
	InstallHint string `json:"install_hint,omitempty"`
}

type TimeWindowDTO struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Days  int    `json:"days"`
}

type ProviderDTO struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Capabilities        []string `json:"capabilities,omitempty"`
	DocsURL             string   `json:"docs_url,omitempty"`
	Quickstart          []string `json:"quickstart,omitempty"`
	AuthType            string   `json:"auth_type"`
	AuthTypes           []string `json:"auth_types,omitempty"`
	APIKeyEnv           string   `json:"api_key_env,omitempty"`
	DefaultAccountID    string   `json:"default_account_id,omitempty"`
	BrowserCookieDomain string   `json:"browser_cookie_domain,omitempty"`
	BrowserCookieName   string   `json:"browser_cookie_name,omitempty"`
	BrowserConsoleURL   string   `json:"browser_console_url,omitempty"`
}

type CredentialStatusDTO struct {
	Present bool   `json:"present"`
	Kind    string `json:"kind,omitempty"`
	Source  string `json:"source,omitempty"`
	EnvVar  string `json:"env_var,omitempty"`
}

type BrowserSessionDTO struct {
	Configured    bool   `json:"configured"`
	Connected     bool   `json:"connected"`
	Domain        string `json:"domain,omitempty"`
	CookieName    string `json:"cookie_name,omitempty"`
	SourceBrowser string `json:"source_browser,omitempty"`
	CapturedAt    string `json:"captured_at,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	Expired       bool   `json:"expired"`
}

type AccountDTO struct {
	ID             string              `json:"id"`
	ProviderID     string              `json:"provider_id"`
	AuthType       string              `json:"auth_type"`
	Configured     bool                `json:"configured"`
	Discovered     bool                `json:"discovered"`
	Credential     CredentialStatusDTO `json:"credential"`
	BrowserSession BrowserSessionDTO   `json:"browser_session"`
}

type DashboardProviderDTO struct {
	AccountID string `json:"account_id"`
	Enabled   bool   `json:"enabled"`
	HideCosts *bool  `json:"hide_costs,omitempty"`
}

type WidgetSectionDTO struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

type UISettingsDTO struct {
	RefreshIntervalSeconds int     `json:"refresh_interval_seconds"`
	WarnThreshold          float64 `json:"warn_threshold"`
	CritThreshold          float64 `json:"crit_threshold"`
}

type DataSettingsDTO struct {
	TimeWindow    string `json:"time_window"`
	RetentionDays int    `json:"retention_days"`
}

type DashboardSettingsDTO struct {
	View                   string                 `json:"view"`
	HideSectionsWithNoData bool                   `json:"hide_sections_with_no_data"`
	HideCosts              *bool                  `json:"hide_costs,omitempty"`
	Providers              []DashboardProviderDTO `json:"providers,omitempty"`
	WidgetSections         []WidgetSectionDTO     `json:"widget_sections,omitempty"`
	DetailSections         []WidgetSectionDTO     `json:"detail_sections,omitempty"`
}

type TelemetrySettingsDTO struct {
	ProviderLinks map[string]string `json:"provider_links,omitempty"`
}

type SettingsDTO struct {
	Theme              string                        `json:"theme"`
	AutoDetect         bool                          `json:"auto_detect"`
	UI                 UISettingsDTO                 `json:"ui"`
	Data               DataSettingsDTO               `json:"data"`
	Dashboard          DashboardSettingsDTO          `json:"dashboard"`
	Telemetry          TelemetrySettingsDTO          `json:"telemetry"`
	ModelNormalization core.ModelNormalizationConfig `json:"model_normalization"`
}

type ThemeDTO struct {
	Name   string            `json:"name"`
	Icon   string            `json:"icon,omitempty"`
	Colors map[string]string `json:"colors,omitempty"`
}

type IntegrationDTO struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Installed        bool   `json:"installed"`
	Configured       bool   `json:"configured"`
	InstalledVersion string `json:"installed_version,omitempty"`
	DesiredVersion   string `json:"desired_version,omitempty"`
	NeedsUpgrade     bool   `json:"needs_upgrade"`
	State            string `json:"state"`
	Summary          string `json:"summary,omitempty"`
	Declined         bool   `json:"declined,omitempty"`
}

type SnapshotDTO struct {
	ProviderID  string                      `json:"provider_id"`
	AccountID   string                      `json:"account_id"`
	Timestamp   time.Time                   `json:"timestamp"`
	Status      core.Status                 `json:"status"`
	Metrics     map[string]core.Metric      `json:"metrics"`
	Resets      map[string]time.Time        `json:"resets,omitempty"`
	Attributes  map[string]string           `json:"attributes,omitempty"`
	Diagnostics map[string]string           `json:"diagnostics,omitempty"`
	ModelUsage  []core.ModelUsageRecord     `json:"model_usage,omitempty"`
	DailySeries map[string][]core.TimePoint `json:"daily_series,omitempty"`
	Message     string                      `json:"message,omitempty"`
	HideCosts   bool                        `json:"hide_costs"`
}

type BootstrapResponse struct {
	APIVersion   string           `json:"api_version"`
	ServedAt     time.Time        `json:"served_at"`
	RequestToken string           `json:"request_token"`
	App          AppDTO           `json:"app"`
	Daemon       DaemonStateDTO   `json:"daemon"`
	TimeWindows  []TimeWindowDTO  `json:"time_windows"`
	Providers    []ProviderDTO    `json:"providers"`
	Accounts     []AccountDTO     `json:"accounts"`
	Settings     SettingsDTO      `json:"settings"`
	Themes       []ThemeDTO       `json:"themes,omitempty"`
	Integrations []IntegrationDTO `json:"integrations"`
}

type SnapshotsResponse struct {
	Window    string                 `json:"window"`
	ServedAt  time.Time              `json:"served_at"`
	Snapshots map[string]SnapshotDTO `json:"snapshots"`
}

type DaemonHealthDTO struct {
	Status             string `json:"status"`
	Healthy            bool   `json:"healthy"`
	Version            string `json:"version,omitempty"`
	APIVersion         string `json:"api_version,omitempty"`
	IntegrationVersion string `json:"integration_version,omitempty"`
	ProviderRegistry   string `json:"provider_registry_hash,omitempty"`
}

type HealthResponse struct {
	Status   string          `json:"status"`
	ServedAt time.Time       `json:"served_at"`
	Daemon   DaemonHealthDTO `json:"daemon"`
}

type BrowsersResponse struct {
	Browsers []string  `json:"browsers"`
	ServedAt time.Time `json:"served_at"`
}

type IntegrationsResponse struct {
	Integrations []IntegrationDTO `json:"integrations"`
	ServedAt     time.Time        `json:"served_at"`
}

type CredentialResponse struct {
	AccountID  string              `json:"account_id"`
	ProviderID string              `json:"provider_id"`
	Credential CredentialStatusDTO `json:"credential"`
}

type BrowserSessionResponse struct {
	AccountID      string            `json:"account_id"`
	ProviderID     string            `json:"provider_id"`
	BrowserSession BrowserSessionDTO `json:"browser_session"`
}
