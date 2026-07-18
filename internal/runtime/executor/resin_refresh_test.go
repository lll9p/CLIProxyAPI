package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestOAuthRefreshRoutesThroughResin(t *testing.T) {
	tests := []struct {
		name         string
		provider     string
		expectedPath string
		response     string
		metadata     map[string]any
		refresh      func(context.Context, *config.Config, *cliproxyauth.Auth) (*cliproxyauth.Auth, error)
	}{
		{
			name:         "codex",
			provider:     "codex",
			expectedPath: "/secret/cpa/https/auth.openai.com/oauth/token",
			response:     `{"access_token":"codex-access","refresh_token":"codex-refresh-new","expires_in":3600}`,
			metadata:     map[string]any{"refresh_token": "codex-refresh"},
			refresh: func(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
				return NewCodexExecutor(cfg).Refresh(ctx, auth)
			},
		},
		{
			name:         "claude",
			provider:     "claude",
			expectedPath: "/secret/cpa/https/api.anthropic.com/v1/oauth/token",
			response:     `{"access_token":"claude-access","refresh_token":"claude-refresh-new","expires_in":3600,"account":{"email_address":"user@example.com"}}`,
			metadata:     map[string]any{"refresh_token": "claude-refresh"},
			refresh: func(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
				return NewClaudeExecutor(cfg).Refresh(ctx, auth)
			},
		},
		{
			name:         "kimi",
			provider:     "kimi",
			expectedPath: "/secret/cpa/https/auth.kimi.com/api/oauth/token",
			response:     `{"access_token":"kimi-access","refresh_token":"kimi-refresh-new","expires_in":3600}`,
			metadata:     map[string]any{"refresh_token": "kimi-refresh", "device_id": "device-1"},
			refresh: func(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
				return NewKimiExecutor(cfg).Refresh(ctx, auth)
			},
		},
		{
			name:         "xai",
			provider:     "xai",
			expectedPath: "/secret/cpa/https/auth.x.ai/oauth/token",
			response:     `{"access_token":"xai-access","refresh_token":"xai-refresh-new","expires_in":3600}`,
			metadata: map[string]any{
				"refresh_token":  "xai-refresh",
				"token_endpoint": "https://auth.x.ai/oauth/token",
			},
			refresh: func(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
				return NewXAIExecutor(cfg).Refresh(ctx, auth)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if got := req.URL.EscapedPath(); got != test.expectedPath {
					t.Errorf("Resin path = %q, want %q", got, test.expectedPath)
				}
				if got := req.Header.Get("X-Resin-Account"); got != test.provider+"-user.json" {
					t.Errorf("X-Resin-Account = %q", got)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.response))
			}))
			defer resinServer.Close()

			cfg := &config.Config{SDKConfig: config.SDKConfig{
				ResinURL:          resinServer.URL + "/secret",
				ResinPlatformName: "cpa",
			}}
			auth := &cliproxyauth.Auth{
				Provider: test.provider,
				FileName: test.provider + "-user.json",
				Metadata: test.metadata,
			}
			updated, errRefresh := test.refresh(context.Background(), cfg, auth)
			if errRefresh != nil {
				t.Fatalf("Refresh() error = %v", errRefresh)
			}
			if got := updated.Metadata["access_token"]; got != test.provider+"-access" {
				t.Fatalf("access_token = %v", got)
			}
		})
	}
}
