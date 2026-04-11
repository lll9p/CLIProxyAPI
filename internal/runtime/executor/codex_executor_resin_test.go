package executor

import (
	"context"
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func TestNewCodexHTTPClientUsesDirectTransportWhenResinApplied(t *testing.T) {
	t.Parallel()

	client := newCodexHTTPClient(context.Background(), &config.Config{
		SDKConfig: sdkconfig.SDKConfig{ProxyURL: "http://global-proxy.example.com:8080"},
	}, nil, true)

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("expected direct transport without proxy when Resin routing is active")
	}
}

func TestNewCodexHTTPClientFallsBackToConfiguredProxyWhenResinDisabled(t *testing.T) {
	t.Parallel()

	client := newCodexHTTPClient(context.Background(), &config.Config{
		SDKConfig: sdkconfig.SDKConfig{ProxyURL: "http://global-proxy.example.com:8080"},
	}, nil, false)

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy == nil {
		t.Fatal("expected configured proxy transport when Resin routing is disabled")
	}
}
