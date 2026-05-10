package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	codexauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/codex"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

func TestCodexExecutorHttpRequestUsesResinAndBypassesProxy(t *testing.T) {
	var proxyCalls int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxyCalls, 1)
		http.Error(w, "proxy should not be used", http.StatusBadGateway)
	}))
	defer proxyServer.Close()

	resinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const wantPath = "/base/codex/https/upstream.example.com/v1/quota"
		if r.URL.Path != wantPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, wantPath)
		}
		if r.URL.RawQuery != "x=1" {
			t.Fatalf("query = %q, want x=1", r.URL.RawQuery)
		}
		if got := r.Header.Get(codexauth.ResinAccountHeader); got != "codex-account.json" {
			t.Fatalf("%s = %q, want codex-account.json", codexauth.ResinAccountHeader, got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("Authorization = %q, want Bearer sk-test", got)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer resinServer.Close()

	cfg := &config.Config{SDKConfig: config.SDKConfig{
		ProxyURL:          proxyServer.URL,
		ResinURL:          resinServer.URL + "/base",
		ResinPlatformName: "codex",
	}}
	exec := NewCodexExecutor(cfg)
	auth := &cliproxyauth.Auth{
		Provider: "codex",
		FileName: "codex-account.json",
		ProxyURL: proxyServer.URL,
		Attributes: map[string]string{
			"api_key": "sk-test",
		},
	}
	req, err := http.NewRequest(http.MethodGet, "https://upstream.example.com/v1/quota?x=1", nil)
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}

	resp, err := exec.HttpRequest(context.Background(), auth, req)
	if err != nil {
		t.Fatalf("HttpRequest error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		t.Fatalf("read response: %v", errRead)
	}
	if string(data) != "ok" {
		t.Fatalf("body = %q, want ok", string(data))
	}
	if got := atomic.LoadInt32(&proxyCalls); got != 0 {
		t.Fatalf("proxy calls = %d, want 0", got)
	}
}

func TestCodexExecutorExecuteUsesResinAndBypassesProxy(t *testing.T) {
	var proxyCalls int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&proxyCalls, 1)
		http.Error(w, "proxy should not be used", http.StatusBadGateway)
	}))
	defer proxyServer.Close()

	var resinCalls int32
	resinServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&resinCalls, 1)
		const wantPath = "/base/codex/https/upstream.example.com/backend-api/codex/responses"
		if r.URL.Path != wantPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, wantPath)
		}
		if got := r.Header.Get(codexauth.ResinAccountHeader); got != "codex-account.json" {
			t.Fatalf("%s = %q, want codex-account.json", codexauth.ResinAccountHeader, got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("Authorization = %q, want Bearer sk-test", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]},\"output_index\":0}\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"created_at\":1775555723,\"status\":\"completed\",\"model\":\"gpt-5-codex\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"))
	}))
	defer resinServer.Close()

	exec := NewCodexExecutor(&config.Config{SDKConfig: config.SDKConfig{
		DisableImageGeneration: config.DisableImageGenerationAll,
		ProxyURL:               proxyServer.URL,
		ResinURL:               resinServer.URL + "/base",
		ResinPlatformName:      "codex",
	}})
	auth := &cliproxyauth.Auth{
		Provider: "codex",
		FileName: "codex-account.json",
		ProxyURL: proxyServer.URL,
		Attributes: map[string]string{
			"api_key":  "sk-test",
			"base_url": "https://upstream.example.com/backend-api/codex",
		},
	}

	resp, err := exec.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-5-codex",
		Payload: []byte(`{"model":"gpt-5-codex","messages":[{"role":"user","content":"Say ok"}]}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       false,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(string(resp.Payload), "ok") {
		t.Fatalf("payload = %s, want translated ok response", string(resp.Payload))
	}
	if got := atomic.LoadInt32(&resinCalls); got != 1 {
		t.Fatalf("resin calls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&proxyCalls); got != 0 {
		t.Fatalf("proxy calls = %d, want 0", got)
	}
}
