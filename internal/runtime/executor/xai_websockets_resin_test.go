package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/resin"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestXAIPrepareWebsocketResinClonesHeadersAndCleansHost(t *testing.T) {
	cfg := &config.Config{SDKConfig: config.SDKConfig{
		ResinURL:          "http://resin.example:2260/ThePasswd",
		ResinPlatformName: "cpa",
	}}
	auth := &cliproxyauth.Auth{
		ID:       "xai-auth-resin-headers",
		FileName: "xai-account.json",
		Provider: "xai",
		Metadata: map[string]any{"access_token": "xai-token"},
	}
	target, errParse := url.Parse("wss://api.x.ai/v1/responses")
	if errParse != nil {
		t.Fatalf("parse target URL: %v", errParse)
	}
	headers := http.Header{
		"Authorization":   {"Bearer xai-token"},
		"Content-Type":    {"application/json"},
		"HOST":            {"api.x.ai"},
		"x-rEsIn-AcCoUnT": {"stale-account"},
	}

	dialURL, dialHeaders, dialKey, routed := resin.PrepareWebSocket(cfg, auth, target, headers)

	if !routed {
		t.Fatal("PrepareWebSocket() routed = false, want true")
	}
	if dialURL == nil || dialURL.String() != "ws://resin.example:2260/ThePasswd/cpa/https/api.x.ai/v1/responses" {
		t.Fatalf("dial URL = %v, want Resin websocket route", dialURL)
	}
	if dialKey == "" {
		t.Fatal("PrepareWebSocket() returned an empty dial key")
	}
	if got := websocketHeaderValuesFold(dialHeaders, "Host"); len(got) != 0 {
		t.Fatalf("routed Host headers = %#v, want none", got)
	}
	if got := websocketHeaderValuesFold(dialHeaders, "X-Resin-Account"); len(got) != 1 || got[0] != auth.FileName {
		t.Fatalf("routed X-Resin-Account headers = %#v, want [%q]", got, auth.FileName)
	}
	if got := dialHeaders.Get("Authorization"); got != "Bearer xai-token" {
		t.Fatalf("routed Authorization = %q, want Bearer xai-token", got)
	}
	if got := dialHeaders.Get("Content-Type"); got != "application/json" {
		t.Fatalf("routed Content-Type = %q, want application/json", got)
	}
	if got := headers["HOST"]; len(got) != 1 || got[0] != "api.x.ai" {
		t.Fatalf("caller Host header mutated: %#v", got)
	}
	if got := headers["x-rEsIn-AcCoUnT"]; len(got) != 1 || got[0] != "stale-account" {
		t.Fatalf("caller X-Resin-Account header mutated: %#v", got)
	}
	dialHeaders.Set("Authorization", "Bearer changed")
	if got := headers.Get("Authorization"); got != "Bearer xai-token" {
		t.Fatalf("caller Authorization changed through routed header clone: %q", got)
	}
}

