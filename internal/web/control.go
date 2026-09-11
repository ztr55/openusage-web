package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/integrations"
	"github.com/janekbaraniewski/openusage/internal/tui"
)

const maxControlBodyBytes int64 = 64 << 10

var errAccountProviderMismatch = errors.New("account provider mismatch")

type nullableBoolPatch struct {
	set   bool
	value *bool
}

func (p *nullableBoolPatch) UnmarshalJSON(data []byte) error {
	p.set = true
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		p.value = nil
		return nil
	}
	var value bool
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("expected boolean or null")
	}
	p.value = &value
	return nil
}

type boolPatch struct {
	set   bool
	value bool
}

func (p *boolPatch) UnmarshalJSON(data []byte) error {
	p.set = true
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		return errors.New("expected boolean")
	}
	if err := json.Unmarshal(data, &p.value); err != nil {
		return errors.New("expected boolean")
	}
	return nil
}

type stringPatch struct {
	set   bool
	value string
}

func (p *stringPatch) UnmarshalJSON(data []byte) error {
	p.set = true
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		return errors.New("expected string")
	}
	if err := json.Unmarshal(data, &p.value); err != nil {
		return errors.New("expected string")
	}
	return nil
}

type dashboardSettingsPatch struct {
	HideCosts              nullableBoolPatch `json:"hide_costs"`
	HideSectionsWithNoData boolPatch         `json:"hide_sections_with_no_data"`
	View                   stringPatch       `json:"view"`
}

type timeWindowPatch struct {
	Window string `json:"window"`
}

type themePatch struct {
	Theme string `json:"theme"`
}

type uiSettingsPatch struct {
	RefreshIntervalSeconds *int     `json:"refresh_interval_seconds"`
	WarnThreshold          *float64 `json:"warn_threshold"`
	CritThreshold          *float64 `json:"crit_threshold"`
	AutoDetect             *bool    `json:"auto_detect"`
}

type providerPreferencePatch struct {
	AccountID string            `json:"account_id"`
	Enabled   boolPatch         `json:"enabled"`
	HideCosts nullableBoolPatch `json:"hide_costs"`
}

type sectionPatch struct {
	ID      string    `json:"id"`
	Enabled boolPatch `json:"enabled"`
}

type credentialPatch struct {
	ProviderID string `json:"provider_id"`
	APIKey     string `json:"api_key"`
}

type browserSessionPatch struct {
	ProviderID string `json:"provider_id"`
	Browser    string `json:"browser"`
}

type telemetryLinkPatch struct {
	Source string    `json:"source"`
	Target string    `json:"target"`
	Delete boolPatch `json:"delete"`
}

func (s *Server) requireMutation(w http.ResponseWriter, r *http.Request) bool {
	if s.authorizeMutation(r) {
		return true
	}
	writeJSONError(w, http.StatusForbidden, "forbidden")
	return false
}

func decodeControlJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Body == nil {
		r.Body = http.NoBody
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxControlBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func writeControlDecodeError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	writeJSONError(w, http.StatusBadRequest, "invalid request body")
}

func (s *Server) handleDashboardSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireMutation(w, r) {
		return
	}
	var patch dashboardSettingsPatch
	if err := decodeControlJSON(w, r, &patch); err != nil {
		writeControlDecodeError(w, err)
		return
	}
	if !patch.HideCosts.set && !patch.HideSectionsWithNoData.set && !patch.View.set {
		writeJSONError(w, http.StatusBadRequest, "no dashboard settings supplied")
		return
	}
	if patch.View.set {
		patch.View.value = strings.ToLower(strings.TrimSpace(patch.View.value))
		if !validDashboardView(patch.View.value) {
			writeJSONError(w, http.StatusBadRequest, "invalid dashboard view")
			return
		}
	}

	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if patch.HideCosts.set {
		if err := config.SaveDashboardHideCosts(patch.HideCosts.value); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "dashboard settings save failed")
			return
		}
	}
	if patch.HideSectionsWithNoData.set {
		if err := config.SaveDashboardHideSectionsWithNoData(patch.HideSectionsWithNoData.value); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "dashboard settings save failed")
			return
		}
	}
	if patch.View.set {
		if err := config.SaveDashboardView(patch.View.value); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "dashboard settings save failed")
			return
		}
	}
	s.writeSettings(w)
}

