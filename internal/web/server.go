package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/auth"
	"github.com/janekbaraniewski/openusage/internal/browsercookies"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
	"github.com/janekbaraniewski/openusage/internal/dashboardapp"
	"github.com/janekbaraniewski/openusage/internal/detect"
	"github.com/janekbaraniewski/openusage/internal/integrations"
	"github.com/janekbaraniewski/openusage/internal/providers"
)

type Server struct {
	runtime       *daemon.ViewRuntime
	socketPath    string
	staticDir     string
	allowPublic   bool
	authToken     string
	dashboardPath string

	configLoader             func() (config.Config, error)
	configSaver              func(config.Config) error
	configUpdater            func(func(*config.Config) error) error
	credentialsLoader        func() (config.Credentials, error)
	discover                 func() detect.Result
	applyCredentials         func(*detect.Result)
	providerLister           func() []core.UsageProvider
	integrationLister        func() []integrations.Status
	browserLister            func(context.Context) ([]string, error)
	dashboardService         *dashboardapp.Service
	validateAPIKey           func(accountID, providerID, apiKey string) (bool, string)
	validateAPIKeyForAccount func(core.AccountConfig, string) (bool, string)
	connectBrowserSession    func(accountID, domain, cookieName, browser string) (core.BrowserSessionInfo, error)
	disconnectBrowserSession func(accountID string) error
	installIntegration       func(integrations.ID) ([]integrations.Status, error)
	uninstallIntegration     func(integrations.ID) error
	saveIntegrationState     func(string, config.IntegrationState) error
	installDaemon            func() error
	oauthClient              *auth.OAuthClient
	saveOAuthCredential      func(string, core.OAuthCredential) error
	deleteOAuthCredential    func(string) error
	now                      func() time.Time
	onReady                  func(string)

	requestToken string
	controlMu    sync.Mutex
	oauthMu      sync.Mutex
	oauthFlows   map[string]*oauthFlow
}

