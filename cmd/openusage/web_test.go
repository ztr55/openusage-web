package main

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/web"
)

func TestNewWebCommandFlags(t *testing.T) {
	cmd := newWebCommand()
	if cmd.Use != "web" {
		t.Fatalf("Use = %q, want web", cmd.Use)
	}

	for _, name := range []string{"listen", "static-dir", "dashboard-path", "no-open"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("web command missing %q flag", name)
		}
	}
	listen, err := cmd.Flags().GetString("listen")
	if err != nil || listen != defaultWebListenAddr {
		t.Fatalf("listen default = %q, want %q", listen, defaultWebListenAddr)
	}
	dashboardPath, err := cmd.Flags().GetString("dashboard-path")
	if err != nil || dashboardPath != defaultWebDashboardPath {
		t.Fatalf("dashboard path default = %q, want %q", dashboardPath, defaultWebDashboardPath)
	}
}

func TestOpenWebURLRejectsUnsafeURLs(t *testing.T) {
	for _, rawURL := range []string{
		"https://127.0.0.1:8787/app/",
		"http://example.com:8787/app/",
		"http://127.0.0.1:8787/other",
		"http://127.0.0.1:8787/app/?open=1",
		"http://127.0.0.1:8787/app/?access_token=secret",
	} {
		if err := openWebURL(rawURL, false, defaultWebDashboardPath); err == nil {
			t.Fatalf("openWebURL(%q) unexpectedly succeeded", rawURL)
		}
	}
}

func TestWebPublicBindingRequiresToken(t *testing.T) {
	if err := web.ValidateListenAddrForMode("0.0.0.0:8787", true, ""); err == nil {
		t.Fatal("public binding without token unexpectedly succeeded")
	}
	if err := web.ValidateListenAddrForMode("0.0.0.0:8787", true, "secret"); err != nil {
		t.Fatalf("public binding with token failed: %v", err)
	}
}
