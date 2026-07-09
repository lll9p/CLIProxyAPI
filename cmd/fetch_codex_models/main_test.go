package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdkauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestCodexModelsURL(t *testing.T) {
	got, err := codexModelsURL(" 0.144.1 ")
	if err != nil {
		t.Fatalf("codexModelsURL: %v", err)
	}
	want := "https://chatgpt.com/backend-api/codex/models?client_version=0.144.1"
	if got != want {
		t.Fatalf("codexModelsURL = %q, want %q", got, want)
	}
}

func TestCountModels(t *testing.T) {
	count, err := countModels([]byte(`{"models":[{"slug":"a"},{"slug":"b"}]}`))
	if err != nil {
		t.Fatalf("countModels(valid): %v", err)
	}
	if count != 2 {
		t.Fatalf("countModels(valid) = %d, want 2", count)
	}

	// Upstream dumps may omit CPA catalog-required fields; counting must still work.
	count, err = countModels([]byte(`{"models":[{"slug":"gpt-5.6-sol"}]}`))
	if err != nil {
		t.Fatalf("countModels(incomplete upstream model): %v", err)
	}
	if count != 1 {
		t.Fatalf("countModels(incomplete upstream model) = %d, want 1", count)
	}

	count, err = countModels([]byte(`{"models":[]}`))
	if err != nil {
		t.Fatalf("countModels(empty): %v", err)
	}
	if count != 0 {
		t.Fatalf("countModels(empty) = %d, want 0", count)
	}

	if _, err := countModels([]byte(`{"models":`)); err == nil {
		t.Fatal("countModels(malformed) error = nil, want error")
	}
	if _, err := countModels([]byte(`{}`)); err == nil {
		t.Fatal("countModels(missing models) error = nil, want error")
	}
}

func TestCodexModelsRefreshAndFetchUseResin(t *testing.T) {
	var refreshRouted atomic.Bool
	var modelsRouted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Resin-Account"); got != "codex-test.json" {
			t.Errorf("X-Resin-Account = %q, want codex-test.json", got)
		}
		switch r.URL.Path {
		case "/cpa/https/auth.openai.com/oauth/token":
			refreshRouted.Store(true)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`))
		case "/cpa/https/chatgpt.com/backend-api/codex/models":
			modelsRouted.Store(true)
			if got := r.Header.Get("Authorization"); got != "Bearer new-access" {
				t.Errorf("Authorization = %q, want Bearer new-access", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"slug":"test-model"}]}`))
		default:
			http.Error(w, fmt.Sprintf("unexpected path %s", r.URL.Path), http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{SDKConfig: config.SDKConfig{
		ResinURL:          server.URL,
		ResinPlatformName: "cpa",
	}}
	authDir := t.TempDir()
	store := sdkauth.NewFileTokenStore()
	store.SetBaseDir(authDir)
	auth := &coreauth.Auth{
		ID:       "codex-test.json",
		FileName: "codex-test.json",
		Provider: "codex",
		Metadata: map[string]any{
			"refresh_token": "refresh-test-resin",
			"type":          "codex",
		},
		Attributes: map[string]string{
			coreauth.AttributePath: filepath.Join(authDir, "codex-test.json"),
		},
	}

	accessToken, refreshed, err := ensureAccessToken(context.Background(), store, cfg, auth)
	if err != nil {
		t.Fatalf("ensureAccessToken error: %v", err)
	}
	if !refreshed || accessToken != "new-access" {
		t.Fatalf("ensureAccessToken = (%q, %v), want (new-access, true)", accessToken, refreshed)
	}

	_, count, err := fetchModels(context.Background(), cfg, auth, accessToken, "test-version")
	if err != nil {
		t.Fatalf("fetchModels error: %v", err)
	}
	if count != 1 {
		t.Fatalf("model count = %d, want 1", count)
	}
	if !refreshRouted.Load() || !modelsRouted.Load() {
		t.Fatalf("routed requests: refresh=%v models=%v", refreshRouted.Load(), modelsRouted.Load())
	}
}
