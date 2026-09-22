package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

func TestMetaRuntimeRoutesThroughResin(t *testing.T) {
	var resinHits atomic.Int32
	resinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		resinHits.Add(1)
		if got := req.URL.EscapedPath(); !strings.HasSuffix(got, "/http/example.test/responses") {
			t.Errorf("Resin path = %q", got)
		}
		if got, want := req.Header.Get("X-Resin-Account"), "meta-user.json"; got != want {
			t.Errorf("X-Resin-Account = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer resinServer.Close()

	cfg := &config.Config{SDKConfig: config.SDKConfig{
		ResinURL:          resinServer.URL + "/secret",
		ResinPlatformName: "cpa",
	}}
	auth := &cliproxyauth.Auth{
		Provider: "meta",
		FileName: "meta-user.json",
		Attributes: map[string]string{
			cliproxyauth.AttributeAuthKind: cliproxyauth.AuthKindOAuth,
			"api_key":                      "meta-token",
			"base_url":                     "http://example.test",
		},
	}
	_, err := NewMetaExecutor(cfg).Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "muse-spark-1.3",
		Payload: []byte(`{"model":"muse-spark-1.3","messages":[{"role":"user","content":"hello"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")})
	if err == nil {
		t.Fatal("Execute() error = nil, want rate-limit error")
	}
	if got := resinHits.Load(); got != 1 {
		t.Fatalf("Resin hits = %d, want 1", got)
	}
}
