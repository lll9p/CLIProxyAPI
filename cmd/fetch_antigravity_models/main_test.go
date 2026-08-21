package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdkauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestFetchModelsDiscoversAndSavesProjectIDThroughResin(t *testing.T) {
	var step atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Resin-Account"); got != "antigravity-test.json" {
			t.Errorf("X-Resin-Account = %q, want antigravity-test.json", got)
		}
		switch r.URL.Path {
		case "/cpa/https/cloudcode-pa.googleapis.com/v1internal:loadCodeAssist":
			if got := step.Add(1); got != 1 {
				t.Errorf("project discovery request step = %d, want 1", got)
			}
			_, _ = w.Write([]byte(`{"cloudaicompanionProject":"project-resin"}`))
		case "/cpa/https/cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels":
			if got := step.Add(1); got != 2 {
				t.Errorf("models request step = %d, want 2", got)
			}
			body, errRead := io.ReadAll(r.Body)
			if errRead != nil {
				t.Errorf("read models request: %v", errRead)
			}
			if !strings.Contains(string(body), `"project": "project-resin"`) {
				t.Errorf("models request body = %s, want discovered project", body)
			}
			_, _ = w.Write([]byte(`{"models":{"gemini-test":{"displayName":"Gemini Test"}}}`))
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
	authPath := filepath.Join(authDir, "antigravity-test.json")
	store := sdkauth.NewFileTokenStore()
	store.SetBaseDir(authDir)
	auth := &coreauth.Auth{
		ID:       "antigravity-test.json",
		FileName: "antigravity-test.json",
		Provider: "antigravity",
		Metadata: map[string]any{
			"access_token": "access-token",
			"type":         "antigravity",
		},
		Attributes: map[string]string{
			coreauth.AttributePath: authPath,
		},
	}

	models, err := fetchModels(context.Background(), cfg, store, auth)
	if err != nil {
		t.Fatalf("fetchModels error: %v", err)
	}
	if len(models) != 1 || models[0].ID != "gemini-test" {
		t.Fatalf("models = %#v, want gemini-test", models)
	}
	if got := metaStringValue(auth.Metadata, "project_id"); got != "project-resin" {
		t.Fatalf("auth project_id = %q, want project-resin", got)
	}
	if got := step.Load(); got != 2 {
		t.Fatalf("request count = %d, want 2", got)
	}

	raw, errRead := os.ReadFile(authPath)
	if errRead != nil {
		t.Fatalf("read saved auth: %v", errRead)
	}
	var saved map[string]any
	if errUnmarshal := json.Unmarshal(raw, &saved); errUnmarshal != nil {
		t.Fatalf("decode saved auth: %v", errUnmarshal)
	}
	if got, _ := saved["project_id"].(string); got != "project-resin" {
		t.Fatalf("saved project_id = %q, want project-resin", got)
	}
}
