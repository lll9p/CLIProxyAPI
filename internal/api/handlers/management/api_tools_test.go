package management

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	codexauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/codex"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestAPICallTransportDirectBypassesGlobalProxy(t *testing.T) {
	t.Parallel()

	h := &Handler{
		cfg: &config.Config{
			SDKConfig: sdkconfig.SDKConfig{ProxyURL: "http://global-proxy.example.com:8080"},
		},
	}

	transport := h.apiCallTransport(&coreauth.Auth{ProxyURL: "direct"})
	httpTransport, ok := transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", transport)
	}
	if httpTransport.Proxy != nil {
		t.Fatal("expected direct transport to disable proxy function")
	}
}

func TestAPICallTransportInvalidAuthFallsBackToGlobalProxy(t *testing.T) {
	t.Parallel()

	h := &Handler{
		cfg: &config.Config{
			SDKConfig: sdkconfig.SDKConfig{ProxyURL: "http://global-proxy.example.com:8080"},
		},
	}

	transport := h.apiCallTransport(&coreauth.Auth{ProxyURL: "bad-value"})
	httpTransport, ok := transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", transport)
	}

	req, errRequest := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if errRequest != nil {
		t.Fatalf("http.NewRequest returned error: %v", errRequest)
	}

	proxyURL, errProxy := httpTransport.Proxy(req)
	if errProxy != nil {
		t.Fatalf("httpTransport.Proxy returned error: %v", errProxy)
	}
	if proxyURL == nil || proxyURL.String() != "http://global-proxy.example.com:8080" {
		t.Fatalf("proxy URL = %v, want http://global-proxy.example.com:8080", proxyURL)
	}
}

func TestAPICallTransportAPIKeyAuthFallsBackToConfigProxyURL(t *testing.T) {
	t.Parallel()

	h := &Handler{
		cfg: &config.Config{
			SDKConfig: sdkconfig.SDKConfig{ProxyURL: "http://global-proxy.example.com:8080"},
			GeminiKey: []config.GeminiKey{{
				APIKey:   "gemini-key",
				ProxyURL: "http://gemini-proxy.example.com:8080",
			}},
			ClaudeKey: []config.ClaudeKey{{
				APIKey:   "claude-key",
				ProxyURL: "http://claude-proxy.example.com:8080",
			}},
			CodexKey: []config.CodexKey{{
				APIKey:   "codex-key",
				ProxyURL: "http://codex-proxy.example.com:8080",
			}},
			OpenAICompatibility: []config.OpenAICompatibility{{
				Name:    "bohe",
				BaseURL: "https://bohe.example.com",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{{
					APIKey:   "compat-key",
					ProxyURL: "http://compat-proxy.example.com:8080",
				}},
			}},
		},
	}

	cases := []struct {
		name      string
		auth      *coreauth.Auth
		wantProxy string
	}{
		{
			name: "gemini",
			auth: &coreauth.Auth{
				Provider:   "gemini",
				Attributes: map[string]string{"api_key": "gemini-key"},
			},
			wantProxy: "http://gemini-proxy.example.com:8080",
		},
		{
			name: "claude",
			auth: &coreauth.Auth{
				Provider:   "claude",
				Attributes: map[string]string{"api_key": "claude-key"},
			},
			wantProxy: "http://claude-proxy.example.com:8080",
		},
		{
			name: "codex",
			auth: &coreauth.Auth{
				Provider:   "codex",
				Attributes: map[string]string{"api_key": "codex-key"},
			},
			wantProxy: "http://codex-proxy.example.com:8080",
		},
		{
			name: "openai-compatibility",
			auth: &coreauth.Auth{
				Provider: "bohe",
				Attributes: map[string]string{
					"api_key":      "compat-key",
					"compat_name":  "bohe",
					"provider_key": "bohe",
				},
			},
			wantProxy: "http://compat-proxy.example.com:8080",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			transport := h.apiCallTransport(tc.auth)
			httpTransport, ok := transport.(*http.Transport)
			if !ok {
				t.Fatalf("transport type = %T, want *http.Transport", transport)
			}

			req, errRequest := http.NewRequest(http.MethodGet, "https://example.com", nil)
			if errRequest != nil {
				t.Fatalf("http.NewRequest returned error: %v", errRequest)
			}

			proxyURL, errProxy := httpTransport.Proxy(req)
			if errProxy != nil {
				t.Fatalf("httpTransport.Proxy returned error: %v", errProxy)
			}
			if proxyURL == nil || proxyURL.String() != tc.wantProxy {
				t.Fatalf("proxy URL = %v, want %s", proxyURL, tc.wantProxy)
			}
		})
	}
}

