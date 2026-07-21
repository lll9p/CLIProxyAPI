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

func TestNewDirectWebsocketDialerPreservesWebsocketSettings(t *testing.T) {
	dialer := newDirectWebsocketDialer()

	if dialer.Proxy != nil {
		t.Fatal("direct websocket dialer Proxy must be nil")
	}
	if dialer.HandshakeTimeout != codexResponsesWebsocketHandshakeTO {
		t.Fatalf("HandshakeTimeout = %v, want %v", dialer.HandshakeTimeout, codexResponsesWebsocketHandshakeTO)
	}
	if !dialer.EnableCompression {
		t.Fatal("direct websocket dialer must enable compression negotiation")
	}
	if dialer.NetDialContext == nil {
		t.Fatal("direct websocket dialer NetDialContext is nil")
	}
}

func TestCodexPrepareWebsocketResinClonesHeadersAndCleansHost(t *testing.T) {
	cfg := &config.Config{SDKConfig: config.SDKConfig{
		ResinURL:          "http://resin.example:2260/ThePasswd",
		ResinPlatformName: "cpa",
	}}
	auth := &cliproxyauth.Auth{
		ID:       "codex-auth-resin-headers",
		FileName: "codex-account.json",
		Provider: "codex",
		Metadata: map[string]any{"access_token": "codex-token"},
	}
	target, errParse := url.Parse("wss://chatgpt.com/backend-api/codex/responses")
	if errParse != nil {
		t.Fatalf("parse target URL: %v", errParse)
	}
	headers := http.Header{
		"Authorization":   {"Bearer codex-token"},
		"OpenAI-Beta":     {codexResponsesWebsocketBetaHeaderValue},
		"hOsT":            {"chatgpt.com"},
		"x-ReSiN-aCcOuNt": {"stale-account"},
	}

	dialURL, dialHeaders, dialKey, routed := resin.PrepareWebSocket(cfg, auth, target, headers)

	if !routed {
		t.Fatal("PrepareWebSocket() routed = false, want true")
	}
	if dialURL == nil || dialURL.String() != "ws://resin.example:2260/ThePasswd/cpa/https/chatgpt.com/backend-api/codex/responses" {
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
	if got := dialHeaders.Get("Authorization"); got != "Bearer codex-token" {
		t.Fatalf("routed Authorization = %q, want Bearer codex-token", got)
	}
	if got := websocketHeaderValuesFold(dialHeaders, "OpenAI-Beta"); len(got) != 1 || got[0] != codexResponsesWebsocketBetaHeaderValue {
		t.Fatalf("routed OpenAI-Beta headers = %#v, want [%q]", got, codexResponsesWebsocketBetaHeaderValue)
	}
	if got := headers["hOsT"]; len(got) != 1 || got[0] != "chatgpt.com" {
		t.Fatalf("caller Host header mutated: %#v", got)
	}
	if got := headers["x-ReSiN-aCcOuNt"]; len(got) != 1 || got[0] != "stale-account" {
		t.Fatalf("caller X-Resin-Account header mutated: %#v", got)
	}
	dialHeaders.Set("Authorization", "Bearer changed")
	if got := headers.Get("Authorization"); got != "Bearer codex-token" {
		t.Fatalf("caller Authorization changed through routed header clone: %q", got)
	}
}

func TestCodexDialWebsocketUsesDirectDialerForResin(t *testing.T) {
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
		ID:       "codex-auth-resin-direct",
		FileName: "codex-direct.json",
		Provider: "codex",
		ProxyURL: "http://127.0.0.1:2",
		Metadata: map[string]any{"access_token": "codex-token"},
	}
	headers := http.Header{"Authorization": {"Bearer codex-token"}, "hOsT": {"chatgpt.com"}}
	exec := NewCodexWebsocketsExecutor(cfg)

	conn, resp, errDial := exec.dialCodexWebsocket(context.Background(), auth, "wss://chatgpt.com/backend-api/codex/responses", headers)
	if errDial != nil {
		t.Fatalf("dialCodexWebsocket() error = %v", errDial)
	}
	closeHTTPResponseBody(resp, "close Codex Resin handshake response")
	defer func() { _ = conn.Close() }()

	select {
	case got := <-requestCh:
		wantHost := strings.TrimPrefix(server.URL, "http://")
		if got.host != wantHost {
			t.Fatalf("Resin request Host = %q, want %q", got.host, wantHost)
		}
		if got.path != "/cpa/https/chatgpt.com/backend-api/codex/responses" {
			t.Fatalf("Resin request path = %q", got.path)
		}
		if got.account != auth.FileName {
			t.Fatalf("X-Resin-Account = %q, want %q", got.account, auth.FileName)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for direct Resin websocket request")
	}
	if got := headers["hOsT"]; len(got) != 1 || got[0] != "chatgpt.com" {
		t.Fatalf("caller headers mutated during Resin dial: %#v", headers)
	}
}

func TestCodexEnsureUpstreamConnReusesMatchingDialKeyAndReconnectsOnChange(t *testing.T) {
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

	exec := NewCodexWebsocketsExecutor(&config.Config{})
	sess := &codexWebsocketSession{
		sessionID:            "codex-dial-key-session",
		upstreamDisconnectCh: make(chan error, 1),
	}
	auth := &cliproxyauth.Auth{ID: "codex-auth-dial-key", Provider: "codex", Metadata: map[string]any{"access_token": "token-1"}}
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	headers := http.Header{"Authorization": {"Bearer token-1"}}

	conn1, resp1, errFirst := exec.ensureUpstreamConn(context.Background(), auth, sess, auth.ID, wsURL, headers)
	if errFirst != nil {
		t.Fatalf("first ensureUpstreamConn() error = %v", errFirst)
	}
	closeHTTPResponseBody(resp1, "close first Codex handshake response")
	waitForWebsocketConnection(t, accepted, "first Codex websocket connection")

	conn2, resp2, errSecond := exec.ensureUpstreamConn(context.Background(), auth, sess, auth.ID, wsURL, headers)
	if errSecond != nil {
		t.Fatalf("second ensureUpstreamConn() error = %v", errSecond)
	}
	closeHTTPResponseBody(resp2, "close reused Codex handshake response")
	if conn2 != conn1 {
		t.Fatal("matching dial key did not reuse the Codex websocket connection")
	}
	select {
	case <-accepted:
		t.Fatal("matching dial key opened an extra Codex websocket connection")
	case <-time.After(100 * time.Millisecond):
	}

	changedHeaders := headers.Clone()
	changedHeaders.Set("Authorization", "Bearer token-2")
	conn3, resp3, errThird := exec.ensureUpstreamConn(context.Background(), auth, sess, auth.ID, wsURL, changedHeaders)
	if errThird != nil {
		t.Fatalf("changed-key ensureUpstreamConn() error = %v", errThird)
	}
	closeHTTPResponseBody(resp3, "close reconnected Codex handshake response")
	if conn3 == conn1 {
		t.Fatal("changed dial key reused the old Codex websocket connection")
	}
	waitForWebsocketConnection(t, accepted, "reconnected Codex websocket connection")

	time.Sleep(50 * time.Millisecond)
	select {
	case errDisconnect, ok := <-sess.upstreamDisconnectCh:
		t.Fatalf("stale Codex read loop reported a disconnect after intentional reconnect: err=%v open=%v", errDisconnect, ok)
	default:
	}
	closeCodexWebsocketSession(sess, "test_complete")
}

func websocketHeaderValuesFold(headers http.Header, name string) []string {
	var values []string
	for key, current := range headers {
		if strings.EqualFold(key, name) {
			values = append(values, current...)
		}
	}
	return values
}

func waitForWebsocketConnection(t *testing.T, accepted <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