func (s *Server) handleTimeWindowSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireMutation(w, r) {
		return
	}
	var patch timeWindowPatch
	if err := decodeControlJSON(w, r, &patch); err != nil {
		writeControlDecodeError(w, err)
		return
	}
	window, ok := parseValidTimeWindow(patch.Window)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid time window")
		return
	}

	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if err := config.SaveTimeWindow(string(window)); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "time window save failed")
		return
	}
	if s.runtime != nil {
		s.runtime.SetTimeWindow(window)
	}
	s.writeSettings(w)
}

func (s *Server) handleThemeSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireMutation(w, r) {
		return
	}
	var patch themePatch
	if err := decodeControlJSON(w, r, &patch); err != nil {
		writeControlDecodeError(w, err)
		return
	}
	themeName, ok := resolveThemeName(patch.Theme)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "unknown theme")
		return
	}

	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if err := config.SaveTheme(themeName); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "theme save failed")
		return
	}
	_ = tui.SetThemeByName(themeName)
	s.writeSettings(w)
}

func (s *Server) handleUISettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireMutation(w, r) {
		return
	}
	var patch uiSettingsPatch
	if err := decodeControlJSON(w, r, &patch); err != nil {
		writeControlDecodeError(w, err)
		return
	}
	if patch.RefreshIntervalSeconds == nil && patch.WarnThreshold == nil &&
		patch.CritThreshold == nil && patch.AutoDetect == nil {
		writeJSONError(w, http.StatusBadRequest, "no ui settings supplied")
		return
	}
	if patch.RefreshIntervalSeconds != nil && (*patch.RefreshIntervalSeconds <= 0 || *patch.RefreshIntervalSeconds > 3600) {
		writeJSONError(w, http.StatusBadRequest, "refresh interval must be between 1 and 3600 seconds")
		return
	}
	if patch.WarnThreshold != nil && (*patch.WarnThreshold <= 0 || *patch.WarnThreshold > 1) {
		writeJSONError(w, http.StatusBadRequest, "warn threshold must be greater than 0 and at most 1")
		return
	}
	if patch.CritThreshold != nil && (*patch.CritThreshold <= 0 || *patch.CritThreshold > 1) {
		writeJSONError(w, http.StatusBadRequest, "critical threshold must be greater than 0 and at most 1")
		return
	}

	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	cfg, err := s.configLoader()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "configuration unavailable")
		return
	}
	if patch.RefreshIntervalSeconds != nil {
		cfg.UI.RefreshIntervalSeconds = *patch.RefreshIntervalSeconds
	}
	if patch.WarnThreshold != nil {
		cfg.UI.WarnThreshold = *patch.WarnThreshold
	}
	if patch.CritThreshold != nil {
		cfg.UI.CritThreshold = *patch.CritThreshold
	}
	if patch.AutoDetect != nil {
		cfg.AutoDetect = *patch.AutoDetect
	}
	if err := s.configSaver(cfg); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "ui settings save failed")
		return
	}
	s.writeSettings(w)
}

func (s *Server) handleProviderSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireMutation(w, r) {
		return
	}
	var patch struct {
		Providers json.RawMessage `json:"providers"`
	}
	if err := decodeControlJSON(w, r, &patch); err != nil {
		writeControlDecodeError(w, err)
		return
	}
	providers, present, err := decodeProviderPreferences(patch.Providers)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid provider settings")
		return
	}
	if !present {
		writeJSONError(w, http.StatusBadRequest, "providers are required")
		return
	}

	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if err := config.SaveDashboardProviders(providers); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "provider settings save failed")
		return
	}
	s.writeSettings(w)
}