func TestXAIDialWebsocketUsesDirectDialerForResin(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	requestCh := make(chan struct {
		host    string
		path    string
		account string
	}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, errUpgrade := upgrader.Upgrade(w, r, nil)
		if errUpgrade != nil {
			t.Errorf("upgrade websocket: %v", errUpgrade)
			return
		}
		requestCh <- struct {
			host    string
			path    string
			account string
		}{host: r.Host, path: r.URL.EscapedPath(), account: r.Header.Get("X-Resin-Account")}
		defer func() { _ = conn.Close() }()
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()

	cfg := &config.Config{SDKConfig: config.SDKConfig{
		ProxyURL:          "http://127.0.0.1:1",
		ResinURL:          server.URL,
		ResinPlatformName: "cpa",
	}}
	auth := &cliproxyauth.Auth{
		ID:       "xai-auth-resin-direct",
		FileName: "xai-direct.json",
		Provider: "xai",
		ProxyURL: "http://127.0.0.1:2",
		Metadata: map[string]any{"access_token": "xai-token"},
	}
	headers := http.Header{"Authorization": {"Bearer xai-token"}, "HOST": {"api.x.ai"}}
	exec := NewXAIWebsocketsExecutor(cfg)

	conn, resp, errDial := exec.dialXAIWebsocket(context.Background(), auth, "wss://api.x.ai/v1/responses", headers)
	if errDial != nil {
		t.Fatalf("dialXAIWebsocket() error = %v", errDial)
	}
	closeHTTPResponseBody(resp, "close xAI Resin handshake response")
	defer func() { _ = conn.Close() }()

	select {
	case got := <-requestCh:
		wantHost := strings.TrimPrefix(server.URL, "http://")
		if got.host != wantHost {
			t.Fatalf("Resin request Host = %q, want %q", got.host, wantHost)
		}
		if got.path != "/cpa/https/api.x.ai/v1/responses" {
			t.Fatalf("Resin request path = %q", got.path)
		}
		if got.account != auth.FileName {
			t.Fatalf("X-Resin-Account = %q, want %q", got.account, auth.FileName)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for direct Resin websocket request")
	}
	if got := headers["HOST"]; len(got) != 1 || got[0] != "api.x.ai" {
		t.Fatalf("caller headers mutated during Resin dial: %#v", headers)
	}
}

func TestXAIEnsureUpstreamConnReusesMatchingDialKeyAndReconnectsOnChange(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	accepted := make(chan struct{}, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, errUpgrade := upgrader.Upgrade(w, r, nil)
		if errUpgrade != nil {
			t.Errorf("upgrade websocket: %v", errUpgrade)
			return
		}
		accepted <- struct{}{}
		defer func() { _ = conn.Close() }()
		for {
			if _, _, errRead := conn.ReadMessage(); errRead != nil {
				return
			}
		}
	}))
	defer server.Close()

	exec := NewXAIWebsocketsExecutor(&config.Config{})
	sess := &codexWebsocketSession{
		sessionID:            "xai-dial-key-session",
		upstreamDisconnectCh: make(chan error, 1),
	}
	auth := &cliproxyauth.Auth{ID: "xai-auth-dial-key", Provider: "xai", Metadata: map[string]any{"access_token": "token-1"}}
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	headers := http.Header{"Authorization": {"Bearer token-1"}}

	conn1, resp1, errFirst := exec.ensureUpstreamConn(context.Background(), auth, sess, auth.ID, wsURL, headers)
	if errFirst != nil {
		t.Fatalf("first ensureUpstreamConn() error = %v", errFirst)
	}
	closeHTTPResponseBody(resp1, "close first xAI handshake response")
	waitForWebsocketConnection(t, accepted, "first xAI websocket connection")

	conn2, resp2, errSecond := exec.ensureUpstreamConn(context.Background(), auth, sess, auth.ID, wsURL, headers)
	if errSecond != nil {
		t.Fatalf("second ensureUpstreamConn() error = %v", errSecond)
	}
	closeHTTPResponseBody(resp2, "close reused xAI handshake response")
	if conn2 != conn1 {
		t.Fatal("matching dial key did not reuse the xAI websocket connection")
	}
	select {
	case <-accepted:
		t.Fatal("matching dial key opened an extra xAI websocket connection")
	case <-time.After(100 * time.Millisecond):
	}

	changedHeaders := headers.Clone()
	changedHeaders.Set("Authorization", "Bearer token-2")
	conn3, resp3, errThird := exec.ensureUpstreamConn(context.Background(), auth, sess, auth.ID, wsURL, changedHeaders)
	if errThird != nil {
		t.Fatalf("changed-key ensureUpstreamConn() error = %v", errThird)
	}
	closeHTTPResponseBody(resp3, "close reconnected xAI handshake response")
	if conn3 == conn1 {
		t.Fatal("changed dial key reused the old xAI websocket connection")
	}
	waitForWebsocketConnection(t, accepted, "reconnected xAI websocket connection")

	time.Sleep(50 * time.Millisecond)
	select {
	case errDisconnect, ok := <-sess.upstreamDisconnectCh:
		t.Fatalf("stale xAI read loop reported a disconnect after intentional reconnect: err=%v open=%v", errDisconnect, ok)
	default:
	}
	closeXAIWebsocketSession(sess, "test_complete")
}