func NewServer(options Options) (*Server, error) {
	token, err := newRequestToken()
	if err != nil {
		return nil, fmt.Errorf("create web request token: %w", err)
	}

	staticDir := strings.TrimSpace(options.StaticDir)
	if staticDir != "" {
		staticDir, err = filepath.Abs(staticDir)
		if err != nil {
			return nil, fmt.Errorf("resolve web static dir: %w", err)
		}
		if info, statErr := os.Stat(staticDir); statErr == nil && !info.IsDir() {
			return nil, fmt.Errorf("web static dir is not a directory: %s", staticDir)
		}
	}
	dashboardPath, err := NormalizeDashboardPath(options.DashboardPath)
	if err != nil {
		return nil, err
	}

	s := &Server{
		runtime:                  options.Runtime,
		socketPath:               strings.TrimSpace(options.SocketPath),
		staticDir:                staticDir,
		allowPublic:              options.AllowPublic,
		authToken:                strings.TrimSpace(options.AuthToken),
		dashboardPath:            dashboardPath,
		configLoader:             options.ConfigLoader,
		configSaver:              options.ConfigSaver,
		configUpdater:            options.ConfigUpdater,
		credentialsLoader:        options.CredentialsLoader,
		discover:                 options.Discover,
		applyCredentials:         options.ApplyCredentials,
		providerLister:           options.ProviderLister,
		integrationLister:        options.IntegrationLister,
		browserLister:            options.BrowserLister,
		dashboardService:         options.DashboardService,
		validateAPIKey:           options.ValidateAPIKey,
		validateAPIKeyForAccount: options.ValidateAPIKeyForAccount,
		connectBrowserSession:    options.ConnectBrowserSession,
		disconnectBrowserSession: options.DisconnectBrowserSession,
		installIntegration:       options.InstallIntegration,
		uninstallIntegration:     options.UninstallIntegration,
		saveIntegrationState:     options.SaveIntegrationState,
		installDaemon:            options.InstallDaemon,
		oauthClient:              options.OAuthClient,
		saveOAuthCredential:      options.SaveOAuthCredential,
		deleteOAuthCredential:    options.DeleteOAuthCredential,
		now:                      options.Now,
		onReady:                  options.OnReady,
		requestToken:             token,
		oauthFlows:               make(map[string]*oauthFlow),
	}

	if s.configLoader == nil {
		s.configLoader = config.Load
	}
	if s.configSaver == nil {
		s.configSaver = config.Save
	}
	if s.configUpdater == nil && options.ConfigLoader == nil && options.ConfigSaver == nil {
		s.configUpdater = config.Update
	}
	if s.credentialsLoader == nil {
		s.credentialsLoader = config.LoadCredentials
	}
	if s.dashboardService == nil {
		s.dashboardService = dashboardapp.NewService(context.Background())
	}
	if s.discover == nil {
		s.discover = detect.AutoDetect
	}
	if s.applyCredentials == nil {
		s.applyCredentials = detect.ApplyCredentials
	}
	if s.providerLister == nil {
		s.providerLister = providers.AllProviders
	}
	if s.integrationLister == nil {
		s.integrationLister = func() []integrations.Status {
			return integrations.NewDefaultManager().ListStatuses()
		}
	}
	if s.browserLister == nil {
		s.browserLister = func(ctx context.Context) ([]string, error) {
			return browsercookies.New().AvailableBrowsers(ctx)
		}
	}
	if options.ValidateAPIKey != nil {
		s.validateAPIKey = options.ValidateAPIKey
	} else {
		s.validateAPIKey = s.dashboardService.ValidateAPIKey
	}
	if options.ValidateAPIKeyForAccount != nil {
		s.validateAPIKeyForAccount = options.ValidateAPIKeyForAccount
	} else if options.ValidateAPIKey == nil {
		s.validateAPIKeyForAccount = s.dashboardService.ValidateAPIKeyForAccount
	}
	if options.ConnectBrowserSession != nil {
		s.connectBrowserSession = options.ConnectBrowserSession
	} else {
		s.connectBrowserSession = s.dashboardService.ConnectBrowserSession
	}
	if options.DisconnectBrowserSession != nil {
		s.disconnectBrowserSession = options.DisconnectBrowserSession
	} else {
		s.disconnectBrowserSession = s.dashboardService.DisconnectBrowserSession
	}
	if options.InstallIntegration != nil {
		s.installIntegration = options.InstallIntegration
	} else {
		s.installIntegration = s.dashboardService.InstallIntegration
	}
	if options.UninstallIntegration != nil {
		s.uninstallIntegration = options.UninstallIntegration
	} else {
		s.uninstallIntegration = func(id integrations.ID) error {
			definition, ok := integrations.DefinitionByID(id)
			if !ok {
				return errors.New("unknown integration")
			}
			return integrations.Uninstall(definition, integrations.NewDefaultDirs())
		}
	}
	if s.saveIntegrationState == nil {
		s.saveIntegrationState = config.SaveIntegrationState
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.oauthClient == nil {
		s.oauthClient = auth.NewOAuthClient(nil)
	}
	if s.saveOAuthCredential == nil {
		s.saveOAuthCredential = config.SaveOAuthCredential
	}
	if s.deleteOAuthCredential == nil {
		s.deleteOAuthCredential = config.DeleteOAuthCredential
	}
	if s.installDaemon == nil {
		s.installDaemon = func() error {
			socketPath := s.socketPath
			if socketPath == "" && s.runtime != nil {
				if client := s.runtime.CurrentClient(); client != nil {
					socketPath = client.SocketPath
				}
			}
			if socketPath == "" {
				return errors.New("daemon socket path is not configured")
			}
			return daemon.InstallService(socketPath)
		}
	}

	return s, nil
}

func (s *Server) Handler() http.Handler {
	if s == nil {
		return http.NotFoundHandler()
	}
	return http.HandlerFunc(s.serveHTTP)
}

func (s *Server) RequestToken() string {
	if s == nil {
		return ""
	}
	return s.requestToken
}

// Serve runs the web server until ctx is canceled. Non-loopback addresses are
// accepted only when the caller explicitly enables public mode and supplies an
// access token.
func (s *Server) Serve(ctx context.Context, listenAddr string) error {
	if s == nil {
		return errors.New("web server is nil")
	}
	if err := validateListenAddr(listenAddr, s.allowPublic, s.authToken); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	listener, err := net.Listen("tcp", strings.TrimSpace(listenAddr))
	if err != nil {
		return fmt.Errorf("listen web server: %w", err)
	}

	httpServer := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	shutdownDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(shutdownCtx)
		case <-shutdownDone:
		}
	}()

	if s.onReady != nil {
		s.onReady(serverURLWithPath(listener.Addr(), s.dashboardPath))
	}

	serveErr := httpServer.Serve(listener)
	close(shutdownDone)
	if errors.Is(serveErr, http.ErrServerClosed) || ctx.Err() != nil {
		return nil
	}
	return serveErr
}

func newRequestToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func serverURL(addr net.Addr) string {
	return serverURLWithPath(addr, "/app/")
}

func serverURLWithPath(addr net.Addr, dashboardPath string) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "http://" + addr.String() + dashboardPath
	}
	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, port),
		Path:   dashboardPath,
	}).String()
}

// NormalizeDashboardPath returns the trailing-slash URL path used by the web
// server to open the dashboard. The two supported surfaces are the native
// /app/ route and the root path used by the dashboard-only container build.
func NormalizeDashboardPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/app/", nil
	}
	if value != "/" && value != "/app" && value != "/app/" {
		return "", fmt.Errorf("web dashboard path must be / or /app/, got %q", value)
	}
	if value == "/" {
		return value, nil
	}
	return "/app/", nil
}

func ValidateListenAddr(listenAddr string) error {
	return validateListenAddr(listenAddr, false, "")
}

// ValidateListenAddrForMode validates a listen address with the same public
// binding rules used by Serve.
func ValidateListenAddrForMode(listenAddr string, allowPublic bool, authToken string) error {
	return validateListenAddr(listenAddr, allowPublic, authToken)
}

func validateListenAddr(listenAddr string, allowPublic bool, authToken string) error {
	listenAddr = strings.TrimSpace(listenAddr)
	host, _, err := net.SplitHostPort(listenAddr)
	if err != nil || strings.TrimSpace(host) == "" {
		return fmt.Errorf("web listen address must include a host and port, got %q", listenAddr)
	}
	if !isLoopbackHost(host) {
		if !allowPublic {
			return fmt.Errorf("web server must listen on loopback; refusing %q", listenAddr)
		}
		if strings.TrimSpace(authToken) == "" {
			return fmt.Errorf("public web binding requires OPENUSAGE_WEB_TOKEN")
		}
	}
	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handleHealth(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v1/") || r.URL.Path == "/api/v1" {
		if !s.authorizeAPIRequest(w, r) {
			return
		}
		s.serveAPI(w, r)
		return
	}
	s.serveStatic(w, r)
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !safeStaticRequestPath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	if s.staticDir == "" {
		s.serveEmbeddedStatic(w, r)
		return
	}

	root := filepath.Clean(s.staticDir)
	relative := strings.TrimPrefix(r.URL.Path, "/")
	if relative == "" {
		relative = "index.html"
	}
	candidate := filepath.Join(root, filepath.FromSlash(path.Clean(relative)))
	if !withinPath(root, candidate) {
		http.NotFound(w, r)
		return
	}

	if info, err := os.Stat(candidate); err == nil {
		if !s.safeStaticFile(candidate) {
			http.NotFound(w, r)
			return
		}
		if !info.IsDir() {
			http.ServeFile(w, r, candidate)
			return
		}
		nestedIndex := filepath.Join(candidate, "index.html")
		if nestedInfo, nestedErr := os.Stat(nestedIndex); nestedErr == nil && !nestedInfo.IsDir() && s.safeStaticFile(nestedIndex) {
			http.ServeFile(w, r, nestedIndex)
			return
		}
	} else if err != nil && !os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	}

	indexPath := filepath.Join(root, "index.html")
	if !withinPath(root, indexPath) || !s.safeStaticFile(indexPath) {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, indexPath)
}