func (s *Server) handleSectionSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireMutation(w, r) {
		return
	}
	var patch struct {
		WidgetSections         json.RawMessage `json:"widget_sections"`
		DetailSections         json.RawMessage `json:"detail_sections"`
		HideSectionsWithNoData boolPatch       `json:"hide_sections_with_no_data"`
	}
	if err := decodeControlJSON(w, r, &patch); err != nil {
		writeControlDecodeError(w, err)
		return
	}
	widgetSections, widgetPresent, err := decodeWidgetSections(patch.WidgetSections)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid widget sections")
		return
	}
	detailSections, detailPresent, err := decodeDetailSections(patch.DetailSections)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid detail sections")
		return
	}
	if !widgetPresent && !detailPresent && !patch.HideSectionsWithNoData.set {
		writeJSONError(w, http.StatusBadRequest, "no section settings supplied")
		return
	}

	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if widgetPresent {
		if err := config.SaveDashboardWidgetSections(widgetSections); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "section settings save failed")
			return
		}
	}
	if detailPresent {
		if err := config.SaveDetailWidgetSections(detailSections); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "section settings save failed")
			return
		}
	}
	if patch.HideSectionsWithNoData.set {
		if err := config.SaveDashboardHideSectionsWithNoData(patch.HideSectionsWithNoData.value); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "section settings save failed")
			return
		}
	}
	s.writeSettings(w)
}

func (s *Server) handleTelemetryLinkSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireMutation(w, r) {
		return
	}
	var patch telemetryLinkPatch
	if err := decodeControlJSON(w, r, &patch); err != nil {
		writeControlDecodeError(w, err)
		return
	}
	patch.Source = strings.TrimSpace(patch.Source)
	patch.Target = strings.TrimSpace(patch.Target)
	if patch.Source == "" {
		writeJSONError(w, http.StatusBadRequest, "source is required")
		return
	}
	if !patch.Delete.value && patch.Target == "" {
		writeJSONError(w, http.StatusBadRequest, "target is required")
		return
	}

	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	var err error
	if patch.Delete.value {
		err = config.DeleteProviderLink(patch.Source)
	} else {
		err = config.SaveProviderLink(patch.Source, patch.Target)
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "telemetry link save failed")
		return
	}
	s.writeSettings(w)
}

func (s *Server) writeSettings(w http.ResponseWriter) {
	cfg, err := s.configLoader()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "configuration unavailable")
		return
	}
	writeJSON(w, http.StatusOK, settingsDTO(cfg))
}

func validDashboardView(view string) bool {
	switch view {
	case config.DashboardViewGrid,
		config.DashboardViewStacked,
		config.DashboardViewList,
		config.DashboardViewTabs,
		config.DashboardViewSplit,
		config.DashboardViewCompare:
		return true
	default:
		return false
	}
}

func parseValidTimeWindow(value string) (core.TimeWindow, bool) {
	value = strings.TrimSpace(value)
	for _, window := range core.ValidTimeWindows {
		if string(window) == value {
			return window, true
		}
	}
	return "", false
}

func resolveThemeName(value string) (string, bool) {
	_ = tui.LoadThemes(config.ConfigDir())
	value = strings.TrimSpace(value)
	for _, theme := range tui.AvailableThemes() {
		if theme.Name == value || strings.EqualFold(theme.Name, value) {
			return theme.Name, theme.Name != ""
		}
	}
	return "", false
}

func decodeProviderPreferences(raw json.RawMessage) ([]config.DashboardProviderConfig, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, true, nil
	}
	var entries []providerPreferencePatch
	if err := decodeRawJSON(raw, &entries); err != nil {
		return nil, true, err
	}
	out := make([]config.DashboardProviderConfig, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		accountID := strings.TrimSpace(entry.AccountID)
		if accountID == "" || !entry.Enabled.set {
			return nil, true, errors.New("provider account and enabled are required")
		}
		if _, ok := seen[accountID]; ok {
			return nil, true, errors.New("duplicate provider account")
		}
		seen[accountID] = struct{}{}
		out = append(out, config.DashboardProviderConfig{
			AccountID: accountID,
			Enabled:   entry.Enabled.value,
			HideCosts: cloneBool(entry.HideCosts.value),
		})
	}
	return out, true, nil
}

