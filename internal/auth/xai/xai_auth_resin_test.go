package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefreshTokens_DoesNotDeduplicateDifferentRouteAccounts(t *testing.T) {
	resetXAIRefreshGroupForTest()
	t.Cleanup(resetXAIRefreshGroupForTest)

	var calls int32
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		started <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	authA := NewXAIAuthWithHTTPClient(server.Client(), "account-a.json")
	authB := NewXAIAuthWithHTTPClient(server.Client(), "account-b.json")
	errs := make(chan error, 2)
	for _, auth := range []*XAIAuth{authA, authB} {
		go func(auth *XAIAuth) {
			_, errRefresh := auth.RefreshTokens(context.Background(), "shared-refresh-token", server.URL)
			errs <- errRefresh
		}(auth)
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

func TestNewXAIAuthWithHTTPClient(t *testing.T) {
	injected := &http.Client{Timeout: time.Second}
	auth := NewXAIAuthWithHTTPClient(injected, " account.json ")
	if got := auth.httpClient; got != injected {
		t.Fatal("expected injected HTTP client to be used")
	}
	if auth.refreshRouteKey != "account.json" {
		t.Fatalf("refresh route key = %q, want account.json", auth.refreshRouteKey)
	}

	defaultClient := NewXAIAuthWithHTTPClient(nil).httpClient
	originalClient := NewXAIAuth(nil).httpClient
	if defaultClient == nil || originalClient == nil {
		t.Fatalf("expected provider default HTTP clients, got default=%#v original=%#v", defaultClient, originalClient)
	}
	if defaultClient.Timeout != originalClient.Timeout || (defaultClient.Transport == nil) != (originalClient.Transport == nil) {
		t.Fatalf("injected nil client changed provider defaults: default=%#v original=%#v", defaultClient, originalClient)
	}
}
