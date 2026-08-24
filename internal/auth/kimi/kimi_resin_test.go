package kimi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

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

func TestRefreshToken_DoesNotDeduplicateDifferentRouteAccounts(t *testing.T) {
	resetKimiRefreshGroupForTest()
	t.Cleanup(resetKimiRefreshGroupForTest)

	var calls int32
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	transport := kimiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		started <- struct{}{}
		<-release
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"new-access","refresh_token":"new-refresh","token_type":"Bearer","expires_in":3600}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	clientA := NewDeviceFlowClientWithDeviceIDAndHTTPClient("device-1", &http.Client{Transport: transport}, "account-a.json")
	clientB := NewDeviceFlowClientWithDeviceIDAndHTTPClient("device-1", &http.Client{Transport: transport}, "account-b.json")

	errs := make(chan error, 2)
	for _, client := range []*DeviceFlowClient{clientA, clientB} {
		go func(client *DeviceFlowClient) {
			_, errRefresh := client.RefreshToken(context.Background(), "shared-refresh-token")
			errs <- errRefresh
		}(client)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatalf("different route accounts shared one refresh call; calls = %d", atomic.LoadInt32(&calls))
		}
	}
	close(release)
	for i := 0; i < 2; i++ {
		if errRefresh := <-errs; errRefresh != nil {
			t.Fatalf("expected refresh to succeed, got %v", errRefresh)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("refresh calls = %d, want 2", got)
	}
}