func decodeWidgetSections(raw json.RawMessage) ([]config.DashboardWidgetSection, bool, error) {
	var entries []sectionPatch
	present, err := decodeSectionPatches(raw, &entries)
	if err != nil || !present {
		return nil, present, err
	}
	out := make([]config.DashboardWidgetSection, 0, len(entries))
	seen := make(map[core.DashboardStandardSection]struct{}, len(entries))
	for _, entry := range entries {
		id := core.DashboardStandardSection(strings.ToLower(strings.TrimSpace(entry.ID)))
		id = core.NormalizeDashboardStandardSection(id)
		if id == core.DashboardSectionHeader || !core.IsKnownDashboardStandardSection(id) {
			return nil, true, errors.New("unknown widget section")
		}
		if _, ok := seen[id]; ok {
			return nil, true, errors.New("duplicate widget section")
		}
		seen[id] = struct{}{}
		enabled := true
		if entry.Enabled.set {
			enabled = entry.Enabled.value
		}
		out = append(out, config.DashboardWidgetSection{ID: id, Enabled: enabled})
	}
	return out, true, nil
}

func decodeDetailSections(raw json.RawMessage) ([]config.DetailWidgetSection, bool, error) {
	var entries []sectionPatch
	present, err := decodeSectionPatches(raw, &entries)
	if err != nil || !present {
		return nil, present, err
	}
	out := make([]config.DetailWidgetSection, 0, len(entries))
	seen := make(map[core.DetailStandardSection]struct{}, len(entries))
	for _, entry := range entries {
		id := core.DetailStandardSection(strings.ToLower(strings.TrimSpace(entry.ID)))
		if !core.IsKnownDetailStandardSection(id) {
			return nil, true, errors.New("unknown detail section")
		}
		if _, ok := seen[id]; ok {
			return nil, true, errors.New("duplicate detail section")
		}
		seen[id] = struct{}{}
		enabled := true
		if entry.Enabled.set {
			enabled = entry.Enabled.value
		}
		out = append(out, config.DetailWidgetSection{ID: id, Enabled: enabled})
	}
	return out, true, nil
}

func decodeSectionPatches(raw json.RawMessage, dst *[]sectionPatch) (bool, error) {
	if len(raw) == 0 {
		return false, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		*dst = nil
		return true, nil
	}
	if err := decodeRawJSON(raw, dst); err != nil {
		return true, err
	}
	return true, nil
}

func decodeRawJSON(raw []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}

func (s *Server) serveAccountAPI(w http.ResponseWriter, r *http.Request) {
	if accountID, ok := resourceID(r.URL.Path, "/api/v1/accounts/", "/credential"); ok {
		switch r.Method {
		case http.MethodPut:
			s.handleCredential(w, r, accountID)
		case http.MethodDelete:
			s.handleCredentialDelete(w, r, accountID)
		default:
			allowMethod(w, r, http.MethodPut)
		}
		return
	}
	if accountID, ok := resourceID(r.URL.Path, "/api/v1/accounts/", "/browser-session"); ok {
		switch r.Method {
		case http.MethodPost:
			s.handleBrowserSession(w, r, accountID)
		case http.MethodDelete:
			s.handleBrowserSessionDelete(w, r, accountID)
		default:
			allowMethod(w, r, http.MethodPost)
		}
		return
	}
	writeJSONError(w, http.StatusNotFound, "not found")
}

func resourceID(requestPath, prefix, suffix string) (string, bool) {
	if !strings.HasPrefix(requestPath, prefix) || !strings.HasSuffix(requestPath, suffix) {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(requestPath, prefix), suffix)
	id = strings.TrimSpace(id)
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
		return "", false
	}
	return id, true
}

