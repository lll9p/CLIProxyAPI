package executor

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestWebsocketReloadResinConnectionState(t *testing.T) {
	for _, provider := range []string{"codex", "xai"} {
		t.Run(provider, func(t *testing.T) {
			server, _ := newWebsocketTargetServer(t)
			defer server.Close()
			cfg := &config.Config{SDKConfig: config.SDKConfig{ProxyURL: "direct"}}
			sess := &codexWebsocketSession{sessionID: t.Name(), upstreamDisconnectCh: make(chan error, 1)}
			var ensure func(context.Context, *cliproxyauth.Auth, *codexWebsocketSession, string, string, http.Header) (*websocket.Conn, *websocketConnectionCloser, *http.Response, error)
			var closeSession func(*codexWebsocketSession, string)
			if provider == "codex" {
				ensure = NewCodexWebsocketsExecutor(cfg).ensureUpstreamConn
				closeSession = closeCodexWebsocketSession
			} else {
				ensure = NewXAIWebsocketsExecutor(cfg).ensureUpstreamConn
				closeSession = closeXAIWebsocketSession
			}
			t.Cleanup(func() { closeSession(sess, "test_complete") })
			auth := &cliproxyauth.Auth{ID: provider + "-reload", Provider: provider, FileName: provider + ".json", Metadata: map[string]any{"access_token": "token"}}
			wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
			headers := http.Header{"Authorization": {"Bearer token"}}
			dial := func() *websocket.Conn {
				t.Helper()
				conn, _, resp, errDial := ensure(context.Background(), auth, sess, auth.ID, wsURL, headers)
				closeHTTPResponseBody(resp, "close reload test handshake response")
				if errDial != nil {
					t.Fatalf("ensure upstream connection: %v", errDial)
				}
				return conn
			}

			first := dial()
			lifecycle := &trackedWebsocketLifecycle{}
			if errBind := sess.bindExecutionLifecycle(cliproxyexecutor.Options{ExecutionLifecycle: lifecycle}, first, sess.connCloser, "model"); errBind != nil {
				t.Fatalf("bind execution lifecycle: %v", errBind)
			}
			cfg.ProxyURL = "none"
			pinned := sess.effectiveConfig(auth.ID, cfg)
			if pinned == cfg || pinned.ProxyURL != "direct" {
				t.Fatal("connection configuration was not an independent snapshot")
			}
			proxyURL := executionProxyURL(context.Background(), pinned, auth)
			if conn, _ := existingWebsocketSessionConn(sess, auth.ID, wsURL, proxyURL); conn != first {
				t.Fatal("reload lost the existing connection for required continuation")
			}
			second := dial()
			if second == first || sess.proxyURL != "none" || sess.connCfg.ProxyURL != "none" || !sess.hasConnCfg {
				t.Fatal("fallback proxy key change did not install the new connection configuration")
			}
			if lifecycle.ends.Load() != 1 || sess.lifecycle != nil {
				t.Fatal("key change did not end and detach the previous lifecycle")
			}

			cfg.ResinURL = server.URL
			cfg.ResinPlatformName = "cpa"
			cfg.ProxyURL = "http://127.0.0.1:1"
			routed := dial()
			if routed == second || sess.wsURL != wsURL {
				t.Fatal("enabling Resin did not replace the connection while preserving the logical URL")
			}
			auth.ProxyURL = "http://127.0.0.1:2"
			headers.Set("Session_id", "new-turn")
			if conn := dial(); conn != routed {
				t.Fatal("routed connection was replaced for an ignored proxy or per-turn session header")
			}
			cfg.ResinPlatformName = "cpa-new"
			if conn := dial(); conn == routed {
				t.Fatal("Resin identity change reused the old connection")
			}
			if sess.proxyURL != auth.ProxyURL || sess.connCfg.ResinPlatformName != "cpa-new" {
				t.Fatal("reconnection retained old proxy/configuration metadata")
			}
			cfg.ResinPlatformName = "mutated-after-dial"
			if sess.connCfg.ResinPlatformName != "cpa-new" {
				t.Fatal("connection retained a mutable configuration pointer")
			}

			closeSession(sess, "test_complete")
			if sess.conn != nil || sess.connCloser != nil || sess.connCfg != nil || sess.hasConnCfg || sess.dialKey != "" {
				t.Fatal("session close retained connection configuration or dial identity")
			}
		})
	}
}
