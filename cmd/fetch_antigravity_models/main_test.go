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
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdkauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestDefaultAntigravityFetchBaseURLs(t *testing.T) {
	want := []string{
		antigravityBaseURLDaily,
		antigravityBaseURLProd,
		antigravitySandboxBaseURLDaily,
	}

	got := defaultAntigravityFetchBaseURLs()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaultAntigravityFetchBaseURLs() = %#v, want %#v", got, want)
	}
}

func TestFetchModelsRetryPerEndpoint(t *testing.T) {
	var endpoint1Calls atomic.Int32
	var endpoint2Calls atomic.Int32

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := endpoint1Calls.Add(1)
		if call == 1 {
			http.Error(w, `{"error":"temporary server error"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"models": {
				"gemini-3.6-flash": {
					"displayName": "Gemini 3.6 Flash",
					"maxTokens": 1048576,
					"maxOutputTokens": 8192
				}
			}
		}`))
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint2Calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"models": {
				"gemini-1.5-pro": {
					"displayName": "Gemini 1.5 Pro"
				}
			}
		}`))
	}))
	defer server2.Close()

	auth := &coreauth.Auth{
		Metadata: map[string]interface{}{
			"access_token": "test-token",
			"project_id":   "test-project",
		},
	}

	// Case 1: First endpoint fails on attempt 1, succeeds on attempt 2.
	// It should succeed without calling endpoint 2, and endpoint 1 should have been called 2 times.
	models := fetchModelsFromBaseURLs(context.Background(), auth, []string{server1.URL, server2.URL}, server1.Client())
	if len(models) != 1 || models[0].ID != "gemini-3.6-flash" {
		t.Fatalf("expected 1 model (gemini-3.6-flash), got: %#v", models)
	}
	if calls := endpoint1Calls.Load(); calls != 2 {
		t.Fatalf("expected endpoint 1 to be called 2 times, got %d", calls)
	}
	if calls := endpoint2Calls.Load(); calls != 0 {
		t.Fatalf("expected endpoint 2 not to be called, got %d", calls)
	}
}

func TestFetchModelsFallbackAfterTwoAttempts(t *testing.T) {
	var endpoint1Calls atomic.Int32
	var endpoint2Calls atomic.Int32

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint1Calls.Add(1)
		http.Error(w, `{"error":"unavailable"}`, http.StatusServiceUnavailable)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint2Calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"models": {
				"gemini-2.5-flash": {
					"displayName": "Gemini 2.5 Flash"
				}
			}
		}`))
	}))
	defer server2.Close()

	auth := &coreauth.Auth{
		Metadata: map[string]interface{}{
			"access_token": "test-token",
		},
	}

	// Case 2: First endpoint fails all 2 attempts, then falls back to endpoint 2.
	models := fetchModelsFromBaseURLs(context.Background(), auth, []string{server1.URL, server2.URL}, server1.Client())
	if len(models) != 1 || models[0].ID != "gemini-2.5-flash" {
		t.Fatalf("expected 1 model (gemini-2.5-flash), got: %#v", models)
	}
	if calls := endpoint1Calls.Load(); calls != 2 {
		t.Fatalf("expected endpoint 1 to be called 2 times before fallback, got %d", calls)
	}
	if calls := endpoint2Calls.Load(); calls != 1 {
		t.Fatalf("expected endpoint 2 to be called 1 time, got %d", calls)
	}
}

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

	models, err := fetchModelsWithResin(context.Background(), cfg, store, auth)
	if err != nil {
		t.Fatalf("fetchModelsWithResin error: %v", err)
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
