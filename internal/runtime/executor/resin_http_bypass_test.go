package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestGenericHTTPRequestsBypassResin(t *testing.T) {
	var resinHits atomic.Int32
	resinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resinHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer resinServer.Close()

	var upstreamHits atomic.Int32
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstreamServer.Close()

	cfg := &config.Config{SDKConfig: config.SDKConfig{
		ResinURL:          resinServer.URL + "/secret",
		ResinPlatformName: "cpa",
	}}
	tests := []struct {
		provider string
		request  func(context.Context, *cliproxyauth.Auth, *http.Request) (*http.Response, error)
	}{
		{provider: "codex", request: NewCodexExecutor(cfg).HttpRequest},
		{provider: "codex-auto", request: NewCodexAutoExecutor(cfg).HttpRequest},
		{provider: "claude", request: NewClaudeExecutor(cfg).HttpRequest},
		{provider: "gemini", request: NewGeminiExecutor(cfg).HttpRequest},
		{provider: "gemini-interactions", request: NewGeminiInteractionsExecutor(cfg).HttpRequest},
		{provider: "vertex", request: NewGeminiVertexExecutor(cfg).HttpRequest},
		{provider: "kimi", request: NewKimiExecutor(cfg).HttpRequest},
		{provider: "xai", request: NewXAIExecutor(cfg).HttpRequest},
		{provider: "xai-auto", request: NewXAIAutoExecutor(cfg).HttpRequest},
		{provider: "antigravity", request: NewAntigravityExecutor(cfg).HttpRequest},
	}

	for _, test := range tests {
		t.Run(test.provider, func(t *testing.T) {
			req, errRequest := http.NewRequestWithContext(context.Background(), http.MethodGet, upstreamServer.URL+"/arbitrary", nil)
			if errRequest != nil {
				t.Fatalf("create request: %v", errRequest)
			}
			auth := &cliproxyauth.Auth{
				ID:       test.provider + "-auth",
				Provider: test.provider,
				FileName: test.provider + "-user.json",
				Metadata: map[string]any{
					"access_token": "token",
					"expired":      time.Now().Add(2 * time.Hour).Format(time.RFC3339),
				},
			}
			resp, errDo := test.request(context.Background(), auth, req)
			if errDo != nil {
				t.Fatalf("HttpRequest() error = %v", errDo)
			}
			if errClose := resp.Body.Close(); errClose != nil {
				t.Fatalf("close response body: %v", errClose)
			}
		})
	}

	if got, want := upstreamHits.Load(), int32(len(tests)); got != want {
		t.Fatalf("upstream hits = %d, want %d", got, want)
	}
	if got := resinHits.Load(); got != 0 {
		t.Fatalf("Resin hits = %d, want 0", got)
	}
}
