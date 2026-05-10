package codex

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRefreshTokensWithRetry_NonRetryableOnlyAttemptsOnce(t *testing.T) {
	var calls int32
	auth := &CodexAuth{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				atomic.AddInt32(&calls, 1)
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_grant","code":"refresh_token_reused"}`)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}),
		},
	}

	_, err := auth.RefreshTokensWithRetry(context.Background(), "dummy_refresh_token", 3)
	if err == nil {
		t.Fatalf("expected error for non-retryable refresh failure")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "refresh_token_reused") {
		t.Fatalf("expected refresh_token_reused in error, got: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 refresh attempt, got %d", got)
	}
}

func TestNewCodexAuthWithProxyURL_OverrideDirectDisablesProxy(t *testing.T) {
	cfg := &config.Config{SDKConfig: config.SDKConfig{ProxyURL: "http://proxy.example.com:8080"}}
	auth := NewCodexAuthWithProxyURL(cfg, "direct")

	transport, ok := auth.httpClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("expected http.Transport, got %T", auth.httpClient.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("expected direct transport to disable proxy function")
	}
}

func TestNewCodexAuthWithProxyURL_OverrideProxyTakesPrecedence(t *testing.T) {
	cfg := &config.Config{SDKConfig: config.SDKConfig{ProxyURL: "http://global.example.com:8080"}}
	auth := NewCodexAuthWithProxyURL(cfg, "http://override.example.com:8081")

	transport, ok := auth.httpClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("expected http.Transport, got %T", auth.httpClient.Transport)
	}
	req, errReq := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if errReq != nil {
		t.Fatalf("new request: %v", errReq)
	}
	proxyURL, errProxy := transport.Proxy(req)
	if errProxy != nil {
		t.Fatalf("proxy func: %v", errProxy)
	}
	if proxyURL == nil || proxyURL.String() != "http://override.example.com:8081" {
		t.Fatalf("proxy URL = %v, want http://override.example.com:8081", proxyURL)
	}
}

func TestRefreshTokensUsesResinRouteAndStableAccount(t *testing.T) {
	var proxyCalls int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxyCalls, 1)
		http.Error(w, "proxy should not be used", http.StatusBadGateway)
	}))
	defer proxyServer.Close()

	var resinHost string
	resinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		const wantPath = "/resin/codex/https/auth.openai.com/oauth/token"
		if r.URL.Path != wantPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, wantPath)
		}
		if r.Host != resinHost {
			t.Fatalf("Host = %q, want Resin host %q", r.Host, resinHost)
		}
		if got := r.Header.Get(ResinAccountHeader); got != "codex-account.json" {
			t.Fatalf("%s = %q, want codex-account.json", ResinAccountHeader, got)
		}
		body, errRead := io.ReadAll(r.Body)
		if errRead != nil {
			t.Fatalf("read body: %v", errRead)
		}
		bodyStr := string(body)
		if !strings.Contains(bodyStr, "grant_type=refresh_token") || !strings.Contains(bodyStr, "refresh_token=refresh-1") {
			t.Fatalf("refresh form body = %q", bodyStr)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-2","refresh_token":"refresh-2","expires_in":3600}`))
	}))
	defer resinServer.Close()
	resinHost = strings.TrimPrefix(resinServer.URL, "http://")

	cfg := &config.Config{SDKConfig: config.SDKConfig{
		ProxyURL:          proxyServer.URL,
		ResinURL:          resinServer.URL + "/resin",
		ResinPlatformName: "codex",
	}}
	auth := NewCodexAuthWithProxyURLForAccount(cfg, proxyServer.URL, "codex-account.json")

	tokenData, err := auth.RefreshTokens(context.Background(), "refresh-1")
	if err != nil {
		t.Fatalf("RefreshTokens error: %v", err)
	}
	if tokenData.AccessToken != "access-2" || tokenData.RefreshToken != "refresh-2" {
		t.Fatalf("token data = %#v", tokenData)
	}
	if got := atomic.LoadInt32(&proxyCalls); got != 0 {
		t.Fatalf("proxy calls = %d, want 0", got)
	}
}
