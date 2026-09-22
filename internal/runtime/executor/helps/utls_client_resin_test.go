package helps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestNewUtlsHTTPClientRoutesProtectedHostThroughResin(t *testing.T) {
	t.Parallel()

	resinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got, want := req.URL.EscapedPath(), "/secret/cpa/https/api.anthropic.com/v1/oauth/token"; got != want {
			t.Errorf("Resin path = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer resinServer.Close()

	cfg := &config.Config{SDKConfig: sdkconfig.SDKConfig{
		ResinURL:          resinServer.URL + "/secret",
		ResinPlatformName: "cpa",
	}}
	auth := &cliproxyauth.Auth{
		Provider: "claude",
		FileName: "claude-user.json",
		Metadata: map[string]any{"access_token": "token"},
	}
	client := NewUtlsHTTPClient(context.Background(), cfg, auth, 0)
	resp, err := client.Get("https://api.anthropic.com/v1/oauth/token")
	if err != nil {
		t.Fatalf("client.Get returned error: %v", err)
	}
	if errClose := resp.Body.Close(); errClose != nil {
		t.Fatalf("response body close returned error: %v", errClose)
	}
}