func (s *Server) handleCredential(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireMutation(w, r) {
		return
	}
	var patch credentialPatch
	if err := decodeControlJSON(w, r, &patch); err != nil {
		writeControlDecodeError(w, err)
		return
	}
	apiKey := strings.TrimSpace(patch.APIKey)
	providerID, spec, ok := s.providerSpec(patch.ProviderID)
	if !ok || !spec.Auth.SupportsAuth(core.ProviderAuthTypeAPIKey) || apiKey == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid credential request")
		return
	}
	if err := s.validateExistingAccountProvider(accountID, providerID); err != nil {
		if errors.Is(err, errAccountProviderMismatch) {
			writeJSONError(w, http.StatusBadRequest, "account provider mismatch")
		} else {
			writeJSONError(w, http.StatusInternalServerError, "configuration unavailable")
		}
		return
	}
	if s.validateAPIKey == nil && s.validateAPIKeyForAccount == nil {
		writeJSONError(w, http.StatusInternalServerError, "credential validation unavailable")
		return
	}
	account, accountErr := s.accountForValidation(accountID, providerID, spec)
	if accountErr != nil {
		writeJSONError(w, http.StatusInternalServerError, "configuration unavailable")
		return
	}
	valid := false
	if s.validateAPIKeyForAccount != nil {
		valid, _ = s.validateAPIKeyForAccount(account, apiKey)
	} else {
		valid, _ = s.validateAPIKey(accountID, providerID, apiKey)
	}
	if !valid {
		writeJSONError(w, http.StatusBadRequest, "credential rejected")
		return
	}

	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if err := s.persistAccountConfig(accountID, providerID, string(core.ProviderAuthTypeAPIKey), strings.TrimSpace(spec.Auth.APIKeyEnv), nil); err != nil {
		if errors.Is(err, errAccountProviderMismatch) {
			writeJSONError(w, http.StatusBadRequest, "account provider mismatch")
		} else {
			writeJSONError(w, http.StatusInternalServerError, "account save failed")
		}
		return
	}
	if err := s.dashboardService.SaveCredential(accountID, apiKey); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "credential save failed")
		return
	}
	writeJSON(w, http.StatusOK, CredentialResponse{
		AccountID:  accountID,
		ProviderID: providerID,
		Credential: CredentialStatusDTO{
			Present: true,
			Kind:    string(core.ProviderAuthTypeAPIKey),
			Source:  "stored",
			EnvVar:  strings.TrimSpace(spec.Auth.APIKeyEnv),
		},
	})
}

func (s *Server) handleCredentialDelete(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireMutation(w, r) {
		return
	}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if err := s.dashboardService.DeleteCredential(accountID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "credential delete failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"account_id": accountID,
		"status":     "deleted",
	})
}

func (s *Server) handleBrowserSession(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireMutation(w, r) {
		return
	}
	var patch browserSessionPatch
	if err := decodeControlJSON(w, r, &patch); err != nil {
		writeControlDecodeError(w, err)
		return
	}
	providerID, spec, ok := s.providerSpec(patch.ProviderID)
	domain := strings.TrimSpace(spec.Auth.BrowserCookieDomain)
	cookieName := strings.TrimSpace(spec.Auth.BrowserCookieName)
	if !ok || !spec.Auth.SupportsAuth(core.ProviderAuthTypeBrowserSession) || domain == "" || cookieName == "" {
		writeJSONError(w, http.StatusBadRequest, "provider has no browser session configuration")
		return
	}
	browser := strings.ToLower(strings.TrimSpace(patch.Browser))
	if !supportedBrowser(browser) {
		writeJSONError(w, http.StatusBadRequest, "unsupported browser")
		return
	}
	if err := s.validateExistingAccountProvider(accountID, providerID); err != nil {
		if errors.Is(err, errAccountProviderMismatch) {
			writeJSONError(w, http.StatusBadRequest, "account provider mismatch")
		} else {
			writeJSONError(w, http.StatusInternalServerError, "configuration unavailable")
		}
		return
	}
	if s.connectBrowserSession == nil {
		writeJSONError(w, http.StatusInternalServerError, "browser session unavailable")
		return
	}

	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	info, err := s.connectBrowserSession(accountID, domain, cookieName, browser)
	if err != nil || !info.Connected {
		writeJSONError(w, http.StatusBadRequest, "browser session connection failed")
		return
	}
	sourceBrowser := strings.TrimSpace(info.SourceBrowser)
	if sourceBrowser == "" {
		sourceBrowser = browser
	}
	if err := s.persistAccountConfig(accountID, providerID, string(core.ProviderAuthTypeBrowserSession), "", &core.BrowserCookieRef{
		Domain:        domain,
		CookieName:    cookieName,
		SourceBrowser: sourceBrowser,
	}); err != nil {
		if errors.Is(err, errAccountProviderMismatch) {
			writeJSONError(w, http.StatusBadRequest, "account provider mismatch")
		} else {
			writeJSONError(w, http.StatusInternalServerError, "account save failed")
		}
		return
	}
	writeJSON(w, http.StatusOK, BrowserSessionResponse{
		AccountID:      accountID,
		ProviderID:     providerID,
		BrowserSession: browserSessionInfoDTO(info, s.nowUTC()),
	})
}

