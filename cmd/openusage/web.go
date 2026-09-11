package main

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/daemon"
	"github.com/janekbaraniewski/openusage/internal/web"
	"github.com/spf13/cobra"
)

const (
	defaultWebListenAddr = "127.0.0.1:8787"
	defaultWebStaticDir  = ""
	webAccessTokenEnv    = "OPENUSAGE_WEB_TOKEN"
)

func newWebCommand() *cobra.Command {
	var (
		listenAddr  string
		staticDir   string
		noOpen      bool
		allowPublic bool
	)

	cmd := &cobra.Command{
		Use:   "web",
		Short: "Run the local web dashboard",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWeb(cmd, listenAddr, staticDir, noOpen, allowPublic)
		},
	}
	cmd.Flags().StringVar(&listenAddr, "listen", defaultWebListenAddr, "web listen address (loopback unless --allow-public)")
	cmd.Flags().StringVar(&staticDir, "static-dir", defaultWebStaticDir, "external directory containing the built web dashboard (default: embedded assets)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open the dashboard in a browser")
	cmd.Flags().BoolVar(&allowPublic, "allow-public", false, "allow non-loopback binding (requires OPENUSAGE_WEB_TOKEN)")
	return cmd
}

func runWeb(cmd *cobra.Command, listenAddr, staticDir string, noOpen, allowPublic bool) error {
	authToken := strings.TrimSpace(os.Getenv(webAccessTokenEnv))
	if err := web.ValidateListenAddrForMode(listenAddr, allowPublic, authToken); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	socketPath, err := daemon.ResolveSocketPathWithError()
	if err != nil {
		return fmt.Errorf("resolve telemetry socket: %w", err)
	}

	runtimeView := daemon.NewViewRuntime(nil, socketPath, core.DebugEnabled())
	runtimeView.SetTimeWindow(core.ParseTimeWindow(cfg.Data.TimeWindow))

	server, err := web.NewServer(web.Options{
		Runtime:     runtimeView,
		SocketPath:  socketPath,
		StaticDir:   strings.TrimSpace(staticDir),
		AllowPublic: allowPublic,
		AuthToken:   authToken,
		OnReady: func(dashboardURL string) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "OpenUsage web dashboard: %s\n", dashboardURL)
			if authToken != "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Web access token required; append ?access_token=$%s to the URL.\n", webAccessTokenEnv)
			}
			if noOpen {
				return
			}
			openURL := dashboardURL
			if authToken != "" {
				openURL = dashboardURLWithToken(dashboardURL, authToken)
			}
			if err := openWebURL(openURL, allowPublic); err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: open browser: %v\n", err)
			}
		},
	})
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return server.Serve(ctx, listenAddr)
}

func openWebURL(rawURL string, allowPublic bool) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Host == "" ||
		parsed.Path != "/app/" || parsed.Fragment != "" {
		return fmt.Errorf("refusing unsafe dashboard URL")
	}
	if !allowPublic && !isLoopbackWebHost(parsed.Hostname()) {
		return fmt.Errorf("refusing non-loopback dashboard URL")
	}
	if parsed.RawQuery != "" {
		query := parsed.Query()
		if len(query) != 1 || strings.TrimSpace(query.Get("access_token")) == "" {
			return fmt.Errorf("refusing unsafe dashboard query")
		}
	}

	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", rawURL).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	default:
		return exec.Command("xdg-open", rawURL).Start()
	}
}

func dashboardURLWithToken(rawURL, token string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	query.Set("access_token", token)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func isLoopbackWebHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}
