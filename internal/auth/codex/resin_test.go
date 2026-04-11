package codex

import (
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestStableResinAccountPrefersFileName(t *testing.T) {
	t.Parallel()

	auth := &cliproxyauth.Auth{FileName: "codex-user.json", ID: "fallback-id"}
	if got := StableResinAccount(auth); got != "codex-user.json" {
		t.Fatalf("StableResinAccount() = %q, want %q", got, "codex-user.json")
	}
}

func TestApplyResinReverseProxyRewritesHTTPRequest(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.ResinURL = "http://127.0.0.1:2260/my-token"
	cfg.ResinPlatformName = "openai"
	auth := &cliproxyauth.Auth{FileName: "codex-user.json"}
	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses?trace=1", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	applied, err := ApplyResinReverseProxy(req, cfg, auth)
	if err != nil {
		t.Fatalf("ApplyResinReverseProxy() error = %v", err)
	}
	if !applied {
		t.Fatal("expected Resin reverse proxy to be applied")
	}
	if got := req.URL.String(); got != "http://127.0.0.1:2260/my-token/openai/https/chatgpt.com/backend-api/codex/responses?trace=1" {
		t.Fatalf("rewritten URL = %q", got)
	}
	if got := req.Header.Get(ResinAccountHeader); got != "codex-user.json" {
		t.Fatalf("%s = %q, want %q", ResinAccountHeader, got, "codex-user.json")
	}
	if req.Host != "" {
		t.Fatalf("request host = %q, want empty", req.Host)
	}
}

func TestResolveResinWebsocketRouteUsesWSClientScheme(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.ResinURL = "http://127.0.0.1:2260/my-token"
	cfg.ResinPlatformName = "openai"
	auth := &cliproxyauth.Auth{FileName: "codex-user.json"}

	route, err := ResolveResinWebsocketRoute(cfg, auth, "wss://chatgpt.com/backend-api/codex/responses?model=gpt-5")
	if err != nil {
		t.Fatalf("ResolveResinWebsocketRoute() error = %v", err)
	}
	if route == nil {
		t.Fatal("expected websocket Resin route")
	}
	if route.URL != "ws://127.0.0.1:2260/my-token/openai/https/chatgpt.com/backend-api/codex/responses?model=gpt-5" {
		t.Fatalf("route URL = %q", route.URL)
	}
	if route.Account != "codex-user.json" {
		t.Fatalf("route Account = %q, want %q", route.Account, "codex-user.json")
	}
}
