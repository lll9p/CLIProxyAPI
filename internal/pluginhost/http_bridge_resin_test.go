package pluginhost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestHostHTTPClientWireProfileRoutesThroughResin(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits.Add(1)
		_, _ = w.Write([]byte("upstream"))
	}))
	defer upstream.Close()
	resinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got, want := req.URL.EscapedPath(), "/secret/cpa/http/"+strings.TrimPrefix(upstream.URL, "http://")+"/test"; got != want {
			t.Errorf("Resin path = %q, want %q", got, want)
		}
		if got := req.Header.Get("X-Resin-Account"); got != "codex-user.json" {
			t.Errorf("X-Resin-Account = %q, want codex-user.json", got)
		}
		_, _ = w.Write([]byte("resin"))
	}))
	defer resinServer.Close()

	host := New()
	host.runtimeConfig = &config.Config{SDKConfig: config.SDKConfig{ResinURL: resinServer.URL + "/secret", ResinPlatformName: "cpa"}}
	auth := &cliproxyauth.Auth{Provider: "codex", FileName: "codex-user.json", Metadata: map[string]any{"access_token": "token"}}
	resp, err := host.newHTTPClient(auth).Do(context.Background(), pluginapi.HTTPRequest{
		URL: upstream.URL + "/test", WireProfile: &pluginapi.HTTPWireProfile{HTTP1Only: true},
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if string(resp.Body) != "resin" || upstreamHits.Load() != 0 {
		t.Fatalf("response body = %q, upstream hits = %d; want Resin only", resp.Body, upstreamHits.Load())
	}
}

func TestHostHTTPClientRoutesEligibleAuthThroughResin(t *testing.T) {
	var upstreamHits atomic.Int32
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits.Add(1)
		_, _ = w.Write([]byte("upstream"))
	}))
	defer upstreamServer.Close()
	upstreamHost := strings.TrimPrefix(upstreamServer.URL, "http://")

	resinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got, want := req.URL.EscapedPath(), "/secret/cpa/http/"+upstreamHost; got != want {
			t.Errorf("Resin path = %q, want %q", got, want)
		}
		if got := req.Header.Get("X-Resin-Account"); got != "codex-user.json" {
			t.Errorf("X-Resin-Account = %q, want codex-user.json", got)
		}
		_, _ = w.Write([]byte("resin"))
	}))
	defer resinServer.Close()

	host := New()
	host.mu.Lock()
	host.runtimeConfig = &config.Config{SDKConfig: config.SDKConfig{
		ResinURL:          resinServer.URL + "/secret",
		ResinPlatformName: "cpa",
	}}
	host.mu.Unlock()
	auth := &cliproxyauth.Auth{
		Provider: "codex",
		FileName: "codex-user.json",
		Metadata: map[string]any{"access_token": "token"},
	}

	resp, err := host.newHTTPClient(auth).Do(context.Background(), pluginapi.HTTPRequest{URL: upstreamServer.URL})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if got := string(resp.Body); got != "resin" {
		t.Fatalf("response body = %q, want resin (plugin bridge must route eligible auth through Resin)", got)
	}
	if got := upstreamHits.Load(); got != 0 {
		t.Fatalf("upstream hits = %d, want 0", got)
	}
}
