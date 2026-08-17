package executor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

type resinRouteRecord struct {
	path    string
	account string
}

func TestGenericHTTPRequestsRouteThroughResin(t *testing.T) {
	records := make(chan resinRouteRecord, 1)
	resinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		records <- resinRouteRecord{path: req.URL.EscapedPath(), account: req.Header.Get("X-Resin-Account")}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer resinServer.Close()

	var upstreamHits atomic.Int32
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstreamServer.Close()
	upstreamHost := strings.TrimPrefix(upstreamServer.URL, "http://")

	cfg := &config.Config{SDKConfig: config.SDKConfig{
		ResinURL:          resinServer.URL + "/secret",
		ResinPlatformName: "cpa",
	}}
	tests := []struct {
		name     string
		provider string
		request  func(context.Context, *cliproxyauth.Auth, *http.Request) (*http.Response, error)
	}{
		{name: "codex", provider: "codex", request: NewCodexExecutor(cfg).HttpRequest},
		{name: "codex-auto", provider: "codex", request: NewCodexAutoExecutor(cfg).HttpRequest},
		{name: "claude", provider: "claude", request: NewClaudeExecutor(cfg).HttpRequest},
		{name: "gemini", provider: "gemini", request: NewGeminiExecutor(cfg).HttpRequest},
		{name: "gemini-interactions", provider: "gemini-interactions", request: NewGeminiInteractionsExecutor(cfg).HttpRequest},
		{name: "vertex", provider: "vertex", request: NewGeminiVertexExecutor(cfg).HttpRequest},
		{name: "kimi", provider: "kimi", request: NewKimiExecutor(cfg).HttpRequest},
		{name: "xai", provider: "xai", request: NewXAIExecutor(cfg).HttpRequest},
		{name: "xai-auto", provider: "xai", request: NewXAIAutoExecutor(cfg).HttpRequest},
		{name: "antigravity", provider: "antigravity", request: NewAntigravityExecutor(cfg).HttpRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req, errRequest := http.NewRequestWithContext(context.Background(), http.MethodGet, upstreamServer.URL+"/arbitrary", nil)
			if errRequest != nil {
				t.Fatalf("create request: %v", errRequest)
			}
			auth := &cliproxyauth.Auth{
				ID:       test.name + "-auth",
				Provider: test.provider,
				FileName: test.name + "-user.json",
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
			select {
			case record := <-records:
				wantPath := fmt.Sprintf("/secret/cpa/http/%s/arbitrary", upstreamHost)
				if record.path != wantPath {
					t.Fatalf("Resin path = %q, want %q", record.path, wantPath)
				}
				if record.account != auth.FileName {
					t.Fatalf("X-Resin-Account = %q, want %q", record.account, auth.FileName)
				}
			default:
				t.Fatal("expected the generic HTTP request to reach Resin")
			}
		})
	}

	if got := upstreamHits.Load(); got != 0 {
		t.Fatalf("upstream hits = %d, want 0", got)
	}
}

func TestGenericHTTPRequestsIneligibleAuthBypassResin(t *testing.T) {
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
		name string
		auth *cliproxyauth.Auth
	}{
		{
			name: "api key auth",
			auth: &cliproxyauth.Auth{
				ID:         "codex-api-key",
				Provider:   "codex",
				FileName:   "codex-api-key.json",
				Attributes: map[string]string{"api_key": "sk-key"},
				Metadata:   map[string]any{"access_token": "token"},
			},
		},
		{
			name: "unsupported provider",
			auth: &cliproxyauth.Auth{
				ID:       "compat-auth",
				Provider: "openai-compatibility",
				FileName: "compat-user.json",
				Metadata: map[string]any{"access_token": "token"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req, errRequest := http.NewRequestWithContext(context.Background(), http.MethodGet, upstreamServer.URL+"/arbitrary", nil)
			if errRequest != nil {
				t.Fatalf("create request: %v", errRequest)
			}
			resp, errDo := NewCodexExecutor(cfg).HttpRequest(context.Background(), test.auth, req)
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
