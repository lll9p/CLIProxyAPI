package helps

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

type proxyTestRoundTripFunc func(*http.Request) (*http.Response, error)

func (f proxyTestRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRefreshRouteKey(t *testing.T) {
	tests := []struct {
		name string
		auth *cliproxyauth.Auth
		want string
	}{
		{name: "nil"},
		{name: "file name", auth: &cliproxyauth.Auth{FileName: " account.json ", ID: "id"}, want: "account.json"},
		{name: "id fallback", auth: &cliproxyauth.Auth{ID: " id "}, want: "id"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := RefreshRouteKey(test.auth); got != test.want {
				t.Fatalf("RefreshRouteKey() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNewProxyAwareHTTPClientRoutesEligibleAuthThroughResin(t *testing.T) {
	t.Parallel()

	resinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got, want := req.URL.EscapedPath(), "/secret/cpa/https/api.example.com/v1/a%2Fb"; got != want {
			t.Errorf("Resin path = %q, want %q", got, want)
		}
		if got := req.Header.Get("X-Resin-Account"); got != "codex-user.json" {
			t.Errorf("X-Resin-Account = %q, want codex-user.json", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer resinServer.Close()

	cfg := &config.Config{SDKConfig: sdkconfig.SDKConfig{
		ResinURL:          resinServer.URL + "/secret",
		ResinPlatformName: "cpa",
	}}
	auth := &cliproxyauth.Auth{
		Provider: "codex",
		FileName: "codex-user.json",
		Metadata: map[string]any{"access_token": "token"},
	}
	client := NewProxyAwareHTTPClient(context.Background(), cfg, auth, 0)
	resp, err := client.Get("https://api.example.com/v1/a%2Fb")
	if err != nil {
		t.Fatalf("client.Get returned error: %v", err)
	}
	if errClose := resp.Body.Close(); errClose != nil {
		t.Fatalf("response body close returned error: %v", errClose)
	}
}

func TestNewRawProxyAwareHTTPClientBypassesResin(t *testing.T) {
	t.Parallel()

	var resinHits atomic.Int32
	resinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resinHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer resinServer.Close()

	var rawHits atomic.Int32
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", proxyTestRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		rawHits.Add(1)
		if got := req.URL.String(); got != "https://api.example.com/v1/models" {
			t.Errorf("raw request URL = %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("{}")),
			Request:    req,
		}, nil
	}))
	cfg := &config.Config{SDKConfig: sdkconfig.SDKConfig{
		ResinURL:          resinServer.URL + "/secret",
		ResinPlatformName: "cpa",
	}}
	auth := &cliproxyauth.Auth{
		Provider: "codex",
		FileName: "codex-user.json",
		Metadata: map[string]any{"access_token": "token"},
	}
	client := NewRawProxyAwareHTTPClient(ctx, cfg, auth, 0)
	resp, err := client.Get("https://api.example.com/v1/models")
	if err != nil {
		t.Fatalf("client.Get returned error: %v", err)
	}
	if errClose := resp.Body.Close(); errClose != nil {
		t.Fatalf("response body close returned error: %v", errClose)
	}
	if got := rawHits.Load(); got != 1 {
		t.Fatalf("raw transport hits = %d, want 1", got)
	}
	if got := resinHits.Load(); got != 0 {
		t.Fatalf("Resin hits = %d, want 0", got)
	}
}
