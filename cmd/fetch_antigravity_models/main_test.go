package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestFetchModelsThroughResin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Resin-Account"); got != "antigravity-test.json" {
			t.Errorf("X-Resin-Account = %q, want antigravity-test.json", got)
		}
		if got := r.URL.Path; got != "/cpa/https/cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels" {
			http.Error(w, fmt.Sprintf("unexpected path %s", got), http.StatusNotFound)
			return
		}
		body, errRead := io.ReadAll(r.Body)
		if errRead != nil {
			t.Errorf("read models request: %v", errRead)
		}
		if !strings.Contains(string(body), `"project": "project-resin"`) {
			t.Errorf("models request body = %s, want configured project", body)
		}
		_, _ = w.Write([]byte(`{"models":{"gemini-test":{"displayName":"Gemini Test"}}}`))
	}))
	defer server.Close()

	cfg := &config.Config{SDKConfig: config.SDKConfig{
		ResinURL:          server.URL,
		ResinPlatformName: "cpa",
	}}
	auth := &coreauth.Auth{
		ID:       "antigravity-test.json",
		FileName: "antigravity-test.json",
		Provider: "antigravity",
		Metadata: map[string]any{
			"access_token": "access-token",
			"project_id":   "project-resin",
			"type":         "antigravity",
		},
	}

	models := fetchModels(context.Background(), cfg, auth)
	if len(models) != 1 || models[0].ID != "gemini-test" {
		t.Fatalf("models = %#v, want gemini-test", models)
	}
}