func (s *Server) handleBrowserSessionDelete(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireMutation(w, r) {
		return
	}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if s.disconnectBrowserSession == nil || s.disconnectBrowserSession(accountID) != nil {
		writeJSONError(w, http.StatusInternalServerError, "browser session delete failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"account_id": accountID,
		"status":     "deleted",
	})
}

func browserSessionInfoDTO(info core.BrowserSessionInfo, now time.Time) BrowserSessionDTO {
	result := BrowserSessionDTO{
		Configured:    true,
		Connected:     info.Connected,
		Domain:        strings.TrimSpace(info.Domain),
		CookieName:    strings.TrimSpace(info.CookieName),
		SourceBrowser: strings.TrimSpace(info.SourceBrowser),
		CapturedAt:    strings.TrimSpace(info.CapturedAt),
		ExpiresAt:     strings.TrimSpace(info.ExpiresAt),
		Expired:       info.Expired,
	}
	if !result.Expired && result.ExpiresAt != "" {
		if expires, err := time.Parse(time.RFC3339, result.ExpiresAt); err == nil {
			result.Expired = now.After(expires)
		}
	}
	return result
}

func supportedBrowser(browser string) bool {
	switch browser {
	case "", "chrome", "chromium", "firefox", "safari", "edge", "brave", "vivaldi", "opera":
		return true
	default:
		return false
	}
}

func (s *Server) providerSpec(providerID string) (string, core.ProviderSpec, bool) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return "", core.ProviderSpec{}, false
	}
	for _, provider := range s.providerLister() {
		if provider == nil {
			continue
		}
		id := strings.TrimSpace(provider.ID())
		spec := provider.Spec()
		if id == "" {
			id = strings.TrimSpace(spec.ID)
		}
		if strings.EqualFold(id, providerID) {
			return id, spec, true
		}
	}
	return "", core.ProviderSpec{}, false
}

func (s *Server) validateExistingAccountProvider(accountID, providerID string) error {
	cfg, err := s.configLoader()
	if err != nil {
		return err
	}
	for _, account := range append(append([]core.AccountConfig(nil), cfg.Accounts...), cfg.AutoDetectedAccounts...) {
		if strings.TrimSpace(account.ID) != accountID {
			continue
		}
		if existing := strings.TrimSpace(account.Provider); existing != "" && !strings.EqualFold(existing, providerID) {
			return errAccountProviderMismatch
		}
	}
	return nil
}

func (s *Server) accountForValidation(accountID, providerID string, spec core.ProviderSpec) (core.AccountConfig, error) {
	cfg, err := s.configLoader()
	if err != nil {
		return core.AccountConfig{}, err
	}
	for _, account := range append(append([]core.AccountConfig(nil), cfg.Accounts...), cfg.AutoDetectedAccounts...) {
		if strings.TrimSpace(account.ID) != accountID {
			continue
		}
		if existing := strings.TrimSpace(account.Provider); existing != "" && !strings.EqualFold(existing, providerID) {
			return core.AccountConfig{}, errAccountProviderMismatch
		}
		account.ID = accountID
		account.Provider = providerID
		return account, nil
	}
	return core.AccountConfig{
		ID:         accountID,
		Provider:   providerID,
		Auth:       string(core.ProviderAuthTypeAPIKey),
		APIKeyEnv:  strings.TrimSpace(spec.Auth.APIKeyEnv),
		ProbeModel: "",
	}, nil
}

