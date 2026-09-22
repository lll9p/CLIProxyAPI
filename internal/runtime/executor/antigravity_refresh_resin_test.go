package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestAntigravityRefresh_DoesNotDeduplicateDifferentRouteAccounts(t *testing.T) {
	resetAntigravityRefreshGroupForTest()
	t.Cleanup(resetAntigravityRefreshGroupForTest)
	resetAntigravityCreditsRetryState()
	t.Cleanup(resetAntigravityCreditsRetryState)

	var tokenCalls int32
	bothStarted := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if atomic.AddInt32(&tokenCalls, 1) == 2 {
				once.Do(func() { close(bothStarted) })
			}
			<-release
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{
				"access_token":"new-access",
				"refresh_token":"new-refresh",
				"token_type":"Bearer",
				"expires_in":3600
			}`)
		case "/v1internal:loadCodeAssist":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"paidTier":{"id":"tier","availableCredits":[]}}`)
		default:
			t.Errorf("unexpected antigravity test request path: %s", r.URL.Path)
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	serverURL, errParse := url.Parse(server.URL)
	if errParse != nil {
		t.Fatalf("parse test server URL: %v", errParse)
	}
	useAntigravityRefreshTestTransport(t, serverURL.Host)

	executor := &AntigravityExecutor{}
	auths := []*cliproxyauth.Auth{
		{
			ID:       "auth-a",
			FileName: "auth-a.json",
			Provider: "antigravity",
			Metadata: map[string]any{
				"refresh_token": "shared-refresh-token",
				"project_id":    "project-a",
			},
		},
		{
			ID:       "auth-b",
			FileName: "auth-b.json",
			Provider: "antigravity",
			Metadata: map[string]any{
				"refresh_token": "shared-refresh-token",
				"project_id":    "project-b",
			},
		},
	}

	errs := make(chan error, len(auths))
	for _, auth := range auths {
		go func(auth *cliproxyauth.Auth) {
			_, errRefresh := executor.Refresh(context.Background(), auth)
			errs <- errRefresh
		}(auth)
	}

	select {
	case <-bothStarted:
	case <-time.After(time.Second):
		close(release)
		t.Fatalf("different route accounts did not start independent refreshes; token calls = %d", atomic.LoadInt32(&tokenCalls))
	}
	close(release)

	for range auths {
		if errRefresh := <-errs; errRefresh != nil {
			t.Fatalf("expected refresh to succeed, got %v", errRefresh)
		}
	}
	if got := atomic.LoadInt32(&tokenCalls); got != 2 {
		t.Fatalf("expected different route accounts to make two token calls, got %d", got)
	}
}
