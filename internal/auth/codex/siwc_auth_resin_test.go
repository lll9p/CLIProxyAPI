package codex

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRefreshSIWCTokens_DoesNotDeduplicateDifferentRouteAccounts(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	defer close(release)
	errs := make(chan error, 2)
	for _, account := range []string{"account-a.json", "account-b.json"} {
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			started <- account
			<-release
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"access_token":"new-access","refresh_token":"new-refresh","scope":"chatgpt.tokens.use.direct","expires_in":3600}`)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})}
		auth := NewCodexAuthWithHTTPClient(client, account)
		go func() {
			_, errRefresh := auth.RefreshSIWCTokens(context.Background(), t.Name(), "issued-client", "device-id")
			errs <- errRefresh
		}()
	}

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("different route accounts shared one SIWC refresh call")
		}
	}
	// Both requests must be in flight before either is allowed to complete.
	release <- struct{}{}
	release <- struct{}{}
	for i := 0; i < 2; i++ {
		if errRefresh := <-errs; errRefresh != nil {
			t.Fatalf("RefreshSIWCTokens() error = %v", errRefresh)
		}
	}
}
