package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	codexauth "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/codex"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestCodexAccountRefreshRoutesOuterAndSIWCThroughResin(t *testing.T) {
	for _, test := range []struct {
		name  string
		outer bool
		siwc  bool
	}{
		{name: "outer only", outer: true},
		{name: "SIWC only", siwc: true},
		{name: "both", outer: true, siwc: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := make(chan string, 6)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if got := req.Header.Get("X-Resin-Account"); got != "codex-siwc.json" {
					t.Errorf("X-Resin-Account = %q, want codex-siwc.json", got)
				}
				if errParse := req.ParseForm(); errParse != nil {
					t.Errorf("ParseForm() error = %v", errParse)
				}
				if req.Method != http.MethodPost || req.PostForm.Get("grant_type") != "refresh_token" {
					t.Errorf("unexpected refresh request: %s %v", req.Method, req.PostForm)
				}
				requests <- req.URL.Path
				w.Header().Set("Content-Type", "application/json")
				switch req.URL.Path {
				case "/secret/cpa/https/auth.openai.com/oauth/token":
					if got := req.PostForm.Get("refresh_token"); got != "outer-old-refresh" {
						t.Errorf("outer refresh token = %q", got)
					}
					_, _ = w.Write([]byte(`{"access_token":"outer-new","refresh_token":"outer-new-refresh","expires_in":3600}`))
				case "/secret/cpa/https/auth.openai.com/api/accounts/oauth/token":
					if req.PostForm.Get("client_id") != "issued-client" || req.PostForm.Get("refresh_token") != "siwc-old-refresh" || req.PostForm.Get("resource") != codexauth.SIWCResource {
						t.Errorf("unexpected SIWC form: %v", req.PostForm)
					}
					_, _ = w.Write([]byte(`{"access_token":"siwc-new","refresh_token":"siwc-new-refresh","scope":"chatgpt.tokens.use.direct","expires_in":3600}`))
				default:
					http.Error(w, "unexpected path", http.StatusBadRequest)
				}
			}))
			defer server.Close()

			cfg := &config.Config{AuthDir: t.TempDir(), SDKConfig: config.SDKConfig{
				ResinURL:          server.URL + "/secret",
				ResinPlatformName: "cpa",
				ProxyURL:          "http://127.0.0.1:1",
			}}
			freshExpiry := time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339)
			staleExpiry := time.Now().Add(-time.Hour).Format(time.RFC3339)
			metadata := map[string]any{
				"type":          "codex",
				"access_token":  "outer-old",
				"refresh_token": "outer-old-refresh",
				"expired":       freshExpiry,
				"use_siwc":      true,
				"siwc": map[string]any{
					"access_token":  "siwc-old",
					"refresh_token": "siwc-old-refresh",
					"client_id":     "issued-client",
					"device_id":     "device-id",
					"expired":       freshExpiry,
				},
			}
			if test.outer {
				metadata["expired"] = staleExpiry
			}
			if test.siwc {
				metadata["siwc"].(map[string]any)["expired"] = staleExpiry
			}
			path := filepath.Join(cfg.AuthDir, "codex-siwc.json")
			raw, errMarshal := json.Marshal(metadata)
			if errMarshal != nil {
				t.Fatal(errMarshal)
			}
			if errWrite := os.WriteFile(path, raw, 0o600); errWrite != nil {
				t.Fatal(errWrite)
			}
			storage := &codexauth.CodexTokenStorage{AccessToken: "outer-old", SIWC: cloneSIWCMap(metadata["siwc"].(map[string]any))}
			auth := &cliproxyauth.Auth{
				ID:       "different-id",
				FileName: "codex-siwc.json",
				Provider: "codex",
				ProxyURL: "http://127.0.0.1:1",
				Metadata: metadata,
				Storage:  storage,
			}
			updated, errRefresh := NewCodexExecutor(cfg).Refresh(context.Background(), auth)
			if errRefresh != nil {
				t.Fatalf("Refresh() error = %v", errRefresh)
			}
			var wantPaths []string
			wantOuter, wantSIWC := "outer-old", "siwc-old"
			if test.outer {
				wantPaths = append(wantPaths, "/secret/cpa/https/auth.openai.com/oauth/token")
				wantOuter = "outer-new"
			}
			if test.siwc {
				wantPaths = append(wantPaths, "/secret/cpa/https/auth.openai.com/api/accounts/oauth/token")
				wantSIWC = "siwc-new"
			}
			if len(requests) != len(wantPaths) {
				t.Fatalf("refresh requests = %d, want %d", len(requests), len(wantPaths))
			}
			for _, wantPath := range wantPaths {
				if got := <-requests; got != wantPath {
					t.Fatalf("refresh path = %q, want %q", got, wantPath)
				}
			}
			if updated.Metadata["access_token"] != wantOuter || updated.Metadata["siwc"].(map[string]any)["access_token"] != wantSIWC || updated.Metadata["use_siwc"] != true {
				t.Fatalf("refresh lost or incorrectly updated credentials: %#v", updated.Metadata)
			}
			diskBytes, errRead := os.ReadFile(path)
			if errRead != nil {
				t.Fatal(errRead)
			}
			var diskMetadata map[string]any
			if errUnmarshal := json.Unmarshal(diskBytes, &diskMetadata); errUnmarshal != nil {
				t.Fatal(errUnmarshal)
			}
			diskSIWC, ok := diskMetadata["siwc"].(map[string]any)
			if !ok || diskMetadata["access_token"] != wantOuter || diskSIWC["access_token"] != wantSIWC || diskMetadata["use_siwc"] != true || diskSIWC["client_id"] != "issued-client" || diskSIWC["device_id"] != "device-id" {
				t.Fatalf("persisted credentials lost or incorrectly updated: %#v", diskMetadata)
			}
			if storage.AccessToken != "outer-old" || storage.SIWC["access_token"] != "siwc-old" {
				t.Fatal("refresh mutated the original storage snapshot")
			}
		})
	}
}
