package codex

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefreshTokens_DoesNotDeduplicateDifferentRouteAccounts(t *testing.T) {
	resetCodexRefreshGroupForTest()
	t.Cleanup(resetCodexRefreshGroupForTest)

	var calls int32
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
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
	authA := NewCodexAuthWithHTTPClient(&http.Client{Transport: transport}, "account-a.json")
	authB := NewCodexAuthWithHTTPClient(&http.Client{Transport: transport}, "account-b.json")

	errs := make(chan error, 2)
	go func() {
		_, errRefresh := authA.RefreshTokens(context.Background(), "shared-refresh-token")
		errs <- errRefresh
	}()
	go func() {
		_, errRefresh := authB.RefreshTokens(context.Background(), "shared-refresh-token")
		errs <- errRefresh
	}()

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

func TestNewCodexAuthWithHTTPClient(t *testing.T) {
	injected := &http.Client{Timeout: time.Second}
	auth := NewCodexAuthWithHTTPClient(injected, " account.json ")
	if got := auth.httpClient; got != injected {
		t.Fatal("expected injected HTTP client to be used")
	}
	if auth.refreshRouteKey != "account.json" {
		t.Fatalf("refresh route key = %q, want account.json", auth.refreshRouteKey)
	}

	defaultClient := NewCodexAuthWithHTTPClient(nil).httpClient
	if defaultClient == nil || defaultClient.Transport != nil || defaultClient.Timeout != 0 {
		t.Fatalf("expected standard default HTTP client, got %#v", defaultClient)
	}
}
