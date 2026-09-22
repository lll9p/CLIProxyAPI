package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestFetchRawDevinModelsRoutesThroughResin(t *testing.T) {
	var resinHits atomic.Int32
	resinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		resinHits.Add(1)
		if got, want := req.URL.EscapedPath(), "/secret/cpa/https/server.codeium.com/exa.api_server_pb.ApiServerService/GetCliModelConfigs"; got != want {
			t.Errorf("Resin path = %q, want %q", got, want)
		}
		if got, want := req.Header.Get("X-Resin-Account"), "devin-user.json"; got != want {
			t.Errorf("X-Resin-Account = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer resinServer.Close()

	var proxyHits atomic.Int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyHits.Add(1)
		http.Error(w, "unexpected proxy request", http.StatusBadGateway)
	}))
	defer proxyServer.Close()

	cfg := &config.Config{SDKConfig: config.SDKConfig{
		ResinURL:          resinServer.URL + "/secret",
		ResinPlatformName: "cpa",
	}}
	auth := &coreauth.Auth{
		ID:       "devin-user.json",
		Provider: "devin",
		FileName: "devin-user.json",
		ProxyURL: proxyServer.URL,
		Attributes: map[string]string{
			coreauth.AttributeAuthKind: coreauth.AuthKindOAuth,
		},
	}

	models, err := fetchRawDevinModels(context.Background(), cfg, auth, "devin-session-token$test")
	if err != nil {
		t.Fatalf("fetchRawDevinModels() error = %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("models = %d, want 0", len(models))
	}
	if got := resinHits.Load(); got != 1 {
		t.Fatalf("Resin hits = %d, want 1", got)
	}
	if got := proxyHits.Load(); got != 0 {
		t.Fatalf("proxy hits = %d, want 0", got)
	}
}