func (s *Server) persistAccountConfig(accountID, providerID, auth, apiKeyEnv string, browserCookie *core.BrowserCookieRef) error {
	cfg, err := s.configLoader()
	if err != nil {
		return err
	}
	accountID = strings.TrimSpace(accountID)
	providerID = strings.TrimSpace(providerID)
	for i := range cfg.Accounts {
		if strings.TrimSpace(cfg.Accounts[i].ID) != accountID {
			continue
		}
		if existing := strings.TrimSpace(cfg.Accounts[i].Provider); existing != "" && !strings.EqualFold(existing, providerID) {
			return errAccountProviderMismatch
		}
		cfg.Accounts[i].ID = accountID
		cfg.Accounts[i].Provider = providerID
		cfg.Accounts[i].Auth = auth
		if apiKeyEnv != "" {
			cfg.Accounts[i].APIKeyEnv = apiKeyEnv
		}
		if browserCookie != nil {
			cookie := *browserCookie
			cfg.Accounts[i].BrowserCookie = &cookie
		}
		return s.configSaver(cfg)
	}

	for _, account := range cfg.AutoDetectedAccounts {
		if strings.TrimSpace(account.ID) != accountID {
			continue
		}
		if existing := strings.TrimSpace(account.Provider); existing != "" && !strings.EqualFold(existing, providerID) {
			return errAccountProviderMismatch
		}
	}
	account := core.AccountConfig{
		ID:        accountID,
		Provider:  providerID,
		Auth:      auth,
		APIKeyEnv: apiKeyEnv,
	}
	if browserCookie != nil {
		cookie := *browserCookie
		account.BrowserCookie = &cookie
	}
	cfg.Accounts = append(cfg.Accounts, account)
	return s.configSaver(cfg)
}

func (s *Server) serveIntegrationAPI(w http.ResponseWriter, r *http.Request) {
	if id, ok := resourceID(r.URL.Path, "/api/v1/integrations/", "/install"); ok {
		if !allowMethod(w, r, http.MethodPost) {
			return
		}
		s.handleIntegrationMutation(w, r, integrations.ID(id), true)
		return
	}
	if id, ok := resourceID(r.URL.Path, "/api/v1/integrations/", "/uninstall"); ok {
		if !allowMethod(w, r, http.MethodPost) {
			return
		}
		s.handleIntegrationMutation(w, r, integrations.ID(id), false)
		return
	}
	writeJSONError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleIntegrationMutation(w http.ResponseWriter, r *http.Request, id integrations.ID, install bool) {
	if !s.requireMutation(w, r) {
		return
	}
	if _, ok := integrations.DefinitionByID(id); !ok {
		writeJSONError(w, http.StatusBadRequest, "unknown integration")
		return
	}

	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	var err error
	if install {
		if s.installIntegration == nil {
			err = errors.New("integration installer unavailable")
		} else {
			_, err = s.installIntegration(id)
		}
	} else if s.uninstallIntegration == nil {
		err = errors.New("integration uninstaller unavailable")
	} else {
		err = s.uninstallIntegration(id)
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "integration operation failed")
		return
	}
	state := config.IntegrationState{}
	if install {
		state = config.IntegrationState{Installed: true, Version: integrations.IntegrationVersion}
	}
	if s.saveIntegrationState == nil || s.saveIntegrationState(string(id), state) != nil {
		writeJSONError(w, http.StatusInternalServerError, "integration state save failed")
		return
	}
	cfg, _ := s.configLoader()
	writeJSON(w, http.StatusOK, IntegrationsResponse{
		Integrations: s.integrationDTOs(cfg),
		ServedAt:     s.nowUTC(),
	})
}
