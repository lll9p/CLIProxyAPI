package helps

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/httptransport"
)

type resinObservingTransport struct {
	http.RoundTripper
	t                *testing.T
	handshakeTimeout time.Duration
}

func (r resinObservingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.String() != "https://upstream.example/test" {
		r.t.Errorf("decorator observed physical route: %s", req.URL)
	}
	if got := tlsHandshakeTimeoutFromContext(req.Context()); got != r.handshakeTimeout {
		r.t.Errorf("handshake timeout=%s, want %s", got, r.handshakeTimeout)
	}
	return r.RoundTripper.RoundTrip(req)
}

func TestResinTransportDecoratorOrdering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/secret/cpa/https/upstream.example/test" || req.Header.Get("X-Resin-Account") != "account.json" {
			t.Errorf("incorrect Resin route: %s, account=%q", req.URL, req.Header.Get("X-Resin-Account"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	cfg := &config.Config{}
	cfg.ResinURL = server.URL + "/secret"
	cfg.ResinPlatformName = "cpa"
	cfg.TLSHandshakeTimeoutSeconds = 2
	auth := &coreauth.Auth{ID: "account", FileName: "account.json", Provider: "codex", ProxyURL: "http://127.0.0.1:1"}
	for name, factory := range map[string]func(context.Context, *config.Config, *coreauth.Auth, time.Duration) *http.Client{
		"plain": NewProxyAwareHTTPClient, "utls": NewUtlsHTTPClient, "devin": NewDevinHTTPClient,
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			var handshakeTimeout time.Duration
			if name == "utls" {
				handshakeTimeout = 2 * time.Second
			}
			ctx := httptransport.WithDecorator(context.Background(), func(next http.RoundTripper) http.RoundTripper {
				calls++
				return resinObservingTransport{RoundTripper: next, t: t, handshakeTimeout: handshakeTimeout}
			})
			client := factory(ctx, cfg, auth, 0)
			resp, err := client.Get("https://upstream.example/test")
			if err != nil {
				t.Fatal(err)
			}
			if errClose := resp.Body.Close(); errClose != nil {
				t.Fatal(errClose)
			}
			if calls != 1 {
				t.Fatalf("decorator calls=%d, want 1", calls)
			}
		})
	}
}
