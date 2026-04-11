package codex

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
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

func TestHTTPClientForRequestUsesDirectClientWhenResinApplies(t *testing.T) {
	t.Parallel()

	proxyClient := &http.Client{}
	directClient := &http.Client{}
	auth := &CodexAuth{
		cfg: &config.Config{
			SDKConfig: config.SDKConfig{
				ResinURL:          "http://127.0.0.1:2260/my-token",
				ResinPlatformName: "openai",
			},
		},
		httpClient:       proxyClient,
		directHTTPClient: directClient,
		resinAccount:     "codex-user.json",
	}
	req, err := http.NewRequest(http.MethodPost, TokenURL, strings.NewReader("grant_type=refresh_token"))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	httpClient, err := auth.httpClientForRequest(req)
	if err != nil {
		t.Fatalf("httpClientForRequest() error = %v", err)
	}
	if httpClient != directClient {
		t.Fatal("expected direct HTTP client when Resin applies")
	}
	if got := req.URL.String(); got != "http://127.0.0.1:2260/my-token/openai/https/auth.openai.com/oauth/token" {
		t.Fatalf("rewritten request URL = %q", got)
	}
	if got := req.Header.Get(ResinAccountHeader); got != "codex-user.json" {
		t.Fatalf("%s = %q, want %q", ResinAccountHeader, got, "codex-user.json")
	}
}

func TestHTTPClientForRequestFallsBackWithoutResin(t *testing.T) {
	t.Parallel()

	proxyClient := &http.Client{}
	directClient := &http.Client{}
	auth := &CodexAuth{
		cfg:              &config.Config{},
		httpClient:       proxyClient,
		directHTTPClient: directClient,
		resinAccount:     "codex-user.json",
	}
	req, err := http.NewRequest(http.MethodPost, TokenURL, strings.NewReader("grant_type=refresh_token"))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	httpClient, err := auth.httpClientForRequest(req)
	if err != nil {
		t.Fatalf("httpClientForRequest() error = %v", err)
	}
	if httpClient != proxyClient {
		t.Fatal("expected proxy/default HTTP client when Resin is disabled")
	}
	if got := req.URL.String(); got != TokenURL {
		t.Fatalf("request URL = %q, want %q", got, TokenURL)
	}
	if got := req.Header.Get(ResinAccountHeader); got != "" {
		t.Fatalf("%s = %q, want empty", ResinAccountHeader, got)
	}
}