func TestAPICallCodexUsesResinAndBypassesForwardProxies(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var proxyCalls int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxyCalls, 1)
		http.Error(w, "proxy should not be used", http.StatusBadGateway)
	}))
	defer proxyServer.Close()

	var resinCalls int32
	resinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&resinCalls, 1)
		const wantPath = "/base/codex/https/api.openai.com/dashboard/billing/credit_grants"
		if r.URL.Path != wantPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, wantPath)
		}
		if r.URL.RawQuery != "account=1" {
			t.Fatalf("query = %q, want account=1", r.URL.RawQuery)
		}
		if got := r.Header.Get(codexauth.ResinAccountHeader); got != "codex-account.json" {
			t.Fatalf("%s = %q, want codex-account.json", codexauth.ResinAccountHeader, got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("Authorization = %q, want Bearer access-token", got)
		}
		if got := r.Header.Get("X-Test-Header"); got != "kept" {
			t.Fatalf("X-Test-Header = %q, want kept", got)
		}
		if strings.Contains(r.Host, "custom-host.example.com") {
			t.Fatalf("Host override leaked to Resin request: %q", r.Host)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("resin-ok"))
	}))
	defer resinServer.Close()

	auth := &coreauth.Auth{
		ID:       "codex-auth",
		Provider: "codex",
		FileName: "codex-account.json",
		ProxyURL: proxyServer.URL,
		Attributes: map[string]string{
			"api_key": "codex-key",
		},
		Metadata: map[string]any{
			"access_token": "access-token",
		},
	}
	manager := coreauth.NewManager(nil, nil, nil)
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	authIndex := auth.EnsureIndex()

	h := &Handler{
		cfg: &config.Config{
			SDKConfig: sdkconfig.SDKConfig{
				ProxyURL:          proxyServer.URL,
				ResinURL:          resinServer.URL + "/base",
				ResinPlatformName: "codex",
			},
			CodexKey: []config.CodexKey{{APIKey: "codex-key", ProxyURL: proxyServer.URL}},
		},
		authManager: manager,
	}

	body, err := json.Marshal(apiCallRequest{
		AuthIndexSnake: &authIndex,
		Method:         http.MethodGet,
		URL:            "https://api.openai.com/dashboard/billing/credit_grants?account=1",
		Header: map[string]string{
			"Authorization": "Bearer $TOKEN$",
			"Host":          "custom-host.example.com",
			"X-Test-Header": "kept",
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/api-call", bytes.NewReader(body))
	ginCtx.Request.Header.Set("Content-Type", "application/json")
	h.APICall(ginCtx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload apiCallResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.StatusCode != http.StatusAccepted {
		t.Fatalf("upstream status = %d, want %d", payload.StatusCode, http.StatusAccepted)
	}
	if payload.Body != "resin-ok" {
		t.Fatalf("body = %q, want resin-ok", payload.Body)
	}
	if got := atomic.LoadInt32(&resinCalls); got != 1 {
		t.Fatalf("resin calls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&proxyCalls); got != 0 {
		t.Fatalf("proxy calls = %d, want 0", got)
	}
}

func TestAPICallNonCodexKeepsConfiguredProxyPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var resinCalls int32
	resinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&resinCalls, 1)
		http.Error(w, "resin should not be used", http.StatusBadGateway)
	}))
	defer resinServer.Close()

	var proxyCalls int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxyCalls, 1)
		if got := r.URL.String(); got != "http://claude.example.com/v1/quota?x=1" {
			t.Fatalf("proxy request URL = %q, want http://claude.example.com/v1/quota?x=1", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer claude-token" {
			t.Fatalf("Authorization = %q, want Bearer claude-token", got)
		}
		_, _ = w.Write([]byte("proxy-ok"))
	}))
	defer proxyServer.Close()

	auth := &coreauth.Auth{
		ID:       "claude-auth",
		Provider: "claude",
		ProxyURL: proxyServer.URL,
		Metadata: map[string]any{
			"access_token": "claude-token",
		},
	}
	manager := coreauth.NewManager(nil, nil, nil)
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	authIndex := auth.EnsureIndex()

	h := &Handler{
		cfg: &config.Config{SDKConfig: sdkconfig.SDKConfig{ResinURL: resinServer.URL + "/base", ResinPlatformName: "codex"}},
		authManager: manager,
	}
	body, err := json.Marshal(apiCallRequest{
		AuthIndexSnake: &authIndex,
		Method:         http.MethodGet,
		URL:            "http://claude.example.com/v1/quota?x=1",
		Header:         map[string]string{"Authorization": "Bearer $TOKEN$"},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/api-call", bytes.NewReader(body))
	ginCtx.Request.Header.Set("Content-Type", "application/json")
	h.APICall(ginCtx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload apiCallResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Body != "proxy-ok" {
		t.Fatalf("body = %q, want proxy-ok", payload.Body)
	}
	if got := atomic.LoadInt32(&proxyCalls); got != 1 {
		t.Fatalf("proxy calls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&resinCalls); got != 0 {
		t.Fatalf("resin calls = %d, want 0", got)
	}
}

func TestApplyCodexResinAPICallSkipsInactiveInputs(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
		auth *coreauth.Auth
		url  string
	}{
		{
			name: "missing config",
			cfg:  &config.Config{},
			auth: &coreauth.Auth{Provider: "codex", FileName: "codex-account.json"},
			url:  "https://api.openai.com/v1/quota",
		},
		{
			name: "missing account",
			cfg:  &config.Config{SDKConfig: sdkconfig.SDKConfig{ResinURL: "https://resin.example.com", ResinPlatformName: "codex"}},
			auth: &coreauth.Auth{Provider: "codex"},
			url:  "https://api.openai.com/v1/quota",
		},
		{
			name: "unsupported scheme",
			cfg:  &config.Config{SDKConfig: sdkconfig.SDKConfig{ResinURL: "https://resin.example.com", ResinPlatformName: "codex"}},
			auth: &coreauth.Auth{Provider: "codex", FileName: "codex-account.json"},
			url:  "ftp://api.openai.com/v1/quota",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tc.url, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Host = "custom-host.example.com"
			h := &Handler{cfg: tc.cfg}

			if h.applyCodexResinAPICall(req, tc.auth) {
				t.Fatal("expected Resin api-call rewrite to be skipped")
			}
			if got := req.Header.Get(codexauth.ResinAccountHeader); got != "" {
				t.Fatalf("%s = %q, want empty", codexauth.ResinAccountHeader, got)
			}
			if req.Host != "custom-host.example.com" {
				t.Fatalf("Host = %q, want original custom host", req.Host)
			}
		})
	}
}

func TestAuthByIndexDistinguishesSharedAPIKeysAcrossProviders(t *testing.T) {
	t.Parallel()

	manager := coreauth.NewManager(nil, nil, nil)
	geminiAuth := &coreauth.Auth{
		ID:       "gemini:apikey:123",
		Provider: "gemini",
		Attributes: map[string]string{
			"api_key": "shared-key",
		},
	}
	compatAuth := &coreauth.Auth{
		ID:       "openai-compatibility:bohe:456",
		Provider: "bohe",
		Label:    "bohe",
		Attributes: map[string]string{
			"api_key":      "shared-key",
			"compat_name":  "bohe",
			"provider_key": "bohe",
		},
	}

	if _, errRegister := manager.Register(context.Background(), geminiAuth); errRegister != nil {
		t.Fatalf("register gemini auth: %v", errRegister)
	}
	if _, errRegister := manager.Register(context.Background(), compatAuth); errRegister != nil {
		t.Fatalf("register compat auth: %v", errRegister)
	}

	geminiIndex := geminiAuth.EnsureIndex()
	compatIndex := compatAuth.EnsureIndex()
	if geminiIndex == compatIndex {
		t.Fatalf("shared api key produced duplicate auth_index %q", geminiIndex)
	}

	h := &Handler{authManager: manager}

	gotGemini := h.authByIndex(geminiIndex)
	if gotGemini == nil {
		t.Fatal("expected gemini auth by index")
	}
	if gotGemini.ID != geminiAuth.ID {
		t.Fatalf("authByIndex(gemini) returned %q, want %q", gotGemini.ID, geminiAuth.ID)
	}

	gotCompat := h.authByIndex(compatIndex)
	if gotCompat == nil {
		t.Fatal("expected compat auth by index")
	}
	if gotCompat.ID != compatAuth.ID {
		t.Fatalf("authByIndex(compat) returned %q, want %q", gotCompat.ID, compatAuth.ID)
	}
}
