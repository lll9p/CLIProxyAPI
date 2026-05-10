package codex

import (
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestStableResinAccountUsesFileNameBeforeID(t *testing.T) {
	auth := &cliproxyauth.Auth{ID: "auth-id", FileName: "codex-account.json"}
	if got := StableResinAccount(auth); got != "codex-account.json" {
		t.Fatalf("StableResinAccount = %q, want file name", got)
	}

	auth.FileName = ""
	if got := StableResinAccount(auth); got != "auth-id" {
		t.Fatalf("StableResinAccount fallback = %q, want auth ID", got)
	}
}

func TestResolveResinHTTPRouteBuildsRouteAndPreservesQuery(t *testing.T) {
	cfg := &config.Config{SDKConfig: config.SDKConfig{ResinURL: "https://resin.example.com/base", ResinPlatformName: "codex"}}
	auth := &cliproxyauth.Auth{ID: "auth-id", FileName: "codex-account.json"}

	route, ok, err := ResolveResinHTTPRoute(cfg, "https://chatgpt.com/backend-api/codex/responses?foo=bar&z=1", auth)
	if err != nil {
		t.Fatalf("ResolveResinHTTPRoute error: %v", err)
	}
	if !ok {
		t.Fatal("expected Resin route")
	}
	if route.Account != "codex-account.json" {
		t.Fatalf("route account = %q, want codex-account.json", route.Account)
	}
	const wantURL = "https://resin.example.com/base/codex/https/chatgpt.com/backend-api/codex/responses?foo=bar&z=1"
	if route.URL != wantURL {
		t.Fatalf("route URL = %q, want %q", route.URL, wantURL)
	}
}

func TestResolveResinHTTPRouteDisabledFallback(t *testing.T) {
	cases := []struct {
		name    string
		cfg     *config.Config
		account string
	}{
		{name: "nil config", cfg: nil, account: "account"},
		{name: "missing Resin URL", cfg: &config.Config{SDKConfig: config.SDKConfig{ResinPlatformName: "codex"}}, account: "account"},
		{name: "missing platform", cfg: &config.Config{SDKConfig: config.SDKConfig{ResinURL: "https://resin.example.com"}}, account: "account"},
		{name: "missing account", cfg: &config.Config{SDKConfig: config.SDKConfig{ResinURL: "https://resin.example.com", ResinPlatformName: "codex"}}, account: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok, err := ResolveResinHTTPRouteForAccount(tc.cfg, "https://chatgpt.com/responses", tc.account)
			if err != nil {
				t.Fatalf("ResolveResinHTTPRouteForAccount error: %v", err)
			}
			if ok {
				t.Fatal("expected Resin route to be disabled")
			}
		})
	}
}

func TestResolveResinHTTPRouteRejectsUnsupportedScheme(t *testing.T) {
	cfg := &config.Config{SDKConfig: config.SDKConfig{ResinURL: "https://resin.example.com", ResinPlatformName: "codex"}}

	_, ok, err := ResolveResinHTTPRouteForAccount(cfg, "ftp://chatgpt.com/responses", "account")
	if err != nil {
		t.Fatalf("ResolveResinHTTPRouteForAccount error: %v", err)
	}
	if ok {
		t.Fatal("expected unsupported HTTP scheme to skip Resin")
	}
}

func TestResolveResinWebsocketRouteMapsProtocolAndUsesWS(t *testing.T) {
	cfg := &config.Config{SDKConfig: config.SDKConfig{ResinURL: "https://resin.example.com/root", ResinPlatformName: "codex"}}
	auth := &cliproxyauth.Auth{ID: "auth-id"}

	route, ok, err := ResolveResinWebsocketRoute(cfg, "wss://chatgpt.com/backend-api/codex/responses?stream=1", auth)
	if err != nil {
		t.Fatalf("ResolveResinWebsocketRoute error: %v", err)
	}
	if !ok {
		t.Fatal("expected Resin websocket route")
	}
	const wantURL = "ws://resin.example.com/root/codex/https/chatgpt.com/backend-api/codex/responses?stream=1"
	if route.URL != wantURL {
		t.Fatalf("route URL = %q, want %q", route.URL, wantURL)
	}
}

func TestApplyResinReverseProxyRewritesRequest(t *testing.T) {
	cfg := &config.Config{SDKConfig: config.SDKConfig{ResinURL: "http://resin.example.com/base", ResinPlatformName: "codex"}}
	auth := &cliproxyauth.Auth{ID: "auth-id", FileName: "codex-account.json"}
	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses?x=1", nil)
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	req.Host = "chatgpt.com"

	if !ApplyResinReverseProxy(cfg, req, auth) {
		t.Fatal("expected Resin rewrite")
	}
	const wantURL = "http://resin.example.com/base/codex/https/chatgpt.com/backend-api/codex/responses?x=1"
	if got := req.URL.String(); got != wantURL {
		t.Fatalf("request URL = %q, want %q", got, wantURL)
	}
	if req.Host != "" {
		t.Fatalf("request Host override = %q, want empty", req.Host)
	}
	if got := req.Header.Get(ResinAccountHeader); got != "codex-account.json" {
		t.Fatalf("%s = %q, want codex-account.json", ResinAccountHeader, got)
	}
}
