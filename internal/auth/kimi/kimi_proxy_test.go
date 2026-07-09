package kimi

import (
	"net/http"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestNewDeviceFlowClientWithDeviceIDAndProxyURL_OverrideDirectDisablesProxy(t *testing.T) {
	cfg := &config.Config{SDKConfig: config.SDKConfig{ProxyURL: "http://proxy.example.com:8080"}}
	client := NewDeviceFlowClientWithDeviceIDAndProxyURL(cfg, "device-1", "direct")

	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("expected http.Transport, got %T", client.httpClient.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("expected direct transport to disable proxy function")
	}
}

func TestNewDeviceFlowClientWithDeviceIDAndProxyURL_OverrideProxyTakesPrecedence(t *testing.T) {
	cfg := &config.Config{SDKConfig: config.SDKConfig{ProxyURL: "http://global.example.com:8080"}}
	client := NewDeviceFlowClientWithDeviceIDAndProxyURL(cfg, "device-1", "http://override.example.com:8081")

	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("expected http.Transport, got %T", client.httpClient.Transport)
	}
	req, errReq := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if errReq != nil {
		t.Fatalf("new request: %v", errReq)
	}
	proxyURL, errProxy := transport.Proxy(req)
	if errProxy != nil {
		t.Fatalf("proxy func: %v", errProxy)
	}
	if proxyURL == nil || proxyURL.String() != "http://override.example.com:8081" {
		t.Fatalf("proxy URL = %v, want http://override.example.com:8081", proxyURL)
	}
}

func TestNewDeviceFlowClientWithDeviceIDAndHTTPClient(t *testing.T) {
	injected := &http.Client{Timeout: time.Second}
	client := NewDeviceFlowClientWithDeviceIDAndHTTPClient("device-1", injected, " account.json ")
	if client.httpClient != injected {
		t.Fatal("expected injected HTTP client to be used")
	}
	if client.deviceID != "device-1" {
		t.Fatalf("device ID = %q, want device-1", client.deviceID)
	}
	if client.refreshRouteKey != "account.json" {
		t.Fatalf("refresh route key = %q, want account.json", client.refreshRouteKey)
	}

	defaultClient := NewDeviceFlowClientWithDeviceIDAndHTTPClient("device-1", nil).httpClient
	if defaultClient == nil || defaultClient.Timeout != 30*time.Second {
		t.Fatalf("expected 30s default HTTP client timeout, got %#v", defaultClient)
	}
}
