package cliproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestFetchAntigravityModelCapabilityHintsUsesResin(t *testing.T) {
	var sawFetch atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cpa/https/upstream.example/v1internal:fetchAvailableModels" {
			t.Errorf("path = %q, want Resin model route", r.URL.Path)
		}
		if got := r.Header.Get("X-Resin-Account"); got != "antigravity-resin.json" {
			t.Errorf("X-Resin-Account = %q, want antigravity-resin.json", got)
		}
		sawFetch.Store(true)
		_, _ = w.Write([]byte(`{"webSearchModelIds":["gemini-resin"]}`))
	}))
	defer server.Close()

	service := &Service{cfg: &config.Config{SDKConfig: config.SDKConfig{
		ResinURL:          server.URL,
		ResinPlatformName: "cpa",
	}}}
	auth := &coreauth.Auth{
		ID:       "antigravity-resin.json",
		FileName: "antigravity-resin.json",
		Provider: "antigravity",
		Attributes: map[string]string{
			"base_url": "https://upstream.example",
		},
		Metadata: map[string]any{
			"access_token": "token",
		},
	}

	hints := service.fetchAntigravityModelCapabilityHintsForAuth(context.Background(), auth)
	if !sawFetch.Load() {
		t.Fatal("expected Resin fetchAvailableModels request")
	}
	if _, ok := hints.WebSearchModelIDs["gemini-resin"]; !ok {
		t.Fatalf("hints = %#v, want gemini-resin", hints.WebSearchModelIDs)
	}
}