func safeStaticRequestPath(requestPath string) bool {
	if requestPath == "" || strings.ContainsRune(requestPath, '\x00') || strings.Contains(requestPath, "\\") {
		return false
	}
	for _, segment := range strings.Split(requestPath, "/") {
		if segment == ".." {
			return false
		}
	}
	return true
}

func (s *Server) safeStaticFile(filePath string) bool {
	root, err := filepath.EvalSymlinks(s.staticDir)
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		return false
	}
	return withinPath(root, resolved)
}

func withinPath(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil || relative == ".." {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (s *Server) authorizeMutation(r *http.Request) bool {
	if s == nil || r == nil {
		return false
	}
	token := r.Header.Get(RequestTokenHeader)
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.requestToken)) != 1 {
		return false
	}

	host, hostPort, ok := requestHostPort(r.Host)
	if !ok || (!s.allowPublic && !isLoopbackHost(host)) {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" || strings.EqualFold(origin, "null") {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	originHost, originPort, ok := requestHostPort(parsed.Host)
	if !ok || (!s.allowPublic && !isLoopbackHost(originHost)) || !strings.EqualFold(host, originHost) {
		return false
	}
	if hostPort == "" {
		hostPort = "80"
	}
	if originPort == "" {
		originPort = "80"
	}
	return hostPort == originPort
}

const webAccessTokenCookie = "openusage_web_access_token"

// authorizeAPIRequest protects the API when public mode is explicitly enabled.
// The bootstrap URL may carry the token once; it is then stored in a strict,
// HttpOnly cookie so reloads do not need to keep the token in the address bar.
func (s *Server) authorizeAPIRequest(w http.ResponseWriter, r *http.Request) bool {
	if s == nil || s.authToken == "" {
		return true
	}

	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	provided := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	fromHeader := provided != ""
	fromQuery := false
	if provided == "" {
		if cookie, err := r.Cookie(webAccessTokenCookie); err == nil {
			provided = strings.TrimSpace(cookie.Value)
		}
	}
	if provided == "" && r.URL.Path == "/api/v1/bootstrap" {
		provided = strings.TrimSpace(r.URL.Query().Get("access_token"))
		fromQuery = provided != ""
	}
	if subtle.ConstantTimeCompare([]byte(provided), []byte(s.authToken)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="openusage-web"`)
		writeJSONError(w, http.StatusUnauthorized, "web access token required")
		return false
	}
	if fromHeader || fromQuery {
		s.setAccessTokenCookie(w, r)
	}
	return true
}

func (s *Server) setAccessTokenCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     webAccessTokenCookie,
		Value:    s.authToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https"),
		SameSite: http.SameSiteStrictMode,
	})
}

func requestHostPort(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n") {
		return "", "", false
	}
	if host, port, err := net.SplitHostPort(raw); err == nil {
		if host == "" || !validPort(port) {
			return "", "", false
		}
		return strings.Trim(host, "[]"), port, true
	}
	trimmed := strings.Trim(raw, "[]")
	if net.ParseIP(trimmed) != nil || !strings.Contains(raw, ":") {
		return trimmed, "", true
	}
	return "", "", false
}

func validPort(port string) bool {
	value, err := strconv.Atoi(port)
	return err == nil && value > 0 && value <= 65535
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
